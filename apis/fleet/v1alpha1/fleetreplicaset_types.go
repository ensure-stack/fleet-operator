package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// FleetReplicaSet is the Schema for the FleetReplicaSets API
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Replicas",type="integer",JSONPath=".status.replicas"
// +kubebuilder:printcolumn:name="Available",type="integer",JSONPath=".status.availableReplicas"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type FleetReplicaSet struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   FleetReplicaSetSpec   `json:"spec,omitempty"`
	Status FleetReplicaSetStatus `json:"status,omitempty"`
}

// FleetReplicaSetList contains a list of FleetReplicaSet
// +kubebuilder:object:root=true
type FleetReplicaSetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []FleetReplicaSet `json:"items"`
}

// FleetReplicaSetSpec defines the desired state of FleetReplicaSet.
type FleetReplicaSetSpec struct {
	// RemoteCluster selector targets the clusters this object should be rolled out to.
	RemoteClusterSelector metav1.LabelSelector `json:"remoteClusterSelector"`
	// Selector selects the RemoteObjects that this FleetReplicaSet should manage.
	Selector metav1.LabelSelector `json:"selector"`
	// Template of the RemoteObject to create.
	Template RemoteObjectTemplate `json:"template"`
}

type FleetReplicaSetStatus struct {
	// The most recent generation observed by the controller.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// The number of replicas managed across the fleet.
	Replicas int32 `json:"replicas,omitempty"`
	// The number of available replicas across the fleet.
	AvailableReplicas int32 `json:"availableReplicas,omitempty"`
}

func init() {
	SchemeBuilder.Register(&FleetReplicaSet{}, &FleetReplicaSetList{})
}
