package apis

import (
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/ensure-stack/fleet-operator/apis/fleet"
)

// AddToSchemes may be used to add all resources defined in the project to a Scheme
var AddToSchemes runtime.SchemeBuilder = runtime.SchemeBuilder{
	fleet.AddToScheme,
}

// AddToScheme adds all addon Resources to the Scheme
func AddToScheme(s *runtime.Scheme) error {
	return AddToSchemes.AddToScheme(s)
}
