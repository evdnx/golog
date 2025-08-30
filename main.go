package golog

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"cloud.google.com/go/logging"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

/* -------------------------------------------------------------------------- */
/*                               Log Levels                                   */
/* -------------------------------------------------------------------------- */

type Level int

const (
	DebugLevel Level = iota
	InfoLevel
	WarnLevel
	ErrorLevel
	FatalLevel
)

/* -------------------------------------------------------------------------- */
/*                              Encoder Types                                  */
/* -------------------------------------------------------------------------- */

type EncoderType string

const (
	JSONEncoder    EncoderType = "json"
	ConsoleEncoder EncoderType = "console"
)

/* -------------------------------------------------------------------------- */
/*                         Provider Interface & Closers                        */
/* -------------------------------------------------------------------------- */

// provider is the internal abstraction each output target implements.
type provider interface {
	newCore(level zapcore.Level) (zapcore.Core, error)
	// close is optional – only providers that allocate external resources need it.
	close() error
}

/* -------------------------------------------------------------------------- */
/*                           StdOut Provider                                   */
/* -------------------------------------------------------------------------- */

type stdOutProvider struct {
	encoderType EncoderType
}

func (p stdOutProvider) newCore(level zapcore.Level) (zapcore.Core, error) {
	enc, err := buildEncoder(p.encoderType)
	if err != nil {
		return nil, err
	}
	syncer := zapcore.AddSync(os.Stdout)
	return zapcore.NewCore(enc, syncer, level), nil
}
func (p stdOutProvider) close() error { return nil }

/* -------------------------------------------------------------------------- */
/*                           Writer Provider                                    */
/* -------------------------------------------------------------------------- */

type writerProvider struct {
	writer      io.Writer
	encoderType EncoderType
}

func (p writerProvider) newCore(level zapcore.Level) (zapcore.Core, error) {
	enc, err := buildEncoder(p.encoderType)
	if err != nil {
		return nil, err
	}
	syncer := zapcore.AddSync(p.writer)
	return zapcore.NewCore(enc, syncer, level), nil
}
func (p writerProvider) close() error { return nil }

/* -------------------------------------------------------------------------- */
/*                            GCP Provider                                      */
/* -------------------------------------------------------------------------- */

type gcpProvider struct {
	projectID string
	logName   string

	// internal fields populated during newCore
	client *logging.Client
	logger *logging.Logger
}

func (p *gcpProvider) newCore(level zapcore.Level) (zapcore.Core, error) {
	ctx := context.Background()
	client, err := logging.NewClient(ctx, p.projectID)
	if err != nil {
		return nil, fmt.Errorf("gcpProvider: failed to create client: %w", err)
	}
	p.client = client
	p.logger = client.Logger(p.logName)

	return &gcpZapCore{
		logger: p.logger,
		level:  level,
		fields: make(map[string]interface{}),
	}, nil
}
func (p *gcpProvider) close() error {
	if p.client != nil {
		// Flush pending entries before closing.
		if err := p.client.Close(); err != nil {
			return fmt.Errorf("gcpProvider: error closing client: %w", err)
		}
	}
	return nil
}

/*
	--------------------------------------------------------------
	  fileProvider – uses pointer receivers so that the
	  lumberjack logger assigned in newCore is kept on the same
	  instance that will later be closed.

--------------------------------------------------------------
*/
type fileProvider struct {
	filename   string
	maxSize    int // MB
	maxBackups int
	maxAge     int // days
	compress   bool

	// Holds the lumberjack logger for later shutdown.
	lumberjackLogger *lumberjack.Logger
}

/*
	--------------------------------------------------------------
	  newCore creates a zapcore.Core that writes JSON‑encoded logs
	  to a rotating file.  It also stores the underlying
	  lumberjack.Logger on the same *fileProvider* instance so that
	  Close() can flush and release the file.

--------------------------------------------------------------
*/
func (p *fileProvider) newCore(level zapcore.Level) (zapcore.Core, error) {
	// Validate rotation parameters – negative values are nonsensical.
	if p.maxSize < 0 || p.maxBackups < 0 || p.maxAge < 0 {
		return nil, errors.New("fileProvider: rotation parameters must be non‑negative")
	}
	enc, err := buildEncoder(JSONEncoder) // file logs are always JSON
	if err != nil {
		return nil, err
	}
	lj := &lumberjack.Logger{
		Filename:   p.filename,
		MaxSize:    p.maxSize,
		MaxBackups: p.maxBackups,
		MaxAge:     p.maxAge,
		Compress:   p.compress,
	}
	// Save the logger for later cleanup.
	p.lumberjackLogger = lj

	syncer := zapcore.AddSync(lj)
	return zapcore.NewCore(enc, syncer, level), nil
}

