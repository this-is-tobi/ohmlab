package v1alpha1

import (
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// S3Access is what an app's key may do in its bucket.
//
// +kubebuilder:validation:Enum=readWrite;readOnly
type S3Access string

const (
	// S3AccessReadWrite lists, reads, writes and deletes objects. Deleting a
	// version or changing the bucket's configuration stays denied.
	S3AccessReadWrite S3Access = "readWrite"
	// S3AccessReadOnly lists and reads objects.
	S3AccessReadOnly S3Access = "readOnly"
)

// S3BucketSpec declares one claim: a bucket in a store of the storage target,
// and the key an app reads from the claim's record.
type S3BucketSpec struct {
	// Store names a store of the storage target's configuration. Never an
	// endpoint: where to connect and as whom is the controller's own business.
	//
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$`
	Store string `json:"store"`

	// Bucket is the bucket's name: the store's bucketPrefix, then
	// <mount>-<app>-<claim> of the record.
	//
	// +kubebuilder:validation:Pattern=`^[a-z0-9][a-z0-9-]{1,61}[a-z0-9]$`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="bucket is immutable: delete the object and create another"
	Bucket string `json:"bucket"`

	// Credentials is the claim's record, where the controller writes the key.
	Credentials S3CredentialsRef `json:"credentials"`

	// Access is what the key may do in the bucket.
	//
	// +kubebuilder:default=readWrite
	// +optional
	Access S3Access `json:"access,omitempty"`

	// Quota is the bucket's hard quota: the store refuses the writes beyond it.
	Quota resource.Quantity `json:"quota"`

	// Versioning keeps the versions an overwrite or a delete leaves.
	//
	// +kubebuilder:default=true
	// +optional
	Versioning *bool `json:"versioning,omitempty"`

	// Lifecycle expires noncurrent versions, and objects when expireDays is
	// not 0.
	//
	// +kubebuilder:default={}
	// +optional
	Lifecycle S3Lifecycle `json:"lifecycle,omitempty"`

	// Replicate copies the bucket to the store's replica, when it has one.
	//
	// +optional
	Replicate bool `json:"replicate,omitempty"`

	// Rotate replaces the key once per distinct token. The old key keeps
	// working for the target's rotation grace.
	//
	// +optional
	Rotate *S3RotateSpec `json:"rotate,omitempty"`

	// DeletionPolicy is what deleting the object does. Retain leaves the
	// bucket, its identity and the record. Delete, when the chart allows it,
	// removes the identity and the record's key, and the bucket only when it
	// holds no object version.
	//
	// +kubebuilder:validation:Enum=Retain;Delete
	// +kubebuilder:default=Retain
	// +optional
	DeletionPolicy DeletionPolicy `json:"deletionPolicy,omitempty"`
}

// S3CredentialsRef is a claim's record in Vault.
type S3CredentialsRef struct {
	// Path is the app's record, <mount>/platforms/<env>/<scope>/<app>/s3.
	//
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?/platforms/[a-z0-9]([-a-z0-9]*[a-z0-9])?/(core|tenant)/[a-z0-9]([-a-z0-9]*[a-z0-9])?/s3$`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="credentials.path is immutable: delete the object and create another"
	Path string `json:"path"`

	// Key is the claim's key in the record: the claim's name.
	//
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="credentials.key is immutable: delete the object and create another"
	Key string `json:"key"`
}

// S3Lifecycle is the bucket's one lifecycle rule.
type S3Lifecycle struct {
	// NoncurrentDays is how long a noncurrent version is kept.
	//
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default=7
	// +optional
	NoncurrentDays int32 `json:"noncurrentDays,omitempty"`

	// ExpireDays expires objects that many days after their creation: a
	// backstop for a producer that stops pruning. 0 never expires them.
	//
	// +kubebuilder:validation:Minimum=0
	// +optional
	ExpireDays int32 `json:"expireDays,omitempty"`
}

// S3RotateSpec asks for one rotation of the key.
type S3RotateSpec struct {
	// Token identifies the rotation. A new key pair is made when it differs
	// from the one recorded on the record, then the token is recorded. At most
	// 128 characters: it is kept in the record's custom_metadata.
	//
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=128
	Token string `json:"token"`
}

// S3BucketStatus is what the controller observed. It never holds a key.
type S3BucketStatus struct {
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

	// KVVersion is the record's version last read or written.
	// +optional
	KVVersion int64 `json:"kvVersion,omitempty"`

	// QuotaBytes is the quota in bytes.
	// +optional
	QuotaBytes int64 `json:"quotaBytes,omitempty"`
}

// S3Bucket declares one claim's bucket, its quota and lifecycle, and the key
// that reaches it, which the controller keeps in the claim's Vault record.
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:categories=ohmlab
// +kubebuilder:metadata:annotations="ohmlab.fr/api-revision=1"
// +kubebuilder:printcolumn:name="Bucket",type=string,JSONPath=`.spec.bucket`
// +kubebuilder:printcolumn:name="Store",type=string,JSONPath=`.spec.store`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Reason",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].reason`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type S3Bucket struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   S3BucketSpec   `json:"spec"`
	Status S3BucketStatus `json:"status,omitempty"`
}

// S3BucketList is a list of S3Bucket.
//
// +kubebuilder:object:root=true
type S3BucketList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []S3Bucket `json:"items"`
}
