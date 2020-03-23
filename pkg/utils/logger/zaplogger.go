// Copyright Amazon.com Inc. or its affiliates. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may
// not use this file except in compliance with the License. A copy of the
// License is located at
//
//     http://aws.amazon.com/apache2.0/
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing
// permissions and limitations under the License.

package logger

import (
	"os"
	"runtime"
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	lumberjack "gopkg.in/natefinch/lumberjack.v2"
)

type structuredLogger struct {
	zapLogger *zap.SugaredLogger
}

// getZapLevel converts log level string to zapcore.Level
func getZapLevel(inputLogLevel string) zapcore.Level {
	lvl := strings.ToLower(inputLogLevel)

	switch lvl {
	case "debug":
		return zapcore.DebugLevel
	case "info":
		return zapcore.InfoLevel
	case "warn":
		return zapcore.WarnLevel
	case "error":
		return zapcore.ErrorLevel
	case "fatal":
		return zapcore.FatalLevel
	default:
		return zapcore.DebugLevel
	}
}

func (logf *structuredLogger) Debugf(format string, args ...interface{}) {
	logf.zapLogger.Debugf(format, args...)
}

func (logf *structuredLogger) Debug(format string) {
	logf.zapLogger.Desugar().Debug(format)
}

func (logf *structuredLogger) Infof(format string, args ...interface{}) {
	logf.zapLogger.Infof(format, args...)
}

func (logf *structuredLogger) Info(format string) {
	logf.zapLogger.Desugar().Info(format)
}

func (logf *structuredLogger) Warnf(format string, args ...interface{}) {
	logf.zapLogger.Warnf(format, args...)
}

func (logf *structuredLogger) Warn(format string) {
	logf.zapLogger.Desugar().Warn(format)
}

func (logf *structuredLogger) Error(format string) {
	logf.zapLogger.Desugar().Error(format)
}

func (logf *structuredLogger) Errorf(format string, args ...interface{}) {
	logf.zapLogger.Errorf(format, args...)
}

func (logf *structuredLogger) Fatalf(format string, args ...interface{}) {
	logf.zapLogger.Fatalf(format, args...)
}

func (logf *structuredLogger) Panicf(format string, args ...interface{}) {
	logf.zapLogger.Fatalf(format, args...)
}

func (logf *structuredLogger) WithFields(fields Fields) Logger {
	var f = make([]interface{}, 0)
	for k, v := range fields {
		f = append(f, k)
		f = append(f, v)
	}
	newLogger := logf.zapLogger.With(f...)
	return &structuredLogger{newLogger}
}

func getEncoder() zapcore.Encoder {
	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	return zapcore.NewJSONEncoder(encoderConfig)
}

func (logConfig *Configuration) newZapLogger() *structuredLogger {
	var cores []zapcore.Core
	var writer zapcore.WriteSyncer

	logLevel := getZapLevel(logConfig.LogLevel)

	logFilePath := logConfig.LogLocation

	if strings.ToLower(logFilePath) != "stdout" {
		writer = getLogWriter(logFilePath)
	} else {
		writer = zapcore.Lock(os.Stdout)
	}
	cores = append(cores, zapcore.NewCore(getEncoder(), writer, logLevel))

	combinedCore := zapcore.NewTee(cores...)

	logger := zap.New(combinedCore,
		zap.AddCaller(),
		zap.AddCallerSkip(2),
	)
	defer logger.Sync()
	sugar := logger.Sugar()

	return &structuredLogger{
		zapLogger: sugar,
	}
}

//getLogWriter is for lumberjack
func getLogWriter(logFilePath string) zapcore.WriteSyncer {
	lumberJackLogger := &lumberjack.Logger{
		Filename:   logFilePath,
		MaxSize:    100,
		MaxBackups: 5,
		MaxAge:     30,
		Compress:   true,
	}
	return zapcore.AddSync(lumberJackLogger)
}

// DefaultLogger creates and returns a new default logger.
func DefaultLogger() Logger {
	productionConfig := zap.NewProductionConfig()
	productionConfig.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	productionConfig.EncoderConfig.EncodeCaller = func(caller zapcore.EntryCaller, enc zapcore.PrimitiveArrayEncoder) {
		_, caller.File, caller.Line, _ = runtime.Caller(8)
		enc.AppendString(caller.FullPath())
	}
	logger, _ := productionConfig.Build()
	defer logger.Sync()
	sugar := logger.Sugar()
	return &structuredLogger{
		zapLogger: sugar,
	}
}
