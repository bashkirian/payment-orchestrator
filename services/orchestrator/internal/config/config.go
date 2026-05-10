package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Env             string        `yaml:"env"`
	LogLevel        string        `yaml:"log_level"`
	GRPCAddr        string        `yaml:"grpc_addr"`
	HTTPAddr        string        `yaml:"http_addr"`
	ShutdownTimeout time.Duration `yaml:"shutdown_timeout"`
	DatabaseURL     string        `yaml:"database_url"`
	Stripe          StripeConfig  `yaml:"stripe"`
}

// StripeConfig holds Stripe-specific tunables.
type StripeConfig struct {
	APIKey         string `yaml:"api_key"`
	MaxRetries     int64  `yaml:"max_retries"`
	TimeoutSeconds int    `yaml:"timeout_seconds"`
}

func Load(path string) (Config, error) {
	cfg := Config{
		Env:             "development",
		LogLevel:        "info",
		GRPCAddr:        ":50051",
		HTTPAddr:        ":8081",
		ShutdownTimeout: 10 * time.Second,
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config %s: %w", path, err)
	}

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config %s: %w", path, err)
	}

	return cfg, nil
}
