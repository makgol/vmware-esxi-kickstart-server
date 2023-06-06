package common

import (
	"context"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func NewLogger(ctx context.Context, logfilepath string) (*zap.Logger, error) {
	cfg := zap.Config{
		OutputPaths: []string{logfilepath, "stdout"},
		Level:       zap.NewAtomicLevelAt(zapcore.InfoLevel),
		Encoding:    "json",
		EncoderConfig: zapcore.EncoderConfig{
			LevelKey:     "level",
			TimeKey:      "time",
			MessageKey:   "msg",
			CallerKey:    "caller",
			EncodeTime:   zapcore.ISO8601TimeEncoder,
			EncodeLevel:  zapcore.CapitalLevelEncoder,
			EncodeCaller: zapcore.ShortCallerEncoder,
		},
	}

	logger, err := cfg.Build()
	if err != nil {
		return nil, err
	}

	return logger, nil
}
