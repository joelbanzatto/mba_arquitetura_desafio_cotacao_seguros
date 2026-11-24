package quotation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"sort"
	"time"

	"github.com/GuilhermeOliveira591/mba_arquitetura_desafio_cotacao_seguros/internal/cache"
	"github.com/GuilhermeOliveira591/mba_arquitetura_desafio_cotacao_seguros/internal/partner"
	"github.com/GuilhermeOliveira591/mba_arquitetura_desafio_cotacao_seguros/internal/platform"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type Quoter interface {
	Quote(ctx context.Context, p platform.Partner, request any) (partner.Quote, error)
}

type Service struct {
	partners []platform.Partner
	quoter   Quoter
	cache    cache.Store
	cacheTTL time.Duration
	metrics  *platform.ResilienceMetrics
}

type Option func(*Service)

func WithCache(store cache.Store, ttl time.Duration) Option {
	return func(service *Service) {
		service.cache = store
		service.cacheTTL = ttl
	}
}

func WithMetrics(metrics *platform.ResilienceMetrics) Option {
	return func(service *Service) {
		service.metrics = metrics
	}
}

func NewService(partners []platform.Partner, quoter Quoter, options ...Option) *Service {
	service := &Service{partners: partners, quoter: quoter}
	for _, option := range options {
		option(service)
	}
	return service
}

func (s *Service) Quote(ctx context.Context, tenant string, request Request) (Response, error) {
	start := time.Now()
	key := quotationCacheKey(tenant, request)
	if response, found := s.cached(ctx, key, tenant, start); found {
		return response, nil
	}

	forPartner := partnerRequest{Broker: tenant, Request: request}

	quotes := make([]partner.Quote, 0, len(s.partners))
	unavailable := make([]string, 0, len(s.partners))
	var lastError error
	for _, p := range s.partners {
		quote, err := s.quoter.Quote(ctx, p, forPartner)
		if err != nil {
			unavailable = append(unavailable, p.Name)
			lastError = err
			continue
		}
		quotes = append(quotes, quote)
	}
	if len(quotes) == 0 {
		return Response{}, lastError
	}

	sort.Slice(quotes, func(i, j int) bool {
		return quotes[i].PremiumCents < quotes[j].PremiumCents
	})

	response := Response{
		TenantID:            tenant,
		Quotes:              quotes,
		ElapsedMs:           time.Since(start).Milliseconds(),
		Source:              "partner",
		Degraded:            len(unavailable) > 0,
		UnavailablePartners: unavailable,
	}
	if !response.Degraded {
		s.store(ctx, key, quotes)
	}
	return response, nil
}

type cachedQuotation struct {
	Quotes   []partner.Quote `json:"quotes"`
	StoredAt time.Time       `json:"stored_at"`
}

func (s *Service) cached(ctx context.Context, key, tenant string, started time.Time) (Response, bool) {
	if s.cache == nil {
		return Response{}, false
	}
	data, found, err := s.cache.Get(ctx, key)
	if err != nil {
		s.metrics.RecordCache("error")
		return Response{}, false
	}
	if !found {
		s.metrics.RecordCache("miss")
		return Response{}, false
	}

	var stored cachedQuotation
	if err := json.Unmarshal(data, &stored); err != nil || len(stored.Quotes) == 0 {
		s.metrics.RecordCache("error")
		return Response{}, false
	}
	s.metrics.RecordCache("hit")
	trace.SpanFromContext(ctx).SetAttributes(attribute.Bool("resilience.cache_hit", true))
	return Response{
		TenantID:        tenant,
		Quotes:          stored.Quotes,
		ElapsedMs:       time.Since(started).Milliseconds(),
		Source:          "cache",
		CacheAgeSeconds: int64(time.Since(stored.StoredAt).Seconds()),
	}, true
}

func (s *Service) store(ctx context.Context, key string, quotes []partner.Quote) {
	if s.cache == nil {
		return
	}
	data, err := json.Marshal(cachedQuotation{Quotes: quotes, StoredAt: time.Now().UTC()})
	if err != nil || s.cache.Set(ctx, key, data, s.cacheTTL) != nil {
		s.metrics.RecordCache("error")
	}
}

// quotationCacheKey keeps tenant isolation visible in the Redis key while a
// SHA-256 digest prevents raw CPF or plate values from being exposed there.
func quotationCacheKey(tenant string, request Request) string {
	canonical, _ := json.Marshal(struct {
		Document  string `json:"document"`
		BirthYear int    `json:"birth_year"`
		Plate     string `json:"plate"`
		Model     string `json:"model"`
		Year      int    `json:"year"`
		Value     int64  `json:"value_cents"`
		Coverage  string `json:"coverage"`
	}{
		Document:  request.Driver.Document,
		BirthYear: request.Driver.BirthYear,
		Plate:     request.Vehicle.Plate,
		Model:     request.Vehicle.Model,
		Year:      request.Vehicle.Year,
		Value:     request.Vehicle.ValueCents,
		Coverage:  request.Coverage,
	})
	digest := sha256.Sum256(canonical)
	return "quotation:v1:tenant:" + url.PathEscape(tenant) + ":risk:" + hex.EncodeToString(digest[:])
}
