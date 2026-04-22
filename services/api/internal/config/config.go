package config

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	Env               string
	HTTPAddr          string
	LogLevel          string
	ReadTimeout       time.Duration
	ReadHeaderTimeout time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	ShutdownTimeout   time.Duration
}

func Load() (Config, error) {
	var err error

	// Best-effort local development support. Real environment variables still win.
	_ = godotenv.Load()

	cfg := Config{
		Env:      getEnv("API_ENV", "development"),
		HTTPAddr: getEnv("API_HTTP_ADDR", ":8080"),
		LogLevel: getEnv("API_LOG_LEVEL", "info"),
	}

	if cfg.ReadTimeout, err = getDurationEnv("API_READ_TIMEOUT", 5*time.Second); err != nil {
		return Config{}, fmt.Errorf("parse API_READ_TIMEOUT: %w", err)
	}
	if cfg.ReadHeaderTimeout, err = getDurationEnv("API_READ_HEADER_TIMEOUT", 2*time.Second); err != nil {
		return Config{}, fmt.Errorf("parse API_READ_HEADER_TIMEOUT: %w", err)
	}
	if cfg.WriteTimeout, err = getDurationEnv("API_WRITE_TIMEOUT", 10*time.Second); err != nil {
		return Config{}, fmt.Errorf("parse API_WRITE_TIMEOUT: %w", err)
	}
	if cfg.IdleTimeout, err = getDurationEnv("API_IDLE_TIMEOUT", 30*time.Second); err != nil {
		return Config{}, fmt.Errorf("parse API_IDLE_TIMEOUT: %w", err)
	}
	if cfg.ShutdownTimeout, err = getDurationEnv("API_SHUTDOWN_TIMEOUT", 10*time.Second); err != nil {
		return Config{}, fmt.Errorf("parse API_SHUTDOWN_TIMEOUT: %w", err)
	}

	return cfg, nil
}

func getEnv(key string, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func getDurationEnv(key string, fallback time.Duration) (time.Duration, error) {
	value := os.Getenv(key)
	if value == "" {
		return fallback, nil
	}

	duration, err := time.ParseDuration(value)
	if err == nil {
		return duration, nil
	}

	seconds, intErr := strconv.Atoi(value)
	if intErr != nil {
		return 0, err
	}

	return time.Duration(seconds) * time.Second, nil
}