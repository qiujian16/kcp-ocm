package synceraddons

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"embed"
	"encoding/pem"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/cloudflare/cfssl/config"
	"github.com/cloudflare/cfssl/signer"
	"github.com/cloudflare/cfssl/signer/local"

	"github.com/openshift/library-go/pkg/assets"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	certificatesv1 "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"open-cluster-management.io/addon-framework/pkg/agent"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

const (
	defaultSyncerImage = "quay.io/skeeey/kcp-syncer:latest"

	clusterJson = `{
		"apiVersion": "workload.kcp.dev/v1alpha1",
		"kind": "WorkloadCluster",
		"metadata": {
			"name": "guestbook1"
		},
		"spec": {
			"kubeconfig": ""
		}
	}`
)

// An addon-framework implementation to deploy syncer and register the syncer to a lcluster on kcp
// It also needs to setup the rbac on lcluster for the syncer.

type syncerAddon struct {
	addonName string

	kcpRestConfig       *rest.Config
	kcpServer           string
	kcpWorkspaceBaseURL string
	kcpLogicalCluster   string

	certsEnabled bool
	syncerCA     []byte
	syncerKey    []byte

	recorder events.Recorder
}

var (
	genericScheme = runtime.NewScheme()
	genericCodecs = serializer.NewCodecFactory(genericScheme)
	genericCodec  = genericCodecs.UniversalDeserializer()

	permisionFiles = []string{
		"manifests/kcp_clusterrolebinding.yaml",
	}

	deployFiles = []string{
		"manifests/clusterrolebinding.yaml",
		"manifests/namespace.yaml",
		"manifests/deployment.yaml",
		"manifests/service_account.yaml",
	}

	clusterGVR = schema.GroupVersionResource{
		Group:    "workload.kcp.dev",
		Version:  "v1alpha1",
		Resource: "workloadclusters",
	}
)

//go:embed manifests
var manifestFiles embed.FS

func init() {
	scheme.AddToScheme(genericScheme)
}

func NewSyncerAddon(addonName, workspaceBaseURL string, ca, key []byte, kcpRestConfig *rest.Config, recoder events.Recorder) agent.AgentAddon {
	kcpURL, err := url.Parse(workspaceBaseURL)
	if err != nil {
		panic(err)
	}

	certsEnabled := false
	if ca != nil && key != nil {
		certsEnabled = true
	}

	return &syncerAddon{
		addonName:           addonName,
		kcpRestConfig:       kcpRestConfig,
		kcpWorkspaceBaseURL: workspaceBaseURL,
		kcpServer:           fmt.Sprintf("%s://%s", kcpURL.Scheme, kcpURL.Host),
		kcpLogicalCluster:   strings.TrimPrefix(kcpURL.Path, "/clusters/"),
		certsEnabled:        certsEnabled,
		syncerCA:            ca,
		syncerKey:           key,
		recorder:            recoder,
	}
}

func (s *syncerAddon) Manifests(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn) ([]runtime.Object, error) {
	objects := []runtime.Object{}
	for _, file := range deployFiles {
		object, err := s.loadManifestFromFile(file, cluster, addon)
		if err != nil {
			return nil, err
		}
		objects = append(objects, object)
	}

	// create the kubeconfig to connect to kcp lcluster
	kubeconfig := buildKubeconfig(s.certsEnabled, s.kcpRestConfig, s.kcpServer)
	kubeConfigData, err := clientcmd.Write(kubeconfig)
	if err != nil {
		return nil, err
	}

	objects = append(objects, &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Secret",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "syncer-kubeconfig",
			Namespace: addon.Spec.InstallNamespace,
		},
		Data: map[string][]byte{
			"kubeconfig": kubeConfigData,
		},
	})
	return objects, nil
}

