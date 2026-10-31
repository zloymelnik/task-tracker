package config

import (
	"errors"
	"fmt"
	"os"
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
	Env    string `yaml:"env" env:"ENV" env-default:"local"`
	Server Server `yaml:"server"`
	DB     DB     `yaml:"mysql"`
	Auth   Auth   `yaml:"auth"`
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
	JWTSecret string        `yaml:"-"       env:"JWT_SECRET"`
	JWTTtl    time.Duration `yaml:"jwt_ttl" env:"JWT_TTL"`
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
	return nil
}

func (s *Server) Address() string {
	return fmt.Sprintf(":%d", s.Port)
}
