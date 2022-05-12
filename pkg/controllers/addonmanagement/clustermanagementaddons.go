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
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	"open-cluster-management.io/addon-framework/pkg/addonmanager"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
	addoninformerv1alpha1 "open-cluster-management.io/api/client/addon/informers/externalversions/addon/v1alpha1"
	addonlisterv1alpha1 "open-cluster-management.io/api/client/addon/listers/addon/v1alpha1"
)

// This controller has a controller loop on clustermanagementaddon.
// If a clustermanagementaddon with the name "kcp-sycner-<workspace id>" is created, the
// controller maintains a kcp-syncer addon manger for each workspace.

const (
	cmaddonFinalizer = "addon.open-cluster-management.io/cleanup"
)

type clusterManagementAddonController struct {
	hubKubconfig                 *rest.Config
	kcpRootRestConfig            *rest.Config
	addonClient                  addonv1alpha1client.Interface
	clusterManagementAddonLister addonlisterv1alpha1.ClusterManagementAddOnLister
	ca                           []byte
	key                          []byte
	sycnerAddonMap               map[string]context.CancelFunc
	eventRecorder                events.Recorder
}

func NewClusterManagementAddonController(
	hubKubconfig, kcpRootRestConfig *rest.Config,
	addonClient addonv1alpha1client.Interface,
	clusterManagementAddonInformer addoninformerv1alpha1.ClusterManagementAddOnInformer,
	ca, key []byte,
	recorder events.Recorder) factory.Controller {
	c := &clusterManagementAddonController{
		hubKubconfig:                 hubKubconfig,
		kcpRootRestConfig:            kcpRootRestConfig,
		addonClient:                  addonClient,
		clusterManagementAddonLister: clusterManagementAddonInformer.Lister(),
		ca:                           ca,
		key:                          key,
		sycnerAddonMap:               map[string]context.CancelFunc{},
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

	// get the workspace rest config
	workspaceConfig := c.getWorkspaceConfig(ctx, cmaddon)
	if workspaceConfig == nil {
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

	if err := mgr.AddAgent(synceraddons.NewSyncerAddon(cmaddonName, c.ca, c.key, workspaceConfig, c.eventRecorder)); err != nil {
		return err
	}

	addonCtx, cancel := context.WithCancel(ctx)
	if err := mgr.Start(addonCtx); err != nil {
		cancel()
		return err
	}
	c.sycnerAddonMap[cmaddonName] = cancel

	c.eventRecorder.Eventf("AddOnManagerStatred", "Start one addon manager for workspace %s", workspaceConfig.Host)
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

func (c *clusterManagementAddonController) getWorkspaceConfig(ctx context.Context, cmaddon *addonapiv1alpha1.ClusterManagementAddOn) *rest.Config {
	workspaceId, ok := cmaddon.Annotations["kcp-workspace"]
	if !ok {
		return nil
	}

	workspaceConfig := rest.CopyConfig(c.kcpRootRestConfig)
	workspaceConfig.Host = fmt.Sprintf("%s:%s", workspaceConfig.Host, workspaceId)
	return workspaceConfig
}