func (s *syncerAddon) GetAgentAddonOptions() agent.AgentAddonOptions {
	if !s.certsEnabled {
		return agent.AgentAddonOptions{
			AddonName: s.addonName,
			Registration: &agent.RegistrationOption{
				CSRConfigurations: func(cluster *clusterv1.ManagedCluster) []addonapiv1alpha1.RegistrationConfig {
					return []addonapiv1alpha1.RegistrationConfig{}
				},
				PermissionConfig: s.setupAgentPermissions,
			},
		}
	}

	return agent.AgentAddonOptions{
		AddonName: s.addonName,
		Registration: &agent.RegistrationOption{
			CSRConfigurations: s.signerConfiguration,
			CSRApproveCheck:   agent.ApprovalAllCSRs,
			CSRSign:           s.signer,
			PermissionConfig:  s.setupAgentPermissions,
		},
	}
}

func (s *syncerAddon) signerConfiguration(cluster *clusterv1.ManagedCluster) []addonapiv1alpha1.RegistrationConfig {
	return []addonapiv1alpha1.RegistrationConfig{
		{
			SignerName: "kcp.dev/syncer-signer",
			Subject: addonapiv1alpha1.Subject{
				User:   agent.DefaultUser(cluster.Name, s.addonName, "agent"),
				Groups: agent.DefaultGroups(cluster.Name, s.addonName),
			},
		},
	}
}

func (s *syncerAddon) signer(csr *certificatesv1.CertificateSigningRequest) []byte {
	blockTlsCrt, _ := pem.Decode(s.syncerCA) // note: the second return value is not error for pem.Decode; it's ok to omit it.
	certs, err := x509.ParseCertificates(blockTlsCrt.Bytes)
	if err != nil {
		return nil
	}

	blockTlsKey, _ := pem.Decode(s.syncerKey)
	key, err := x509.ParsePKCS1PrivateKey(blockTlsKey.Bytes)
	if err != nil {
		return nil
	}

	data, err := signCSR(csr, certs[0], key)
	if err != nil {
		return nil
	}
	return data
}

func (s *syncerAddon) setupAgentPermissions(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn) error {
	return s.applyManifestFromFile(cluster.Name, addon.Name, s.recorder)
}

func (s *syncerAddon) loadManifestFromFile(file string, cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn) (runtime.Object, error) {
	image := os.Getenv("SYNCER_IMAGE_NAME")
	if len(image) == 0 {
		image = defaultSyncerImage
	}

	manifestConfig := struct {
		AddonName      string
		Cluster        string
		LogicalCluster string
		Image          string
		Namespace      string
		CertsEnabled   bool
	}{
		AddonName:      s.addonName,
		Cluster:        cluster.Name,
		LogicalCluster: s.kcpLogicalCluster,
		Image:          image,
		Namespace:      addon.Spec.InstallNamespace,
		CertsEnabled:   s.certsEnabled,
	}

	template, err := manifestFiles.ReadFile(file)
	if err != nil {
		return nil, err
	}
	raw := assets.MustCreateAssetFromTemplate(file, template, &manifestConfig).Data
	object, _, err := genericCodec.Decode(raw, nil, nil)
	if err != nil {
		return nil, err
	}
	return object, nil
}

func (s *syncerAddon) applyManifestFromFile(clusterName, addonName string, recorder events.Recorder) error {
	// apploy clusterrolebindings for addon
	kconfig := rest.CopyConfig(s.kcpRestConfig)
	workspace := strings.TrimPrefix(addonName, "kcp-syncer-")

	kubeclient, err := kubernetes.NewForConfig(kconfig)
	if err != nil {
		return err
	}

	// apply syncer permission to the lcluster
	groups := agent.DefaultGroups(clusterName, addonName)
	config := struct {
		Cluster   string
		Workspace string
		Group     string
	}{
		Cluster:   clusterName,
		Workspace: workspace,
		Group:     groups[0],
	}

	results := resourceapply.ApplyDirectly(context.Background(),
		resourceapply.NewKubeClientHolder(kubeclient),
		recorder,
		func(name string) ([]byte, error) {
			file, err := manifestFiles.ReadFile(name)
			if err != nil {
				return nil, err
			}
			return assets.MustCreateAssetFromTemplate(name, file, config).Data, nil
		},
		permisionFiles...,
	)

	for _, result := range results {
		if result.Error != nil {
			return result.Error
		}
	}

	// Update config host to workspace and generate kubeclient to apploy kcp clusters
	workspaceKconfig := rest.CopyConfig(s.kcpRestConfig)
	workspaceKconfig.Host = s.kcpWorkspaceBaseURL
	dynamicClient, err := dynamic.NewForConfig(workspaceKconfig)
	if err != nil {
		return err
	}

	return s.applyCluster(dynamicClient, clusterName)
}

