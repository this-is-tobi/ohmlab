package v1alpha1

// The deepcopy functions and the CRDs are generated; CI fails when the
// committed files differ from the generated ones.
//
//go:generate go tool controller-gen object paths=.
//go:generate go tool controller-gen crd paths=. output:crd:dir=../../../argo-cd/apps/ohmlab-controller/files/crds
