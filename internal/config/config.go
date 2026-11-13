package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ilyakaznacheev/cleanenv"
)

const (
	LocalEnv = "local"
	DevEnv   = "dev"
	ProdEnv  = "prod"
)

// Config is the root configuration loaded from a YAML file and overridden by
// environment variables. Secrets (passwords, tokens) are ENV-only: their
// yaml tag is "-" so they can never be committed in plain text.
type Config struct {
	Env       string    `yaml:"env" env:"ENV" env-default:"local"`
	Server    Server    `yaml:"server"`
	DB        DB        `yaml:"mysql"`
	Auth      Auth      `yaml:"auth"`
	Redis     Redis     `yaml:"redis"`
	Docs      Docs      `yaml:"docs"`
	RateLimit RateLimit `yaml:"rate_limit"    env-prefix:"RATE_LIMIT_"`
}

// Server holds HTTP server tuning parameters.
type Server struct {
	Port            int           `yaml:"port"             env:"APP_PORT"`
	ReadTimeout     time.Duration `yaml:"read_timeout"     env:"SERVER_READ_TIMEOUT"`
	WriteTimeout    time.Duration `yaml:"write_timeout"    env:"SERVER_WRITE_TIMEOUT"`
	ShutdownTimeout time.Duration `yaml:"shutdown_timeout" env:"SERVER_SHUTDOWN_TIMEOUT"`
}

// Database holds MySQL connection parameters.
// Password is read exclusively from DB_PASSWORD env variable.
type DB struct {
	Host           string        `yaml:"host"              env:"DB_HOST"`
	Port           int           `yaml:"port"              env:"DB_PORT"`
	Name           string        `yaml:"name"              env:"DB_NAME"`
	User           string        `yaml:"user"              env:"DB_USER"`
	Password       string        `yaml:"-"                 env:"DB_PASSWORD"`
	MaxRequestTime time.Duration `yaml:"max_request_time"  env:"DB_POOL_MAX_REQUEST_TIME"`
	Pool           DBPool        `yaml:"pool"`
	Retry          DBRetry       `yaml:"retry"`
}

// DBPool holds connection pool limits and the maximum connection lifetime.
type DBPool struct {
	MaxOpenConns    int           `yaml:"max_open_conns"    env:"DB_POOL_MAX_OPEN_CONNS"`
	MaxIdleConns    int           `yaml:"max_idle_conns"    env:"DB_POOL_MAX_IDLE_CONNS"`
	ConnMaxLifetime time.Duration `yaml:"conn_max_lifetime" env:"DB_POOL_CONN_MAX_LIFETIME"`
}

// DBRetry holds the maximum number of connection attempts and the delay between them.
type DBRetry struct {
	MaxAttempts int           `yaml:"max_attempts" env:"DB_RETRY_MAX_ATTEMPTS"`
	Delay       time.Duration `yaml:"delay"        env:"DB_RETRY_DELAY"`
}

// Auth holds authorization parameters.
type Auth struct {
	JWTSecret     string        `yaml:"-"              env:"JWT_SECRET"`
	JWTTtl        time.Duration `yaml:"jwt_ttl"        env:"JWT_TTL"`
	LoginLimit    RateLimit     `yaml:"login_limit"    env-prefix:"AUTH_LOGIN_"`
	RegisterLimit RateLimit     `yaml:"register_limit" env-prefix:"AUTH_REGISTER_"`
}

// Redis holds redis parameters.
type Redis struct {
	Addr           string        `yaml:"addr"             env:"REDIS_ADDR"`
	DB             int           `yaml:"db"               env:"REDIS_DB"`
	Password       string        `yaml:"-"                env:"REDIS_PASSWORD"`
	MaxRequestTime time.Duration `yaml:"max_request_time" env:"REDIS_MAX_REQUEST_TIME"`
	PoolSize       int           `yaml:"pool_size"        env:"REDIS_POOL_SIZE"`
	CacheTTL       time.Duration `yaml:"cache_ttl"        env:"REDIS_CACHE_TTL"`
	LoadTimeout    time.Duration `yaml:"load_timeout"     env:"REDIS_LOAD_TIMEOUT"`
	Retry          RedisRetry    `yaml:"retry"`
}

// RedisRetry holds the maximum number of connection attempts and the delay between them.
type RedisRetry struct {
	MaxAttempts int           `yaml:"max_attempts" env:"REDIS_RETRY_MAX_ATTEMPTS"`
	Delay       time.Duration `yaml:"delay"        env:"REDIS_RETRY_DELAY"`
}

// Docs holds the ability to display swaggert for API.
type Docs struct {
	Enabled bool `yaml:"enabled" env:"DOCS_ENABLED"`
}

// RateLimit holds the settings for rate limiter.
type RateLimit struct {
	Interval   time.Duration `yaml:"interval"    env:"INTERVAL"`
	Burst      int           `yaml:"burst"       env:"BURST"`
	MaxEntries int           `yaml:"max_entries" env:"MAX_ENTRIES"`
}

