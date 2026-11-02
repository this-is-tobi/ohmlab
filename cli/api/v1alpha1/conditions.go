package v1alpha1

// Condition types, following the kstatus conventions.
const (
	// ConditionReady is True when the backend holds what the object declares.
	ConditionReady = "Ready"
	// ConditionReconciling is True while the controller is working towards that
	// and has not got there: a reference that is not there yet, a backend that
	// does not answer.
	ConditionReconciling = "Reconciling"
	// ConditionStalled is True when nothing will change until the spec does.
	ConditionStalled = "Stalled"
	// ConditionDrifted is True in observe-only mode when the backend differs
	// from the declaration.
	ConditionDrifted = "Drifted"
)

// Condition reasons.
const (
	ReasonReconciled         = "Reconciled"
	ReasonBackendUnavailable = "BackendUnavailable"
	ReasonForbidden          = "Forbidden"
	ReasonInvalidSpec        = "InvalidSpec"
	ReasonInvalidRotation    = "InvalidRotation"
	ReasonOwnedByAnother     = "OwnedByAnother"
	ReasonRefPending         = "RefPending"
	ReasonRefCycle           = "RefCycle"
	ReasonDrifted            = "Drifted"
	ReasonObserveOnly        = "ObserveOnly"
)

// Finalizer is set on a VaultSeed whose deletion policy is Delete.
const Finalizer = "ohmlab.fr/vaultseed"
