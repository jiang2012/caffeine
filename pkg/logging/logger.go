package logging

import (
	"context"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
	"os"
	"path/filepath"
)

var logger *zap.Logger

const (
	defaultPath     = "./logs"
	defaultFileName = "test.log"
)

type Config struct {
	Path       string // 日志文件路径
	FileName   string // 日志文件名称
	Level      string // 日志级别 debug info warn error
	JsonFormat bool   // 是否以json格式打印
	MaxSize    int    // 日志文件最大大小
	MaxAge     int    // 日志文件最大保存天数
	MaxBackups int    // 日志文件最大备份数量
	Compress   bool   // 是否压缩日志文件
	Stdout     bool   // 是否打印到控制台
}

func Init(config *Config) {
	core := zapcore.NewCore(getEncoder(config), getWriteSyncer(config), getLevel(config))
	logger = zap.New(core, zap.AddCaller(), zap.AddCallerSkip(1))
}

func Sync() {
	_ = logger.Sync()
}

func Logger() *zap.Logger {
	return logger
}

func LoggerWithContext(ctx context.Context) *zap.Logger {
	traceId := ctx.Value("trace_id")
	if traceId == nil {
		traceId = ""
	}

	return logger.With(zap.Any("trace_id", traceId))
}

func Debug(msg string, fields ...zap.Field) {
	Logger().Debug(msg, fields...)
}

func Info(msg string, fields ...zap.Field) {
	Logger().Info(msg, fields...)
}

func Warn(msg string, fields ...zap.Field) {
	Logger().Warn(msg, fields...)
}

func Error(msg string, fields ...zap.Field) {
	Logger().Error(msg, fields...)
}

func DebugCtx(ctx context.Context, msg string, fields ...zap.Field) {
	LoggerWithContext(ctx).Debug(msg, fields...)
}

func InfoCtx(ctx context.Context, msg string, fields ...zap.Field) {
	LoggerWithContext(ctx).Info(msg, fields...)
}

func WarnCtx(ctx context.Context, msg string, fields ...zap.Field) {
	LoggerWithContext(ctx).Warn(msg, fields...)
}

func ErrorCtx(ctx context.Context, msg string, fields ...zap.Field) {
	LoggerWithContext(ctx).Error(msg, fields...)
}

func getEncoder(config *Config) zapcore.Encoder {
	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	encoderConfig.EncodeLevel = zapcore.CapitalLevelEncoder
	if config.JsonFormat {
		return zapcore.NewJSONEncoder(encoderConfig)
	}
	return zapcore.NewConsoleEncoder(encoderConfig)
}

func getWriteSyncer(config *Config) zapcore.WriteSyncer {
	if _, err := os.Stat(config.Path); os.IsNotExist(err) {
		if config.Path == "" {
			config.Path = defaultPath
		}

		err := os.Mkdir(config.Path, os.ModePerm)
		if err != nil {
			panic(err)
		}
	}

	if config.FileName == "" {
		config.FileName = defaultFileName
	}

	lumberJackLogger := &lumberjack.Logger{
		Filename:   filepath.Join(config.Path, config.FileName),
		MaxSize:    config.MaxSize,
		MaxAge:     config.MaxAge,
		MaxBackups: config.MaxBackups,
		Compress:   config.Compress,
	}

	if config.Stdout {
		return zapcore.NewMultiWriteSyncer(zapcore.AddSync(os.Stdout), zapcore.AddSync(lumberJackLogger))
	}
	return zapcore.AddSync(lumberJackLogger)
}

func getLevel(config *Config) zapcore.Level {
	switch config.Level {
	case "debug":
		return zap.DebugLevel
	case "info":
		return zap.InfoLevel
	case "warn":
		return zap.WarnLevel
	case "error":
		return zap.ErrorLevel
	default:
		return zap.InfoLevel
	}
}
