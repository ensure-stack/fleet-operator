package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

type RemoteObjectTemplate struct {
	Metadata metav1.ObjectMeta `json:"metadata"`
	Spec     RemoteObjectSpec  `json:"spec"`
}
