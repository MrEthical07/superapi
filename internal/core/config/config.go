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

type Config struct {
	Env         string
	Profile     string
	ServiceName string
	HTTP        HTTPConfig
	Log         LogConfig
	Auth        AuthConfig
	RateLimit   RateLimitConfig
	Cache       CacheConfig
	Postgres    PostgresConfig
	Redis       RedisConfig
	Metrics     MetricsConfig
	Tracing     TracingConfig
}

type AuthConfig struct {
	Enabled bool
	Mode    string
}

type RateLimitConfig struct {
	Enabled       bool
	FailOpen      bool
	DefaultLimit  int
	DefaultWindow time.Duration
}

type CacheConfig struct {
	Enabled         bool
	FailOpen        bool
	DefaultMaxBytes int
}

// LogConfig holds structured logging configuration.
type LogConfig struct {
	// Level is the minimum log level: debug, info, warn, error, fatal.
	Level string
	// Format is the output format: "json" (default) or "text" (dev console).
	Format string
}

type HTTPConfig struct {
	Addr              string
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	ShutdownTimeout   time.Duration
	MaxHeaderBytes    int
	Middleware        HTTPMiddlewareConfig
}

type HTTPMiddlewareConfig struct {
	RequestIDEnabled       bool
	RecovererEnabled       bool
	MaxBodyBytes           int64
	SecurityHeadersEnabled bool
	RequestTimeout         time.Duration
	AccessLog              AccessLogConfig
	ClientIP               ClientIPConfig
	CORS                   CORSConfig
}

type AccessLogConfig struct {
	Enabled          bool
	SampleRate       float64
	ExcludePaths     []string
	SlowThreshold    time.Duration
	IncludeUserAgent bool
	IncludeRemoteIP  bool
}

type ClientIPConfig struct {
	TrustedProxies []string
}

type CORSConfig struct {
	Enabled             bool
	AllowOrigins        []string
	DenyOrigins         []string
	AllowMethods        []string
	AllowHeaders        []string
	ExposeHeaders       []string
	AllowCredentials    bool
	MaxAge              time.Duration
	AllowPrivateNetwork bool
}

type PostgresConfig struct {
	Enabled            bool
	URL                string
	MaxConns           int32
	MinConns           int32
	ConnMaxLifetime    time.Duration
	ConnMaxIdleTime    time.Duration
	StartupPingTimeout time.Duration
	HealthCheckTimeout time.Duration
}

type RedisConfig struct {
	Enabled            bool
	Addr               string
	Password           string
	DB                 int
	DialTimeout        time.Duration
	ReadTimeout        time.Duration
	WriteTimeout       time.Duration
	PoolSize           int
	MinIdleConns       int
	StartupPingTimeout time.Duration
	HealthCheckTimeout time.Duration
}

type MetricsConfig struct {
	Enabled   bool
	Path      string
	AuthToken string
}

type TracingConfig struct {
	Enabled      bool
	ServiceName  string
	Exporter     string
	OTLPEndpoint string
	Sampler      string
	SampleRatio  float64
	Insecure     bool
}

