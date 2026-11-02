// Package v1alpha1 is the ohmlab.fr/v1alpha1 API: Kubernetes objects that say
// what the platform's backends must hold. Connection details and credentials
// never appear here; they belong to the controller's own configuration.
//
// +kubebuilder:object:generate=true
// +groupName=ohmlab.fr
package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// APIRevision is bumped whenever a field is added to a kind. Each CRD carries
// the revision it was generated at (annotation ohmlab.fr/api-revision) and the
// controller compares it with its own, which is how a catalog and an image that
// drifted apart are noticed.
const APIRevision = 1

// RevisionAnnotation is the CRD annotation that holds the revision.
const RevisionAnnotation = "ohmlab.fr/api-revision"

var (
	// GroupVersion is the group and version of the API.
	GroupVersion = schema.GroupVersion{Group: "ohmlab.fr", Version: "v1alpha1"}
	// SchemeBuilder collects the kinds of the API.
	SchemeBuilder = runtime.NewSchemeBuilder(addKnownTypes)
	// AddToScheme registers the API in a scheme.
	AddToScheme = SchemeBuilder.AddToScheme
)

func addKnownTypes(s *runtime.Scheme) error {
	s.AddKnownTypes(GroupVersion, &VaultSeed{}, &VaultSeedList{})
	metav1.AddToGroupVersion(s, GroupVersion)
	return nil
}
