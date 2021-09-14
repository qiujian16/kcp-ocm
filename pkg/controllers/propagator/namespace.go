package propagator

import (
	"context"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/qiujian16/kcp-ocm/pkg/helpers"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	coreinformer "k8s.io/client-go/informers/core/v1"
	corelister "k8s.io/client-go/listers/core/v1"
	"k8s.io/klog/v2"
	clusterinformerv1alpha1 "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1alpha1"
	clusterlisterv1alpha1 "open-cluster-management.io/api/client/cluster/listers/cluster/v1alpha1"
	workv1client "open-cluster-management.io/api/client/work/clientset/versioned/typed/work/v1"
	workinformer "open-cluster-management.io/api/client/work/informers/externalversions/work/v1"
	worklister "open-cluster-management.io/api/client/work/listers/work/v1"
	workapiv1 "open-cluster-management.io/api/work/v1"
)

const (
	defaultPlacement = "default"
	workName         = "namespace-syncer"
	placementLabel   = "cluster.open-cluster-management.io/placement"
)

type namespacePropagator struct {
	decisionLister     clusterlisterv1alpha1.PlacementDecisionLister
	workLister         worklister.ManifestWorkLister
	manifestWorkClient workv1client.WorkV1Interface
	kcpNamespaceLister corelister.NamespaceLister
	placementLister    clusterlisterv1alpha1.PlacementLister
	workingNamespace   string
}

func NewNamespacePropagator(
	manifestWorkClient workv1client.WorkV1Interface,
	namespace string,
	kcpNamespaceInformer coreinformer.NamespaceInformer,
	workInformer workinformer.ManifestWorkInformer,
	placementInformer clusterinformerv1alpha1.PlacementInformer,
	placementDecisionInformer clusterinformerv1alpha1.PlacementDecisionInformer,
	recorder events.Recorder,
) factory.Controller {
	c := &namespacePropagator{
		workingNamespace:   namespace,
		workLister:         workInformer.Lister(),
		kcpNamespaceLister: kcpNamespaceInformer.Lister(),
		decisionLister:     placementDecisionInformer.Lister(),
		placementLister:    placementInformer.Lister(),
		manifestWorkClient: manifestWorkClient,
	}
	return factory.New().
		WithInformers(kcpNamespaceInformer.Informer()).
		WithFilteredEventsInformers(
			c.decisionFilter, placementDecisionInformer.Informer()).
		WithSync(c.sync).ToController("namespace-propagator", recorder)
}

func (d *namespacePropagator) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	klog.Infof("namespace-propagator %s sync %s", d.workingNamespace)

	namespaces, err := d.kcpNamespaceLister.List(labels.Everything())
	if err != nil {
		return err
	}

	manifestWork := &workapiv1.ManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Name: workName,
		},
		Spec: workapiv1.ManifestWorkSpec{
			Workload: workapiv1.ManifestsTemplate{
				Manifests: []workapiv1.Manifest{},
			},
		},
	}

	for _, namespace := range namespaces {
		toDeploy := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespace.Name,
			},
			TypeMeta: metav1.TypeMeta{
				APIVersion: corev1.SchemeGroupVersion.String(),
				Kind:       "Namespace",
			},
		}
		manifestWork.Spec.Workload.Manifests = append(manifestWork.Spec.Workload.Manifests, workapiv1.Manifest{
			RawExtension: runtime.RawExtension{Object: toDeploy},
		})
	}

	decisions, err := helpers.GetDecisionsByPlacement(d.decisionLister, defaultPlacement, d.workingNamespace)
	if err != nil {
		return err
	}

	errs := []error{}
	for _, dec := range decisions {
		manifestWorkCopy := manifestWork.DeepCopy()
		manifestWorkCopy.Namespace = dec.ClusterName
		err := helpers.ApplyWork(ctx, d.manifestWorkClient, manifestWorkCopy)
		if err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return utilerrors.NewAggregate(errs)
	}

	return nil
}

func (d *namespacePropagator) decisionFilter(object interface{}) bool {
	placement := helpers.GetPlacementByDecision(d.placementLister, object)

	if placement.Name == defaultPlacement {
		return true
	}

	return false
}
