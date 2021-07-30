package controllers

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	fleetv1alpha1 "github.com/ensure-stack/fleet-operator/apis/fleet/v1alpha1"
)

type FleetReplicaSetReconciler struct {
	client.Client
	Log      logr.Logger
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

func (r *FleetReplicaSetReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(
			&fleetv1alpha1.FleetReplicaSet{},
			builder.WithPredicates(predicate.GenerationChangedPredicate{}),
		).
		Owns(&fleetv1alpha1.RemoteObject{}).
		Watches(&source.Kind{
			Type: &fleetv1alpha1.RemoteCluster{},
		}, handler.EnqueueRequestsFromMapFunc(
			r.requeueAllFleetReplicaSets,
		)).
		Complete(r)
}

// FleetReplicaSetReconciler/Controller entrypoint
func (r *FleetReplicaSetReconciler) Reconcile(
	ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	fleetReplicaSet := &fleetv1alpha1.FleetReplicaSet{}
	if err := r.Get(ctx, req.NamespacedName, fleetReplicaSet); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	remoteClusterSelector, err := metav1.LabelSelectorAsSelector(&fleetReplicaSet.Spec.RemoteClusterSelector)
	if err != nil {
		r.Recorder.Eventf(
			fleetReplicaSet, corev1.EventTypeWarning, "InvalidConfig",
			"invalid RemoteCluster selector: %w", err)
		return ctrl.Result{}, nil
	}

	remoteObjectSelector, err := metav1.LabelSelectorAsSelector(&fleetReplicaSet.Spec.Selector)
	if err != nil {
		r.Recorder.Eventf(
			fleetReplicaSet, corev1.EventTypeWarning, "InvalidConfig",
			"invalid selector: %w", err)
		return ctrl.Result{}, nil
	}

	remoteClusterList := &fleetv1alpha1.RemoteClusterList{}
	if err := r.List(ctx, remoteClusterList, client.MatchingLabelsSelector{
		Selector: remoteClusterSelector,
	}); err != nil {
		return ctrl.Result{}, fmt.Errorf("listing RemoteClusters: %w", err)
	}

	// Reconcile Known Objects
	knownObjects := map[client.ObjectKey]struct{}{}
	for _, remoteCluster := range remoteClusterList.Items {
		remoteObject, err := r.reconcileRemoteObject(ctx, &remoteCluster, fleetReplicaSet, remoteObjectSelector)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("reconciling RemoteObject: %w", err)
		}
		knownObjects[client.ObjectKeyFromObject(remoteObject)] = struct{}{}
	}

	// Check all objects matching our selector
	remoteObjectList := &fleetv1alpha1.RemoteObjectList{}
	if err := r.List(ctx, remoteObjectList, client.MatchingLabelsSelector{
		Selector: remoteObjectSelector,
	}); err != nil {
		return ctrl.Result{}, fmt.Errorf("listing RemoteObjects: %w", err)
	}

	fleetReplicaSet.Status.Replicas = int32(len(remoteObjectList.Items))
	fleetReplicaSet.Status.AvailableReplicas = 0
	for _, remoteObject := range remoteObjectList.Items {
		if _, ok := knownObjects[client.ObjectKeyFromObject(&remoteObject)]; !ok {
			// unknown object matching our selector -> delete!
			if err := r.Delete(ctx, &remoteObject); err != nil {
				return ctrl.Result{}, fmt.Errorf("deleting RemoteObject: %w", err)
			}
		}

		if meta.IsStatusConditionTrue(remoteObject.Status.Conditions, fleetv1alpha1.RemoteObjectAvailable) {
			fleetReplicaSet.Status.AvailableReplicas++
		}
	}

	return ctrl.Result{}, r.Status().Update(ctx, fleetReplicaSet)
}

func (r *FleetReplicaSetReconciler) reconcileRemoteObject(
	ctx context.Context, remoteCluster *fleetv1alpha1.RemoteCluster,
	fleetReplicaSet *fleetv1alpha1.FleetReplicaSet, selector labels.Selector,
) (actualRemoteObject *fleetv1alpha1.RemoteObject, err error) {
	template := fleetReplicaSet.Spec.Template

	remoteObject := &fleetv1alpha1.RemoteObject{
		ObjectMeta: metav1.ObjectMeta{
			Name:        template.Metadata.Name,
			Namespace:   remoteCluster.Status.LocalNamespace,
			Labels:      template.Metadata.Labels,
			Annotations: template.Metadata.Annotations,
		},
		Spec: fleetv1alpha1.RemoteObjectSpec{
			Object:            template.Spec.Object,
			AvailabilityProbe: template.Spec.AvailabilityProbe,
			PriorityClassName: template.Spec.PriorityClassName,
		},
	}
	if err := controllerutil.SetControllerReference(fleetReplicaSet, remoteObject, r.Scheme); err != nil {
		return nil, fmt.Errorf("setting controller reference: %w", err)
	}

	actualRemoteObject = &fleetv1alpha1.RemoteObject{}
	err = r.Get(ctx, client.ObjectKeyFromObject(remoteObject), actualRemoteObject)
	if err != nil && !errors.IsNotFound(err) {
		return nil, fmt.Errorf("getting RemoteObject: %w", err)
	}

	// Don't create/recreate objects we are not selecting
	if !selector.Matches(labels.Set(remoteObject.Labels)) {
		return remoteObject, nil
	}

	if errors.IsNotFound(err) {
		// Create Object
		return remoteObject, r.Create(ctx, remoteObject)
	}

	// Don't update objects we are not selecting
	if !selector.Matches(labels.Set(actualRemoteObject.Labels)) {
		return actualRemoteObject, nil
	}

	if !equality.Semantic.DeepDerivative(remoteObject, actualRemoteObject) ||
		!isOwner(fleetReplicaSet, actualRemoteObject) {
		actualRemoteObject.Spec = remoteObject.Spec
		actualRemoteObject.OwnerReferences = nil
		if err := controllerutil.SetControllerReference(fleetReplicaSet, actualRemoteObject, r.Scheme); err != nil {
			return nil, fmt.Errorf("setting controller reference: %w", err)
		}

		return actualRemoteObject, r.Update(ctx, actualRemoteObject)
	}
	return actualRemoteObject, nil
}

func (r *FleetReplicaSetReconciler) requeueAllFleetReplicaSets(obj client.Object) (
	reqs []reconcile.Request) {
	fleetReplicaSetList := &fleetv1alpha1.FleetReplicaSetList{}
	if err := r.List(context.Background(), fleetReplicaSetList); err != nil {
		r.Log.Error(err, "requeueing all FleetReplicaSets")
		return
	}

	for _, frs := range fleetReplicaSetList.Items {
		reqs = append(reqs, reconcile.Request{
			NamespacedName: client.ObjectKey{
				Name: frs.Name,
			},
		})
	}
	return
}

func isOwner(owner, object client.Object) bool {
	for _, ownerRef := range object.GetOwnerReferences() {
		if ownerRef.UID == owner.GetUID() {
			return true
		}
	}
	return false
}
