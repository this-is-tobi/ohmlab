package vaultseed

import (
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/types"
)

// backoff spaces the retries of one object: 5s, 10s, 20s ... up to 5m. A
// reconcile that succeeds starts the count over.
type backoff struct {
	mu       sync.Mutex
	attempts map[types.NamespacedName]int
	base     time.Duration
	max      time.Duration
}

func newBackoff() *backoff {
	return &backoff{attempts: map[types.NamespacedName]int{}, base: 5 * time.Second, max: 5 * time.Minute}
}

func (b *backoff) next(key types.NamespacedName) time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := b.attempts[key]
	b.attempts[key] = n + 1
	d := b.base
	for i := 0; i < n && d < b.max; i++ {
		d *= 2
	}
	if d > b.max {
		d = b.max
	}
	return d
}

func (b *backoff) reset(key types.NamespacedName) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.attempts, key)
}
