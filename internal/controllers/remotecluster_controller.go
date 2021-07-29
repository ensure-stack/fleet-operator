package controllers

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/yaml"

	"github.com/ensure-stack/operator-utils/reconcile"

	fleetv1alpha1 "github.com/ensure-stack/fleet-operator/apis/fleet/v1alpha1"
)

const (
	remoteClusterFinalizer = "fleet.ensure-stack.org/cleanup"
	clusterResyncInterval  = 1 * time.Minute
)

type RemoteClusterReconciler struct {
	client.Client
	Log      logr.Logger
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

func (r *RemoteClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&fleetv1alpha1.RemoteCluster{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Owns(&corev1.Namespace{}).
		Complete(r)
}

// RemoteClusterReconciler/Controller entrypoint
func (r *RemoteClusterReconciler) Reconcile(
	ctx context.Context, req ctrl.Request) (res ctrl.Result, err error) {
	log := r.Log.WithValues("remote-cluster", req.NamespacedName.String())

	rc := &fleetv1alpha1.RemoteCluster{}
	if err := r.Get(ctx, req.NamespacedName, rc); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Reconcile a Namespace for the Cluster to contain Remote Objects for it.
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster-" + rc.Name,
		},
	}
	if err := controllerutil.SetControllerReference(rc, ns, r.Scheme); err != nil {
		return ctrl.Result{}, fmt.Errorf("setting controller reference on namespace: %w", err)
	}
	if _, err := reconcile.Namespace(ctx, log, r.Client, ns); err != nil {
		return ctrl.Result{}, fmt.Errorf("reconciling Cluster Namespace: %w", err)
	}
	rc.Status.LocalNamespace = ns.Name

	// Create Clients
	remoteClient, remoteDiscoveryClient, err := r.createRemoteClients(ctx, rc, log)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("creating remote clients: %w", err)
	}
	if remoteClient == nil || remoteDiscoveryClient == nil {
		// Could not create client
		return ctrl.Result{
			// Make sure we re-check periodically
			RequeueAfter: clusterResyncInterval,
		}, nil
	}

	// Check connection.
	success, err := r.checkRemoteVersion(ctx, rc, remoteDiscoveryClient)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("could not check RemoteCluster version: %w", err)
	}
	if !success {
		return ctrl.Result{
			// Make sure we re-check periodically
			RequeueAfter: clusterResyncInterval,
		}, nil
	}

	// Sync RemoteObjects.
	err = r.syncRemoteObjects(ctx, rc, remoteClient, log)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("syncing RemoteObjects: %w", err)
	}
	return ctrl.Result{
		// Make sure we re-check periodically
		RequeueAfter: clusterResyncInterval,
	}, nil
}

func (r *RemoteClusterReconciler) syncRemoteObjects(
	ctx context.Context,
	rc *fleetv1alpha1.RemoteCluster,
	remoteClient client.Client,
	log logr.Logger,
) error {
	remoteObjectList := &fleetv1alpha1.RemoteObjectList{}
	if err := r.List(
		ctx, remoteObjectList,
		client.InNamespace(rc.Status.LocalNamespace),
	); err != nil {
		return fmt.Errorf("listing remote objects: %w", err)
	}

	for _, remoteObject := range remoteObjectList.Items {
		if err := r.syncObject(ctx, log, &remoteObject, remoteClient); err != nil {
			return fmt.Errorf("syncing RemoteObject %s: %w", client.ObjectKeyFromObject(&remoteObject), err)
		}
	}

	return nil
}

