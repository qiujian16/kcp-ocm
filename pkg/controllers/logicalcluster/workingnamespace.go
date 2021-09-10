package logicalcluster

import (
	"context"
	"fmt"
	"time"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/qiujian16/kcp-ocm/pkg/controllers/splitter"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	clusterclient "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	clusterinformerv1alpha1 "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1alpha1"
	clusterlisterv1alpha1 "open-cluster-management.io/api/client/cluster/listers/cluster/v1alpha1"
	workclient "open-cluster-management.io/api/client/work/clientset/versioned"
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions"
)

type mapperConfiguration struct {
	workingNamespace string
	cancel           context.CancelFunc
}

// WorkingNamespaceMapper is to map a logical cluster to a working namespace
type WorkingNamespaceMapper struct {
	logicalClusterMapper    map[string]*mapperConfiguration
	clusterSetBindingLister clusterlisterv1alpha1.ManagedClusterSetBindingLister
	clusterClient           clusterclient.Interface
	manifestWorkClient      workclient.Interface
	kcpBaseConfig           *rest.Config
	recorder                events.Recorder
}

func NewWorkingNamespaceMapper(
	clusterClient clusterclient.Interface,
	manifestWorkClient workclient.Interface,
	kcpBaseConfig *rest.Config,
	clusterBindingInformer clusterinformerv1alpha1.ManagedClusterSetBindingInformer,
	recorder events.Recorder,
) factory.Controller {
	c := &WorkingNamespaceMapper{
		logicalClusterMapper:    map[string]*mapperConfiguration{},
		clusterSetBindingLister: clusterBindingInformer.Lister(),
		clusterClient:           clusterClient,
		manifestWorkClient:      manifestWorkClient,
		kcpBaseConfig:           kcpBaseConfig,
		recorder:                recorder,
	}

	return factory.New().
		WithInformersQueueKeyFunc(func(obj runtime.Object) string {
			accessor, _ := meta.Accessor(obj)
			return accessor.GetNamespace()
		}, clusterBindingInformer.Informer()).
		WithSync(c.sync).ToController("ManifestWorkAgent", recorder)
}

func (w *WorkingNamespaceMapper) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	namespace := syncCtx.QueueKey()

	bindings, err := w.clusterSetBindingLister.ManagedClusterSetBindings(namespace).List(labels.Everything())

	if err != nil {
		return err
	}

	config, ok := w.logicalClusterMapper[namespace]

	// There is no binddings in it,  we remove the syncer
	if len(bindings) == 0 {
		if !ok {
			return nil
		}

		config.cancel()
		delete(w.logicalClusterMapper, namespace)
		return nil
	}

	if ok {
		return nil
	}

	cancel, err := w.startMapper(ctx, namespace)
	if err != nil {
		return err
	}

	// Add the working space to the mapper
	w.logicalClusterMapper[namespace] = &mapperConfiguration{
		workingNamespace: namespace,
		cancel:           cancel,
	}

	return nil
}

func (w *WorkingNamespaceMapper) startMapper(ctx context.Context, namespace string) (context.CancelFunc, error) {
	currentCtx, stopFunc := context.WithCancel(ctx)

	clusterInformerFactory := clusterinformers.NewSharedInformerFactoryWithOptions(
		w.clusterClient, 5*time.Minute, clusterinformers.WithNamespace(namespace))

	workInformerFactory := workinformers.NewSharedInformerFactory(w.manifestWorkClient, 5*time.Minute)

	restConfig := rest.CopyConfig(w.kcpBaseConfig)
	restConfig.Host = fmt.Sprintf("%s/clusters/%s", restConfig.Host, namespace)

	kubeClient, err := kubernetes.NewForConfig(restConfig)

	if err != nil {
		return stopFunc, err
	}

	kubeInformer := informers.NewSharedInformerFactory(kubeClient, 5*time.Minute)

	controller := splitter.NewDeploymentSplitter(
		w.clusterClient,
		kubeClient,
		w.manifestWorkClient.WorkV1(),
		namespace,
		kubeInformer.Apps().V1().Deployments(),
		clusterInformerFactory.Cluster().V1alpha1().Placements(),
		clusterInformerFactory.Cluster().V1alpha1().PlacementDecisions(),
		workInformerFactory.Work().V1().ManifestWorks(),
		w.recorder)

	go workInformerFactory.Start(currentCtx.Done())
	go clusterInformerFactory.Start(currentCtx.Done())
	go kubeInformer.Start(currentCtx.Done())
	go controller.Run(currentCtx, 1)

	return stopFunc, nil
}