func LoadConfig(path string) (*Config, error) {
	var cfg Config
	if path == "" {
		return &cfg, errors.New("Config file is not set")
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return &cfg, fmt.Errorf("config file %s does not exist: %w", path, err)
	}
	err := cleanenv.ReadConfig(path, &cfg)
	if err != nil {
		return &cfg, fmt.Errorf("cannot read file %s from environment variables: %w", path, err)
	}
	if err = cfg.isValid(); err != nil {
		return &cfg, fmt.Errorf("config file %s does not valid: %w", path, err)
	}

	return &cfg, nil
}

func (c *Config) isValid() error {
	if c.Env != LocalEnv && c.Env != DevEnv && c.Env != ProdEnv {
		return fmt.Errorf("wrong environment")
	}
	if err := c.Server.isValid(); err != nil {
		return fmt.Errorf("wrong server settings: %w", err)
	}
	if err := c.DB.isValid(); err != nil {
		return fmt.Errorf("wrong DB settings: %w", err)
	}
	if err := c.Auth.isValid(); err != nil {
		return fmt.Errorf("wrong auth settings: %w", err)
	}
	if err := c.Redis.isValid(); err != nil {
		return fmt.Errorf("wrong redis settings: %w", err)
	}
	if err := c.RateLimit.isValid(); err != nil {
		return fmt.Errorf("wrong RateLimit settings: %w", err)
	}
	return nil
}

func (s *Server) isValid() error {
	if s.Port <= 0 || s.Port > 65535 {
		return fmt.Errorf("server port must be between 1 and 65535")
	}
	if s.ReadTimeout <= 0 {
		return fmt.Errorf("wrong server read timeout")
	}
	if s.WriteTimeout <= 0 {
		return fmt.Errorf("wrong server write timeout")
	}
	if s.ShutdownTimeout <= 0 {
		return fmt.Errorf("wrong server shutdown timeout")
	}
	return nil
}

func (db *DB) isValid() error {
	if db.Host == "" {
		return fmt.Errorf("wrong DB host")
	}
	if db.Port <= 0 || db.Port > 65535 {
		return fmt.Errorf("DB port must be between 1 and 65535")
	}
	if db.Name == "" {
		return fmt.Errorf("wrong DB name")
	}
	if db.User == "" {
		return fmt.Errorf("wrong DB user")
	}
	if db.Password == "" {
		return fmt.Errorf("wrong DB password")
	}
	if db.Pool.MaxOpenConns <= 0 {
		return fmt.Errorf("wrong DB max_open_conns")
	}
	if db.Pool.MaxIdleConns <= 0 {
		return fmt.Errorf("wrong DB max_idle_conns")
	}
	if db.Pool.ConnMaxLifetime <= 0 {
		return fmt.Errorf("wrong DB conn_max_lifetime")
	}
	if db.MaxRequestTime <= 0 {
		return fmt.Errorf("wrong DB max_request_time")
	}
	if db.Retry.MaxAttempts <= 0 {
		return fmt.Errorf("wrong DB max_attempts")
	}
	if db.Retry.Delay <= 0 {
		return fmt.Errorf("wrong DB delay")
	}
	return nil
}

func (a *Auth) isValid() error {
	if a.JWTSecret == "" {
		return fmt.Errorf("wrong jwt secret")
	}
	if a.JWTTtl <= 0 {
		return fmt.Errorf("wrong jwt ttl")
	}
	if err := a.LoginLimit.isValid(); err != nil {
		return fmt.Errorf("wrong login limit: %w", err)
	}
	if err := a.RegisterLimit.isValid(); err != nil {
		return fmt.Errorf("wrong register limit: %w", err)
	}
	return nil
}

func (r *Redis) isValid() error {
	if strings.TrimSpace(r.Addr) == "" {
		return fmt.Errorf("wrong addr")
	}
	if r.DB < 0 {
		return fmt.Errorf("wrong db")
	}
	if r.MaxRequestTime <= 0 {
		return fmt.Errorf("wrong max_request_time")
	}
	if r.PoolSize <= 0 {
		return fmt.Errorf("wrong pool_size")
	}
	if r.CacheTTL <= 0 {
		return fmt.Errorf("wrong cache_ttl")
	}
	if r.LoadTimeout <= 0 {
		return fmt.Errorf("wrong load_timeout")
	}
	if r.Retry.MaxAttempts <= 0 {
		return fmt.Errorf("wrong Redis max_attempts")
	}
	if r.Retry.Delay <= 0 {
		return fmt.Errorf("wrong Redis delay")
	}
	return nil
}

func (r *RateLimit) isValid() error {
	if r.Interval <= 0 {
		return fmt.Errorf("wrong RateLimit Interval")
	}
	if r.Burst <= 0 {
		return fmt.Errorf("wrong RateLimit Burst")
	}
	if r.MaxEntries <= 0 {
		return fmt.Errorf("wrong RateLimit MaxEntries")
	}
	return nil
}

func (s *Server) Address() string {
	return fmt.Sprintf(":%d", s.Port)
}