/*
	--------------------------------------------------------------
	  close shuts down the lumberjack logger (if it was created),
	  ensuring the file descriptor is released before the temp
	  directory is removed in tests.

--------------------------------------------------------------
*/
func (p *fileProvider) close() error {
	if p.lumberjackLogger != nil {
		return p.lumberjackLogger.Close()
	}
	return nil
}

/* -------------------------------------------------------------------------- */
/*                     Functional Options & Config Struct                      */
/* -------------------------------------------------------------------------- */

type LoggerOption func(*loggerConfig)

type loggerConfig struct {
	providers []provider
	level     Level
	// closers collects any provider that needs explicit shutdown.
	closers []provider
}

// WithStdOutProvider adds a stdout destination.
func WithStdOutProvider(encoderType EncoderType) LoggerOption {
	return func(cfg *loggerConfig) {
		cfg.providers = append(cfg.providers, stdOutProvider{encoderType: encoderType})
	}
}

// WithWriterProvider adds a custom io.Writer destination.
func WithWriterProvider(writer io.Writer, encoderType EncoderType) LoggerOption {
	return func(cfg *loggerConfig) {
		cfg.providers = append(cfg.providers, writerProvider{writer: writer, encoderType: encoderType})
	}
}

// WithGCPProvider adds Google Cloud Logging as a destination.
func WithGCPProvider(projectID, logName string) LoggerOption {
	return func(cfg *loggerConfig) {
		cfg.providers = append(cfg.providers, &gcpProvider{projectID: projectID, logName: logName})
	}
}

/*
	--------------------------------------------------------------
	  WithFileProvider – registers a *fileProvider* (pointer) so the
	  instance created here is the one whose internal lumberjack logger
	  can later be closed.  Using a pointer ensures that the state
	  mutated in newCore (the stored *lumberjack.Logger*) is retained.

--------------------------------------------------------------
*/
func WithFileProvider(filename string, maxSize, maxBackups, maxAge int, compress bool) LoggerOption {
	return func(cfg *loggerConfig) {
		// Store a pointer so the provider’s internal fields (e.g. the
		// lumberjack logger) survive beyond the newCore call.
		cfg.providers = append(cfg.providers, &fileProvider{
			filename:   filename,
			maxSize:    maxSize,
			maxBackups: maxBackups,
			maxAge:     maxAge,
			compress:   compress,
		})
	}
}

// WithLevel overrides the default log level (Info).
func WithLevel(level Level) LoggerOption {
	return func(cfg *loggerConfig) {
		cfg.level = level
	}
}

/* -------------------------------------------------------------------------- */
/*                                 Logger API                                   */
/* -------------------------------------------------------------------------- */

type Logger struct {
	zapLogger *zap.Logger
	// keep a reference to the config so we can close providers later.
	closers []provider
}

// NewLogger builds a logger from the supplied functional options.
func NewLogger(options ...LoggerOption) (*Logger, error) {
	cfg := &loggerConfig{
		providers: []provider{},
		level:     InfoLevel, // default
	}

	for _, opt := range options {
		opt(cfg)
	}

	if len(cfg.providers) == 0 {
		return nil, errors.New("no providers specified")
	}

	var cores []zapcore.Core
	for _, p := range cfg.providers {
		core, err := p.newCore(toZapLevel(cfg.level))
		if err != nil {
			// Attempt to close any providers already initialised.
			_ = closeProviders(cfg.providers)
			return nil, fmt.Errorf("failed to initialise provider: %w", err)
		}
		cores = append(cores, core)
		// Keep track of providers that implement close().
		cfg.closers = append(cfg.closers, p)
	}

	teeCore := zapcore.NewTee(cores...)
	zapLogger := zap.New(teeCore, zap.AddCaller()) // always capture caller info
	return &Logger{zapLogger: zapLogger, closers: cfg.closers}, nil
}

// Close flushes the zap logger and shuts down any provider resources.
func (l *Logger) Close() error {
	var firstErr error
	// zap.Logger.Sync() never returns zap.ErrClosed, so we just propagate any error it gives.
	if err := l.zapLogger.Sync(); err != nil {
		firstErr = fmt.Errorf("zap sync error: %w", err)
	}
	if err := closeProviders(l.closers); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}

