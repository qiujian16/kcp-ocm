package helpers

import (
	"context"

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	clusterlisterv1alpha1 "open-cluster-management.io/api/client/cluster/listers/cluster/v1alpha1"
	workv1client "open-cluster-management.io/api/client/work/clientset/versioned/typed/work/v1"
	clusterapiv1alpha1 "open-cluster-management.io/api/cluster/v1alpha1"
	workapiv1 "open-cluster-management.io/api/work/v1"
)

const placementLabel = "cluster.open-cluster-management.io/placement"

func ApplyWork(ctx context.Context, manifestWorkClient workv1client.WorkV1Interface, work *workapiv1.ManifestWork) error {
	existing, err := manifestWorkClient.ManifestWorks(work.Namespace).Get(ctx, work.Name, metav1.GetOptions{})

	switch {
	case errors.IsNotFound(err):
		_, err = manifestWorkClient.ManifestWorks(work.Namespace).Create(ctx, work, metav1.CreateOptions{})
		return err
	case err != nil:
		return err
	}

	if manifestsEqual(work.Spec.Workload.Manifests, existing.Spec.Workload.Manifests) {
		return nil
	}

	existing.Spec.Workload.Manifests = work.Spec.Workload.Manifests
	_, err = manifestWorkClient.ManifestWorks(work.Namespace).Update(ctx, existing, metav1.UpdateOptions{})

	return err
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

func GetDecisionsByPlacement(decisionLister clusterlisterv1alpha1.PlacementDecisionLister, placementName, namespace string) ([]clusterapiv1alpha1.ClusterDecision, error) {
	decisions := []clusterapiv1alpha1.ClusterDecision{}
	requirement, err := labels.NewRequirement(placementLabel, selection.Equals, []string{placementName})

	if err != nil {
		return decisions, err
	}

	labelSelector := labels.NewSelector().Add(*requirement)

	placementDecisions, err := decisionLister.PlacementDecisions(namespace).List(labelSelector)
	if err != nil {
		return decisions, err
	}

	for _, dec := range placementDecisions {
		decisions = append(decisions, dec.Status.Decisions...)
	}
	return decisions, nil
}

func GetPlacementByDecision(placementLister clusterlisterv1alpha1.PlacementLister, object interface{}) *clusterapiv1alpha1.Placement {
	accessor, _ := meta.Accessor(object)

	if accessor.GetLabels() == nil {
		return nil
	}

	placementName, ok := accessor.GetLabels()[placementLabel]
	if !ok {
		return nil
	}

	placement, err := placementLister.Placements(accessor.GetNamespace()).Get(placementName)

	if err != nil {
		return nil
	}

	return placement
}
