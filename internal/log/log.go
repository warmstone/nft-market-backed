package log

import (
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var Logger *zap.Logger

func Init(level string) {
	cfg := zap.NewProductionConfig()
	cfg.Level = zap.NewAtomicLevelAt(parseLevel(level))
	cfg.EncoderConfig.TimeKey = "ts"
	cfg.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

	var err error
	Logger, err = cfg.Build()
	if err != nil {
		panic("failed to initialize logger: " + err.Error())
	}
}

func Sync() {
	if Logger != nil {
		_ = Logger.Sync()
	}
}

func parseLevel(s string) zapcore.Level {
	switch strings.ToLower(s) {
	case "debug":
		return zapcore.DebugLevel
	case "warn":
		return zapcore.WarnLevel
	case "error":
		return zapcore.ErrorLevel
	default:
		return zapcore.InfoLevel
	}
}
