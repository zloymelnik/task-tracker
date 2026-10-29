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
}

// Server holds HTTP server tuning parameters.
type Server struct {
	Port            int           `yaml:"port"             env:"APP_PORT"`
	ReadTimeout     time.Duration `yaml:"read_timeout"     env:"SERVER_READ_TIMEOUT"`
	WriteTimeout    time.Duration `yaml:"write_timeout"    env:"SERVER_WRITE_TIMEOUT"`
	ShutdownTimeout time.Duration `yaml:"shutdown_timeout" env:"SERVER_SHUTDOWN_TIMEOUT"`
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
	// cfg.App.SigningKey, err = generateRandomString(16)
	// if err != nil {
	// 	return &cfg, fmt.Errorf("%w. Cannot generate SigningKey", err)
	// }
	return &cfg, nil
}

func (c *Config) isValid() error {
	if c.Env != LocalEnv && c.Env != DevEnv && c.Env != ProdEnv {
		return fmt.Errorf("wrong environment")
	}
	if err := c.Server.isValid(); err != nil {
		return fmt.Errorf("wrong server settings: %w", err)
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

func (s *Server) Address() string {
	return fmt.Sprintf(":%d", s.Port)
}

// func generateRandomString(length int) (string, error) {
// 	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
// 	b := make([]byte, length)
// 	for i := range b {
// 		num, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
// 		if err != nil {
// 			return "", err
// 		}
// 		b[i] = charset[num.Int64()]
// 	}
// 	return string(b), nil
// }
