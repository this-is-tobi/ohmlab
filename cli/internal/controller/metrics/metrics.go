// Package metrics holds the ohmlab_* metrics of the controller. Labels carry
// names and counts, never values.
package metrics

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

// Metrics is the set of collectors one process exposes.
type Metrics struct {
	LastSuccess     *prometheus.GaugeVec
	ObjectReady     *prometheus.GaugeVec
	BackendRequests *prometheus.CounterVec
	BackendDuration *prometheus.HistogramVec
	BackendWrites   *prometheus.CounterVec
	PlannedChanges  *prometheus.GaugeVec
	Rotations       *prometheus.CounterVec
	Adopted         *prometheus.CounterVec
	CRDMismatch     *prometheus.GaugeVec
	// WorkloadRestarts counts the rolls of each workload onto a renewed
	// certificate (the certificates target).
	WorkloadRestarts *prometheus.CounterVec
	// BucketQuota and StoreCapacity are the storage target's bound: the quotas
	// a store holds against the volume it has.
	BucketQuota   *prometheus.GaugeVec
	StoreCapacity *prometheus.GaugeVec

	mu      sync.Mutex
	planned map[string]map[string]int // "target/kind/ns/name" -> action -> count
	buckets map[string][2]string      // "target/kind/ns/name" -> store, bucket
}

// New creates the collectors and registers them.
func New(reg prometheus.Registerer) *Metrics {
	m := &Metrics{planned: map[string]map[string]int{}, buckets: map[string][2]string{}}
	m.LastSuccess = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ohmlab_reconcile_last_success_timestamp_seconds",
		Help: "Unix time of the last reconcile that completed without a backend error.",
	}, []string{"target", "kind"})
	m.ObjectReady = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ohmlab_object_ready",
		Help: "1 when the object is reconciled as far as the controller can tell (Ready, or held back only by observe-only mode), 0 when it is failing, waiting or stalled.",
	}, []string{"target", "kind", "namespace", "name"})
	m.BackendRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "ohmlab_backend_requests_total",
		Help: "Requests to a backend by operation and result (ok, conflict, forbidden, error).",
	}, []string{"target", "op", "result"})
	m.BackendDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "ohmlab_backend_request_duration_seconds",
		Help:    "Duration of requests to a backend.",
		Buckets: prometheus.ExponentialBuckets(0.005, 2, 10),
	}, []string{"target", "op"})
	m.BackendWrites = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "ohmlab_backend_writes_total",
		Help: "Writes the controller made to a backend.",
	}, []string{"target", "kind", "op"})
	m.PlannedChanges = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ohmlab_planned_changes",
		Help: "Changes the controller planned at its last reconcile of each object, summed by action. Non-zero in observe-only mode until the backend matches.",
	}, []string{"target", "kind", "action"})
	m.Rotations = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "ohmlab_rotations_total", Help: "Rotations performed.",
	}, []string{"target"})
	m.Adopted = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "ohmlab_adopted_objects_total", Help: "Backend objects adopted by a custom resource.",
	}, []string{"target", "kind"})
	m.CRDMismatch = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ohmlab_crd_revision_mismatch",
		Help: "1 when the installed CRD was generated at another API revision than the one this controller was built with.",
	}, []string{"target", "kind"})
	m.WorkloadRestarts = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "ohmlab_workload_restarts_total",
		Help: "Rolls of a workload onto a certificate that was issued again.",
	}, []string{"target", "namespace", "kind", "name"})
	m.BucketQuota = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ohmlab_storage_bucket_quota_bytes",
		Help: "The hard quota of each bucket the storage target manages.",
	}, []string{"store", "bucket"})
	m.StoreCapacity = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ohmlab_storage_capacity_bytes",
		Help: "The capacity of each store the storage target manages: the size of its volume.",
	}, []string{"store"})
	reg.MustRegister(m.LastSuccess, m.ObjectReady, m.BackendRequests, m.BackendDuration, m.BackendWrites,
		m.PlannedChanges, m.Rotations, m.Adopted, m.CRDMismatch, m.WorkloadRestarts, m.BucketQuota, m.StoreCapacity)
	return m
}

// kindActions are the actions each kind plans: a kind exposes a series for
// each of its own actions and none for another kind's.
var kindActions = map[string][]string{
	"VaultSeed": {"adopt", "add-key", "update-field", "rotate", "prune"},
	"S3Bucket": {"adopt", "add-key", "update-field", "rotate", "retire-key",
		"create-bucket", "set-versioning", "set-lifecycle", "set-quota", "put-policy", "create-user",
		"attach-policy", "detach-policy", "add-service-account", "remove-service-account"},
}

// SetBucketQuota exposes the quota of one S3Bucket's bucket. Forget drops it.
func (m *Metrics) SetBucketQuota(target, kind, namespace, name, store, bucket string, bytes float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id := target + "/" + kind + "/" + namespace + "/" + name
	if prev, ok := m.buckets[id]; ok && prev != [2]string{store, bucket} {
		m.BucketQuota.DeleteLabelValues(prev[0], prev[1])
	}
	m.buckets[id] = [2]string{store, bucket}
	m.BucketQuota.WithLabelValues(store, bucket).Set(bytes)
}

// SetPlanned records what one object's last reconcile planned and refreshes
// the per-action sums of the kind.
func (m *Metrics) SetPlanned(target, kind, object string, counts map[string]int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id := target + "/" + kind + "/" + object
	if len(counts) == 0 {
		delete(m.planned, id)
	} else {
		m.planned[id] = counts
	}
	m.refreshPlanned(target, kind)
}

// Forget drops everything recorded about one object.
func (m *Metrics) Forget(target, kind, namespace, name string) {
	m.ObjectReady.DeleteLabelValues(target, kind, namespace, name)
	m.mu.Lock()
	defer m.mu.Unlock()
	id := target + "/" + kind + "/" + namespace + "/" + name
	delete(m.planned, id)
	if b, ok := m.buckets[id]; ok {
		m.BucketQuota.DeleteLabelValues(b[0], b[1])
		delete(m.buckets, id)
	}
	m.refreshPlanned(target, kind)
}

func (m *Metrics) refreshPlanned(target, kind string) {
	prefix := target + "/" + kind + "/"
	sums := map[string]int{}
	for id, counts := range m.planned {
		if len(id) > len(prefix) && id[:len(prefix)] == prefix {
			for a, n := range counts {
				sums[a] += n
			}
		}
	}
	for _, a := range kindActions[kind] {
		m.PlannedChanges.WithLabelValues(target, kind, a).Set(float64(sums[a]))
	}
}
