package observability

import (
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type LogConfig struct {
	Env      string
	LogLevel string
}

// NewLogger creates a zap.Logger configured for the given environment and log level.
func NewLogger(cfg LogConfig) (*zap.Logger, error) {
	var zapCfg zap.Config
	if strings.EqualFold(cfg.Env, "production") {
		zapCfg = zap.NewProductionConfig()
	} else {
		zapCfg = zap.NewDevelopmentConfig()
	}

	level := new(zapcore.Level)
	if err := level.UnmarshalText([]byte(cfg.LogLevel)); err != nil {
		return nil, err
	}
	zapCfg.Level = zap.NewAtomicLevelAt(*level)

	return zapCfg.Build()
}