func (r *RemoteClusterReconciler) syncObject(
	ctx context.Context,
	log logr.Logger,
	remoteObject *fleetv1alpha1.RemoteObject,
	remoteClient client.Client,
) error {
	obj, err := unstructuredFromRaw(remoteObject.Spec.Object)
	if err != nil {
		return fmt.Errorf("parsing object: %w", err)
	}

	// handle deletion
	if !remoteObject.DeletionTimestamp.IsZero() {
		if err := remoteClient.Delete(ctx, obj); err != nil &&
			!errors.IsNotFound(err) {
			return fmt.Errorf("deleting RemoteObject: %w", err)
		}
		if controllerutil.ContainsFinalizer(
			remoteObject, remoteClusterFinalizer) {
			controllerutil.RemoveFinalizer(
				remoteObject, remoteClusterFinalizer)
			if err := r.Update(ctx, remoteObject); err != nil {
				return fmt.Errorf("removing finalizer: %w", err)
			}
		}
	}

	// ensure finalizer
	if !controllerutil.ContainsFinalizer(
		remoteObject, remoteClusterFinalizer) {
		controllerutil.AddFinalizer(remoteObject, remoteClusterFinalizer)
		if err := r.Update(ctx, remoteObject); err != nil {
			return fmt.Errorf("adding finalizer: %w", err)
		}
	}

	if err := r.reconcileObject(ctx, obj, remoteClient, log); err != nil {
		// TODO: Update RemoteObject status on non-transient errors, preventing sync.
		return fmt.Errorf("reconciling object: %w", err)
	}

	syncStatus(obj, remoteObject)
	setAvailableCondition(remoteObject)

	meta.SetStatusCondition(&remoteObject.Status.Conditions, metav1.Condition{
		Type:    fleetv1alpha1.RemoteObjectSynced,
		Status:  metav1.ConditionTrue,
		Reason:  "ObjectSynced",
		Message: "Object was synced with the RemoteCluster.",
	})
	remoteObject.Status.UpdatePhase()
	remoteObject.Status.LastHeartbeatTime = metav1.Now()
	if err := r.Status().Update(ctx, remoteObject); err != nil {
		return fmt.Errorf("updating RemoteObject Status: %w", err)
	}

	return nil
}

func setAvailableCondition(remoteObject *fleetv1alpha1.RemoteObject) {
	switch remoteObject.Spec.AvailabilityProbe.Type {
	case fleetv1alpha1.RemoteObjectProbeCondition:
		if remoteObject.Spec.AvailabilityProbe.Condition == nil {
			return
		}

		cond := meta.FindStatusCondition(
			remoteObject.Status.Conditions,
			remoteObject.Spec.AvailabilityProbe.Condition.Type,
		)

		if cond == nil {
			meta.SetStatusCondition(&remoteObject.Status.Conditions, metav1.Condition{
				Type:   fleetv1alpha1.RemoteObjectAvailable,
				Status: metav1.ConditionUnknown,
				Reason: "MissingCondition",
				Message: fmt.Sprintf("Missing %s condition.",
					remoteObject.Spec.AvailabilityProbe.Condition.Type),
			})
		} else if cond.Status == metav1.ConditionTrue {
			meta.SetStatusCondition(&remoteObject.Status.Conditions, metav1.Condition{
				Type:   fleetv1alpha1.RemoteObjectAvailable,
				Status: metav1.ConditionTrue,
				Reason: "ProbeSuccess",
				Message: fmt.Sprintf("Probed condition %s is True.",
					remoteObject.Spec.AvailabilityProbe.Condition.Type),
			})
		} else if cond.Status == metav1.ConditionFalse {
			meta.SetStatusCondition(&remoteObject.Status.Conditions, metav1.Condition{
				Type:   fleetv1alpha1.RemoteObjectAvailable,
				Status: metav1.ConditionFalse,
				Reason: "ProbeFailure",
				Message: fmt.Sprintf("Probed condition %s is False.",
					remoteObject.Spec.AvailabilityProbe.Condition.Type),
			})
		}
	}
}

