package addonmanagement

import (
	"context"
	"fmt"
	"strings"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/qiujian16/kcp-ocm/pkg/helpers"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
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
// a managedclusterset with an annotation of "kcp-workspace=<workspace id>", a
// managedclusteraddon with the name of "kcp-sycner-<workspace name>" will be created
// in the cluster namespace.

const clusterSetLabel = "cluster.open-cluster-management.io/clusterset"

var clusterGVR = schema.GroupVersionResource{
	Group:    "workload.kcp.dev",
	Version:  "v1alpha1",
	Resource: "workloadclusters",
}

type clusterController struct {
	namespace                 string
	caEnabled                 bool
	kcpRootClusterConfig      *rest.Config
	addonClient               addonv1alpha1client.Interface
	managedClusterLister      clusterlister.ManagedClusterLister
	managedClusterSetLister   clusterlisterv1beta1.ManagedClusterSetLister
	managedClusterAddonLister addonlisterv1alpha1.ManagedClusterAddOnLister
	eventRecorder             events.Recorder
}

func NewClusterController(
	namespace string,
	caEnabled bool,
	kcpRootClusterConfig *rest.Config,
	addonClient addonv1alpha1client.Interface,
	clusterInformers clusterinformers.ManagedClusterInformer,
	clusterSetInformer clusterinformerv1beta1.ManagedClusterSetInformer,
	addonInformers addoninformerv1alpha1.ManagedClusterAddOnInformer,
	recorder events.Recorder) factory.Controller {
	c := &clusterController{
		namespace:                 namespace,
		caEnabled:                 caEnabled,
		kcpRootClusterConfig:      kcpRootClusterConfig,
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
				return strings.HasPrefix(accessor.GetName(), "kcp-syncer-")
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
	klog.V(4).Infof("Reconcil cluster %s", clusterName)

	cluster, err := c.managedClusterLister.Get(clusterName)
	switch {
	case errors.IsNotFound(err):
		return nil
	case err != nil:
		return err
	}

	clusterSetName, existed := cluster.Labels[clusterSetLabel]
	if !existed {
		// try to clean all kcp syncer addons
		return c.removeAddons(ctx, clusterName, "")
	}

	clusterSet, err := c.managedClusterSetLister.Get(clusterSetName)
	switch {
	case errors.IsNotFound(err):
		// try to clean all kcp syncer addons
		return c.removeAddons(ctx, clusterName, "")
	case err != nil:
		return err
	}

	// get the workspace identifier from the managedclusterset workspace
	// annotation, the format of the annotation value will be <org>:<workspace>
	workspaceId := helpers.GetWorkspaceIdFromObject(clusterSet)

	// remove addons if they are not needed
	if err := c.removeAddons(ctx, clusterName, workspaceId); err != nil {
		return err
	}

	if len(workspaceId) == 0 {
		return nil
	}

	// get the host endpoint of the workspace
	workspaceHost, err := c.getWorkspaceHost(ctx, workspaceId)
	if err != nil {
		return err
	}

	// if ca is not enabled, create a service account for kcp-syncer in the workspace
	if !c.caEnabled {
		if err := c.applyServiceAccount(ctx, workspaceId, workspaceHost); err != nil {
			return err
		}
	}

	// create a workloadcluster in the workspace to correspond to this managed cluster
	if err := c.applyWorkloadCluster(ctx, workspaceHost, clusterName); err != nil {
		return err
	}

	// apply a clustermanagementaddon to start a addon manager
	if err := c.applyClusterManagementAddOn(ctx, workspaceId); err != nil {
		return err
	}

	// apply a managedclusteraddon for this managed cluster
	return c.applyAddon(ctx, clusterName, workspaceId)
}

// TODO remove the workloadclusters from the workspace
func (c *clusterController) removeAddons(ctx context.Context, clusterName, workspaceId string) error {
	addons, err := c.managedClusterAddonLister.ManagedClusterAddOns(clusterName).List(labels.Everything())
	if err != nil {
		return err
	}

	for _, addon := range addons {
		if !strings.HasPrefix(addon.Name, "kcp-syncer-") {
			continue
		}

		if addon.Name == helpers.GetAddonName(workspaceId) {
			continue
		}

		err := c.addonClient.AddonV1alpha1().ManagedClusterAddOns(clusterName).Delete(ctx, addon.Name, metav1.DeleteOptions{})
		if err != nil {
			return err
		}
	}

	return nil
}

func (c *clusterController) getWorkspaceHost(ctx context.Context, workspaceId string) (string, error) {
	parentWorkspaceConfig := rest.CopyConfig(c.kcpRootClusterConfig)
	parentWorkspaceConfig.Host = c.kcpRootClusterConfig.Host + helpers.GetParentWorkspaceId(workspaceId)

	dynamicClient, err := dynamic.NewForConfig(parentWorkspaceConfig)
	if err != nil {
		return "", err
	}

	workspaceName := helpers.GetWorkspaceName(workspaceId)

	workspace, err := dynamicClient.Resource(helpers.ClusterWorkspaceGVR).Get(ctx, workspaceName, metav1.GetOptions{})
	if err != nil {
		return "", err
	}

	workspaceType := helpers.GetWorkspaceType(workspace)
	if workspaceType != "Universal" {
		return "", fmt.Errorf("the workspace %s type (%s) is not Univeral", workspaceId, workspaceType)
	}

	if helpers.GetWorkspacePhase(workspace) != "Ready" {
		return "", fmt.Errorf("the workspace %s is not ready", workspaceId)
	}

	return helpers.GetWorkspaceURL(workspace), nil
}

func (c *clusterController) applyClusterManagementAddOn(ctx context.Context, workspaceId string) error {
	clusterManagementAddOnName := helpers.GetAddonName(workspaceId)
	_, err := c.addonClient.AddonV1alpha1().ClusterManagementAddOns().Get(ctx, clusterManagementAddOnName, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		clusterManagementAddOn := &addonapiv1alpha1.ClusterManagementAddOn{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterManagementAddOnName,
				Annotations: map[string]string{
					"kcp-workspace": workspaceId,
				},
			},
			Spec: addonapiv1alpha1.ClusterManagementAddOnSpec{
				AddOnMeta: addonapiv1alpha1.AddOnMeta{
					DisplayName: clusterManagementAddOnName,
				},
			},
		}

		if _, err := c.addonClient.AddonV1alpha1().ClusterManagementAddOns().Create(ctx, clusterManagementAddOn, metav1.CreateOptions{}); err != nil {
			return err
		}

		c.eventRecorder.Eventf("KCPSyncerClusterManagementAddOnCreated", "The kcp-syncer clusterManagement addon %s is created", clusterManagementAddOnName)
		return nil
	}

	return err
}

func (c *clusterController) applyServiceAccount(ctx context.Context, workspaceId, workspaceHost string) error {
	workspaceConfig := rest.CopyConfig(c.kcpRootClusterConfig)
	workspaceConfig.Host = workspaceHost

	kubeClient, err := kubernetes.NewForConfig(workspaceConfig)
	if err != nil {
		return err
	}

	saName := fmt.Sprintf("%s-sa", helpers.GetAddonName(workspaceId))
	_, err = kubeClient.CoreV1().ServiceAccounts("default").Get(ctx, saName, metav1.GetOptions{})
	if err == nil {
		return nil
	}

	if !errors.IsNotFound(err) {
		return err
	}

	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name: saName,
		},
	}
	if _, err := kubeClient.CoreV1().ServiceAccounts("default").Create(ctx, sa, metav1.CreateOptions{}); err != nil {
		return err
	}

	c.eventRecorder.Eventf("WorkspaceServiceAccountCreated", "The service account default/%s is created in workspace %s", saName, workspaceId)
	return nil

}

