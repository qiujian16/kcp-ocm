package workspace

import (
	"context"
	"fmt"
	"time"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/qiujian16/kcp-ocm/pkg/helpers"

	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
)

type organizationWorkspaceController struct {
	kcpCAEnabled      bool
	kcpRootRestConfig *rest.Config
	kubeClient        kubernetes.Interface
	addonClient       addonv1alpha1client.Interface
	workspaceLister   cache.GenericLister
	workspaces        map[string]context.CancelFunc
	eventRecorder     events.Recorder
}

func NewOrganizationWorkspaceController(
	kcpCAEnabled bool,
	kcpRestConfig *rest.Config,
	kubeClient kubernetes.Interface,
	addonClient addonv1alpha1client.Interface,
	workspaceInformer informers.GenericInformer,
	recorder events.Recorder) factory.Controller {
	w := &organizationWorkspaceController{
		kcpCAEnabled:      kcpCAEnabled,
		kcpRootRestConfig: kcpRestConfig,
		kubeClient:        kubeClient,
		addonClient:       addonClient,
		workspaceLister:   workspaceInformer.Lister(),
		workspaces:        map[string]context.CancelFunc{},
		eventRecorder:     recorder,
	}

	return factory.New().
		WithInformersQueueKeyFunc(
			func(obj runtime.Object) string {
				accessor, _ := meta.Accessor(obj)
				return accessor.GetName()
			},
			workspaceInformer.Informer(),
		).
		WithSync(w.sync).
		ToController("syncer-orgworkspaces-controller", recorder.WithComponentSuffix("syncer-orgworkspaces-controller"))
}

func (ow *organizationWorkspaceController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	workspaceName := syncCtx.QueueKey()
	klog.V(4).Infof("Reconcil workspace %s", workspaceName)

	workspace, err := ow.workspaceLister.Get(workspaceName)
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}

	// only handle organization workspace
	if helpers.GetWorkspaceType(workspace) != "Organization" {
		return nil
	}

	//TODO add finalizer on workspace to handle workspace deletation

	if helpers.GetWorkspacePhase(workspace) != "Ready" {
		return nil
	}

	// start a controller for this workspace
	return ow.startNegotiationWorkspaceController(ctx, workspaceName, helpers.GetWorkspaceURL(workspace))
}

func (ow *organizationWorkspaceController) startNegotiationWorkspaceController(ctx context.Context, workspaceName, workspaceURL string) error {
	if ow.workspaces[workspaceName] != nil {
		return nil
	}

	workspaceConfig := rest.CopyConfig(ow.kcpRootRestConfig)
	workspaceConfig.Host = workspaceURL
	workspaceDynamicClient, err := dynamic.NewForConfig(workspaceConfig)
	if err != nil {
		return err
	}

	workspaceCtx, cancel := context.WithCancel(ctx)

	workspaceDynamicInformer := dynamicinformer.NewDynamicSharedInformerFactory(workspaceDynamicClient, 10*time.Minute)

	negotiationWorkspaceController := NewNegotiationWorkspaceController(
		fmt.Sprintf("syncer-workspace-%s-controller", workspaceName),
		workspaceName,
		ow.kcpCAEnabled,
		ow.kcpRootRestConfig,
		ow.kubeClient,
		ow.addonClient,
		workspaceDynamicInformer.ForResource(helpers.ClusterWorkspaceGVR),
		ow.eventRecorder,
	)

	go workspaceDynamicInformer.Start(workspaceCtx.Done())
	go negotiationWorkspaceController.Run(workspaceCtx, 1)

	ow.workspaces[workspaceName] = cancel

	return nil
}
