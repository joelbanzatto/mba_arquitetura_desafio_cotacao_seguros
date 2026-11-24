package platform

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// ResilienceMetrics contains only low-cardinality business measurements. Risk
// identifiers never become metric labels or trace attributes.
type ResilienceMetrics struct {
	breakerTransitions metric.Int64Counter
	cacheRequests      metric.Int64Counter
	partnerDuration    metric.Float64Histogram
}

func NewResilienceMetrics() (*ResilienceMetrics, error) {
	meter := otel.Meter("quotation-api/resilience")
	breakerTransitions, err := meter.Int64Counter(
		"quotation_breaker_transitions",
		metric.WithDescription("Circuit breaker state transitions by partner"),
	)
	if err != nil {
		return nil, err
	}
	cacheRequests, err := meter.Int64Counter(
		"quotation_cache_requests",
		metric.WithDescription("Quotation cache lookups by result"),
	)
	if err != nil {
		return nil, err
	}
	partnerDuration, err := meter.Float64Histogram(
		"quotation_partner_quote_duration",
		metric.WithUnit("s"),
		metric.WithDescription("Duration of attempted partner quote calls"),
	)
	if err != nil {
		return nil, err
	}
	return &ResilienceMetrics{
		breakerTransitions: breakerTransitions,
		cacheRequests:      cacheRequests,
		partnerDuration:    partnerDuration,
	}, nil
}

func (m *ResilienceMetrics) RecordBreakerTransition(partner, from, to string) {
	if m == nil {
		return
	}
	m.breakerTransitions.Add(context.Background(), 1,
		metric.WithAttributes(
			attribute.String("partner", partner),
			attribute.String("from_state", from),
			attribute.String("to_state", to),
		),
	)
}

func (m *ResilienceMetrics) RecordCache(result string) {
	if m == nil {
		return
	}
	m.cacheRequests.Add(context.Background(), 1, metric.WithAttributes(attribute.String("result", result)))
}

func (m *ResilienceMetrics) RecordPartnerDuration(ctx context.Context, partner string, elapsed time.Duration) {
	if m == nil {
		return
	}
	m.partnerDuration.Record(ctx, elapsed.Seconds(), metric.WithAttributes(attribute.String("partner", partner)))
}
