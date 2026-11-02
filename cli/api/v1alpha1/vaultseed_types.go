package v1alpha1

import (
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// DeletionPolicy says what deleting the object does to the path.
type DeletionPolicy string

const (
	// DeletionPolicyRetain leaves the path in Vault. It is the default.
	DeletionPolicyRetain DeletionPolicy = "Retain"
	// DeletionPolicyDelete removes the path, if it carries this object's owner
	// marker and the chart allows deletion.
	DeletionPolicyDelete DeletionPolicy = "Delete"
)

// VaultSeedSpec declares one KV v2 path.
type VaultSeedSpec struct {
	// Path is "<mount>/<path>" of the KV v2 secret.
	//
	// +kubebuilder:validation:Pattern=`^[a-zA-Z0-9_-]+(/[a-zA-Z0-9_.-]+)+$`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="path is immutable: delete the object and create another"
	Path string `json:"path"`

	// Data is the tree the path must hold. Leaves are plain values or
	// templates: <random:N>, <hex:N>, <uuid>, <age:secret>, <age:public>,
	// <bcrypt:key.path> and <ref:mount/path#key.path>. Keys that exist are never
	// overwritten, except those copied from a reference, which follow it.
	//
	// +kubebuilder:validation:Type=object
	// +kubebuilder:validation:MinProperties=1
	// +kubebuilder:pruning:PreserveUnknownFields
	Data apiextensionsv1.JSON `json:"data"`

	// Rotate regenerates generated keys once per distinct token.
	//
	// +optional
	Rotate *RotateSpec `json:"rotate,omitempty"`

	// Prune removes the keys this controller wrote and the spec no longer
	// declares. Keys written by anyone else are never removed.
	//
	// +optional
	Prune bool `json:"prune,omitempty"`

	// DeletionPolicy is what deleting the object does to the path.
	//
	// +kubebuilder:validation:Enum=Retain;Delete
	// +kubebuilder:default=Retain
	// +optional
	DeletionPolicy DeletionPolicy `json:"deletionPolicy,omitempty"`
}

// RotateSpec asks for one rotation.
type RotateSpec struct {
	// Token identifies the rotation. The keys are regenerated when it differs
	// from the one recorded on the path, then the token is recorded.
	//
	// +kubebuilder:validation:MinLength=1
	Token string `json:"token"`

	// Keys are dot paths of generated keys to regenerate. Empty means every
	// generated key.
	//
	// +kubebuilder:validation:items:MinLength=1
	// +optional
	Keys []string `json:"keys,omitempty"`
}

// VaultSeedStatus is what the controller observed. It never holds a value.
type VaultSeedStatus struct {
	// ObservedGeneration is the generation of the spec the status describes.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions are Ready, Reconciling, Stalled and, in observe-only mode,
	// Drifted.
	//
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// KVVersion is the KV version last read or written.
	// +optional
	KVVersion int64 `json:"kvVersion,omitempty"`

	// ManagedKeys are the dot paths of the keys this controller wrote. Names
	// only. Pruning removes nothing else.
	//
	// +listType=set
	// +optional
	ManagedKeys []string `json:"managedKeys,omitempty"`
}

// VaultSeed declares one KV v2 path of Vault.
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:categories=ohmlab
// +kubebuilder:metadata:annotations="ohmlab.fr/api-revision=1"
// +kubebuilder:printcolumn:name="Path",type=string,JSONPath=`.spec.path`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Reason",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].reason`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type VaultSeed struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VaultSeedSpec   `json:"spec"`
	Status VaultSeedStatus `json:"status,omitempty"`
}

// VaultSeedList is a list of VaultSeed.
//
// +kubebuilder:object:root=true
type VaultSeedList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VaultSeed `json:"items"`
}
