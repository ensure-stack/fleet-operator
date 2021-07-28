package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// RemoteObjectSet is the Schema for the RemoteObjectSets API
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Remote Name",type="string",JSONPath=".spec.object.metadata.name"
// +kubebuilder:printcolumn:name="Remote Namespace",type="string",JSONPath=".spec.object.metadata.namespace"
// +kubebuilder:printcolumn:name="Kind",type="string",JSONPath=".spec.object.kind"
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type RemoteObjectSet struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec RemoteObjectSetSpec `json:"spec,omitempty"`
	// +kubebuilder:default={phase:Pending}
	Status RemoteObjectSetStatus `json:"status,omitempty"`
}

// RemoteObjectSetList contains a list of RemoteObjectSet
// +kubebuilder:object:root=true
type RemoteObjectSetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []RemoteObjectSet `json:"items"`
}

// RemoteObjectSetSpec defines the desired state of RemoteObjectSet.
type RemoteObjectSetSpec struct {
	// +kubebuilder:validation:EmbeddedResource
	// +kubebuilder:pruning:PreserveUnknownFields
	Object runtime.RawExtension `json:"object"`

	// Selects Remote Clusters to create RemoteObjects for.
	RemoteClusterSelector metav1.LabelSelector `json:"remoteClusterSelector"`

	// Probes
}

type RemoteObjectSetStatus struct {
	// The most recent generation observed by the controller.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// Conditions is a list of status conditions ths object is in.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// DEPRECATED: This field is not part of any API contract
	// it will go away as soon as kubectl can print conditions!
	// Human readable status - please use .Conditions from code
	Phase RemoteObjectPhase `json:"phase,omitempty"`
}

func init() {
	SchemeBuilder.Register(&RemoteObjectSet{}, &RemoteObjectSetList{})
}