func (s *syncerAddon) applyCluster(dynamicClient dynamic.Interface, cluster string) error {
	_, err := dynamicClient.Resource(clusterGVR).Get(context.Background(), cluster, metav1.GetOptions{})
	if err == nil {
		return nil
	}

	if !errors.IsNotFound(err) {
		return err
	}

	obj := &unstructured.Unstructured{}
	err = obj.UnmarshalJSON([]byte(clusterJson))

	if err != nil {
		return err
	}

	obj.SetName(cluster)
	_, err = dynamicClient.Resource(clusterGVR).Create(context.Background(), obj, metav1.CreateOptions{})
	return err
}

// buildKubeconfig builds a kubeconfig based on a rest config template with a cert/key pair
func buildKubeconfig(certsEnabled bool, clientConfig *rest.Config, kcpServer string) clientcmdapi.Config {
	// Build kubeconfig.
	kubeconfig := clientcmdapi.Config{
		Clusters: map[string]*clientcmdapi.Cluster{"default-cluster": {
			Server:                kcpServer,
			InsecureSkipTLSVerify: true,
		}},
		// TODO use sa token instead of this
		AuthInfos: map[string]*clientcmdapi.AuthInfo{"default-auth": {
			Token: clientConfig.BearerToken,
		}},
		// Define a context that connects the auth info and cluster, and set it as the default
		Contexts: map[string]*clientcmdapi.Context{"default-context": {
			Cluster:   "default-cluster",
			AuthInfo:  "default-auth",
			Namespace: "configuration",
		}},
		CurrentContext: "default-context",
	}

	if certsEnabled {
		// Define auth based on the obtained client cert.
		kubeconfig.AuthInfos = map[string]*clientcmdapi.AuthInfo{"default-auth": {
			ClientCertificate: "/syncer-certs/tls.crt",
			ClientKey:         "/syncer-certs/tls.key",
		}}
	}

	return kubeconfig
}

func signCSR(csr *certificatesv1.CertificateSigningRequest, caCert *x509.Certificate, caKey *rsa.PrivateKey) ([]byte, error) {
	var usages []string
	for _, usage := range csr.Spec.Usages {
		usages = append(usages, string(usage))
	}

	certExpiryDuration := 365 * 24 * time.Hour
	durationUntilExpiry := time.Until(caCert.NotAfter)
	if durationUntilExpiry <= 0 {
		return nil, fmt.Errorf("signer has expired, expired time: %v", caCert.NotAfter)
	}
	if durationUntilExpiry < certExpiryDuration {
		certExpiryDuration = durationUntilExpiry
	}
	policy := &config.Signing{
		Default: &config.SigningProfile{
			Usage:        usages,
			Expiry:       certExpiryDuration,
			ExpiryString: certExpiryDuration.String(),
		},
	}

	cfs, err := local.NewSigner(caKey, caCert, signer.DefaultSigAlgo(caKey), policy)
	if err != nil {
		return nil, err
	}
	signedCert, err := cfs.Sign(signer.SignRequest{
		Request: string(csr.Spec.Request),
	})
	if err != nil {
		return nil, err
	}
	return signedCert, nil
}