func Load() (*Config, error) {
	profile := strings.TrimSpace(os.Getenv("APP_PROFILE"))
	restoreProfileDefaults, err := activateProfile(profile)
	if err != nil {
		return nil, err
	}
	defer restoreProfileDefaults()

	env := getenv("APP_ENV", "dev")
	isProdEnv := strings.EqualFold(strings.TrimSpace(env), "prod") || strings.EqualFold(strings.TrimSpace(env), "production")
	securityHeadersDefault := isProdEnv
	tracingInsecureDefault := !isProdEnv

	cfg := &Config{
		Env:         env,
		Profile:     profile,
		ServiceName: getenv("APP_SERVICE_NAME", "api-template"),
		HTTP: HTTPConfig{
			Addr:              getenv("HTTP_ADDR", ":8080"),
			ReadHeaderTimeout: getDuration("HTTP_READ_HEADER_TIMEOUT", 5*time.Second),
			ReadTimeout:       getDuration("HTTP_READ_TIMEOUT", 15*time.Second),
			WriteTimeout:      getDuration("HTTP_WRITE_TIMEOUT", 15*time.Second),
			IdleTimeout:       getDuration("HTTP_IDLE_TIMEOUT", 60*time.Second),
			ShutdownTimeout:   getDuration("HTTP_SHUTDOWN_TIMEOUT", 10*time.Second),
			MaxHeaderBytes:    getInt("HTTP_MAX_HEADER_BYTES", 1<<20), // 1 MiB
			Middleware: HTTPMiddlewareConfig{
				RequestIDEnabled:       getBool("HTTP_MIDDLEWARE_REQUEST_ID_ENABLED", true),
				RecovererEnabled:       getBool("HTTP_MIDDLEWARE_RECOVERER_ENABLED", true),
				MaxBodyBytes:           getInt64("HTTP_MIDDLEWARE_MAX_BODY_BYTES", 1<<20), // 1 MiB
				SecurityHeadersEnabled: getBool("HTTP_MIDDLEWARE_SECURITY_HEADERS_ENABLED", securityHeadersDefault),
				RequestTimeout:         getDuration("HTTP_MIDDLEWARE_REQUEST_TIMEOUT", 0),
				AccessLog: AccessLogConfig{
					Enabled:          getBool("HTTP_MIDDLEWARE_ACCESS_LOG_ENABLED", true),
					SampleRate:       getFloat64("HTTP_MIDDLEWARE_ACCESS_LOG_SAMPLE_RATE", 0.05),
					ExcludePaths:     getCSV("HTTP_MIDDLEWARE_ACCESS_LOG_EXCLUDE_PATHS", []string{"/healthz", "/readyz", "/metrics"}),
					SlowThreshold:    getDuration("HTTP_MIDDLEWARE_ACCESS_LOG_SLOW_THRESHOLD", 2*time.Second),
					IncludeUserAgent: getBool("HTTP_MIDDLEWARE_ACCESS_LOG_INCLUDE_USER_AGENT", false),
					IncludeRemoteIP:  getBool("HTTP_MIDDLEWARE_ACCESS_LOG_INCLUDE_REMOTE_IP", false),
				},
				ClientIP: ClientIPConfig{
					TrustedProxies: getCSV("HTTP_TRUSTED_PROXIES", nil),
				},
				CORS: CORSConfig{
					Enabled:             getBool("HTTP_MIDDLEWARE_CORS_ENABLED", false),
					AllowOrigins:        getCSV("HTTP_MIDDLEWARE_CORS_ALLOW_ORIGINS", nil),
					DenyOrigins:         getCSV("HTTP_MIDDLEWARE_CORS_DENY_ORIGINS", nil),
					AllowMethods:        getCSV("HTTP_MIDDLEWARE_CORS_ALLOW_METHODS", nil),
					AllowHeaders:        getCSV("HTTP_MIDDLEWARE_CORS_ALLOW_HEADERS", nil),
					ExposeHeaders:       getCSV("HTTP_MIDDLEWARE_CORS_EXPOSE_HEADERS", nil),
					AllowCredentials:    getBool("HTTP_MIDDLEWARE_CORS_ALLOW_CREDENTIALS", false),
					MaxAge:              getDuration("HTTP_MIDDLEWARE_CORS_MAX_AGE", 0),
					AllowPrivateNetwork: getBool("HTTP_MIDDLEWARE_CORS_ALLOW_PRIVATE_NETWORK", false),
				},
			},
		},
		Log: LogConfig{
			Level:  getenv("LOG_LEVEL", "info"),
			Format: getenv("LOG_FORMAT", "json"),
		},
		Auth: AuthConfig{
			Enabled: getBool("AUTH_ENABLED", false),
			Mode:    getenv("AUTH_MODE", "hybrid"),
		},
		RateLimit: RateLimitConfig{
			Enabled:       getBool("RATELIMIT_ENABLED", false),
			FailOpen:      getBool("RATELIMIT_FAIL_OPEN", true),
			DefaultLimit:  getInt("RATELIMIT_DEFAULT_LIMIT", 10),
			DefaultWindow: getDuration("RATELIMIT_DEFAULT_WINDOW", time.Minute),
		},
		Cache: CacheConfig{
			Enabled:         getBool("CACHE_ENABLED", false),
			FailOpen:        getBool("CACHE_FAIL_OPEN", true),
			DefaultMaxBytes: getInt("CACHE_DEFAULT_MAX_BYTES", 256*1024),
		},
		Postgres: PostgresConfig{
			Enabled:            getBool("POSTGRES_ENABLED", false),
			URL:                getenv("POSTGRES_URL", ""),
			MaxConns:           int32(getInt("POSTGRES_MAX_CONNS", 10)),
			MinConns:           int32(getInt("POSTGRES_MIN_CONNS", 0)),
			ConnMaxLifetime:    getDuration("POSTGRES_CONN_MAX_LIFETIME", 30*time.Minute),
			ConnMaxIdleTime:    getDuration("POSTGRES_CONN_MAX_IDLE_TIME", 5*time.Minute),
			StartupPingTimeout: getDuration("POSTGRES_STARTUP_PING_TIMEOUT", 3*time.Second),
			HealthCheckTimeout: getDuration("POSTGRES_HEALTH_CHECK_TIMEOUT", 1*time.Second),
		},
		Redis: RedisConfig{
			Enabled:            getBool("REDIS_ENABLED", false),
			Addr:               getenv("REDIS_ADDR", "127.0.0.1:6379"),
			Password:           getenv("REDIS_PASSWORD", ""),
			DB:                 getInt("REDIS_DB", 0),
			DialTimeout:        getDuration("REDIS_DIAL_TIMEOUT", 2*time.Second),
			ReadTimeout:        getDuration("REDIS_READ_TIMEOUT", 2*time.Second),
			WriteTimeout:       getDuration("REDIS_WRITE_TIMEOUT", 2*time.Second),
			PoolSize:           getInt("REDIS_POOL_SIZE", 10),
			MinIdleConns:       getInt("REDIS_MIN_IDLE_CONNS", 0),
			StartupPingTimeout: getDuration("REDIS_STARTUP_PING_TIMEOUT", 3*time.Second),
			HealthCheckTimeout: getDuration("REDIS_HEALTH_CHECK_TIMEOUT", 1*time.Second),
		},
		Metrics: MetricsConfig{
			Enabled:   getBool("METRICS_ENABLED", true),
			Path:      getenv("METRICS_PATH", "/metrics"),
			AuthToken: getenv("METRICS_AUTH_TOKEN", ""),
		},
		Tracing: TracingConfig{
			Enabled:      getBool("TRACING_ENABLED", false),
			ServiceName:  getenv("TRACING_SERVICE_NAME", ""),
			Exporter:     getenv("TRACING_EXPORTER", "otlpgrpc"),
			OTLPEndpoint: getenv("TRACING_OTLP_ENDPOINT", "localhost:4317"),
			Sampler:      getenv("TRACING_SAMPLER", "traceidratio"),
			SampleRatio:  getFloat64("TRACING_SAMPLE_RATIO", 0.05),
			Insecure:     getBool("TRACING_INSECURE", tracingInsecureDefault),
		},
	}

	if cfg.Tracing.ServiceName == "" {
		cfg.Tracing.ServiceName = cfg.ServiceName
	}

	return cfg, nil
}

