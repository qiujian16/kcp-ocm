package addonmanagement

import (
	"context"
	"strings"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/qiujian16/kcp-ocm/pkg/controllers/synceraddons"
	"github.com/qiujian16/kcp-ocm/pkg/helpers"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	"open-cluster-management.io/addon-framework/pkg/addonmanager"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
	addoninformerv1alpha1 "open-cluster-management.io/api/client/addon/informers/externalversions/addon/v1alpha1"
	addonlisterv1alpha1 "open-cluster-management.io/api/client/addon/listers/addon/v1alpha1"
)

// This controller has a controller loop that when a clustermanagementaddon with the name is
// "kcp-sycner-<organization workspace name>-<negotiation workspace name>" is created, the
// controller maintains an syncer-addon for each negotiation workspace in one organization
// workspace.
// This ensure that all syncers for this cluster will be spawned later.

const (
	cmaddonFinalizer = "addon.open-cluster-management.io/cleanup"
)

type clusterManagementAddonController struct {
	kcpDynamicClient             dynamic.Interface
	addonClient                  addonv1alpha1client.Interface
	clusterManagementAddonLister addonlisterv1alpha1.ClusterManagementAddOnLister
	sycnerAddonMap               map[string]context.CancelFunc
	ca                           []byte
	key                          []byte
	hubKubconfig                 *rest.Config
	kcpRootRestConfig            *rest.Config
	eventRecorder                events.Recorder
}

func NewClusterManagementAddonController(
	kcpDynamicClient dynamic.Interface,
	addonClient addonv1alpha1client.Interface,
	clusterManagementAddonInformer addoninformerv1alpha1.ClusterManagementAddOnInformer,
	hubKubconfig *rest.Config,
	kcpRootRestConfig *rest.Config,
	ca, key []byte,
	recorder events.Recorder,
) factory.Controller {
	c := &clusterManagementAddonController{
		kcpDynamicClient:             kcpDynamicClient,
		addonClient:                  addonClient,
		clusterManagementAddonLister: clusterManagementAddonInformer.Lister(),
		sycnerAddonMap:               map[string]context.CancelFunc{},
		hubKubconfig:                 hubKubconfig,
		ca:                           ca,
		key:                          key,
		kcpRootRestConfig:            kcpRootRestConfig,
		eventRecorder:                recorder.WithComponentSuffix("syncer-addon-controller"),
	}

	return factory.New().WithFilteredEventsInformersQueueKeyFunc(
		func(obj runtime.Object) string {
			accessor, _ := meta.Accessor(obj)
			return accessor.GetName()
		},
		func(obj interface{}) bool {
			accessor, _ := meta.Accessor(obj)
			return strings.HasPrefix(accessor.GetName(), "kcp-syncer-")
		},
		clusterManagementAddonInformer.Informer()).
		WithSync(c.sync).ToController("syncer-addon-controller", recorder)
}

func (c *clusterManagementAddonController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	cmaddonName := syncCtx.QueueKey()
	klog.V(4).Infof("Reconcil clustermanagementaddon %s", cmaddonName)

	cmaddon, err := c.clusterManagementAddonLister.Get(cmaddonName)
	switch {
	case errors.IsNotFound(err):
		return nil
	case err != nil:
		return err
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

	// ClusterManagementAddOn is deleting, we remove its related resources
	if !cmaddon.DeletionTimestamp.IsZero() {
		if c.sycnerAddonMap[cmaddonName] != nil {
			c.sycnerAddonMap[cmaddonName]()
			delete(c.sycnerAddonMap, cmaddonName)
		}
		return c.removeFinalizer(ctx, cmaddon)
	}

	// ensure the mapped workspace exists
	workspaceRestConfig, err := c.getWorkspaceRestConfig(ctx, cmaddon)
	if err != nil {
		return err
	}
	if workspaceRestConfig == nil {
		return nil
	}

	// start addonmanager
	if c.sycnerAddonMap[cmaddon.Name] != nil {
		return nil
	}

	mgr, err := addonmanager.New(c.hubKubconfig)
	if err != nil {
		return err
	}

	agent := synceraddons.NewSyncerAddon(cmaddonName, c.ca, c.key, workspaceRestConfig, c.eventRecorder)
	mgr.AddAgent(agent)
	addonCtx, cancel := context.WithCancel(ctx)
	mgr.Start(addonCtx)
	c.sycnerAddonMap[cmaddonName] = cancel

	c.eventRecorder.Eventf("AddOnManagerStatred", "Start one addon manager for workspace %s", workspaceRestConfig.Host)
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

func (c *clusterManagementAddonController) getWorkspaceRestConfig(ctx context.Context, cmaddon *addonapiv1alpha1.ClusterManagementAddOn) (*rest.Config, error) {
	orgWorkspaceName, ok := cmaddon.Labels["organization-workspace"]
	if !ok {
		return nil, nil
	}

	negotiationWorkspaceName, ok := cmaddon.Labels["negotiation-workspace"]
	if !ok {
		return nil, nil
	}

	orgWorkspace, err := c.kcpDynamicClient.Resource(helpers.ClusterWorkspaceGVR).Get(ctx, orgWorkspaceName, metav1.GetOptions{})
	switch {
	case errors.IsNotFound(err):
		return nil, nil
	case err != nil:
		return nil, err
	}

	orgWorkspaceRestConfig := rest.CopyConfig(c.kcpRootRestConfig)
	orgWorkspaceRestConfig.Host = helpers.GetWorkspaceURL(orgWorkspace)
	orgWorkspaceDynamicClient, err := dynamic.NewForConfig(orgWorkspaceRestConfig)
	if err != nil {
		return nil, err
	}
	negotiationWorkspace, err := orgWorkspaceDynamicClient.Resource(helpers.ClusterWorkspaceGVR).Get(ctx, negotiationWorkspaceName, metav1.GetOptions{})
	switch {
	case errors.IsNotFound(err):
		return nil, nil
	case err != nil:
		return nil, err
	}

	negotiationWorkspaceRestConfig := rest.CopyConfig(c.kcpRootRestConfig)
	negotiationWorkspaceRestConfig.Host = helpers.GetWorkspaceURL(negotiationWorkspace)
	return negotiationWorkspaceRestConfig, nil
}