// Sync Status from obj to remoteObject checking observedGeneration.
func syncStatus(
	obj *unstructured.Unstructured,
	remoteObject *fleetv1alpha1.RemoteObject,
) {
	for _, cond := range conditionsFromUnstructured(obj) {
		// Update Condition ObservedGeneration to relate to the current Generation of the RemoteObject in the Fleet Cluster,
		// if the Condition of the object in the RemoteCluster is not stale and such a property exists.
		if cond.ObservedGeneration != 0 &&
			cond.ObservedGeneration == obj.GetGeneration() {
			cond.ObservedGeneration = remoteObject.Generation
		}

		meta.SetStatusCondition(
			&remoteObject.Status.Conditions, cond)
	}

	// Update general ObservedGeneration to relate to the current Generation of the RemoteObject,
	// if the Status of the object in the RemoteCluster is not stale and such a property exists.
	observedGeneration, ok, err := unstructured.NestedInt64(obj.Object, "status", "observedGeneration")
	if !ok || err != nil {
		// TODO: enhanced error reporting to find miss-typed fields?
		// observedGeneration field not present or of invalid type -> nothing to do
		return
	}
	if observedGeneration != 0 &&
		observedGeneration == obj.GetGeneration() {
		remoteObject.Status.ObservedGeneration = remoteObject.Generation
	}
}

func conditionsFromUnstructured(obj *unstructured.Unstructured) []metav1.Condition {
	var foundConditions []metav1.Condition

	conditions, exist, err := unstructured.
		NestedSlice(obj.Object, "status", "conditions")
	if err != nil || !exist {
		// no status conditions
		return nil
	}

	for _, condI := range conditions {
		cond, ok := condI.(map[string]interface{})
		if !ok {
			// no idea what that is supposed to be
			continue
		}

		// Check conditions observed generation, if set
		observedGeneration, _, _ := unstructured.NestedInt64(
			cond, "observedGeneration",
		)

		condType, _ := cond["type"].(string)
		condStatus, _ := cond["status"].(string)
		condReason, _ := cond["reason"].(string)
		condMessage, _ := cond["message"].(string)

		foundConditions = append(foundConditions, metav1.Condition{
			Type:               condType,
			Status:             metav1.ConditionStatus(condStatus),
			Reason:             condReason,
			Message:            condMessage,
			ObservedGeneration: observedGeneration,
		})
	}

	return foundConditions
}

func (r *RemoteClusterReconciler) reconcileObject(
	ctx context.Context,
	obj *unstructured.Unstructured,
	remoteClient client.Client,
	log logr.Logger,
) error {
	currentObj := obj.DeepCopy()
	err := remoteClient.Get(ctx, client.ObjectKeyFromObject(obj), currentObj)
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("getting: %w", err)
	}

	if errors.IsNotFound(err) {
		err := remoteClient.Create(ctx, obj)
		if err != nil {
			return fmt.Errorf("creating: %w", err)
		}
	}

	// Update
	if !equality.Semantic.DeepDerivative(
		obj.Object, currentObj.Object) {
		log.Info("patching spec", "obj", client.ObjectKeyFromObject(obj))
		// this is only updating "known" fields,
		// so annotations/labels and other properties will be preserved.
		err := remoteClient.Patch(
			ctx, obj, client.MergeFrom(&unstructured.Unstructured{}))

		// Alternative to override the object completely:
		// err := r.Update(ctx, obj)
		if err != nil {
			return fmt.Errorf("patching spec: %w", err)
		}
	} else {
		*obj = *currentObj
	}
	return nil
}

func unstructuredFromRaw(raw *runtime.RawExtension) (*unstructured.Unstructured, error) {
	obj := &unstructured.Unstructured{}
	if err := yaml.Unmarshal(raw.Raw, obj); err != nil {
		return nil, fmt.Errorf("converting RawExtension into unstructured: %w", err)
	}
	return obj, nil
}