func (c *Config) Lint() error {
	if profile := strings.TrimSpace(os.Getenv("APP_PROFILE")); profile != "" {
		if _, err := resolveProfileDefaults(profile); err != nil {
			return err
		}
	}

	if c.ServiceName == "" {
		return errors.New("service name cannot be empty")
	}
	if strings.TrimSpace(c.HTTP.Addr) == "" {
		return errors.New("http addr cannot be empty")
	}
	if c.HTTP.ReadHeaderTimeout <= 0 {
		return fmt.Errorf("http read header timeout must be > 0")
	}
	if c.HTTP.ReadTimeout <= 0 || c.HTTP.WriteTimeout <= 0 {
		return fmt.Errorf("http read/write timeout must be > 0")
	}
	if c.HTTP.IdleTimeout <= 0 {
		return fmt.Errorf("http idle timeout must be > 0")
	}
	if c.HTTP.ShutdownTimeout <= 0 {
		return fmt.Errorf("http shutdown timeout must be > 0")
	}
	if c.HTTP.MaxHeaderBytes < 4096 {
		return fmt.Errorf("http max header bytes too low: %d", c.HTTP.MaxHeaderBytes)
	}
	if c.HTTP.Middleware.MaxBodyBytes < 0 {
		return fmt.Errorf("http middleware max body bytes must be >= 0")
	}
	if c.HTTP.Middleware.RequestTimeout < 0 {
		return fmt.Errorf("http middleware request timeout must be >= 0")
	}
	if c.HTTP.Middleware.RequestTimeout > 0 && c.HTTP.Middleware.RequestTimeout > c.HTTP.WriteTimeout {
		return fmt.Errorf("http middleware request timeout (%s) cannot exceed http write timeout (%s)", c.HTTP.Middleware.RequestTimeout, c.HTTP.WriteTimeout)
	}
	if c.HTTP.Middleware.AccessLog.SampleRate < 0 || c.HTTP.Middleware.AccessLog.SampleRate > 1 {
		return fmt.Errorf("http middleware access log sample rate must be in [0,1]")
	}
	if c.HTTP.Middleware.AccessLog.SlowThreshold < 0 {
		return fmt.Errorf("http middleware access log slow threshold must be >= 0")
	}
	if c.HTTP.Middleware.CORS.MaxAge < 0 {
		return fmt.Errorf("http middleware cors max age must be >= 0")
	}
	if c.HTTP.Middleware.CORS.AllowCredentials && containsWildcard(c.HTTP.Middleware.CORS.AllowOrigins) {
		return fmt.Errorf("http middleware cors allow credentials cannot be used with wildcard allow origins")
	}
	if err := validateOrigins("http middleware cors allow origins", c.HTTP.Middleware.CORS.AllowOrigins); err != nil {
		return err
	}
	if err := validateOrigins("http middleware cors deny origins", c.HTTP.Middleware.CORS.DenyOrigins); err != nil {
		return err
	}
	if err := validateTokens("http middleware cors allow methods", c.HTTP.Middleware.CORS.AllowMethods); err != nil {
		return err
	}
	if err := validateTokens("http middleware cors allow headers", c.HTTP.Middleware.CORS.AllowHeaders); err != nil {
		return err
	}
	if err := validateTokens("http middleware cors expose headers", c.HTTP.Middleware.CORS.ExposeHeaders); err != nil {
		return err
	}
	if err := validateTrustedProxies(c.HTTP.Middleware.ClientIP.TrustedProxies); err != nil {
		return err
	}

	switch strings.ToLower(strings.TrimSpace(c.Auth.Mode)) {
	case "jwt_only", "jwt-only", "jwtonly", "hybrid", "strict", "":
		// valid
	default:
		return fmt.Errorf("invalid auth mode: %q (valid: jwt_only, hybrid, strict)", c.Auth.Mode)
	}
	if c.Auth.Enabled && !c.Redis.Enabled {
		return fmt.Errorf("auth enabled requires redis enabled")
	}
	if c.Auth.Enabled && !c.Postgres.Enabled {
		return fmt.Errorf("auth enabled requires postgres enabled")
	}
	if c.RateLimit.Enabled && !c.Redis.Enabled {
		return fmt.Errorf("ratelimit enabled requires redis enabled")
	}
	if c.Cache.Enabled && !c.Redis.Enabled {
		return fmt.Errorf("cache enabled requires redis enabled")
	}
	if c.RateLimit.DefaultLimit <= 0 {
		return fmt.Errorf("ratelimit default limit must be > 0")
	}
	if c.RateLimit.DefaultWindow <= 0 {
		return fmt.Errorf("ratelimit default window must be > 0")
	}
	if c.Cache.DefaultMaxBytes <= 0 {
		return fmt.Errorf("cache default max bytes must be > 0")
	}

	for _, p := range c.HTTP.Middleware.AccessLog.ExcludePaths {
		if p == "" {
			return fmt.Errorf("http middleware access log exclude path cannot be empty")
		}
		if p[0] != '/' {
			return fmt.Errorf("http middleware access log exclude path must start with '/': %q", p)
		}
	}

	if c.Postgres.Enabled && c.Postgres.URL == "" {
		return fmt.Errorf("postgres url cannot be empty when enabled")
	}
	if c.Postgres.MaxConns <= 0 {
		return fmt.Errorf("postgres max conns must be > 0")
	}
	if c.Postgres.MinConns < 0 {
		return fmt.Errorf("postgres min conns must be >= 0")
	}
	if c.Postgres.MinConns > c.Postgres.MaxConns {
		return fmt.Errorf("postgres min conns cannot exceed max conns")
	}
	if c.Postgres.ConnMaxLifetime < 0 {
		return fmt.Errorf("postgres conn max lifetime must be >= 0")
	}
	if c.Postgres.ConnMaxIdleTime < 0 {
		return fmt.Errorf("postgres conn max idle time must be >= 0")
	}
	if c.Postgres.StartupPingTimeout <= 0 {
		return fmt.Errorf("postgres startup ping timeout must be > 0")
	}
	if c.Postgres.HealthCheckTimeout <= 0 {
		return fmt.Errorf("postgres health check timeout must be > 0")
	}

	if c.Redis.Enabled && c.Redis.Addr == "" {
		return fmt.Errorf("redis addr cannot be empty when enabled")
	}
	if c.Redis.DB < 0 {
		return fmt.Errorf("redis db must be >= 0")
	}
	if c.Redis.PoolSize <= 0 {
		return fmt.Errorf("redis pool size must be > 0")
	}
	if c.Redis.MinIdleConns < 0 {
		return fmt.Errorf("redis min idle conns must be >= 0")
	}
	if c.Redis.MinIdleConns > c.Redis.PoolSize {
		return fmt.Errorf("redis min idle conns cannot exceed pool size")
	}
	if c.Redis.DialTimeout <= 0 {
		return fmt.Errorf("redis dial timeout must be > 0")
	}
	if c.Redis.ReadTimeout <= 0 {
		return fmt.Errorf("redis read timeout must be > 0")
	}
	if c.Redis.WriteTimeout <= 0 {
		return fmt.Errorf("redis write timeout must be > 0")
	}
	if c.Redis.StartupPingTimeout <= 0 {
		return fmt.Errorf("redis startup ping timeout must be > 0")
	}
	if c.Redis.HealthCheckTimeout <= 0 {
		return fmt.Errorf("redis health check timeout must be > 0")
	}
	if c.Metrics.Path == "" {
		return fmt.Errorf("metrics path cannot be empty")
	}
	if c.Metrics.Path[0] != '/' {
		return fmt.Errorf("metrics path must start with '/'")
	}
	if strings.EqualFold(strings.TrimSpace(c.Env), "prod") || strings.EqualFold(strings.TrimSpace(c.Env), "production") {
		if c.Metrics.Enabled && strings.TrimSpace(c.Metrics.AuthToken) == "" {
			return fmt.Errorf("metrics auth token cannot be empty in prod when metrics is enabled")
		}
	}
	if c.Tracing.Enabled {
		if c.Tracing.ServiceName == "" {
			return fmt.Errorf("tracing service name cannot be empty when enabled")
		}
		if c.Tracing.OTLPEndpoint == "" {
			return fmt.Errorf("tracing otlp endpoint cannot be empty when enabled")
		}
		switch c.Tracing.Exporter {
		case "otlpgrpc":
		default:
			return fmt.Errorf("invalid tracing exporter: %q (valid: otlpgrpc)", c.Tracing.Exporter)
		}
		switch c.Tracing.Sampler {
		case "always_on", "always_off", "traceidratio":
		default:
			return fmt.Errorf("invalid tracing sampler: %q (valid: always_on, always_off, traceidratio)", c.Tracing.Sampler)
		}
		if c.Tracing.SampleRatio < 0 || c.Tracing.SampleRatio > 1 {
			return fmt.Errorf("tracing sample ratio must be in [0,1]")
		}
	}

	if v, ok := os.LookupEnv("HTTP_MIDDLEWARE_REQUEST_ID_ENABLED"); ok {
		if _, err := strconv.ParseBool(v); err != nil {
			return fmt.Errorf("invalid HTTP_MIDDLEWARE_REQUEST_ID_ENABLED: %q", v)
		}
	}
	if v, ok := os.LookupEnv("HTTP_MIDDLEWARE_RECOVERER_ENABLED"); ok {
		if _, err := strconv.ParseBool(v); err != nil {
			return fmt.Errorf("invalid HTTP_MIDDLEWARE_RECOVERER_ENABLED: %q", v)
		}
	}
	if v, ok := os.LookupEnv("HTTP_MIDDLEWARE_SECURITY_HEADERS_ENABLED"); ok {
		if _, err := strconv.ParseBool(v); err != nil {
			return fmt.Errorf("invalid HTTP_MIDDLEWARE_SECURITY_HEADERS_ENABLED: %q", v)
		}
	}
	if v, ok := os.LookupEnv("HTTP_MIDDLEWARE_REQUEST_TIMEOUT"); ok {
		d, err := time.ParseDuration(v)
		if err != nil {
			return fmt.Errorf("invalid HTTP_MIDDLEWARE_REQUEST_TIMEOUT: %q", v)
		}
		if d < 0 {
			return fmt.Errorf("http middleware request timeout must be >= 0")
		}
	}
	if v, ok := os.LookupEnv("HTTP_ADDR"); ok {
		if strings.TrimSpace(v) == "" {
			return fmt.Errorf("http addr cannot be empty")
		}
	}
	if err := lintBoolEnv("HTTP_MIDDLEWARE_ACCESS_LOG_ENABLED"); err != nil {
		return err
	}
	if err := lintBoolEnv("HTTP_MIDDLEWARE_CORS_ENABLED"); err != nil {
		return err
	}
	if err := lintBoolEnv("HTTP_MIDDLEWARE_CORS_ALLOW_CREDENTIALS"); err != nil {
		return err
	}
	if err := lintBoolEnv("HTTP_MIDDLEWARE_CORS_ALLOW_PRIVATE_NETWORK"); err != nil {
		return err
	}
	if err := lintBoolEnv("AUTH_ENABLED"); err != nil {
		return err
	}
	if err := lintBoolEnv("RATELIMIT_ENABLED"); err != nil {
		return err
	}
	if err := lintBoolEnv("RATELIMIT_FAIL_OPEN"); err != nil {
		return err
	}
	if err := lintIntEnv("RATELIMIT_DEFAULT_LIMIT"); err != nil {
		return err
	}
	if err := lintDurationEnv("RATELIMIT_DEFAULT_WINDOW"); err != nil {
		return err
	}
	if err := lintIntEnv("HTTP_MAX_HEADER_BYTES"); err != nil {
		return err
	}
	if err := lintInt64Env("HTTP_MIDDLEWARE_MAX_BODY_BYTES"); err != nil {
		return err
	}
	if err := lintDurationEnv("HTTP_READ_HEADER_TIMEOUT"); err != nil {
		return err
	}
	if err := lintDurationEnv("HTTP_READ_TIMEOUT"); err != nil {
		return err
	}
	if err := lintDurationEnv("HTTP_WRITE_TIMEOUT"); err != nil {
		return err
	}
	if err := lintDurationEnv("HTTP_IDLE_TIMEOUT"); err != nil {
		return err
	}
	if err := lintDurationEnv("HTTP_SHUTDOWN_TIMEOUT"); err != nil {
		return err
	}
	if err := lintDurationEnv("HTTP_MIDDLEWARE_CORS_MAX_AGE"); err != nil {
		return err
	}
	if err := lintFloat64Env("HTTP_MIDDLEWARE_ACCESS_LOG_SAMPLE_RATE"); err != nil {
		return err
	}
	if err := lintDurationEnv("HTTP_MIDDLEWARE_ACCESS_LOG_SLOW_THRESHOLD"); err != nil {
		return err
	}
	if err := lintBoolEnv("HTTP_MIDDLEWARE_ACCESS_LOG_INCLUDE_USER_AGENT"); err != nil {
		return err
	}
	if err := lintBoolEnv("HTTP_MIDDLEWARE_ACCESS_LOG_INCLUDE_REMOTE_IP"); err != nil {
		return err
	}

	if err := lintBoolEnv("POSTGRES_ENABLED"); err != nil {
		return err
	}
	if err := lintIntEnv("POSTGRES_MAX_CONNS"); err != nil {
		return err
	}
	if err := lintIntEnv("POSTGRES_MIN_CONNS"); err != nil {
		return err
	}
	if err := lintDurationEnv("POSTGRES_CONN_MAX_LIFETIME"); err != nil {
		return err
	}
	if err := lintDurationEnv("POSTGRES_CONN_MAX_IDLE_TIME"); err != nil {
		return err
	}
	if err := lintDurationEnv("POSTGRES_STARTUP_PING_TIMEOUT"); err != nil {
		return err
	}
	if err := lintDurationEnv("POSTGRES_HEALTH_CHECK_TIMEOUT"); err != nil {
		return err
	}

	if err := lintBoolEnv("REDIS_ENABLED"); err != nil {
		return err
	}
	if err := lintIntEnv("REDIS_DB"); err != nil {
		return err
	}
	if err := lintIntEnv("REDIS_POOL_SIZE"); err != nil {
		return err
	}
	if err := lintIntEnv("REDIS_MIN_IDLE_CONNS"); err != nil {
		return err
	}
	if err := lintDurationEnv("REDIS_DIAL_TIMEOUT"); err != nil {
		return err
	}
	if err := lintDurationEnv("REDIS_READ_TIMEOUT"); err != nil {
		return err
	}
	if err := lintDurationEnv("REDIS_WRITE_TIMEOUT"); err != nil {
		return err
	}
	if err := lintDurationEnv("REDIS_STARTUP_PING_TIMEOUT"); err != nil {
		return err
	}
	if err := lintDurationEnv("REDIS_HEALTH_CHECK_TIMEOUT"); err != nil {
		return err
	}
	if err := lintBoolEnv("METRICS_ENABLED"); err != nil {
		return err
	}
	if err := lintBoolEnv("TRACING_ENABLED"); err != nil {
		return err
	}
	if err := lintFloat64Env("TRACING_SAMPLE_RATIO"); err != nil {
		return err
	}
	if err := lintBoolEnv("TRACING_INSECURE"); err != nil {
		return err
	}

	switch c.Log.Level {
	case "debug", "info", "warn", "warning", "error", "fatal", "":
		// valid
	default:
		return fmt.Errorf("invalid log level: %q (valid: debug, info, warn, error, fatal)", c.Log.Level)
	}

	switch c.Log.Format {
	case "json", "text", "":
		// valid
	default:
		return fmt.Errorf("invalid log format: %q (valid: json, text)", c.Log.Format)
	}

	return nil
}

