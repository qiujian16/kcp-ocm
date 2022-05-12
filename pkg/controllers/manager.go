package controllers

import (
	"context"
	"io/ioutil"
	"time"

	"github.com/qiujian16/kcp-ocm/pkg/controllers/addonmanagement"
	"github.com/spf13/pflag"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	addonclient "open-cluster-management.io/api/client/addon/clientset/versioned"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	clusterclient "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions"
)

// ManagerOptions defines the flags for kcp-ocm integration controller manager
type ManagerOptions struct {
	KCPCAFile         string
	KCPKeyFile        string
	KCPKubeConfigFile string
}

// NewManagerOptions returns the flags with default value set
func NewManagerOptions() *ManagerOptions {
	return &ManagerOptions{}
}

// AddFlags register and binds the default flags
func (o *ManagerOptions) AddFlags(flags *pflag.FlagSet) {
	flags.StringVar(&o.KCPCAFile, "kcp-ca", o.KCPCAFile, "Location of kcp ca file to connect to kcp.")
	flags.StringVar(&o.KCPKeyFile, "kcp-key", o.KCPKeyFile, "Location of kcp key file to connect to kcp.")
	flags.StringVar(&o.KCPKubeConfigFile, "kcp-kubeconfig", o.KCPKubeConfigFile, "Location of kcp kubeconfig file to connect to kcp root cluster.")
}

// Run starts all of controllers for kcp-ocm integration
func (o *ManagerOptions) Run(ctx context.Context, controllerContext *controllercmd.ControllerContext) error {
	kcpRootClusterConfig, err := clientcmd.BuildConfigFromFlags("", o.KCPKubeConfigFile)
	if err != nil {
		return err
	}

	clusterClient, err := clusterclient.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return err
	}

	addonClient, err := addonclient.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return err
	}

	caEnabled := false
	if o.KCPCAFile != "" && o.KCPKeyFile != "" {
		caEnabled = true
	}

	var ca, key []byte
	if caEnabled {
		ca, err = ioutil.ReadFile(o.KCPCAFile)
		if err != nil {
			return err
		}

		key, err = ioutil.ReadFile(o.KCPKeyFile)
		if err != nil {
			return err
		}
	}

	addonInformers := addoninformers.NewSharedInformerFactory(addonClient, 10*time.Minute)
	clusterInformers := clusterinformers.NewSharedInformerFactory(clusterClient, 10*time.Minute)

	clusterController := addonmanagement.NewClusterController(
		controllerContext.OperatorNamespace,
		caEnabled,
		kcpRootClusterConfig,
		addonClient,
		clusterInformers.Cluster().V1().ManagedClusters(),
		clusterInformers.Cluster().V1beta1().ManagedClusterSets(),
		addonInformers.Addon().V1alpha1().ManagedClusterAddOns(),
		controllerContext.EventRecorder,
	)

	clusterManagementAddonController := addonmanagement.NewClusterManagementAddonController(
		controllerContext.KubeConfig,
		kcpRootClusterConfig,
		addonClient,
		addonInformers.Addon().V1alpha1().ClusterManagementAddOns(),
		ca,
		key,
		controllerContext.EventRecorder,
	)

	go addonInformers.Start(ctx.Done())
	go clusterInformers.Start(ctx.Done())

	go clusterController.Run(ctx, 1)
	go clusterManagementAddonController.Run(ctx, 1)

	<-ctx.Done()
	return nil
}
