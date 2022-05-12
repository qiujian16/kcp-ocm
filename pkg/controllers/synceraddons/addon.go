package synceraddons

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"embed"
	"encoding/pem"
	"fmt"
	"net/url"
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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
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

const defaultSyncerImage = "quay.io/skeeey/kcp-syncer:release-0.4"

// An addon-framework implementation to deploy syncer and register the syncer to a workspace on kcp
// It also needs to setup the rbac in the workspace for the syncer.

type syncerAddon struct {
	addonName string

	kcpWorkspaceRestConfig *rest.Config
	kcpServer              string
	kcpLogicalCluster      string

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

func NewSyncerAddon(addonName string, ca, key []byte, kcpWorkspaceRestConfig *rest.Config, recoder events.Recorder) agent.AgentAddon {
	kcpURL, err := url.Parse(kcpWorkspaceRestConfig.Host)
	if err != nil {
		panic(err)
	}

	certsEnabled := false
	if ca != nil && key != nil {
		certsEnabled = true
	}

	return &syncerAddon{
		addonName:              addonName,
		kcpWorkspaceRestConfig: kcpWorkspaceRestConfig,
		kcpServer:              fmt.Sprintf("%s://%s", kcpURL.Scheme, kcpURL.Host),
		kcpLogicalCluster:      strings.TrimPrefix(kcpURL.Path, "/clusters/"),
		certsEnabled:           certsEnabled,
		syncerCA:               ca,
		syncerKey:              key,
		recorder:               recoder,
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

	kubeConfigData, err := s.buildKubeconfig()
	if err != nil {
		return nil, err
	}

	objects = append(objects, &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Secret",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kcp-syncer-config",
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

func (s *syncerAddon) setupAgentPermissions(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn) error {
	// bind the cluster role in the kcp workspace
	return s.bindClusterRole(cluster.Name, addon.Name, s.recorder)
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

func (s *syncerAddon) loadManifestFromFile(file string, cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn) (runtime.Object, error) {
	manifestConfig := struct {
		AddonName           string
		Cluster             string
		LogicalCluster      string
		LogicalClusterLabel string
		Image               string
		Namespace           string
		CertsEnabled        bool
	}{
		AddonName:           s.addonName,
		Cluster:             cluster.Name,
		LogicalCluster:      s.kcpLogicalCluster,
		LogicalClusterLabel: strings.ReplaceAll(s.kcpLogicalCluster, ":", "_"),
		Image:               defaultSyncerImage,
		Namespace:           addon.Spec.InstallNamespace,
		CertsEnabled:        s.certsEnabled,
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

func (s *syncerAddon) bindClusterRole(clusterName, addonName string, recorder events.Recorder) error {
	// apply clusterrolebindings for addon
	kconfig := rest.CopyConfig(s.kcpWorkspaceRestConfig)
	kubeclient, err := kubernetes.NewForConfig(kconfig)
	if err != nil {
		return err
	}

	groups := agent.DefaultGroups(clusterName, addonName)
	config := struct {
		AddonName    string
		Cluster      string
		Group        string
		CertsEnabled bool
	}{
		AddonName:    addonName,
		Cluster:      clusterName,
		Group:        groups[0],
		CertsEnabled: s.certsEnabled,
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

	return nil
}

// buildKubeconfig builds a kubeconfig based on a rest config template
func (s *syncerAddon) buildKubeconfig() ([]byte, error) {
	kubeconfig := clientcmdapi.Config{
		Clusters: map[string]*clientcmdapi.Cluster{"default-cluster": {
			Server:                s.kcpServer,
			InsecureSkipTLSVerify: true,
		}},
		AuthInfos: map[string]*clientcmdapi.AuthInfo{"default-auth": {
			ClientCertificate: "/kcp-syncer-certs/tls.crt",
			ClientKey:         "/kcp-syncer-certs/tls.key",
		}},
		// Define a context that connects the auth info and cluster, and set it as the default
		Contexts: map[string]*clientcmdapi.Context{"default-context": {
			Cluster:   "default-cluster",
			AuthInfo:  "default-auth",
			Namespace: "configuration",
		}},
		CurrentContext: "default-context",
	}

	if !s.certsEnabled {
		token, err := s.getAddOnSAToken()
		if err != nil {
			return nil, err
		}
		// Define auth based on the obtained client cert.
		kubeconfig.AuthInfos = map[string]*clientcmdapi.AuthInfo{"default-auth": {
			Token: token,
		}}
	}

	return clientcmd.Write(kubeconfig)
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

func (s *syncerAddon) getAddOnSAToken() (string, error) {
	kubeclient, err := kubernetes.NewForConfig(s.kcpWorkspaceRestConfig)
	if err != nil {
		return "", err
	}

	workspaceSAName := fmt.Sprintf("%s-sa", s.addonName)
	sa, err := kubeclient.CoreV1().ServiceAccounts("default").Get(context.Background(), workspaceSAName, metav1.GetOptions{})
	if err != nil {
		return "", err
	}

	for _, secretRef := range sa.Secrets {
		if !strings.HasPrefix(secretRef.Name, workspaceSAName) {
			continue
		}
		secret, err := kubeclient.CoreV1().Secrets("default").Get(context.Background(), secretRef.Name, metav1.GetOptions{})
		if err != nil {
			continue
		}

		if secret.Type != corev1.SecretTypeServiceAccountToken {
			continue
		}

		token, ok := secret.Data["token"]
		if !ok {
			continue
		}
		if len(token) == 0 {
			continue
		}

		return string(token), nil
	}

	return "", fmt.Errorf("failed to get the token of worksapce sa %s in namespace kcp-ocm", workspaceSAName)
}
