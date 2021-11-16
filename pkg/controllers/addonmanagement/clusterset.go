package addonmanagement

import (
	"context"
	"fmt"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
	addoninformerv1alpha1 "open-cluster-management.io/api/client/addon/informers/externalversions/addon/v1alpha1"
	addonlisterv1alpha1 "open-cluster-management.io/api/client/addon/listers/addon/v1alpha1"
	clusterinformerv1beta1 "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1beta1"
	clusterlisterv1beta1 "open-cluster-management.io/api/client/cluster/listers/cluster/v1beta1"
)

// This controller has a controller loop that when a clusterset with
// an annotation of kcp-lcluster=<name of lcluster> is created, the controller
// maintains an syncer-addon and apply a clustermanagementaddon.
// The name of the addon will be "sycner-<lcluster name>-<clusterset name>"
// This ensure that all syncers for this cluster will be spawned later.

type clusterSetController struct {
	addonClient                  addonv1alpha1client.Interface
	clusterManagementAddonLister addonlisterv1alpha1.ClusterManagementAddOnLister
	clusterSetLister             clusterlisterv1beta1.ManagedClusterSetLister
	sycnerAddonMap               map[string]context.CancelFunc
	eventRecorder                events.Recorder
}

func NewClusterSetController(
	addonClient addonv1alpha1client.Interface,
	clusterManagementAddonInformer addoninformerv1alpha1.ClusterManagementAddOnInformer,
	clusterSetInformer clusterinformerv1beta1.ManagedClusterSetInformer,
	recorder events.Recorder,
) factory.Controller {
	c := &clusterSetController{
		addonClient:                  addonClient,
		clusterManagementAddonLister: clusterManagementAddonInformer.Lister(),
		clusterSetLister:             clusterSetInformer.Lister(),
		sycnerAddonMap:               map[string]context.CancelFunc{},
		eventRecorder:                recorder.WithComponentSuffix("syncer-clusterset-controller"),
	}

	return factory.New().WithInformersQueueKeyFunc(
		func(obj runtime.Object) string {
			accessor, _ := meta.Accessor(obj)
			return accessor.GetName()
		},
		clusterSetInformer.Informer()).
		WithInformersQueueKeyFunc(
			func(obj runtime.Object) string {
				accessor, _ := meta.Accessor(obj)
				// TODO get clusterset name from the clustermanagementaddon
				return accessor.GetName()
			},
			clusterManagementAddonInformer.Informer(),
		).
		WithSync(c.sync).ToController(fmt.Sprintf("syncer-clusterset-controller"), recorder)
}

func (c *clusterSetController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	// check if related clusterset exists
	// check if clusterset has workspace annotation
	// apply clustermanagementaddon
	return nil
}
