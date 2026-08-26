package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultHTTPAddr   = ":8080"
	DefaultGossipAddr = ":7946"
)

// Config contains all process configuration. Every field is immutable after
// Load returns, which keeps configuration out of package-level mutable state.
type Config struct {
	NodeID              string
	BootID              string
	ClusterID           string
	HTTPAddr            string
	HTTPAdvertiseAddr   string
	GossipAddr          string
	GossipAdvertiseAddr string
	GossipSeeds         []string
	GossipSecret        string
	AllowedOrigins      []string
	LogLevel            string
	DemoMode            bool
	DemoSeed            bool
	DemoInstanceCount   int
	DemoRandomSeed      int64

	ShardCount          int
	EventCapacity       int
	TickInterval        time.Duration
	DelayedAfter        time.Duration
	LeaseTTL            time.Duration
	EvictedDisplayTTL   time.Duration
	TombstoneTTL        time.Duration
	GossipInterval      time.Duration
	AntiEntropyInterval time.Duration
	SuspicionTimeout    time.Duration
	DeadTimeout         time.Duration
	MaxClockSkew        time.Duration
	Fanout              int
	MaxUDPBytes         int
	MaxHTTPBodyBytes    int64
	RateLimitPerMinute  int
}

func Load() (Config, error) {
	var parseErrors []error
	parseInt := func(key string, fallback int) int {
		value, err := intEnv(key, fallback)
		if err != nil {
			parseErrors = append(parseErrors, err)
		}
		return value
	}
	parseDuration := func(key string, fallback time.Duration) time.Duration {
		value, err := durationEnv(key, fallback)
		if err != nil {
			parseErrors = append(parseErrors, err)
		}
		return value
	}
	cfg := Config{
		NodeID:              env("NODE_ID", "node-1"),
		BootID:              newID("boot"),
		ClusterID:           env("CLUSTER_ID", "mini-eureka"),
		HTTPAddr:            env("HTTP_ADDR", DefaultHTTPAddr),
		HTTPAdvertiseAddr:   env("HTTP_ADVERTISE_ADDR", "http://127.0.0.1:8080"),
		GossipAddr:          env("GOSSIP_ADDR", DefaultGossipAddr),
		GossipAdvertiseAddr: env("GOSSIP_ADVERTISE_ADDR", "127.0.0.1:7946"),
		GossipSecret:        os.Getenv("GOSSIP_SECRET"),
		LogLevel:            strings.ToLower(env("LOG_LEVEL", "info")),
		ShardCount:          parseInt("REGISTRY_SHARDS", 64),
		EventCapacity:       parseInt("EVENT_CAPACITY", 2048),
		TickInterval:        parseDuration("TTL_TICK", time.Second),
		DelayedAfter:        parseDuration("LEASE_DELAYED_AFTER", 15*time.Second),
		LeaseTTL:            parseDuration("LEASE_TTL", 30*time.Second),
		EvictedDisplayTTL:   parseDuration("EVICTED_DISPLAY_TTL", 60*time.Second),
		TombstoneTTL:        parseDuration("TOMBSTONE_TTL", 5*time.Minute),
		GossipInterval:      parseDuration("GOSSIP_INTERVAL", time.Second),
		AntiEntropyInterval: parseDuration("ANTI_ENTROPY_INTERVAL", 10*time.Second),
		SuspicionTimeout:    parseDuration("SUSPICION_TIMEOUT", 5*time.Second),
		DeadTimeout:         parseDuration("DEAD_TIMEOUT", 15*time.Second),
		MaxClockSkew:        parseDuration("MAX_CLOCK_SKEW", 30*time.Second),
		Fanout:              parseInt("GOSSIP_FANOUT", 3),
		MaxUDPBytes:         parseInt("GOSSIP_MAX_UDP_BYTES", 1200),
		MaxHTTPBodyBytes:    int64(parseInt("HTTP_MAX_BODY_BYTES", 64<<10)),
		RateLimitPerMinute:  parseInt("RATE_LIMIT_PER_MINUTE", 10000),
		DemoInstanceCount:   parseInt("DEMO_INSTANCE_COUNT", 120),
	}
	cfg.GossipSeeds = splitCSV(os.Getenv("GOSSIP_SEEDS"))
	cfg.AllowedOrigins = splitCSV(os.Getenv("ALLOWED_ORIGINS"))
	if len(parseErrors) > 0 {
		return Config{}, errors.Join(parseErrors...)
	}
	var err error
	cfg.DemoMode, err = boolEnv("DEMO_MODE", false)
	if err != nil {
		return Config{}, err
	}
	cfg.DemoSeed, err = boolEnv("DEMO_SEED", false)
	if err != nil {
		return Config{}, err
	}
	cfg.DemoRandomSeed, err = int64Env("DEMO_RANDOM_SEED", 1)
	if err != nil {
		return Config{}, err
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	var errs []error
	if !validName(c.NodeID, 128) {
		errs = append(errs, errors.New("NODE_ID must be 1..128 safe characters"))
	}
	if !validName(c.ClusterID, 128) {
		errs = append(errs, errors.New("CLUSTER_ID must be 1..128 safe characters"))
	}
	if c.GossipSecret == "" {
		errs = append(errs, errors.New("GOSSIP_SECRET is required"))
	}
	if c.ShardCount <= 0 || c.ShardCount&(c.ShardCount-1) != 0 {
		errs = append(errs, errors.New("REGISTRY_SHARDS must be a positive power of two"))
	}
	if c.EventCapacity < 64 {
		errs = append(errs, errors.New("EVENT_CAPACITY must be at least 64"))
	}
	if c.TickInterval <= 0 || c.DelayedAfter <= 0 || c.LeaseTTL <= c.DelayedAfter {
		errs = append(errs, errors.New("lease durations must satisfy 0 < tick, delayed < ttl"))
	}
	if c.TombstoneTTL < c.EvictedDisplayTTL {
		errs = append(errs, errors.New("TOMBSTONE_TTL must not be shorter than EVICTED_DISPLAY_TTL"))
	}
	if c.Fanout < 1 || c.Fanout > 32 {
		errs = append(errs, errors.New("GOSSIP_FANOUT must be 1..32"))
	}
	if c.DemoInstanceCount < 0 || c.DemoInstanceCount > 20000 {
		errs = append(errs, errors.New("DEMO_INSTANCE_COUNT must be 0..20000"))
	}
	if c.MaxUDPBytes < 512 || c.MaxUDPBytes > 1200 {
		errs = append(errs, errors.New("GOSSIP_MAX_UDP_BYTES must be 512..1200"))
	}
	if err := validateHostPort(c.GossipAddr, false); err != nil {
		errs = append(errs, fmt.Errorf("GOSSIP_ADDR: %w", err))
	}
	if err := validateHostPort(c.GossipAdvertiseAddr, true); err != nil {
		errs = append(errs, fmt.Errorf("GOSSIP_ADVERTISE_ADDR: %w", err))
	}
	if u, err := url.Parse(c.HTTPAdvertiseAddr); err != nil || u.Scheme == "" || u.Host == "" {
		errs = append(errs, errors.New("HTTP_ADVERTISE_ADDR must be an absolute URL"))
	}
	for _, seed := range c.GossipSeeds {
		if err := validateHostPort(seed, true); err != nil {
			errs = append(errs, fmt.Errorf("GOSSIP_SEEDS entry %q: %w", seed, err))
		}
	}
	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		errs = append(errs, errors.New("LOG_LEVEL must be debug, info, warn, or error"))
	}
	return errors.Join(errs...)
}

func validateHostPort(value string, requireHost bool) error {
	host, portText, err := net.SplitHostPort(value)
	if err != nil {
		return err
	}
	if requireHost && strings.TrimSpace(host) == "" {
		return errors.New("host is required")
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return errors.New("port must be 1..65535")
	}
	return nil
}

func env(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func splitCSV(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return []string{}
	}
	values := strings.Split(raw, ",")
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func intEnv(key string, fallback int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return value, nil
}

func int64Env(key string, fallback int64) (int64, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return value, nil
}

func boolEnv(key string, fallback bool) (bool, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("%s: %w", key, err)
	}
	return value, nil
}

func durationEnv(key string, fallback time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return value, nil
}

func validName(value string, max int) bool {
	if len(value) == 0 || len(value) > max {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || strings.ContainsRune("-_.", r) {
			continue
		}
		return false
	}
	return true
}
