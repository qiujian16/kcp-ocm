package addonmanagement

import (
	"context"
	"fmt"
	"strings"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/qiujian16/kcp-ocm/pkg/controllers/synceraddons"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	"open-cluster-management.io/addon-framework/pkg/addonmanager"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
	addoninformerv1alpha1 "open-cluster-management.io/api/client/addon/informers/externalversions/addon/v1alpha1"
	addonlisterv1alpha1 "open-cluster-management.io/api/client/addon/listers/addon/v1alpha1"
)

// This controller has a controller loop that when a clustermanagementaddon with
// the name is "sycner-<lcluster>" is created, the controller
// maintains an syncer-addon for each lcluster
// This ensure that all syncers for this cluster will be spawned later.

const (
	cmaddonFinalizer = "addon.open-cluster-management.io/cleanup"
	addonPrefix      = "syncer"
)

var workspaceGVR = schema.GroupVersionResource{
	Group:    "tenancy.kcp.dev",
	Version:  "v1alpha1",
	Resource: "workspaces",
}

type clusterManagementAddonController struct {
	namespace                    string
	kcpDynamicClient             dynamic.Interface
	addonClient                  addonv1alpha1client.Interface
	clusterManagementAddonLister addonlisterv1alpha1.ClusterManagementAddOnLister
	sycnerAddonMap               map[string]context.CancelFunc
	ca                           []byte
	key                          []byte
	hubKubconfig                 *rest.Config
	kcpRestConfig                *rest.Config
	eventRecorder                events.Recorder
}

func NewClusterManagementAddonController(
	namespace string,
	kcpDynamicClient dynamic.Interface,
	addonClient addonv1alpha1client.Interface,
	clusterManagementAddonInformer addoninformerv1alpha1.ClusterManagementAddOnInformer,
	hubKubconfig *rest.Config,
	kcpRestConfig *rest.Config,
	ca, key []byte,
	recorder events.Recorder,
) factory.Controller {
	c := &clusterManagementAddonController{
		namespace:                    namespace,
		kcpDynamicClient:             kcpDynamicClient,
		addonClient:                  addonClient,
		clusterManagementAddonLister: clusterManagementAddonInformer.Lister(),
		sycnerAddonMap:               map[string]context.CancelFunc{},
		hubKubconfig:                 hubKubconfig,
		ca:                           ca,
		key:                          key,
		kcpRestConfig:                kcpRestConfig,
		eventRecorder:                recorder.WithComponentSuffix("syncer-addon-controller"),
	}

	return factory.New().WithFilteredEventsInformersQueueKeyFunc(
		func(obj runtime.Object) string {
			accessor, _ := meta.Accessor(obj)
			return accessor.GetName()
		},
		func(obj interface{}) bool {
			accessor, _ := meta.Accessor(obj)
			return strings.HasPrefix(accessor.GetName(), "syncer-")
		},
		clusterManagementAddonInformer.Informer()).
		WithSync(c.sync).ToController("syncer-addon-controller", recorder)
}

func (c *clusterManagementAddonController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	cmaddonName := syncCtx.QueueKey()
	klog.V(4).Infof("Reconcil clustermanagementaddon %s", cmaddonName)

	// get clustermanagementaddon
	cmaddon, err := c.clusterManagementAddonLister.Get(cmaddonName)
	switch {
	case errors.IsNotFound(err):
		return nil
	case err != nil:
		return err
	}

	// ensure the mapped workspace exists
	workspaceName := strings.TrimPrefix(cmaddon.Name, fmt.Sprintf("%s-%s-", addonPrefix, c.namespace))
	workspace, err := c.kcpDynamicClient.Resource(workspaceGVR).Get(ctx, workspaceName, metav1.GetOptions{})
	switch {
	case errors.IsNotFound(err):
		return nil
	case err != nil:
		return err
	}

	unstructuredWorkspace, err := runtime.DefaultUnstructuredConverter.ToUnstructured(workspace)
	if err != nil {
		panic(err)
	}

	baseURL, found, err := unstructured.NestedString(unstructuredWorkspace, "status", "baseURL")
	if err != nil {
		panic(err)
	}
	if !found {
		return nil
	}

	cmaddon = cmaddon.DeepCopy()
	if cmaddon.DeletionTimestamp.IsZero() {
		hasFinalizer := false
		for i := range cmaddon.Finalizers {
			if cmaddon.Finalizers[i] == cmaddonFinalizer {
				hasFinalizer = true
				break
			}
		}
		if !hasFinalizer {
			cmaddon.Finalizers = append(cmaddon.Finalizers, cmaddonFinalizer)
			_, err := c.addonClient.AddonV1alpha1().ClusterManagementAddOns().Update(ctx, cmaddon, metav1.UpdateOptions{})
			return err
		}
	}

	// Spoke cluster is deleting, we remove its related resources
	if !cmaddon.DeletionTimestamp.IsZero() {
		if c.sycnerAddonMap[cmaddonName] != nil {
			c.sycnerAddonMap[cmaddonName]()
			delete(c.sycnerAddonMap, cmaddonName)
		}
		return c.removeFinalizer(ctx, cmaddon)
	}

	// start addonmanager
	if c.sycnerAddonMap[cmaddon.Name] != nil {
		return nil
	}

	mgr, err := addonmanager.New(c.hubKubconfig)
	if err != nil {
		return err
	}

	agent := synceraddons.NewSyncerAddon(cmaddonName, baseURL, c.ca, c.key, c.kcpRestConfig, c.eventRecorder)
	mgr.AddAgent(agent)
	addonCtx, cancel := context.WithCancel(ctx)
	mgr.Start(addonCtx)
	c.sycnerAddonMap[cmaddonName] = cancel

	c.eventRecorder.Eventf("AddOnManagerStatred", "Start one addon manager for workspace %s", workspaceName)
	return nil
}

func (c *clusterManagementAddonController) removeFinalizer(ctx context.Context, addon *addonapiv1alpha1.ClusterManagementAddOn) error {
	copiedFinalizers := []string{}
	for i := range addon.Finalizers {
		if addon.Finalizers[i] == cmaddonFinalizer {
			continue
		}
		copiedFinalizers = append(copiedFinalizers, addon.Finalizers[i])
	}

	if len(addon.Finalizers) != len(copiedFinalizers) {
		addon.Finalizers = copiedFinalizers
		_, err := c.addonClient.AddonV1alpha1().ClusterManagementAddOns().Update(ctx, addon, metav1.UpdateOptions{})
		return err
	}

	return nil
}
