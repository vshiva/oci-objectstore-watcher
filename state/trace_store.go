// Code generated by igenerator. DO NOT EDIT.

package state

import (
	"context"
	"fmt"
	"strings"

	opentracing "github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/ext"
)

// force context to be used
var _ context.Context

// TraceStore wraps another Store and sends trace information to zipkin.
type TraceStore struct {
	store     Store
	tracer    opentracing.Tracer
	component string
}

// NewTraceStore creates a new TraceStore.
func NewTraceStore(store Store, tracer opentracing.Tracer) *TraceStore {
	component := strings.TrimPrefix(fmt.Sprintf("%T", store), "*")
	return &TraceStore{
		store:     store,
		tracer:    tracer,
		component: component,
	}
}

var _ Store = (*TraceStore)(nil)

func (s *TraceStore) trace(ctx context.Context, operationName string, opts ...opentracing.StartSpanOption) (context.Context, opentracing.Span) {
	var span opentracing.Span
	if parentSpan := opentracing.SpanFromContext(ctx); parentSpan != nil {
		opts = append(opts, opentracing.ChildOf(parentSpan.Context()))
		span = s.tracer.StartSpan(operationName, opts...)
	} else {
		span = s.tracer.StartSpan(operationName, opts...)
	}

	span.SetTag(string(ext.Component), s.component)

	return opentracing.ContextWithSpan(ctx, span), span
}

// Initialize calls Initialize on the wrapped store.
func (s *TraceStore) Initialize() error {
	return s.store.Initialize()
}

// Healthy calls Healthy on the wrapped store.
func (s *TraceStore) Healthy() error {
	return s.store.Healthy()
}

// Close calls Close on the wrapped store.
func (s *TraceStore) Close() error {
	return s.store.Close()
}
