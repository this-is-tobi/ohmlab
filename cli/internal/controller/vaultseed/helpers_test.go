package vaultseed

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	eventsv1 "k8s.io/api/events/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func jsonOf(v any) (string, error) {
	b, err := json.Marshal(v)
	return string(b), err
}

// dumpEvents returns every event of a namespace as text.
func dumpEvents(t *testing.T, ns string) string {
	t.Helper()
	var list eventsv1.EventList
	if err := direct.List(context.Background(), &list, client.InNamespace(ns)); err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	for _, e := range list.Items {
		b, _ := json.Marshal(e)
		sb.Write(b)
	}
	return sb.String()
}
