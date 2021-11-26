package synceraddons

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"embed"
	"encoding/base64"
	"encoding/pem"
	"fmt"
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
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"open-cluster-management.io/addon-framework/pkg/agent"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

const addonPrefix = "syncer-"

// An addon-framework implementation to deploy syncer and register the syncer to a lcluster on kcp
// It also needs to setup the rbac on lcluster for the syncer.

type syncerAddon struct {
	addonName string

	syncerCA      []byte
	syncerKey     []byte
	kcpRestConfig *rest.Config
	recorder      events.Recorder
}

var (
	genericScheme = runtime.NewScheme()
	genericCodecs = serializer.NewCodecFactory(genericScheme)
	genericCodec  = genericCodecs.UniversalDeserializer()
)

//go:embed manifests
var manifestFiles embed.FS

var permisionFiles = []string{
	"manifests/kcp_clusterrolebinding.yaml",
	"manifests/kcp_clusterrole.yaml",
	// This is the crd of the deployment, it is just to ensure that when syncer is deployed
	// the crd is already in the logical cluster.
	// TODO we should consider creating this when workspace is created instead of here.
	"manifests/apps_deployments.yaml",
}

var deployFiles = []string{
	"manifests/clusterrolebinding.yaml",
	"manifests/secret.yaml",
	"manifests/namespace.yaml",
	"manifests/deployment.yaml",
	"manifests/service_account.yaml",
}

const defaultSyncerImage = "quay.io/qiujian/syncer:latest"

func init() {
	scheme.AddToScheme(genericScheme)
}

func NewSyncerAddon(addonName string, ca, key []byte, kcpRestConfig *rest.Config) agent.AgentAddon {
	// needs to handle error later
	return &syncerAddon{
		addonName:     addonName,
		syncerCA:      ca,
		syncerKey:     key,
		kcpRestConfig: kcpRestConfig,
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
	return objects, nil
}

func (s *syncerAddon) GetAgentAddonOptions() agent.AgentAddonOptions {
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

	// create the kubeconfig to connect to kcp lcluster
	workspace := strings.TrimPrefix(addon.Name, addonPrefix)
	kubeconfig := buildKubeconfig(s.kcpRestConfig, workspace)
	kubeConfigData, err := clientcmd.Write(kubeconfig)
	if err != nil {
		return nil, err
	}

	manifestConfig := struct {
		AddonName  string
		Cluster    string
		Image      string
		Namespace  string
		KubeConfig string
	}{
		AddonName:  s.addonName,
		Cluster:    cluster.Name,
		Image:      image,
		Namespace:  addon.Spec.InstallNamespace,
		KubeConfig: base64.RawStdEncoding.EncodeToString(kubeConfigData),
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
	// Update config host to lcluster and generate kubeclient
	kconfig := rest.CopyConfig(s.kcpRestConfig)
	workspace := strings.TrimPrefix(addonName, addonPrefix)
	kconfig.Host = fmt.Sprintf("%s/clusters/%s", kconfig.Host, workspace)

	kubeclient, err := kubernetes.NewForConfig(kconfig)
	if err != nil {
		return err
	}

	apiExtensionClient, err := apiextensionsclient.NewForConfig(kconfig)
	if err != nil {
		return err
	}

	// apply syncer permission to the lcluster
	groups := agent.DefaultGroups(clusterName, addonName)
	config := struct {
		Cluster string
		Group   string
	}{
		Cluster: clusterName,
		Group:   groups[0],
	}

	results := resourceapply.ApplyDirectly(context.Background(),
		resourceapply.NewKubeClientHolder(kubeclient).WithAPIExtensionsClient(apiExtensionClient),
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

	return nil
}

// buildKubeconfig builds a kubeconfig based on a rest config template with a cert/key pair
func buildKubeconfig(clientConfig *rest.Config, workspace string) clientcmdapi.Config {
	// Build kubeconfig.
	kubeconfig := clientcmdapi.Config{
		// Define a cluster stanza based on the bootstrap kubeconfig.
		Clusters: map[string]*clientcmdapi.Cluster{"default-cluster": {
			Server:                fmt.Sprintf("%s/clusters/%s", clientConfig.Host, workspace),
			InsecureSkipTLSVerify: true,
		}},
		// Define auth based on the obtained client cert.
		AuthInfos: map[string]*clientcmdapi.AuthInfo{"default-auth": {
			ClientCertificate: "/syncer-certs/tls.crt",
			ClientKey:         "/syncer-certs/tls.key",
		}},
		// Define a context that connects the auth info and cluster, and set it as the default
		Contexts: map[string]*clientcmdapi.Context{"default-context": {
			Cluster:   "default-cluster",
			AuthInfo:  "default-auth",
			Namespace: "configuration",
		}},
		CurrentContext: "default-context",
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