func (c *clusterController) applyWorkloadCluster(ctx context.Context, workspaceHost, clusterName string) error {
	workspaceConfig := rest.CopyConfig(c.kcpRootClusterConfig)
	workspaceConfig.Host = workspaceHost

	dynamicClient, err := dynamic.NewForConfig(workspaceConfig)
	if err != nil {
		return err
	}

	_, err = dynamicClient.Resource(clusterGVR).Get(ctx, clusterName, metav1.GetOptions{})
	if err == nil {
		return nil
	}

	if !errors.IsNotFound(err) {
		return err
	}

	workloadCluster := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "workload.kcp.dev/v1alpha1",
			"kind":       "WorkloadCluster",
			"metadata": map[string]interface{}{
				"name": clusterName,
			},
			"spec": map[string]interface{}{
				"kubeconfig": "",
			},
		},
	}
	if _, err := dynamicClient.Resource(clusterGVR).Create(ctx, workloadCluster, metav1.CreateOptions{}); err != nil {
		return err
	}

	c.eventRecorder.Eventf("KCPWorkloadClusterCreated", "The KCP workload cluster %s is created in workspace %s", clusterName, workspaceConfig.Host)
	return nil
}

func (c *clusterController) applyAddon(ctx context.Context, clusterName, workspaceId string) error {
	addonName := helpers.GetAddonName(workspaceId)
	_, err := c.managedClusterAddonLister.ManagedClusterAddOns(clusterName).Get(addonName)
	if errors.IsNotFound(err) {
		addon := &addonapiv1alpha1.ManagedClusterAddOn{
			ObjectMeta: metav1.ObjectMeta{
				Name:      addonName,
				Namespace: clusterName,
			},
			Spec: addonapiv1alpha1.ManagedClusterAddOnSpec{
				InstallNamespace: addonName,
			},
		}

		if _, err := c.addonClient.AddonV1alpha1().ManagedClusterAddOns(clusterName).Create(ctx, addon, metav1.CreateOptions{}); err != nil {
			return err
		}

		c.eventRecorder.Eventf("KCPSyncerAddOnCreated", "The kcp-syncer addon %s is created in cluster %s", addonName, clusterName)
		return nil
	}

	return err
}
