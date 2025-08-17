package golog

import (
	"context"
	"errors"
	"os"
	"time"

	"cloud.google.com/go/logging"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

// Level represents the logging level.
type Level int

const (
	DebugLevel Level = iota
	InfoLevel
	WarnLevel
	ErrorLevel
	FatalLevel
)

// provider is an internal interface for logging providers.
type provider interface {
	newCore(level zapcore.Level) (zapcore.Core, error)
}

// stdOutProvider is a provider for logging to standard output.
type stdOutProvider struct {
	encoderType string // "json" or "console"
}

// newCore implements the provider interface for stdOutProvider.
func (p stdOutProvider) newCore(level zapcore.Level) (zapcore.Core, error) {
	encoderCfg := zap.NewProductionEncoderConfig()
	var encoder zapcore.Encoder
	if p.encoderType == "console" {
		encoder = zapcore.NewConsoleEncoder(encoderCfg)
	} else {
		encoder = zapcore.NewJSONEncoder(encoderCfg)
	}
	syncer := zapcore.AddSync(os.Stdout)
	core := zapcore.NewCore(encoder, syncer, level)
	return core, nil
}

// gcpProvider is a provider for logging to Google Cloud Logging.
type gcpProvider struct {
	projectID string
	logName   string
}

// newCore implements the provider interface for gcpProvider.
func (p gcpProvider) newCore(level zapcore.Level) (zapcore.Core, error) {
	ctx := context.Background()
	client, err := logging.NewClient(ctx, p.projectID)
	if err != nil {
		return nil, err
	}
	lg := client.Logger(p.logName)
	return &gcpZapCore{
		logger: lg,
		level:  level,
		fields: make(map[string]interface{}),
	}, nil
}

// fileProvider is a provider for logging to a file with rotation.
type fileProvider struct {
	filename   string
	maxSize    int // in megabytes
	maxBackups int
	maxAge     int // in days
	compress   bool
}

// newCore implements the provider interface for fileProvider.
func (p fileProvider) newCore(level zapcore.Level) (zapcore.Core, error) {
	encoderCfg := zap.NewProductionEncoderConfig()
	encoder := zapcore.NewJSONEncoder(encoderCfg)
	syncer := zapcore.AddSync(&lumberjack.Logger{
		Filename:   p.filename,
		MaxSize:    p.maxSize,
		MaxBackups: p.maxBackups,
		MaxAge:     p.maxAge,
		Compress:   p.compress,
	})
	core := zapcore.NewCore(encoder, syncer, level)
	return core, nil
}

// LoggerOption defines a functional option for configuring the logger.
type LoggerOption func(*loggerConfig)

type loggerConfig struct {
	providers []provider
	level     Level
}

// WithStdOutProvider adds a standard output provider to the logger configuration.
func WithStdOutProvider(encoderType string) LoggerOption {
	return func(cfg *loggerConfig) {
		cfg.providers = append(cfg.providers, stdOutProvider{encoderType: encoderType})
	}
}

// WithGCPProvider adds a Google Cloud Logging provider to the logger configuration.
func WithGCPProvider(projectID, logName string) LoggerOption {
	return func(cfg *loggerConfig) {
		cfg.providers = append(cfg.providers, gcpProvider{projectID: projectID, logName: logName})
	}
}

// WithFileProvider adds a file provider with log rotation to the logger configuration.
func WithFileProvider(filename string, maxSize, maxBackups, maxAge int, compress bool) LoggerOption {
	return func(cfg *loggerConfig) {
		cfg.providers = append(cfg.providers, fileProvider{
			filename:   filename,
			maxSize:    maxSize,
			maxBackups: maxBackups,
			maxAge:     maxAge,
			compress:   compress,
		})
	}
}

// WithLevel sets the log level for the logger.
func WithLevel(level Level) LoggerOption {
	return func(cfg *loggerConfig) {
		cfg.level = level
	}
}

// Logger is the main logging interface provided to users.
type Logger struct {
	zapLogger *zap.Logger
}

// NewLogger creates a new Logger based on the provided functional options.
func NewLogger(options ...LoggerOption) (*Logger, error) {
	cfg := &loggerConfig{
		providers: []provider{},
		level:     InfoLevel, // Default level
	}

	for _, opt := range options {
		opt(cfg)
	}

	var cores []zapcore.Core
	for _, p := range cfg.providers {
		core, err := p.newCore(toZapLevel(cfg.level))
		if err != nil {
			return nil, err
		}
		cores = append(cores, core)
	}
	if len(cores) == 0 {
		return nil, errors.New("no providers specified")
	}
	teeCore := zapcore.NewTee(cores...)
	zapLogger := zap.New(teeCore)
	return &Logger{zapLogger: zapLogger}, nil
}

// Debug logs a message at Debug level with optional fields.
func (l *Logger) Debug(msg string, fields ...Field) {
	l.zapLogger.Debug(msg, toZapFields(fields)...)
}

// Info logs a message at Info level with optional fields.
func (l *Logger) Info(msg string, fields ...Field) {
	l.zapLogger.Info(msg, toZapFields(fields)...)
}

// Warn logs a message at Warn level with optional fields.
func (l *Logger) Warn(msg string, fields ...Field) {
	l.zapLogger.Warn(msg, toZapFields(fields)...)
}

// Error logs a message at Error level with optional fields.
func (l *Logger) Error(msg string, fields ...Field) {
	l.zapLogger.Error(msg, toZapFields(fields)...)
}

// Fatal logs a message at Fatal level with optional fields and exits.
func (l *Logger) Fatal(msg string, fields ...Field) {
	l.zapLogger.Fatal(msg, toZapFields(fields)...)
}

// Sync flushes any buffered log entries.
func (l *Logger) Sync() error {
	return l.zapLogger.Sync()
}

// Field represents a key-value pair for structured logging.
type Field struct {
	Key   string
	Value interface{}
}

// String creates a field with a string value.
func String(key, value string) Field {
	return Field{Key: key, Value: value}
}

// Int creates a field with an integer value.
func Int(key string, value int) Field {
	return Field{Key: key, Value: value}
}

// Float64 creates a field with a float64 value.
func Float64(key string, value float64) Field {
	return Field{Key: key, Value: value}
}

// Error creates a field with an error value.
func Error(err error) Field {
	return Field{Key: "error", Value: err}
}

// Duration creates a field with a time.Duration value.
func Duration(key string, value time.Duration) Field {
	return Field{Key: key, Value: value}
}

// toZapLevel converts the package's Level to zapcore.Level.
func toZapLevel(lvl Level) zapcore.Level {
	switch lvl {
	case DebugLevel:
		return zapcore.DebugLevel
	case InfoLevel:
		return zapcore.InfoLevel
	case WarnLevel:
		return zapcore.WarnLevel
	case ErrorLevel:
		return zapcore.ErrorLevel
	case FatalLevel:
		return zapcore.FatalLevel
	default:
		return zapcore.InfoLevel
	}
}

// toZapFields converts the package's Fields to zapcore.Fields.
func toZapFields(fields []Field) []zapcore.Field {
	zapFields := make([]zapcore.Field, len(fields))
	for i, f := range fields {
		switch v := f.Value.(type) {
		case string:
			zapFields[i] = zap.String(f.Key, v)
		case int:
			zapFields[i] = zap.Int(f.Key, v)
		case float64:
			zapFields[i] = zap.Float64(f.Key, v)
		case error:
			zapFields[i] = zap.Error(v)
		case time.Duration:
			zapFields[i] = zap.Duration(f.Key, v)
		default:
			zapFields[i] = zap.Any(f.Key, v)
		}
	}
	return zapFields
}

// gcpZapCore is a custom zapcore.Core for Google Cloud Logging.
type gcpZapCore struct {
	logger *logging.Logger
	level  zapcore.Level
	fields map[string]interface{}
}

// Enabled checks if the log level is enabled.
func (c *gcpZapCore) Enabled(lvl zapcore.Level) bool {
	return lvl >= c.level
}

// With adds fields to the core.
func (c *gcpZapCore) With(fields []zapcore.Field) zapcore.Core {
	clone := *c
	clone.fields = make(map[string]interface{}, len(c.fields)+len(fields))
	for k, v := range c.fields {
		clone.fields[k] = v
	}
	enc := zapcore.NewMapObjectEncoder()
	for _, f := range fields {
		f.AddTo(enc)
	}
	for k, v := range enc.Fields {
		clone.fields[k] = v
	}
	return &clone
}

// Check determines if the entry should be logged.
func (c *gcpZapCore) Check(ent zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if c.Enabled(ent.Level) {
		return ce.AddCore(ent, c)
	}
	return ce
}

// Write writes the log entry.
func (c *gcpZapCore) Write(ent zapcore.Entry, fields []zapcore.Field) error {
	payload := make(map[string]interface{}, len(c.fields)+len(fields)+4)
	for k, v := range c.fields {
		payload[k] = v
	}
	enc := zapcore.NewMapObjectEncoder()
	for _, f := range fields {
		f.AddTo(enc)
	}
	for k, v := range enc.Fields {
		payload[k] = v
	}
	payload["message"] = ent.Message
	// Add caller information as fields since SourceLocation is not available
	if ent.Caller.Defined {
		payload["source_file"] = ent.Caller.File
		payload["source_line"] = ent.Caller.Line
		payload["source_function"] = ent.Caller.Function
	}

	sev := levelToSeverity(ent.Level)
	logEntry := logging.Entry{
		Timestamp: ent.Time,
		Severity:  sev,
		Payload:   payload,
	}
	c.logger.Log(logEntry)
	return nil
}

// Sync flushes the logger.
func (c *gcpZapCore) Sync() error {
	return c.logger.Flush()
}

func levelToSeverity(lvl zapcore.Level) logging.Severity {
	switch lvl {
	case zapcore.DebugLevel:
		return logging.Debug
	case zapcore.InfoLevel:
		return logging.Info
	case zapcore.WarnLevel:
		return logging.Warning
	case zapcore.ErrorLevel:
		return logging.Error
	case zapcore.DPanicLevel:
		return logging.Critical
	case zapcore.PanicLevel:
		return logging.Alert
	case zapcore.FatalLevel:
		return logging.Emergency
	default:
		return logging.Default
	}
}