// Sync is retained for backward compatibility – it simply forwards to zap.Sync().
func (l *Logger) Sync() error { return l.zapLogger.Sync() }

// Debug logs at Debug level.
func (l *Logger) Debug(msg string, fields ...Field) {
	l.zapLogger.Debug(msg, toZapFields(fields)...)
}

// Info logs at Info level.
func (l *Logger) Info(msg string, fields ...Field) {
	l.zapLogger.Info(msg, toZapFields(fields)...)
}

// Warn logs at Warn level.
func (l *Logger) Warn(msg string, fields ...Field) {
	l.zapLogger.Warn(msg, toZapFields(fields)...)
}

// Error logs at Error level.
func (l *Logger) Error(msg string, fields ...Field) {
	l.zapLogger.Error(msg, toZapFields(fields)...)
}

// Fatal logs at Fatal level and then exits the process.
func (l *Logger) Fatal(msg string, fields ...Field) {
	l.zapLogger.Fatal(msg, toZapFields(fields)...)
}

/* -------------------------------------------------------------------------- */
/*                          Structured Fields Helper                           */
/* -------------------------------------------------------------------------- */

type Field struct {
	Key   string
	Value interface{}
}

// Primitive helpers – keep the API identical to the original library.
func String(key, value string) Field          { return Field{Key: key, Value: value} }
func Int(key string, value int) Field         { return Field{Key: key, Value: value} }
func Float64(key string, value float64) Field { return Field{Key: key, Value: value} }
func Err(err error) Field                     { return Field{Key: "error", Value: err} }
func Duration(key string, value time.Duration) Field {
	return Field{Key: key, Value: value}
}
func Any(key string, value interface{}) Field { return Field{Key: key, Value: value} }

// Convert our custom Field slice into zapcore.Fields.
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

/* -------------------------------------------------------------------------- */
/*                         Level Conversion Helpers                            */
/* -------------------------------------------------------------------------- */

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
		// Gracefully fall back – callers get a sensible default.
		return zapcore.InfoLevel
	}
}

/* -------------------------------------------------------------------------- */
/*                     Encoder Construction Utility                             */
/* -------------------------------------------------------------------------- */

func buildEncoder(t EncoderType) (zapcore.Encoder, error) {
	encCfg := zap.NewProductionEncoderConfig()
	// Show durations as human‑readable strings (e.g. “5ms”) instead of a float.
	encCfg.EncodeDuration = zapcore.StringDurationEncoder

	switch t {
	case ConsoleEncoder:
		return zapcore.NewConsoleEncoder(encCfg), nil
	case JSONEncoder:
		return zapcore.NewJSONEncoder(encCfg), nil
	default:
		// Unknown encoder – default to JSON and surface a clear error for the caller.
		return zapcore.NewJSONEncoder(encCfg), fmt.Errorf("unsupported encoder type %q, falling back to JSON", t)
	}
}

/* -------------------------------------------------------------------------- */
/*                     Google Cloud Zap Core Implementation                     */
/* -------------------------------------------------------------------------- */

type gcpZapCore struct {
	logger *logging.Logger
	level  zapcore.Level
	fields map[string]interface{}
}

func (c *gcpZapCore) Enabled(lvl zapcore.Level) bool { return lvl >= c.level }

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

func (c *gcpZapCore) Check(ent zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if c.Enabled(ent.Level) {
		return ce.AddCore(ent, c)
	}
	return ce
}

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
	if ent.Caller.Defined {
		payload["source_file"] = ent.Caller.File
		payload["source_line"] = ent.Caller.Line
		payload["source_function"] = ent.Caller.Function
	}
	severity := levelToSeverity(ent.Level)
	c.logger.Log(logging.Entry{
		Timestamp: ent.Time,
		Severity:  severity,
		Payload:   payload,
	})
	return nil
}

func (c *gcpZapCore) Sync() error { return c.logger.Flush() }

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

/* -------------------------------------------------------------------------- */
/*                     Helper to Close All Providers                           */
/* -------------------------------------------------------------------------- */

func closeProviders(provs []provider) error {
	var first error
	for _, p := range provs {
		if err := p.close(); err != nil && first == nil {
			first = fmt.Errorf("provider close error: %w", err)
		}
	}
	return first
}
