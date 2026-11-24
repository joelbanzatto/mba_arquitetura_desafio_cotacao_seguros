package partner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/GuilhermeOliveira591/mba_arquitetura_desafio_cotacao_seguros/internal/platform"
	"github.com/sony/gobreaker/v2"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type Quote struct {
	Partner         string `json:"partner"`
	QuoteID         string `json:"quote_id"`
	PremiumCents    int64  `json:"premium_cents"`
	Currency        string `json:"currency"`
	CoverageCents   int64  `json:"coverage_cents"`
	ValidForSeconds int64  `json:"valid_for_seconds"`
}

type Client struct {
	http       *http.Client
	resilience platform.Resilience
	metrics    *platform.ResilienceMetrics

	mu       sync.Mutex
	breakers map[string]*gobreaker.CircuitBreaker[Quote]
}

func NewClient(resilience platform.Resilience, metrics *platform.ResilienceMetrics) *Client {
	return &Client{
		http: &http.Client{
			Timeout:   resilience.PartnerTimeout,
			Transport: platform.InstrumentTransport(http.DefaultTransport),
		},
		resilience: resilience,
		metrics:    metrics,
		breakers:   make(map[string]*gobreaker.CircuitBreaker[Quote]),
	}
}

const responseLimit = 1 << 20

func (c *Client) Quote(ctx context.Context, p platform.Partner, request any) (Quote, error) {
	breaker := c.breakerFor(p)
	quote, err := breaker.Execute(func() (Quote, error) {
		started := time.Now()
		defer func() {
			c.metrics.RecordPartnerDuration(ctx, p.Name, time.Since(started))
		}()
		return c.quote(ctx, p, request)
	})
	if err == nil {
		return quote, nil
	}
	if errors.Is(err, gobreaker.ErrOpenState) || errors.Is(err, gobreaker.ErrTooManyRequests) {
		trace.SpanFromContext(ctx).SetAttributes(
			attribute.Bool("resilience.circuit_open", true),
			attribute.String("partner", p.Name),
		)
		return Quote{}, &Error{Partner: p.Name, CircuitOpen: true, Reason: "circuit breaker is open"}
	}
	var partnerError *Error
	if errors.As(err, &partnerError) {
		return Quote{}, partnerError
	}
	return Quote{}, &Error{Partner: p.Name, Reason: err.Error()}
}

func (c *Client) quote(ctx context.Context, p platform.Partner, request any) (Quote, error) {
	body, err := json.Marshal(request)
	if err != nil {
		return Quote{}, &Error{Partner: p.Name, Reason: fmt.Sprintf("invalid request: %v", err)}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.BaseURL+"/quotes", bytes.NewReader(body))
	if err != nil {
		return Quote{}, &Error{Partner: p.Name, Reason: err.Error()}
	}
	req.Header.Set("Content-Type", "application/json")

	response, err := c.http.Do(req)
	if err != nil {
		return Quote{}, &Error{Partner: p.Name, Reason: err.Error()}
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, responseLimit))
		return Quote{}, &Error{
			Partner: p.Name,
			Status:  response.StatusCode,
			Reason:  fmt.Sprintf("partner replied %d", response.StatusCode),
		}
	}

	var quote Quote
	if err := json.NewDecoder(io.LimitReader(response.Body, responseLimit)).Decode(&quote); err != nil {
		return Quote{}, &Error{Partner: p.Name, Reason: fmt.Sprintf("unreadable response: %v", err)}
	}

	quote.Partner = p.Name
	return quote, nil
}

func (c *Client) breakerFor(p platform.Partner) *gobreaker.CircuitBreaker[Quote] {
	c.mu.Lock()
	defer c.mu.Unlock()
	if breaker := c.breakers[p.Name]; breaker != nil {
		return breaker
	}

	breaker := gobreaker.NewCircuitBreaker[Quote](gobreaker.Settings{
		Name:        p.Name,
		Interval:    c.resilience.BreakerOpenTimeout,
		Timeout:     c.resilience.BreakerOpenTimeout,
		MaxRequests: c.resilience.BreakerHalfOpenRequests,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			return counts.ConsecutiveFailures >= c.resilience.BreakerConsecutiveErrors
		},
		OnStateChange: func(_ string, from, to gobreaker.State) {
			c.metrics.RecordBreakerTransition(p.Name, from.String(), to.String())
		},
	})
	c.breakers[p.Name] = breaker
	return breaker
}

type Error struct {
	Partner     string
	Status      int
	CircuitOpen bool
	Reason      string
}

func (e *Error) Error() string {
	return fmt.Sprintf("partner %s: %s", e.Partner, e.Reason)
}
