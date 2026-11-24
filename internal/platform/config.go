package platform

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Port       string
	Partners   []Partner
	Tenants    []string
	Telemetry  Telemetry
	Resilience Resilience
}

type Partner struct {
	Name    string
	BaseURL string
}

type Telemetry struct {
	ServiceName string
	Endpoint    string
	Enabled     bool
}

// Resilience contains the business-controlled limits applied at the insurer
// boundary. These values are deliberately configuration, rather than code,
// because changing them affects both customer experience and partner cost.
type Resilience struct {
	RedisAddress             string
	CacheTTL                 time.Duration
	PartnerTimeout           time.Duration
	BreakerConsecutiveErrors uint32
	BreakerOpenTimeout       time.Duration
	BreakerHalfOpenRequests  uint32
}

const defaultEndpoints = "partner-slow=http://localhost:9001," +
	"partner-flaky=http://localhost:9002," +
	"partner-degrading=http://localhost:9003"

const defaultTenants = "corretora-a,corretora-b"

const defaultCollector = "http://localhost:4317"

const defaultServiceName = "quotation-api"

const (
	defaultRedisAddress             = "localhost:6379"
	defaultCacheTTL                 = time.Hour
	defaultPartnerTimeout           = 2 * time.Second
	defaultBreakerConsecutiveErrors = uint32(3)
	defaultBreakerOpenTimeout       = 30 * time.Second
	defaultBreakerHalfOpenRequests  = uint32(2)
)

func LoadConfig(env func(string) string) (Config, error) {
	cfg := Config{Port: text(env, "PORT", "8080")}

	var err error
	if cfg.Partners, err = parsePartners(text(env, "PARTNER_ENDPOINTS", defaultEndpoints)); err != nil {
		return Config{}, err
	}
	if cfg.Tenants, err = parseTenants(text(env, "TENANTS", defaultTenants)); err != nil {
		return Config{}, err
	}
	if cfg.Telemetry, err = parseTelemetry(env); err != nil {
		return Config{}, err
	}
	if cfg.Resilience, err = parseResilience(env); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func parseResilience(env func(string) string) (Resilience, error) {
	resilience := Resilience{
		RedisAddress:             strings.TrimSpace(text(env, "REDIS_ADDRESS", defaultRedisAddress)),
		CacheTTL:                 duration(env, "QUOTE_CACHE_TTL", defaultCacheTTL),
		PartnerTimeout:           duration(env, "PARTNER_TIMEOUT", defaultPartnerTimeout),
		BreakerOpenTimeout:       duration(env, "BREAKER_OPEN_TIMEOUT", defaultBreakerOpenTimeout),
		BreakerConsecutiveErrors: defaultBreakerConsecutiveErrors,
		BreakerHalfOpenRequests:  defaultBreakerHalfOpenRequests,
	}

	if raw := strings.TrimSpace(env("BREAKER_CONSECUTIVE_ERRORS")); raw != "" {
		value, err := strconv.ParseUint(raw, 10, 32)
		if err != nil || value == 0 {
			return Resilience{}, fmt.Errorf("BREAKER_CONSECUTIVE_ERRORS: %q must be a positive integer", raw)
		}
		resilience.BreakerConsecutiveErrors = uint32(value)
	}
	if raw := strings.TrimSpace(env("BREAKER_HALF_OPEN_REQUESTS")); raw != "" {
		value, err := strconv.ParseUint(raw, 10, 32)
		if err != nil || value == 0 {
			return Resilience{}, fmt.Errorf("BREAKER_HALF_OPEN_REQUESTS: %q must be a positive integer", raw)
		}
		resilience.BreakerHalfOpenRequests = uint32(value)
	}

	switch {
	case resilience.RedisAddress == "":
		return Resilience{}, fmt.Errorf("REDIS_ADDRESS cannot be empty")
	case resilience.CacheTTL <= 0:
		return Resilience{}, fmt.Errorf("QUOTE_CACHE_TTL must be greater than zero")
	case resilience.PartnerTimeout <= 0:
		return Resilience{}, fmt.Errorf("PARTNER_TIMEOUT must be greater than zero")
	case resilience.BreakerOpenTimeout <= 0:
		return Resilience{}, fmt.Errorf("BREAKER_OPEN_TIMEOUT must be greater than zero")
	}
	return resilience, nil
}

func duration(env func(string) string, key string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(env(key))
	if raw == "" {
		return fallback
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		return 0
	}
	return value
}

func parseTelemetry(env func(string) string) (Telemetry, error) {
	telemetry := Telemetry{
		ServiceName: text(env, "OTEL_SERVICE_NAME", defaultServiceName),
		Endpoint:    strings.TrimSpace(text(env, "OTEL_EXPORTER_OTLP_ENDPOINT", defaultCollector)),
	}

	switch disabled := strings.ToLower(strings.TrimSpace(env("OTEL_SDK_DISABLED"))); disabled {
	case "", "false":
		telemetry.Enabled = true
	case "true":
		return Telemetry{Enabled: false}, nil
	default:
		return Telemetry{}, fmt.Errorf("OTEL_SDK_DISABLED: %q is neither true nor false", disabled)
	}

	if u, err := url.Parse(telemetry.Endpoint); err != nil || u.Scheme == "" || u.Host == "" {
		return Telemetry{}, fmt.Errorf("OTEL_EXPORTER_OTLP_ENDPOINT: %q is not an absolute URL", telemetry.Endpoint)
	}
	return telemetry, nil
}

func parsePartners(raw string) ([]Partner, error) {
	var partners []Partner
	seen := map[string]bool{}

	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}

		name, endpoint, found := strings.Cut(item, "=")
		name, endpoint = strings.TrimSpace(name), strings.TrimSpace(endpoint)
		if !found || name == "" || endpoint == "" {
			return nil, fmt.Errorf("PARTNER_ENDPOINTS: %q is not in the name=url format", item)
		}

		if u, err := url.Parse(endpoint); err != nil || u.Scheme == "" || u.Host == "" {
			return nil, fmt.Errorf("PARTNER_ENDPOINTS: %q is not an absolute URL", endpoint)
		}
		if seen[name] {
			return nil, fmt.Errorf("PARTNER_ENDPOINTS: partner %q is repeated", name)
		}

		seen[name] = true
		partners = append(partners, Partner{Name: name, BaseURL: strings.TrimRight(endpoint, "/")})
	}

	if len(partners) == 0 {
		return nil, fmt.Errorf("PARTNER_ENDPOINTS: at least one partner is required")
	}
	return partners, nil
}

func parseTenants(raw string) ([]string, error) {
	var tenants []string
	for _, t := range strings.Split(raw, ",") {
		if t = strings.TrimSpace(t); t != "" {
			tenants = append(tenants, t)
		}
	}
	if len(tenants) == 0 {
		return nil, fmt.Errorf("TENANTS: at least one broker is required")
	}
	return tenants, nil
}

func Environment() func(string) string { return os.Getenv }

func text(env func(string) string, key, fallback string) string {
	if v := env(key); v != "" {
		return v
	}
	return fallback
}
