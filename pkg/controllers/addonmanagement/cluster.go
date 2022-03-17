package addonmanagement

import (
	"context"
	"fmt"
	"strings"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
	addoninformerv1alpha1 "open-cluster-management.io/api/client/addon/informers/externalversions/addon/v1alpha1"
	addonlisterv1alpha1 "open-cluster-management.io/api/client/addon/listers/addon/v1alpha1"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1"
	clusterinformerv1beta1 "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1beta1"
	clusterlister "open-cluster-management.io/api/client/cluster/listers/cluster/v1"
	clusterlisterv1beta1 "open-cluster-management.io/api/client/cluster/listers/cluster/v1beta1"
)

// The controller has the control loop on managedcluster. If a managedcluster is in
// a managedclusterset with annotation of kcp-lcluster=<name of lcluster>, a managedclusteraddon
// with the name of "sycner-<lcluster name>" will be created in the cluster namespace

const clusterSetLabel = "cluster.open-cluster-management.io/clusterset"

type clusterController struct {
	namespace                      string
	addonClient                    addonv1alpha1client.Interface
	managedClusterLister           clusterlister.ManagedClusterLister
	managedClusterSetLister        clusterlisterv1beta1.ManagedClusterSetLister
	managedClusterSetBindingLister clusterlisterv1beta1.ManagedClusterSetBindingLister
	managedClusterAddonLister      addonlisterv1alpha1.ManagedClusterAddOnLister
	eventRecorder                  events.Recorder
}

func NewClusterController(
	namespace string,
	addonClient addonv1alpha1client.Interface,
	clusterInformers clusterinformers.ManagedClusterInformer,
	clusterSetInformer clusterinformerv1beta1.ManagedClusterSetInformer,
	clusterSetBindingInformer clusterinformerv1beta1.ManagedClusterSetBindingInformer,
	addonInformers addoninformerv1alpha1.ManagedClusterAddOnInformer,
	recorder events.Recorder,
) factory.Controller {
	c := &clusterController{
		namespace:                      namespace,
		addonClient:                    addonClient,
		managedClusterLister:           clusterInformers.Lister(),
		managedClusterSetLister:        clusterSetInformer.Lister(),
		managedClusterSetBindingLister: clusterSetBindingInformer.Lister(),
		managedClusterAddonLister:      addonInformers.Lister(),
		eventRecorder:                  recorder.WithComponentSuffix("syncer-cluster-controller"),
	}

	return factory.New().
		WithFilteredEventsInformersQueueKeyFunc(
			func(obj runtime.Object) string {
				accessor, _ := meta.Accessor(obj)
				return accessor.GetNamespace()
			},
			func(obj interface{}) bool {
				accessor, _ := meta.Accessor(obj)
				return strings.HasPrefix(accessor.GetName(), "kcp-syncer")
			},
			addonInformers.Informer(),
		).
		WithInformersQueueKeyFunc(
			func(obj runtime.Object) string {
				accessor, _ := meta.Accessor(obj)
				return accessor.GetName()
			},
			clusterInformers.Informer(),
		).
		WithInformers(clusterSetInformer.Informer()).
		WithInformers(clusterSetBindingInformer.Informer()).
		WithSync(c.sync).ToController("syncer-cluster-controller", recorder)
}

func (c *clusterController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	key := syncCtx.QueueKey()

	// if the sync is triggered by change of ManagedClusterSet or ManagedClusterSetBinding, reconcile all managed clusters
	if key == factory.DefaultQueueKey {
		clusters, err := c.managedClusterLister.List(labels.Everything())
		if err != nil {
			return err
		}
		for _, cluster := range clusters {
			// enqueue the managed cluster to reconcile
			syncCtx.Queue().Add(cluster.Name)
		}

		return nil
	}

	clusterName := key
	klog.V(4).Infof("reconcil cluster %s", clusterName)

	cluster, err := c.managedClusterLister.Get(clusterName)
	switch {
	case errors.IsNotFound(err):
		// clean addons if any
		return nil
	case err != nil:
		return err
	}

	clusterSetName, existed := cluster.Labels[clusterSetLabel]
	if !existed {
		// the cluster is not in a clusterset, try to clean its sync addons
		return c.removeAddons(ctx, clusterName, sets.NewString())
	}

	// find the clustersets that contains this managed cluster
	_, err = c.managedClusterSetLister.Get(clusterSetName)
	switch {
	case errors.IsNotFound(err):
		// clean sync addons
		return c.removeAddons(ctx, clusterName, sets.NewString())
	case err != nil:
		return err
	}

	// ensure the clusterset is binding with a kcp workspace namespace
	workspaces := sets.NewString()
	clusterSetBindings, err := c.managedClusterSetBindingLister.List(labels.Everything())
	if err != nil {
		return err
	}
	for _, clusterSetBinding := range clusterSetBindings {
		if clusterSetBinding.Name == clusterSetName {
			workspaces.Insert(strings.TrimPrefix(clusterSetBinding.Namespace, "kcp-"))
		}
	}

	// remove addons if it is not needed
	if err := c.removeAddons(ctx, clusterName, workspaces); err != nil {
		return err
	}

	// apply managedclusteraddon for each workspace namespace
	return c.applyAddons(ctx, clusterName, workspaces)
}

func (c *clusterController) removeAddons(ctx context.Context, clusterName string, workspaces sets.String) error {
	addons, err := c.managedClusterAddonLister.ManagedClusterAddOns(clusterName).List(labels.Everything())
	if err != nil {
		return err
	}

	for _, addon := range addons {
		if !strings.HasPrefix(addon.Name, "kcp-syncer-") {
			continue
		}

		if workspaces.Has(strings.TrimPrefix(addon.Name, "kcp-syncer-")) {
			continue
		}

		err := c.addonClient.AddonV1alpha1().ManagedClusterAddOns(clusterName).Delete(ctx, addon.Name, metav1.DeleteOptions{})
		if err != nil {
			return err
		}
	}

	return nil
}

func (c *clusterController) applyAddons(ctx context.Context, clusterName string, workspaces sets.String) error {
	for workspace := range workspaces {
		_, err := c.managedClusterAddonLister.ManagedClusterAddOns(clusterName).Get(fmt.Sprintf("syncer-%s", workspace))
		switch {
		case errors.IsNotFound(err):
			addon := &addonapiv1alpha1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("kcp-syncer-%s", workspace),
					Namespace: clusterName,
				},
				Spec: addonapiv1alpha1.ManagedClusterAddOnSpec{
					InstallNamespace: fmt.Sprintf("kcp-syncer-%s", workspace),
				},
			}
			_, err = c.addonClient.AddonV1alpha1().ManagedClusterAddOns(clusterName).Create(ctx, addon, metav1.CreateOptions{})
			return err
		case err != nil:
			return err
		}
	}
	return nil
}
