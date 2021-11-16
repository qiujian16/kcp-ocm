package synceraddons

import (
	"context"
	"embed"
	"fmt"
	"os"

	"github.com/openshift/library-go/pkg/assets"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	certificatesv1 "k8s.io/api/certificates/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"open-cluster-management.io/addon-framework/pkg/agent"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

// An addon-framework implementation to deploy syncer and register the syncer to a lcluster on kcp
// It also needs to setup the rbac on lcluster for the syncer.

type syncerAddon struct {
	addonName  string
	workspace  string
	clusterset string

	syncerCAFile  string
	kcpKubeClient kubernetes.Interface
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
}

var deployFiles = []string{
	"manifests/clusterrolebinding.yaml",
	"manifests/namespace.yaml",
	"manifests/deployment.yaml",
	"manifests/service_account.yaml",
}

const defaultSyncerImage = "quay.io/qiujian16/syncer:latest"

func init() {
	scheme.AddToScheme(genericScheme)
}

func NewSyncerAddon(workspace, clusterset, caFile string, kcpKubeclient kubernetes.Interface) agent.AgentAddon {
	return &syncerAddon{
		workspace:     workspace,
		clusterset:    clusterset,
		addonName:     fmt.Sprintf("sycner-%s-%s", workspace, clusterset),
		syncerCAFile:  caFile,
		kcpKubeClient: kcpKubeclient,
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
		AddonName: "helloworld",
		Registration: &agent.RegistrationOption{
			CSRConfigurations: s.signerConfiguration,
			CSRApproveCheck:   agent.ApprovalAllCSRs,
			CSRSign:           s.signer,
			PermissionConfig:  s.setupAgentPermissions,
		},
		InstallStrategy: agent.InstallAllStrategy("default"),
	}
}

func (s *syncerAddon) signerConfiguration(cluster *clusterv1.ManagedCluster) []addonapiv1alpha1.RegistrationConfig {
	return []addonapiv1alpha1.RegistrationConfig{
		{
			SignerName: "kcp-syncer",
			Subject: addonapiv1alpha1.Subject{
				User:   agent.DefaultUser(cluster.Name, s.addonName, "agent"),
				Groups: agent.DefaultGroups(cluster.Name, s.addonName),
			},
		},
	}
}

func (s *syncerAddon) signer(csr *certificatesv1.CertificateSigningRequest) []byte {
	// TODO add signer
	return nil
}

func (s *syncerAddon) setupAgentPermissions(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn) error {
	for _, file := range permisionFiles {
		if err := s.applyManifestFromFile(file, cluster.Name, addon.Name, s.recorder); err != nil {
			return err
		}
	}

	return nil
}

func (s *syncerAddon) loadManifestFromFile(file string, cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn) (runtime.Object, error) {
	image := os.Getenv("SYNCER_IMAGE_NAME")
	if len(image) == 0 {
		image = defaultSyncerImage
	}

	// TODO need to createt the kubeconfig to connect to kcp lcluster here

	manifestConfig := struct {
		WorkSpace string
		Cluster   string
		Image     string
	}{
		WorkSpace: s.workspace,
		Cluster:   cluster.Name,
		Image:     image,
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

func (s *syncerAddon) applyManifestFromFile(file, clusterName, addonName string, recorder events.Recorder) error {
	groups := agent.DefaultGroups(clusterName, addonName)
	config := struct {
		Cluster string
		Group   string
	}{
		Cluster: clusterName,
		Group:   groups[0],
	}

	results := resourceapply.ApplyDirectly(context.Background(),
		resourceapply.NewKubeClientHolder(s.kcpKubeClient),
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
