package splitter

import (
	"context"
	"fmt"
	"strings"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/selection"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	appsinformer "k8s.io/client-go/informers/apps/v1"
	"k8s.io/client-go/kubernetes"
	appslister "k8s.io/client-go/listers/apps/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	clusterclient "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterinformerv1alpha1 "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1alpha1"
	clusterlisterv1alpha1 "open-cluster-management.io/api/client/cluster/listers/cluster/v1alpha1"
	workv1client "open-cluster-management.io/api/client/work/clientset/versioned/typed/work/v1"
	workinformer "open-cluster-management.io/api/client/work/informers/externalversions/work/v1"
	worklister "open-cluster-management.io/api/client/work/listers/work/v1"
	clusterapiv1alpha1 "open-cluster-management.io/api/cluster/v1alpha1"
	workapiv1 "open-cluster-management.io/api/work/v1"
)

const (
	splitLabel       = "kcp.open-cluster-management.io/splitter"
	placementLabel   = "cluster.open-cluster-management.io/placement"
	defaultPlacement = "default"
)

type DeploymentSplitter struct {
	clusterClient       clusterclient.Interface
	kcpKubeClient       kubernetes.Interface
	manifestWorkClient  workv1client.WorkV1Interface
	kcpDeploymentLister appslister.DeploymentLister
	placementLister     clusterlisterv1alpha1.PlacementLister
	decisionLister      clusterlisterv1alpha1.PlacementDecisionLister
	workLister          worklister.ManifestWorkLister
	workingNamespace    string
}

func NewDeploymentSplitter(
	clusterClient clusterclient.Interface,
	kcpKubeClient kubernetes.Interface,
	manifestWorkClient workv1client.WorkV1Interface,
	namespace string,
	kcpDeploymentInformer appsinformer.DeploymentInformer,
	placementInformer clusterinformerv1alpha1.PlacementInformer,
	placementDecisionInformer clusterinformerv1alpha1.PlacementDecisionInformer,
	workInformer workinformer.ManifestWorkInformer,
	recorder events.Recorder,
) factory.Controller {
	controller := &DeploymentSplitter{
		clusterClient:       clusterClient,
		kcpKubeClient:       kcpKubeClient,
		manifestWorkClient:  manifestWorkClient,
		workingNamespace:    namespace,
		kcpDeploymentLister: kcpDeploymentInformer.Lister(),
		placementLister:     placementInformer.Lister(),
		decisionLister:      placementDecisionInformer.Lister(),
		workLister:          workInformer.Lister(),
	}

	return factory.New().
		WithInformersQueueKeyFunc(func(obj runtime.Object) string {
			key, _ := cache.MetaNamespaceKeyFunc(obj)
			return key
		}, kcpDeploymentInformer.Informer()).
		WithFilteredEventsInformersQueueKeyFunc(func(obj runtime.Object) string {
			accessor, _ := meta.Accessor(obj)
			key, _ := splitDeploymentKey(accessor.GetName())
			return key
		}, func(obj interface{}) bool {
			accessor, _ := meta.Accessor(obj)
			_, valid := splitDeploymentKey(accessor.GetName())
			return valid
		}, workInformer.Informer(), placementInformer.Informer()).
		WithFilteredEventsInformersQueueKeyFunc(
			controller.decisionQueueKey,
			controller.decisionFilter, placementDecisionInformer.Informer()).
		WithSync(controller.sync).ToController("Deployment-Splitter", recorder)
}

func (d *DeploymentSplitter) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	key := syncCtx.QueueKey()
	klog.Infof("Deployment-Splitter %s sync %s", d.workingNamespace, key)

	namespace, name, err := cache.SplitMetaNamespaceKey(key)

	if err != nil {
		return err
	}

	deployment, err := d.kcpDeploymentLister.Deployments(namespace).Get(name)
	//TODO: handle deletion
	if err != nil {
		return err
	}

	placementName := fmt.Sprintf("deployment-%s-%s", namespace, name)

	placement, err := d.placementLister.Placements(d.workingNamespace).Get(placementName)

	switch {
	case errors.IsNotFound(err):
		placement = &clusterapiv1alpha1.Placement{
			ObjectMeta: metav1.ObjectMeta{
				Name:      placementName,
				Namespace: d.workingNamespace,
			},
			Spec: clusterapiv1alpha1.PlacementSpec{},
		}

		_, err = d.clusterClient.ClusterV1alpha1().Placements(d.workingNamespace).Create(ctx, placement, metav1.CreateOptions{})
		return err
	case err != nil:
		return err
	}

	requirement, err := labels.NewRequirement(placementLabel, selection.Equals, []string{placement.Name})

	if err != nil {
		return err
	}

	labelSelector := labels.NewSelector().Add(*requirement)

	placementDecisions, err := d.decisionLister.PlacementDecisions(d.workingNamespace).List(labelSelector)
	if err != nil {
		return err
	}

	decisions := []clusterapiv1alpha1.ClusterDecision{}

	for _, dec := range placementDecisions {
		decisions = append(decisions, dec.Status.Decisions...)
	}

	err = d.generateDeploymentSplitter(ctx, deployment, decisions)

	return err
}

