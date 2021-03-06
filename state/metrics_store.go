// Code generated by igenerator. DO NOT EDIT.

package state

import (
	"context"

	"github.com/wercker/pkg/metrics"
)

// force context to be used
var _ context.Context

// MetricsStore wraps another Store and sends metrics to Prometheus.
type MetricsStore struct {
	store    Store
	observer *metrics.StoreObserver
}

// NewMetricsStore creates a new MetricsStore.
func NewMetricsStore(wrappedStore Store) *MetricsStore {
	store := &MetricsStore{store: wrappedStore, observer: metrics.NewStoreObserver()}

	return store
}

var _ Store = (*MetricsStore)(nil)

// Initialize calls Initialize on the wrapped store.
func (s *MetricsStore) Initialize() error {
	s.observer.Preload(s, "Initialize")
	return s.store.Initialize()
}

// Healthy calls Healthy on the wrapped store.
func (s *MetricsStore) Healthy() error {
	return s.store.Healthy()
}

// Close calls Close on the wrapped store.
func (s *MetricsStore) Close() error {
	return s.store.Close()
}
