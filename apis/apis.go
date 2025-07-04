package apis

import (
	"k8s.io/apimachinery/pkg/runtime"

	samplecontrollerv1 "github.com/krateoplatformops/config-reload/apis/configreload/v1"
)

func init() {
	AddToSchemes = append(AddToSchemes,
		samplecontrollerv1.SchemeBuilder.AddToScheme,
	)
}

// AddToSchemes may be used to add all resources defined in the project to a Scheme
var AddToSchemes runtime.SchemeBuilder

// AddToScheme adds all Resources to the Scheme
func AddToScheme(s *runtime.Scheme) error {
	return AddToSchemes.AddToScheme(s)
}
