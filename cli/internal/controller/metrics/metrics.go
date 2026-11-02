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

	mu      sync.Mutex
	planned map[string]map[string]int // "target/kind/ns/name" -> action -> count
}

// New creates the collectors and registers them.
func New(reg prometheus.Registerer) *Metrics {
	m := &Metrics{planned: map[string]map[string]int{}}
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
	reg.MustRegister(m.LastSuccess, m.ObjectReady, m.BackendRequests, m.BackendDuration, m.BackendWrites,
		m.PlannedChanges, m.Rotations, m.Adopted, m.CRDMismatch)
	return m
}

var allActions = []string{"adopt", "add-key", "update-field", "rotate", "prune"}

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
	delete(m.planned, target+"/"+kind+"/"+namespace+"/"+name)
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
	for _, a := range allActions {
		m.PlannedChanges.WithLabelValues(target, kind, a).Set(float64(sums[a]))
	}
}
