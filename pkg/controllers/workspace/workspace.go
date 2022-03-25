package workspace

import (
	"context"
	"embed"
	"fmt"

	"github.com/openshift/library-go/pkg/assets"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"

	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"

	corev1 "k8s.io/api/core/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
)

//go:embed manifests
var manifestFiles embed.FS

type workspaceController struct {
	kcpCAEnabled    bool
	kcpRestConfig   *rest.Config
	kcpKubeClient   kubernetes.Interface
	kubeClient      kubernetes.Interface
	addonClient     addonv1alpha1client.Interface
	workspaceLister cache.GenericLister
	eventRecorder   events.Recorder
}

func NewWorkspaceController(
	kcpCAEnabled bool,
	kcpRestConfig *rest.Config,
	kcpKubeClient, kubeClient kubernetes.Interface,
	addonClient addonv1alpha1client.Interface,
	workspaceInformer informers.GenericInformer,
	recorder events.Recorder) factory.Controller {
	w := &workspaceController{
		kcpCAEnabled:    kcpCAEnabled,
		kcpRestConfig:   kcpRestConfig,
		kcpKubeClient:   kcpKubeClient,
		kubeClient:      kubeClient,
		addonClient:     addonClient,
		workspaceLister: workspaceInformer.Lister(),
		eventRecorder:   recorder.WithComponentSuffix("syncer-workspace-controller"),
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
		ToController("syncer-workspace-controller", recorder)
}

func (w *workspaceController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	workspaceName := syncCtx.QueueKey()
	klog.V(4).Infof("Reconcil workspace %s", workspaceName)

	workspace, err := w.workspaceLister.Get(workspaceName)
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}

	//TODO: add finalizer on workspace to handle workspace deletation

	unstructuredWorkspace, err := runtime.DefaultUnstructuredConverter.ToUnstructured(workspace)
	if err != nil {
		panic(err)
	}

	// TODO check phase status when kcp becomes stable

	baseURL, found, err := unstructured.NestedString(unstructuredWorkspace, "status", "baseURL")
	if err != nil {
		panic(err)
	}
	if !found {
		return nil
	}

	// apply required crds
	if err := w.applyCRDsToWorkspace(ctx, baseURL); err != nil {
		return err
	}

	if !w.kcpCAEnabled {
		if err := w.createWorkspaceSA(ctx, workspaceName); err != nil {
			return err
		}
		syncCtx.Recorder().Eventf("WorkspaceServiceAccountCreated", "The service account is created for workspace %s", workspaceName)
	}

	// TODO for now, kcp cannot support rbac in a workspace, so we create a
	// clusterrole for workspaces in the kcp as a temporary way
	if err := w.applyWorkspaceClusterrole(ctx, workspaceName); err != nil {
		return err
	}

	// create a namespace for this workspace on the hub
	workspaceNamespaceName := fmt.Sprintf("kcp-%s", workspaceName)
	_, err = w.kubeClient.CoreV1().Namespaces().Get(ctx, workspaceNamespaceName, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		if _, err := w.kubeClient.CoreV1().Namespaces().Create(
			ctx,
			&corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: workspaceNamespaceName,
				},
			},
			metav1.CreateOptions{},
		); err != nil {
			return err
		}

		syncCtx.Recorder().Eventf("WorkspaceNamespaceCreated", "The namespace %s is created", workspaceNamespaceName)
	}
	if err != nil {
		return err
	}

	// create a ClusterManagementAddOn for this workspace on the hub
	clusterManagementAddOnName := fmt.Sprintf("kcp-syncer-%s", workspaceName)
	_, err = w.addonClient.AddonV1alpha1().ClusterManagementAddOns().Get(ctx, clusterManagementAddOnName, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		if _, err := w.addonClient.AddonV1alpha1().ClusterManagementAddOns().Create(
			ctx,
			&addonapiv1alpha1.ClusterManagementAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterManagementAddOnName,
				},
				Spec: addonapiv1alpha1.ClusterManagementAddOnSpec{
					AddOnMeta: addonapiv1alpha1.AddOnMeta{
						DisplayName: clusterManagementAddOnName,
					},
				},
			},
			metav1.CreateOptions{},
		); err != nil {
			return err
		}

		syncCtx.Recorder().Eventf("ClusterManagementAddOnCreated", "The ClusterManagementAddOn %s is created", clusterManagementAddOnName)
		return nil
	}
	if err != nil {
		return err
	}

	return nil
}

func (w *workspaceController) createWorkspaceSA(ctx context.Context, workspaceName string) error {
	_, err := w.kcpKubeClient.CoreV1().Namespaces().Get(ctx, "kcp-ocm", metav1.GetOptions{})
	if errors.IsNotFound(err) {
		if _, err := w.kcpKubeClient.CoreV1().Namespaces().Create(
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

	saName := fmt.Sprintf("%s-sa", workspaceName)
	_, err = w.kcpKubeClient.CoreV1().ServiceAccounts("kcp-ocm").Get(ctx, saName, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		if _, err := w.kcpKubeClient.CoreV1().ServiceAccounts("kcp-ocm").Create(
			ctx,
			&corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name: saName,
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

	return nil
}

func (w *workspaceController) applyWorkspaceClusterrole(ctx context.Context, workspaceName string) error {
	results := resourceapply.ApplyDirectly(
		ctx,
		resourceapply.NewKubeClientHolder(w.kcpKubeClient),
		w.eventRecorder,
		func(name string) ([]byte, error) {
			config := struct {
				Name      string
				Workspace string
			}{
				Name:      fmt.Sprintf("kcp-syncer-%s", workspaceName),
				Workspace: workspaceName,
			}

			file, err := manifestFiles.ReadFile(name)
			if err != nil {
				return nil, err
			}
			return assets.MustCreateAssetFromTemplate(name, file, config).Data, nil
		},
		"manifests/clusterrole.yaml",
	)

	for _, result := range results {
		if result.Error != nil {
			return result.Error
		}
	}

	return nil
}

func (w *workspaceController) applyCRDsToWorkspace(ctx context.Context, baseURL string) error {
	kconfig := rest.CopyConfig(w.kcpRestConfig)
	kconfig.Host = baseURL

	kubeclient, err := kubernetes.NewForConfig(kconfig)
	if err != nil {
		return err
	}

	apiExtensionClient, err := apiextensionsclient.NewForConfig(kconfig)
	if err != nil {
		return err
	}

	results := resourceapply.ApplyDirectly(
		ctx,
		resourceapply.NewKubeClientHolder(kubeclient).WithAPIExtensionsClient(apiExtensionClient),
		w.eventRecorder,
		func(name string) ([]byte, error) {
			file, err := manifestFiles.ReadFile(name)
			if err != nil {
				return nil, err
			}
			return assets.MustCreateAssetFromTemplate(name, file, nil).Data, nil
		},
		// This is the crd of the deployment, it is just to ensure that when syncer is deployed
		"manifests/apps_deployments.yaml",
	)

	for _, result := range results {
		if result.Error != nil {
			return result.Error
		}
	}

	return nil
}
