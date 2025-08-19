package golog

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cloud.google.com/go/logging"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// mockGCPProvider is a test-only provider that mocks Google Cloud Logging.
type mockGCPProvider struct {
	projectID string
	logName   string
	logs      []logging.Entry // Store logs for verification
}

func (p *mockGCPProvider) newCore(level zapcore.Level) (zapcore.Core, error) {
	return &mockGCPZapCore{
		logs:  &p.logs,
		level: level,
	}, nil
}

// mockGCPZapCore is a test-only zapcore.Core for mocking GCP logging.
type mockGCPZapCore struct {
	logs  *[]logging.Entry
	level zapcore.Level
}

func (c *mockGCPZapCore) Enabled(lvl zapcore.Level) bool {
	return lvl >= c.level
}

func (c *mockGCPZapCore) With(fields []zapcore.Field) zapcore.Core {
	return c
}

func (c *mockGCPZapCore) Check(ent zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if c.Enabled(ent.Level) {
		return ce.AddCore(ent, c)
	}
	return ce
}

func (c *mockGCPZapCore) Write(ent zapcore.Entry, fields []zapcore.Field) error {
	enc := zapcore.NewMapObjectEncoder()
	for _, f := range fields {
		f.AddTo(enc)
	}
	enc.Fields["message"] = ent.Message
	*c.logs = append(*c.logs, logging.Entry{
		Timestamp: ent.Time,
		Severity:  levelToSeverity(ent.Level),
		Payload:   enc.Fields,
	})
	return nil
}

func (c *mockGCPZapCore) Sync() error {
	return nil
}

