// Copyright (c) 2022 Runetale Inc & AUTHORS All rights reserved.
// Use of this source code is governed by a BSD 3-Clause License
// license that can be found in the LICENSE file.

package runelog

import (
	"fmt"
	"net/url"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	lumberjack "gopkg.in/natefinch/lumberjack.v2"
)

const (
	DebugLevelStr   string = "debug"
	InfoLevelStr    string = "info"
	WarningLevelStr string = "warning"
	ErrorLevelStr   string = "error"
)

// var (
// 	globalLogger *zap.Logger
// )

type Runelog struct {
	Logger *zap.SugaredLogger
}

func Newrunelog(name string, logLevel string, logFile string, dev bool) (*Runelog, error) {
	l, err := initrunelog(logLevel, logFile, dev)
	if err != nil {
		return nil, err
	}

	return &Runelog{
		Logger: l.Named(name).Sugar(),
	}, nil
}

func initrunelog(logLevel string, logFile string, dev bool) (*zap.Logger, error) {
	var level zapcore.Level
	switch logLevel {
	case DebugLevelStr:
		level = zap.DebugLevel
	case InfoLevelStr:
		level = zap.InfoLevel
	case WarningLevelStr:
		level = zap.WarnLevel
	case ErrorLevelStr:
		level = zap.ErrorLevel
	default:
		return nil, fmt.Errorf("unknown log level %s", logLevel)
	}

	encoderConfig := zapcore.EncoderConfig{
		TimeKey:        "ts",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		MessageKey:     "msg",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.LowercaseLevelEncoder,
		EncodeTime:     zapcore.ISO8601TimeEncoder,
		EncodeDuration: zapcore.SecondsDurationEncoder,
	}

	ll := lumberjack.Logger{
		Filename:   logFile,
		MaxSize:    1024, //MB
		MaxBackups: 30,   // days
		MaxAge:     90,   //days
		Compress:   true,
	}

	zap.RegisterSink("lumberjack", func(*url.URL) (zap.Sink, error) {
		return lumberjackSink{
			Logger: &ll,
		}, nil
	})

	loggerConfig := zap.Config{
		Level:         zap.NewAtomicLevelAt(level),
		Development:   dev,
		Encoding:      "console",
		EncoderConfig: encoderConfig,
		OutputPaths:   []string{fmt.Sprintf("lumberjack:%s", logFile), "stderr"},
	}

	buildedLogger, err := loggerConfig.Build()
	if err != nil {
		panic(fmt.Sprintf("build zap logger from config error: %v", err))
	}

	zap.ReplaceGlobals(buildedLogger)

	return buildedLogger, nil
}

type lumberjackSink struct {
	*lumberjack.Logger
}

func (lumberjackSink) Sync() error {
	return nil
}
