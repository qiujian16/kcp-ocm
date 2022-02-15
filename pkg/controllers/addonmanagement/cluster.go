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
	addonClient               addonv1alpha1client.Interface
	managedClusterLister      clusterlister.ManagedClusterLister
	managedClusterSetLister   clusterlisterv1beta1.ManagedClusterSetLister
	managedClusterAddonLister addonlisterv1alpha1.ManagedClusterAddOnLister
	eventRecorder             events.Recorder
}

func NewClusterController(
	addonClient addonv1alpha1client.Interface,
	clusterInformers clusterinformers.ManagedClusterInformer,
	clusterSetInformer clusterinformerv1beta1.ManagedClusterSetInformer,
	addonInformers addoninformerv1alpha1.ManagedClusterAddOnInformer,
	recorder events.Recorder,
) factory.Controller {
	c := &clusterController{
		addonClient:               addonClient,
		managedClusterLister:      clusterInformers.Lister(),
		managedClusterSetLister:   clusterSetInformer.Lister(),
		managedClusterAddonLister: addonInformers.Lister(),
		eventRecorder:             recorder.WithComponentSuffix("syncer-cluster-controller"),
	}

	return factory.New().
		WithFilteredEventsInformersQueueKeyFunc(
			func(obj runtime.Object) string {
				accessor, _ := meta.Accessor(obj)
				return accessor.GetNamespace()
			},
			func(obj interface{}) bool {
				accessor, _ := meta.Accessor(obj)
				return strings.HasPrefix(accessor.GetName(), "syncer")
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
		WithSync(c.sync).ToController("syncer-cluster-controller", recorder)
}

func (c *clusterController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	key := syncCtx.QueueKey()

	// if the sync is triggered by change of ManagedClusterSet, reconcile all managed clusters
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
	klog.Infof("reconcil cluster %s", clusterName)

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
		// TODO the cluster does not have the clusterset label, try to clean sync addons
		return nil
	}

	// find the clustersets that contains this managed cluster
	clusterSet, err := c.managedClusterSetLister.Get(clusterSetName)
	switch {
	case errors.IsNotFound(err):
		// TODO clean sync addons
		return nil
	case err != nil:
		return err
	}

	workspace := workspaceFromObject(clusterSet)

	// remove addons if it is not needed
	addons, err := c.managedClusterAddonLister.ManagedClusterAddOns(clusterName).List(labels.Everything())
	if err != nil {
		return err
	}

	for _, addon := range addons {
		if !strings.HasPrefix(addon.Name, "syncer") {
			continue
		}

		if len(workspace) != 0 && addon.Name == addonName(workspace) {
			continue
		}

		err := c.addonClient.AddonV1alpha1().ManagedClusterAddOns(clusterName).Delete(ctx, addon.Name, metav1.DeleteOptions{})
		if err != nil {
			return err
		}
	}

	if len(workspace) == 0 {
		// clean addons if any
		return nil
	}

	// apply managedclusteraddon
	_, err = c.managedClusterAddonLister.ManagedClusterAddOns(clusterName).Get(addonName(workspace))
	switch {
	case errors.IsNotFound(err):
		addon := &addonapiv1alpha1.ManagedClusterAddOn{
			ObjectMeta: metav1.ObjectMeta{
				Name:      addonName(workspace),
				Namespace: clusterName,
			},
			Spec: addonapiv1alpha1.ManagedClusterAddOnSpec{
				InstallNamespace: fmt.Sprintf("kcp-%s", addonName(workspace)),
			},
		}
		_, err = c.addonClient.AddonV1alpha1().ManagedClusterAddOns(clusterName).Create(ctx, addon, metav1.CreateOptions{})
		return err
	case err != nil:
		return err
	}

	return nil
}

func addonName(workspace string) string {
	return fmt.Sprintf("syncer-%s", workspace)
}

func workspaceFromObject(obj interface{}) string {
	accessor, _ := meta.Accessor(obj)
	if len(accessor.GetAnnotations()) == 0 {
		return ""
	}

	return accessor.GetAnnotations()["kcp-workspace"]
}