func getenv(key, fallback string) string {
	v := os.Getenv(key)
	if v == "" {
		if profileValue, ok := profileDefaultValue(key); ok {
			v = profileValue
		}
	}
	if v == "" {
		return fallback
	}
	return v
}

func getInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		if profileValue, ok := profileDefaultValue(key); ok {
			v = profileValue
		}
	}
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func getInt64(key string, fallback int64) int64 {
	v := os.Getenv(key)
	if v == "" {
		if profileValue, ok := profileDefaultValue(key); ok {
			v = profileValue
		}
	}
	if v == "" {
		return fallback
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return fallback
	}
	return n
}

func getBool(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		if profileValue, ok := profileDefaultValue(key); ok {
			v = profileValue
		}
	}
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}

func getDuration(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		if profileValue, ok := profileDefaultValue(key); ok {
			v = profileValue
		}
	}
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return d
}

func getFloat64(key string, fallback float64) float64 {
	v := os.Getenv(key)
	if v == "" {
		if profileValue, ok := profileDefaultValue(key); ok {
			v = profileValue
		}
	}
	if v == "" {
		return fallback
	}
	n, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return fallback
	}
	return n
}

func getCSV(key string, fallback []string) []string {
	v := os.Getenv(key)
	if strings.TrimSpace(v) == "" {
		if profileValue, ok := profileDefaultValue(key); ok {
			v = profileValue
		}
	}
	if strings.TrimSpace(v) == "" {
		return fallback
	}

	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	if len(out) == 0 {
		return fallback
	}
	return out
}