func (d *DeploymentSplitter) generateDeploymentSplitter(
	ctx context.Context, deployment *appsv1.Deployment, decisions []clusterapiv1alpha1.ClusterDecision) error {

	if deployment.Spec.Replicas == nil {
		return nil
	}

	numberToDeploy := int(*deployment.Spec.Replicas)

	if len(decisions) < numberToDeploy {
		numberToDeploy = len(decisions)
	}

	if numberToDeploy == 0 {
		return nil
	}

	totalReplica := *deployment.Spec.Replicas

	workName := fmt.Sprintf("deployment-%s-%s", deployment.Namespace, deployment.Name)

	errorArray := []error{}

	deployedClusters := sets.NewString()

	for numberToDeploy > 0 {
		cluster := decisions[numberToDeploy-1].ClusterName

		// Record the  desired cluster to deploy
		deployedClusters.Insert(cluster)

		// Calculate the current replica for this cluster
		replica := totalReplica / int32(numberToDeploy)

		numberToDeploy = numberToDeploy - 1
		totalReplica = totalReplica - replica

		// Build the deployment with the resplica
		toBeDeployed := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:        deployment.Name,
				Namespace:   deployment.Namespace,
				Labels:      deployment.Labels,
				Annotations: deployment.Annotations,
			},
			TypeMeta: metav1.TypeMeta{
				APIVersion: appsv1.SchemeGroupVersion.String(),
				Kind:       "Deployment",
			},
			Spec: deployment.Spec,
		}

		toBeDeployed.Spec.Replicas = &replica

		work := &workapiv1.ManifestWork{
			ObjectMeta: metav1.ObjectMeta{
				Name:      workName,
				Namespace: cluster,
				Labels: map[string]string{
					splitLabel: workName,
				},
			},
			Spec: workapiv1.ManifestWorkSpec{
				Workload: workapiv1.ManifestsTemplate{
					Manifests: []workapiv1.Manifest{
						{
							RawExtension: runtime.RawExtension{Object: toBeDeployed},
						},
					},
				},
			},
		}

		if err := d.applyWork(ctx, work); err != nil {
			errorArray = append(errorArray, err)
			continue
		}
	}

	if len(errorArray) != 0 {
		return utilerrors.NewAggregate(errorArray)
	}

	if err := d.cleanWork(ctx, workName, deployedClusters); err != nil {
		return err
	}

	return nil
}

func (d *DeploymentSplitter) cleanWork(ctx context.Context, workName string, deployedCluster sets.String) error {
	requirement, err := labels.NewRequirement(splitLabel, selection.Equals, []string{workName})

	if err != nil {
		return err
	}

	labelSelector := labels.NewSelector().Add(*requirement)

	works, err := d.workLister.List(labelSelector)

	if err != nil {
		return err
	}

	errorArray := []error{}

	for _, work := range works {
		if deployedCluster.Has(work.Namespace) {
			continue
		}

		err := d.manifestWorkClient.ManifestWorks(work.Namespace).Delete(ctx, workName, metav1.DeleteOptions{})

		if err != nil {
			errorArray = append(errorArray, err)
		}
	}

	if len(errorArray) != 0 {
		return utilerrors.NewAggregate(errorArray)
	}

	return nil
}

func (d *DeploymentSplitter) applyWork(ctx context.Context, work *workapiv1.ManifestWork) error {
	existing, err := d.workLister.ManifestWorks(work.Namespace).Get(work.Name)

	switch {
	case errors.IsNotFound(err):
		_, err = d.manifestWorkClient.ManifestWorks(work.Namespace).Create(ctx, work, metav1.CreateOptions{})
		return err
	case err != nil:
		return err
	}

	if manifestsEqual(work.Spec.Workload.Manifests, existing.Spec.Workload.Manifests) {
		return nil
	}

	existing.Spec.Workload.Manifests = work.Spec.Workload.Manifests
	_, err = d.manifestWorkClient.ManifestWorks(work.Namespace).Update(ctx, existing, metav1.UpdateOptions{})

	return err
}

func (d *DeploymentSplitter) getDecisionByPlacement(object interface{}) *clusterapiv1alpha1.Placement {
	accessor, _ := meta.Accessor(object)

	if accessor.GetLabels() == nil {
		return nil
	}

	placementKey, ok := accessor.GetLabels()[placementLabel]
	if !ok {
		return nil
	}

	namespace, name, _ := cache.SplitMetaNamespaceKey(placementKey)

	placement, err := d.placementLister.Placements(namespace).Get(name)

	if err != nil {
		return nil
	}

	return placement
}

func (d *DeploymentSplitter) decisionFilter(object interface{}) bool {
	placement := d.getDecisionByPlacement(object)

	if placement == nil {
		return false
	}

	_, valid := splitDeploymentKey(placement.Name)
	return valid
}

func (d *DeploymentSplitter) decisionQueueKey(object runtime.Object) string {
	placement := d.getDecisionByPlacement(object)

	if placement == nil {
		return ""
	}

	key, _ := splitDeploymentKey(placement.Name)
	return key
}

func manifestsEqual(new, old []workapiv1.Manifest) bool {
	if len(new) != len(old) {
		return false
	}

	for i := range new {
		if !equality.Semantic.DeepEqual(new[i].Raw, old[i].Raw) {
			return false
		}
	}
	return true
}

func splitDeploymentKey(key string) (string, bool) {
	if !strings.HasPrefix("deployment-", key) {
		return "", false
	}

	keyArray := strings.SplitAfter(strings.TrimPrefix("deployment-", key), "-")

	if len(keyArray) < 2 {
		return "", false
	}

	return fmt.Sprintf("%s/%s", keyArray[0], strings.Join(keyArray[1:], "")), true
}
