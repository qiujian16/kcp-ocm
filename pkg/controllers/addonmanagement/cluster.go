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
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
	addoninformerv1alpha1 "open-cluster-management.io/api/client/addon/informers/externalversions/addon/v1alpha1"
	addonlisterv1alpha1 "open-cluster-management.io/api/client/addon/listers/addon/v1alpha1"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1"
	clusterlister "open-cluster-management.io/api/client/cluster/listers/cluster/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

// The controller has the control loop on managedcluster. If a managedcluster is in
// a managedclusterset with annotation of kcp-lcluster=<name of lcluster>, a managedclusteraddon
// with the name of "sycner-<lcluster name>" will be created in the cluster namespace

type clusterController struct {
	addonClient               addonv1alpha1client.Interface
	managedClusterLister      clusterlister.ManagedClusterLister
	managedClusterAddonLister addonlisterv1alpha1.ManagedClusterAddOnLister
	eventRecorder             events.Recorder
}

func NewClusterController(
	addonClient addonv1alpha1client.Interface,
	clusterInformers clusterinformers.ManagedClusterInformer,
	addonInformers addoninformerv1alpha1.ManagedClusterAddOnInformer,
	recorder events.Recorder,
) factory.Controller {
	c := &clusterController{
		addonClient:               addonClient,
		managedClusterLister:      clusterInformers.Lister(),
		managedClusterAddonLister: addonInformers.Lister(),
		eventRecorder:             recorder.WithComponentSuffix("syncer-cluster-controller"),
	}

	return factory.New().WithFilteredEventsInformersQueueKeyFunc(
		func(obj runtime.Object) string {
			accessor, _ := meta.Accessor(obj)
			return accessor.GetNamespace()
		},
		func(obj interface{}) bool {
			accessor, _ := meta.Accessor(obj)
			if strings.HasPrefix(accessor.GetName(), "syncer") {
				return true
			}
			return false
		},
		addonInformers.Informer()).
		WithInformersQueueKeyFunc(
			func(obj runtime.Object) string {
				accessor, _ := meta.Accessor(obj)
				return accessor.GetName()
			},
			clusterInformers.Informer(),
		).
		WithSync(c.sync).ToController(fmt.Sprintf("syncer-cluster-controller"), recorder)
}

func (c *clusterController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	// check if related clusterset exists
	clusterName := syncCtx.QueueKey()
	klog.Infof("reconcil cluster %s", clusterName)

	cluster, err := c.managedClusterLister.Get(clusterName)
	switch {
	case errors.IsNotFound(err):
		// clean addons if any
		return nil
	case err != nil:
		return err
	}

	// check if clusterset has workspace annotation
	clusterSetName := clusterSetFromCluster(cluster)
	if len(clusterSetName) == 0 {
		return nil
	}

	workspace := workspaceFromObject(cluster)
	if len(workspace) == 0 {
		// clean addons if any
		return nil
	}

	// apply managedclusteraddon
	addon, err := c.managedClusterAddonLister.ManagedClusterAddOns(clusterName).Get(addonName(workspace))
	switch {
	case errors.IsNotFound(err):
		addon = &addonapiv1alpha1.ManagedClusterAddOn{
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

func clusterSetFromCluster(cluster *clusterv1.ManagedCluster) string {
	if len(cluster.Labels) == 0 {
		return ""
	}

	return cluster.Labels["cluster.open-cluster-management.io/clusterset"]
}

func workspaceFromObject(obj interface{}) string {
	accessor, _ := meta.Accessor(obj)
	if len(accessor.GetAnnotations()) == 0 {
		return ""
	}

	return accessor.GetAnnotations()["kcp-lcluster"]
}