func lintBoolEnv(key string) error {
	v, ok := os.LookupEnv(key)
	if !ok {
		return nil
	}
	if _, err := strconv.ParseBool(v); err != nil {
		return fmt.Errorf("invalid %s: %q", key, v)
	}
	return nil
}

func lintIntEnv(key string) error {
	v, ok := os.LookupEnv(key)
	if !ok {
		return nil
	}
	if _, err := strconv.Atoi(v); err != nil {
		return fmt.Errorf("invalid %s: %q", key, v)
	}
	return nil
}

func lintDurationEnv(key string) error {
	v, ok := os.LookupEnv(key)
	if !ok {
		return nil
	}
	if _, err := time.ParseDuration(v); err != nil {
		return fmt.Errorf("invalid %s: %q", key, v)
	}
	return nil
}

func lintFloat64Env(key string) error {
	v, ok := os.LookupEnv(key)
	if !ok {
		return nil
	}
	if _, err := strconv.ParseFloat(v, 64); err != nil {
		return fmt.Errorf("invalid %s: %q", key, v)
	}
	return nil
}

func lintInt64Env(key string) error {
	v, ok := os.LookupEnv(key)
	if !ok {
		return nil
	}
	if _, err := strconv.ParseInt(v, 10, 64); err != nil {
		return fmt.Errorf("invalid %s: %q", key, v)
	}
	return nil
}

