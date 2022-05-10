package workspace

import (
	"context"
	"fmt"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/qiujian16/kcp-ocm/pkg/helpers"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
)

type negotiationWorkspaceController struct {
	kcpCAEnabled      bool
	orgWorkspaceName  string
	kcpRootRestConfig *rest.Config
	hubKubeClient     kubernetes.Interface
	hubAddOnClient    addonv1alpha1client.Interface
	workspaceLister   cache.GenericLister
}

func NewNegotiationWorkspaceController(
	controllerName string,
	orgWorkspaceName string,
	kcpCAEnabled bool,
	kcpRestConfig *rest.Config,
	hubKubeClient kubernetes.Interface,
	addonClient addonv1alpha1client.Interface,
	workspaceInformer informers.GenericInformer,
	recorder events.Recorder) factory.Controller {
	nw := &negotiationWorkspaceController{
		kcpCAEnabled:      kcpCAEnabled,
		orgWorkspaceName:  orgWorkspaceName,
		kcpRootRestConfig: kcpRestConfig,
		hubKubeClient:     hubKubeClient,
		hubAddOnClient:    addonClient,
		workspaceLister:   workspaceInformer.Lister(),
	}

	return factory.New().
		WithInformersQueueKeyFunc(
			func(obj runtime.Object) string {
				accessor, _ := meta.Accessor(obj)
				return accessor.GetName()
			},
			workspaceInformer.Informer(),
		).
		WithSync(nw.sync).
		ToController(controllerName, recorder.WithComponentSuffix(controllerName))
}

func (nw *negotiationWorkspaceController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	workspaceName := syncCtx.QueueKey()
	klog.Infof("Reconcil negotiation workspace %s", workspaceName)

	workspace, err := nw.workspaceLister.Get(workspaceName)
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}

	workspaceType := helpers.GetWorkspaceType(workspace)
	if workspaceType != "Universal" {
		klog.Warningf("workspace %s type (%s) is not Univeral, ignored", workspaceType, workspaceName)
		return nil
	}

	//TODO add finalizer on workspace to handle workspace deletation

	if helpers.GetWorkspacePhase(workspace) != "Ready" {
		return nil
	}

	// if ca is not enabled, we create a service account for syncer
	if !nw.kcpCAEnabled {
		if err := nw.createWorkspaceServiceAccount(ctx, helpers.GetWorkspaceURL(workspace), workspaceName); err != nil {
			return err
		}
		syncCtx.Recorder().Eventf("WorkspaceServiceAccountCreated", "The workspace %s service account kcp-ocm/%s-sa is created", workspaceName, workspaceName)
	}

	// create a namespace for this workspace on the hub
	workspaceNamespaceName := fmt.Sprintf("kcp-%s-%s", nw.orgWorkspaceName, workspaceName)
	if err := nw.createWorkspaceNamespace(ctx, workspaceNamespaceName); err != nil {
		return err
	}
	syncCtx.Recorder().Eventf("WorkspaceNamespaceCreated", "The namespace %s is created on the hub", workspaceNamespaceName)

	// create a ClusterManagementAddOn for this workspace on the hub
	if err := nw.createClusterManagementAddOn(ctx, workspaceName); err != nil {
		return err
	}
	syncCtx.Recorder().Eventf("ClusterManagementAddOnCreated", "The ClusterManagementAddOn kcp-syncer-%s-%s is created on the hub", nw.orgWorkspaceName, workspaceName)
	return nil
}

func (nw *negotiationWorkspaceController) createWorkspaceServiceAccount(ctx context.Context, workspaceURL, workspaceName string) error {
	workspaceConfig := rest.CopyConfig(nw.kcpRootRestConfig)
	workspaceConfig.Host = workspaceURL

	workspaceKubeClient, err := kubernetes.NewForConfig(workspaceConfig)
	if err != nil {
		return err
	}
	_, err = workspaceKubeClient.CoreV1().Namespaces().Get(ctx, "kcp-ocm", metav1.GetOptions{})
	if errors.IsNotFound(err) {
		if _, err := workspaceKubeClient.CoreV1().Namespaces().Create(
			ctx,
			&corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "kcp-ocm",
				},
			},
			metav1.CreateOptions{},
		); err != nil {
			return err
		}
	}
	if err != nil {
		return err
	}

	saName := fmt.Sprintf("kcp-syncer-%s-%s-sa", nw.orgWorkspaceName, workspaceName)
	_, err = workspaceKubeClient.CoreV1().ServiceAccounts("kcp-ocm").Get(ctx, saName, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		_, err := workspaceKubeClient.CoreV1().ServiceAccounts("kcp-ocm").Create(
			ctx,
			&corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name: saName,
				},
			},
			metav1.CreateOptions{},
		)
		return err
	}
	return err
}

func (nw *negotiationWorkspaceController) createWorkspaceNamespace(ctx context.Context, workspaceNamespaceName string) error {
	_, err := nw.hubKubeClient.CoreV1().Namespaces().Get(ctx, workspaceNamespaceName, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		_, err := nw.hubKubeClient.CoreV1().Namespaces().Create(
			ctx,
			&corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: workspaceNamespaceName,
				},
			},
			metav1.CreateOptions{},
		)
		return err

	}
	return err
}

func (nw *negotiationWorkspaceController) createClusterManagementAddOn(ctx context.Context, workspaceName string) error {
	clusterManagementAddOnName := fmt.Sprintf("kcp-syncer-%s-%s", nw.orgWorkspaceName, workspaceName)
	_, err := nw.hubAddOnClient.AddonV1alpha1().ClusterManagementAddOns().Get(ctx, clusterManagementAddOnName, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		_, err := nw.hubAddOnClient.AddonV1alpha1().ClusterManagementAddOns().Create(
			ctx,
			&addonapiv1alpha1.ClusterManagementAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterManagementAddOnName,
					Labels: map[string]string{
						"organization-workspace": nw.orgWorkspaceName,
						"negotiation-workspace":  workspaceName,
					},
				},
				Spec: addonapiv1alpha1.ClusterManagementAddOnSpec{
					AddOnMeta: addonapiv1alpha1.AddOnMeta{
						DisplayName: clusterManagementAddOnName,
					},
				},
			},
			metav1.CreateOptions{},
		)

		return err
	}

	return err
}
