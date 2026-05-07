package logger

import (
	"strings"

	"github.com/bashkirian/fintech-project/services/api/internal/config"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func New(cfg config.Config) (*zap.Logger, error) {
	var zapConfig zap.Config
	if strings.EqualFold(cfg.Env, "production") {
		zapConfig = zap.NewProductionConfig()
	} else {
		zapConfig = zap.NewDevelopmentConfig()
	}

	level := new(zapcore.Level)
	if err := level.UnmarshalText([]byte(cfg.LogLevel)); err != nil {
		return nil, err
	}
	zapConfig.Level = zap.NewAtomicLevelAt(*level)

	return zapConfig.Build()
}

func FieldString(key string, value string) zap.Field {
	return zap.String(key, value)
}