func TestNewLogger(t *testing.T) {
	tests := []struct {
		name    string
		options []LoggerOption
		wantErr bool
	}{
		{
			name:    "No providers",
			options: []LoggerOption{},
			wantErr: true,
		},
		{
			name: "Stdout provider",
			options: []LoggerOption{
				WithStdOutProvider(JSONEncoder),
				WithLevel(InfoLevel),
			},
			wantErr: false,
		},
		{
			name: "Writer provider",
			options: []LoggerOption{
				WithWriterProvider(&bytes.Buffer{}, ConsoleEncoder),
				WithLevel(DebugLevel),
			},
			wantErr: false,
		},
		{
			name: "File provider",
			options: []LoggerOption{
				WithFileProvider(filepath.Join(t.TempDir(), "test.log"), 1, 2, 3, true),
				WithLevel(DebugLevel),
			},
			wantErr: false,
		},
		{
			name: "Multiple providers",
			options: []LoggerOption{
				WithStdOutProvider(ConsoleEncoder),
				WithFileProvider(filepath.Join(t.TempDir(), "test.log"), 1, 2, 3, false),
				WithLevel(WarnLevel),
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewLogger(tt.options...)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewLogger() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestLoggerLevels(t *testing.T) {
	tests := []struct {
		name      string
		logFunc   func(*Logger) func(string, ...Field)
		message   string
		fields    []Field
		wantLevel zapcore.Level
	}{
		{
			name:      "Debug",
			logFunc:   func(l *Logger) func(string, ...Field) { return l.Debug },
			message:   "Debug message",
			fields:    []Field{String("key", "value")},
			wantLevel: zapcore.DebugLevel,
		},
		{
			name:      "Info",
			logFunc:   func(l *Logger) func(string, ...Field) { return l.Info },
			message:   "Info message",
			fields:    []Field{Int("count", 42)},
			wantLevel: zapcore.InfoLevel,
		},
		{
			name:      "Warn",
			logFunc:   func(l *Logger) func(string, ...Field) { return l.Warn },
			message:   "Warn message",
			fields:    []Field{Float64("value", 3.14)},
			wantLevel: zapcore.WarnLevel,
		},
		{
			name:      "Error",
			logFunc:   func(l *Logger) func(string, ...Field) { return l.Error },
			message:   "Error message",
			fields:    []Field{Error(errors.New("test error"))},
			wantLevel: zapcore.ErrorLevel,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a new observer and buffer for each test case
			var buf bytes.Buffer
			observerCore, logs := observer.New(zapcore.DebugLevel)
			encoderCfg := zap.NewProductionEncoderConfig()
			encoder := zapcore.NewConsoleEncoder(encoderCfg)
			syncer := zapcore.AddSync(&buf)
			bufferCore := zapcore.NewCore(encoder, syncer, zapcore.DebugLevel)
			teeCore := zapcore.NewTee(observerCore, bufferCore)
			logger := &Logger{zapLogger: zap.New(teeCore, zap.WithCaller(false))}

			tt.logFunc(logger)(tt.message, tt.fields...)
			if logs.Len() != 1 {
				t.Errorf("Expected 1 log entry, got %d", logs.Len())
			}
			if logs.Len() > 0 {
				entry := logs.All()[0]
				if entry.Level != tt.wantLevel {
					t.Errorf("Expected level %v, got %v", tt.wantLevel, entry.Level)
				}
				if entry.Message != tt.message {
					t.Errorf("Expected message %q, got %q", tt.message, entry.Message)
				}
			}
		})
	}
}

func TestStructuredFields(t *testing.T) {
	tests := []struct {
		name  string
		field Field
		check func(t *testing.T, fields []zapcore.Field)
	}{
		{
			name:  "String",
			field: String("key", "value"),
			check: func(t *testing.T, fields []zapcore.Field) {
				if len(fields) != 1 || fields[0].Key != "key" || fields[0].Type != zapcore.StringType || fields[0].String != "value" {
					t.Errorf("Expected String field {key: value}, got %v", fields)
				}
			},
		},
		{
			name:  "Int",
			field: Int("count", 42),
			check: func(t *testing.T, fields []zapcore.Field) {
				if len(fields) != 1 || fields[0].Key != "count" || fields[0].Type != zapcore.Int64Type || fields[0].Integer != 42 {
					t.Errorf("Expected Int field {count: 42}, got %v", fields)
				}
			},
		},
		{
			name:  "Float64",
			field: Float64("value", 3.14),
			check: func(t *testing.T, fields []zapcore.Field) {
				if len(fields) != 1 || fields[0].Key != "value" || fields[0].Type != zapcore.Float64Type || fields[0].Integer != int64(3.14*1e9) {
					t.Errorf("Expected Float64 field {value: 3.14}, got %v", fields)
				}
			},
		},
		{
			name:  "Error",
			field: Error(errors.New("test error")),
			check: func(t *testing.T, fields []zapcore.Field) {
				if len(fields) != 1 || fields[0].Key != "error" || fields[0].Type != zapcore.ErrorType {
					t.Errorf("Expected Error field {error: test error}, got %v", fields)
				}
				if err, ok := fields[0].Interface.(error); !ok || err.Error() != "test error" {
					t.Errorf("Expected error value 'test error', got %v", fields[0].Interface)
				}
			},
		},
		{
			name:  "Duration",
			field: Duration("elapsed", 150*time.Millisecond),
			check: func(t *testing.T, fields []zapcore.Field) {
				if len(fields) != 1 || fields[0].Key != "elapsed" || fields[0].Type != zapcore.DurationType || fields[0].Integer != int64(150*time.Millisecond) {
					t.Errorf("Expected Duration field {elapsed: 150ms}, got %v", fields)
				}
			},
		},
		{
			name:  "Any",
			field: Any("data", map[string]int{"count": 42}),
			check: func(t *testing.T, fields []zapcore.Field) {
				if len(fields) != 1 || fields[0].Key != "data" || fields[0].Type != zapcore.ReflectType {
					t.Errorf("Expected Any field {data: map}, got %v", fields)
				}
				if m, ok := fields[0].Interface.(map[string]int); !ok || m["count"] != 42 {
					t.Errorf("Expected map[count]=42, got %v", fields[0].Interface)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a new observer and buffer for each test case
			var buf bytes.Buffer
			observerCore, logs := observer.New(zapcore.DebugLevel)
			encoderCfg := zap.NewProductionEncoderConfig()
			encoder := zapcore.NewConsoleEncoder(encoderCfg)
			syncer := zapcore.AddSync(&buf)
			bufferCore := zapcore.NewCore(encoder, syncer, zapcore.DebugLevel)
			teeCore := zapcore.NewTee(observerCore, bufferCore)
			logger := &Logger{zapLogger: zap.New(teeCore, zap.WithCaller(false))}

			logger.Info("Test", tt.field)
			if logs.Len() != 1 {
				t.Errorf("Expected 1 log entry, got %d", logs.Len())
			}
			if logs.Len() > 0 {
				tt.check(t, logs.All()[0].Context)
			}
		})
	}
}

func TestGCPProviderMock(t *testing.T) {
	// Mock GCP provider
	gcpMock := &mockGCPProvider{projectID: "test-project", logName: "test-log"}

	// Override WithGCPProvider for this test
	origWithGCPProvider := WithGCPProvider
	defer func() { WithGCPProvider = origWithGCPProvider }()
	WithGCPProvider = func(projectID, logName string) LoggerOption {
		return func(cfg *loggerConfig) {
			cfg.providers = append(cfg.providers, gcpMock)
		}
	}

	log, err := NewLogger(
		WithGCPProvider("test-project", "test-log"),
		WithLevel(InfoLevel),
	)
	if err != nil {
		t.Fatalf("NewLogger() error = %v", err)
	}

	log.Info("Test message", String("key", "value"))
	if err := log.Sync(); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	if len(gcpMock.logs) != 1 {
		t.Errorf("Expected 1 log entry, got %d", len(gcpMock.logs))
	}
	if len(gcpMock.logs) > 0 {
		entry := gcpMock.logs[0]
		if entry.Payload.(map[string]interface{})["message"] != "Test message" {
			t.Errorf("Expected message %q, got %q", "Test message", entry.Payload.(map[string]interface{})["message"])
		}
		if entry.Payload.(map[string]interface{})["key"] != "value" {
			t.Errorf("Expected key=value, got %v", entry.Payload.(map[string]interface{})["key"])
		}
	}
}

func TestFileProviderConfig(t *testing.T) {
	tempDir := t.TempDir()
	filename := filepath.Join(tempDir, "test.log")
	log, err := NewLogger(
		WithFileProvider(filename, 1, 2, 3, true),
		WithLevel(InfoLevel),
	)
	if err != nil {
		t.Fatalf("NewLogger() error = %v", err)
	}

	log.Info("Test message")
	if err := log.Sync(); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		t.Errorf("Expected log file %s to exist", filename)
	}
}

func TestWriterProvider(t *testing.T) {
	var buf bytes.Buffer
	log, err := NewLogger(
		WithWriterProvider(&buf, ConsoleEncoder),
		WithLevel(DebugLevel),
	)
	if err != nil {
		t.Fatalf("NewLogger() error = %v", err)
	}

	log.Info("Test message", String("key", "value"))
	if err := log.Sync(); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Test message") {
		t.Errorf("Expected output to contain 'Test message', got %q", output)
	}
	if !strings.Contains(output, "key=value") {
		t.Errorf("Expected output to contain 'key=value', got %q", output)
	}
}
