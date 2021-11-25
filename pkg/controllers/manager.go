package controllers

import (
	"context"
	"io/ioutil"
	"time"

	"github.com/qiujian16/kcp-ocm/pkg/controllers/addonmanagement"
	"github.com/spf13/cobra"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	addonclient "open-cluster-management.io/api/client/addon/clientset/versioned"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	clusterclient "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions"
)

// OCMManagerOptions defines the flags for ocm manager
type OCMManagerOptions struct {
	KCPRootCAFile     string
	KCPKeyFile        string
	KCPKubeConfigFile string
}

// NewWorkloadAgentOptions returns the flags with default value set
func NewOCMManagerOptions() *OCMManagerOptions {
	return &OCMManagerOptions{}
}

// AddFlags register and binds the default flags
func (o *OCMManagerOptions) AddFlags(cmd *cobra.Command) {
	flags := cmd.Flags()
	// This command only supports reading from config
	flags.StringVar(&o.KCPRootCAFile, "kcp-ca", o.KCPRootCAFile, "Location of kcp ca file to connect to kcp.")
	flags.StringVar(&o.KCPKeyFile, "kcp-key", o.KCPKeyFile, "Location of kcp key file to connect to kcp.")
	flags.StringVar(&o.KCPKubeConfigFile, "kcp-kubeconfig", o.KCPKubeConfigFile, "Location of kcp kubeconfig file to connect to kcp.")
}

// RunWorkloadAgent starts the controllers on agent to process work from hub.
func (o *OCMManagerOptions) RunManager(ctx context.Context, controllerContext *controllercmd.ControllerContext) error {
	kcpRestConfig, err := clientcmd.BuildConfigFromFlags("", o.KCPKubeConfigFile)
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

	ca, err := ioutil.ReadFile(o.KCPRootCAFile)
	if err != nil {
		return err
	}
	key, err := ioutil.ReadFile(o.KCPKeyFile)
	if err != nil {
		return err
	}

	addonInformers := addoninformers.NewSharedInformerFactory(addonClient, 10*time.Minute)
	clusterInformers := clusterinformers.NewSharedInformerFactory(clusterClient, 10*time.Minute)

	clusterController := addonmanagement.NewClusterController(
		addonClient,
		clusterInformers.Cluster().V1().ManagedClusters(),
		addonInformers.Addon().V1alpha1().ManagedClusterAddOns(),
		controllerContext.EventRecorder,
	)
	clusterManagementAddonController := addonmanagement.NewClusterManagementAddonController(
		addonClient,
		addonInformers.Addon().V1alpha1().ClusterManagementAddOns(),
		clusterInformers.Cluster().V1beta1().ManagedClusterSets(),
		controllerContext.KubeConfig,
		kcpRestConfig,
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
