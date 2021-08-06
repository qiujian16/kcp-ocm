package controllers

import (
	"context"
	"time"

	"github.com/spf13/cobra"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/qiujian16/kcp-ocm/pkg/controllers/logicalcluster"
	clusterclient "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	workclient "open-cluster-management.io/api/client/work/clientset/versioned"
)

// OCMManagerOptions defines the flags for ocm manager
type OCMManagerOptions struct {
	KCPBaseKubeConfig string
}

// NewWorkloadAgentOptions returns the flags with default value set
func NewOCMManagerOptions() *OCMManagerOptions {
	return &OCMManagerOptions{}
}

// AddFlags register and binds the default flags
func (o *OCMManagerOptions) AddFlags(cmd *cobra.Command) {
	flags := cmd.Flags()
	// This command only supports reading from config
	flags.StringVar(&o.KCPBaseKubeConfig, "kcp-kubeconfig", o.KCPBaseKubeConfig, "Location of kubeconfig file to connect to kcp.")
}

// RunWorkloadAgent starts the controllers on agent to process work from hub.
func (o *OCMManagerOptions) RunManager(ctx context.Context, controllerContext *controllercmd.ControllerContext) error {
	clusterClient, err := clusterclient.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return err
	}

	workClient, err := workclient.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return err
	}

	clusterInformerFactory := clusterinformers.NewSharedInformerFactory(clusterClient, 5*time.Minute)

	kcpRestConfig, err := clientcmd.BuildConfigFromFlags("", o.KCPBaseKubeConfig)
	if err != nil {
		return err
	}

	controller := logicalcluster.NewWorkingNamespaceMapper(
		clusterClient,
		workClient,
		kcpRestConfig,
		clusterInformerFactory.Cluster().V1alpha1().ManagedClusterSetBindings(),
		controllerContext.EventRecorder,
	)

	go clusterInformerFactory.Start(ctx.Done())
	go controller.Run(ctx, 1)

	<-ctx.Done()
	return nil
}
