package vaultseed

import (
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/types"
)

func TestBackoffDoublesFromFiveSecondsToFiveMinutes(t *testing.T) {
	b := newBackoff()
	k := types.NamespacedName{Namespace: "n", Name: "a"}
	var got []time.Duration
	for i := 0; i < 8; i++ {
		got = append(got, b.next(k))
	}
	want := []time.Duration{5 * time.Second, 10 * time.Second, 20 * time.Second, 40 * time.Second, 80 * time.Second, 160 * time.Second, 5 * time.Minute, 5 * time.Minute}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("attempt %d = %s, want %s", i, got[i], want[i])
		}
	}
	b.reset(k)
	if d := b.next(k); d != 5*time.Second {
		t.Errorf("after a reset: %s", d)
	}
	if d := b.next(types.NamespacedName{Namespace: "n", Name: "other"}); d != 5*time.Second {
		t.Errorf("objects count apart: %s", d)
	}
}
