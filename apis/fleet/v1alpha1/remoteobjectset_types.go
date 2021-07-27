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

func init() {
	SchemeBuilder.Register(&RemoteObjectSet{}, &RemoteObjectSetList{})
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

type RemoteObjectSetStatus struct{}
