package golog

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap/zapcore"
)

/*
	--------------------------------------------------------------
	  Mock provider – used to verify that Close() invokes the provider’s
	  close() method without needing external services (e.g. GCP).

--------------------------------------------------------------
*/
type mockProvider struct {
	closed bool
}

func (m *mockProvider) newCore(level zapcore.Level) (zapcore.Core, error) {
	enc, _ := buildEncoder(JSONEncoder)
	syncer := zapcore.AddSync(io.Discard)
	return zapcore.NewCore(enc, syncer, level), nil
}
func (m *mockProvider) close() error {
	m.closed = true
	return nil
}

/*
	--------------------------------------------------------------
	  Helper to capture output from a logger that writes to an
	  io.Writer (bytes.Buffer in the tests).

--------------------------------------------------------------
*/
func newBufferLogger(t *testing.T, lvl Level) (*Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	logger, err := NewLogger(
		WithWriterProvider(&buf, JSONEncoder),
		WithLevel(lvl),
	)
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}
	return logger, &buf
}

/*
	--------------------------------------------------------------
	  Test basic logging to a bytes.Buffer and field conversion.

--------------------------------------------------------------
*/
func TestLogger_BufferOutput(t *testing.T) {
	logger, buf := newBufferLogger(t, InfoLevel)
	defer logger.Close()

	logger.Info("hello world",
		String("foo", "bar"),
		Int("num", 42),
		Duration("dur", 2*time.Second),
		Err(errors.New("sample error")),
	)

	if buf.Len() == 0 {
		t.Fatalf("expected log output, got empty buffer")
	}
}

/*
	--------------------------------------------------------------
	  Test that the logger respects the configured log level.
	  Debug messages should be omitted when level is Info.

--------------------------------------------------------------
*/
func TestLogger_LevelFiltering(t *testing.T) {
	logger, buf := newBufferLogger(t, InfoLevel)
	defer logger.Close()

	logger.Debug("debug should be filtered")
	logger.Info("info should appear")

	output := buf.String()
	if strings.Contains(output, "debug should be filtered") {
		t.Errorf("debug message was not filtered")
	}
	if !strings.Contains(output, "info should appear") {
		t.Errorf("info message missing")
	}
}

/*
	--------------------------------------------------------------
	  Test file provider – write to a temporary file and verify
	  that the file contains valid JSON.

--------------------------------------------------------------
*/
func TestLogger_FileProvider(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "test.log")

	logger, err := NewLogger(
		WithFileProvider(filePath, 1, 1, 1, false), // tiny rotation params – irrelevant for test
		WithLevel(DebugLevel),
	)
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}
	// Write a log entry.
	logger.Info("file test")
	// Flush and close the logger so the file handle is released.
	if err := logger.Close(); err != nil {
		t.Fatalf("close failed: %v", err)
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("could not read log file: %v", err)
	}
	if len(data) == 0 {
		t.Fatalf("log file is empty")
	}
	if !bytes.Contains(data, []byte(`"file test"`)) {
		t.Errorf("log file does not contain expected message")
	}
}

/*
	--------------------------------------------------------------
	  Test that Close() calls each provider’s close() method.

--------------------------------------------------------------
*/
func TestLogger_CloseCallsProviderClose(t *testing.T) {
	mock := &mockProvider{}

	// Use a buffer (or io.Discard) instead of stdout.
	var buf bytes.Buffer
	logger, err := NewLogger(
		WithWriterProvider(&buf, JSONEncoder), // ← replaces the stdout provider
		WithLevel(InfoLevel),
	)
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}

	// Inject the mock provider so Close() will invoke its close().
	logger.closers = []provider{mock}

	if err := logger.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if !mock.closed {
		t.Errorf("mock provider close() was not called")
	}
}

/*
	--------------------------------------------------------------
	  Test conversion helpers – ensure they produce the expected zapcore
	  fields (indirectly by checking the JSON output).

--------------------------------------------------------------
*/
func TestFieldHelpers_JSONOutput(t *testing.T) {
	logger, buf := newBufferLogger(t, InfoLevel)
	defer logger.Close()

	logger.Info("fields test",
		String("s", "val"),
		Int("i", 7),
		Float64("f", 3.14),
		Duration("d", 5*time.Millisecond),
		Any("any", map[string]string{"k": "v"}),
	)

	out := buf.String()
	expected := []string{
		`"s":"val"`,
		`"i":7`,
		`"f":3.14`,
		`"d":"5ms"`,
		`"any":{"k":"v"}`,
	}
	for _, exp := range expected {
		if !strings.Contains(out, exp) {
			t.Errorf("expected output to contain %s, got %s", exp, out)
		}
	}
}