func (r *RemoteClusterReconciler) checkRemoteVersion(
	ctx context.Context, rc *fleetv1alpha1.RemoteCluster, remoteDiscoveryClient discovery.DiscoveryInterface,
) (success bool, err error) {
	rc.Status.LastHeartbeatTime = metav1.Now()

	version, err := remoteDiscoveryClient.ServerVersion()
	if err != nil {
		meta.SetStatusCondition(&rc.Status.Conditions, metav1.Condition{
			Type:               fleetv1alpha1.RemoteClusterReachable,
			Status:             metav1.ConditionFalse,
			ObservedGeneration: rc.Generation,
			Reason:             "APIError",
			Message:            "Remote cluster API is not responding.",
		})
		rc.Status.UpdatePhase()
		return false, r.Status().Update(ctx, rc)
	}

	rc.Status.Remote.Version = version.String()
	meta.SetStatusCondition(&rc.Status.Conditions, metav1.Condition{
		Type:               fleetv1alpha1.RemoteClusterReachable,
		Status:             metav1.ConditionTrue,
		ObservedGeneration: rc.Generation,
		Reason:             "APIResponding",
		Message:            "Remote cluster API is responding.",
	})
	rc.Status.UpdatePhase()
	return true, r.Status().Update(ctx, rc)
}

func (r *RemoteClusterReconciler) createRemoteClients(
	ctx context.Context, rc *fleetv1alpha1.RemoteCluster, log logr.Logger,
) (
	remoteClient client.Client,
	remoteDiscoveryClient discovery.DiscoveryInterface,
	err error,
) {
	// Lookup Kubeconfig Secret
	secretKey := client.ObjectKey{
		Name:      rc.Spec.KubeconfigSecret.Name,
		Namespace: rc.Spec.KubeconfigSecret.Namespace,
	}
	kubeconfigSecret := &corev1.Secret{}
	if err := r.Get(ctx, secretKey, kubeconfigSecret); errors.IsNotFound(err) {
		log.Info("missing kubeconfig secret", "secret", secretKey)
		r.Recorder.Eventf(rc, corev1.EventTypeWarning, "InvalidConfig", "missing kubeconfig secret %q", secretKey)
		return nil, nil, nil
	} else if err != nil {
		return nil, nil, fmt.Errorf("getting Kubeconfig secret: %w", err)
	}

	// Kubeconfig sanity check
	if kubeconfigSecret.Type != fleetv1alpha1.SecretTypeKubeconfig {
		log.Info("invalid secret type", "secret", secretKey)
		r.Recorder.Eventf(rc, corev1.EventTypeWarning, "InvalidConfig",
			"invalid secret type %q, want %q", kubeconfigSecret.Type, fleetv1alpha1.SecretTypeKubeconfig)
		return nil, nil, nil
	}
	kubeconfig, ok := kubeconfigSecret.Data[fleetv1alpha1.SecretKubeconfigKey]
	if !ok {
		log.Info(fmt.Sprintf("missing %q key in kubeconfig secret", fleetv1alpha1.SecretKubeconfigKey), "secret", secretKey)
		r.Recorder.Eventf(rc, corev1.EventTypeWarning, "InvalidConfig",
			"missing %q key in kubeconfig secret", fleetv1alpha1.SecretKubeconfigKey)
		return nil, nil, nil
	}

	// Create Clients
	remoteRESTConfig, err := clientcmd.RESTConfigFromKubeConfig(kubeconfig)
	if err != nil {
		log.Error(err, "invalid kubeconfig", "secret", secretKey)
		r.Recorder.Eventf(rc, corev1.EventTypeWarning, "InvalidConfig",
			"invalid kubeconfig: %w", err)
		return nil, nil, nil
	}
	remoteDiscoveryClient, err = discovery.NewDiscoveryClientForConfig(remoteRESTConfig)
	if err != nil {
		return nil, nil, fmt.Errorf("creating RemoteCluster discovery client: %w", err)
	}
	remoteClient, err = client.New(remoteRESTConfig, client.Options{})
	if err != nil {
		return nil, nil, fmt.Errorf("creating RemoteCluster client: %w", err)
	}

	// Update RemoteCluster status with remote host
	rc.Status.Remote.APIServer = remoteRESTConfig.Host

	return remoteClient, remoteDiscoveryClient, nil
}