func containsWildcard(items []string) bool {
	for _, item := range items {
		if strings.TrimSpace(item) == "*" {
			return true
		}
	}
	return false
}

func validateOrigins(label string, origins []string) error {
	for _, origin := range origins {
		trimmed := strings.TrimSpace(origin)
		if trimmed == "" {
			continue
		}
		if trimmed == "*" || strings.EqualFold(trimmed, "null") {
			continue
		}

		u, err := url.Parse(trimmed)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return fmt.Errorf("%s contains invalid origin: %q", label, origin)
		}
		if u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
			return fmt.Errorf("%s origin must not include path, query, or fragment: %q", label, origin)
		}
	}
	return nil
}

func validateTokens(label string, items []string) error {
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		if strings.ContainsAny(trimmed, " \t\r\n") || strings.Contains(trimmed, ",") {
			return fmt.Errorf("%s contains invalid token: %q", label, item)
		}
	}
	return nil
}

func validateTrustedProxies(items []string) error {
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		if strings.Contains(trimmed, "/") {
			if _, _, err := net.ParseCIDR(trimmed); err != nil {
				return fmt.Errorf("http trusted proxy is invalid cidr: %q", item)
			}
			continue
		}
		if ip := net.ParseIP(trimmed); ip == nil {
			return fmt.Errorf("http trusted proxy is invalid ip: %q", item)
		}
	}
	return nil
}
