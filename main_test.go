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

/*
TestSugarMethods validates every sugar wrapper you introduced:

	Debugf / Infof / Warnf / Errorf / Fatalf
	Debugw / Infow / Warnw / Errorw / Fatalw

The test:

	1️⃣ Creates a logger that writes JSON to a bytes.Buffer.
	2️⃣ Calls each method once with distinct data.
	3️⃣ Flushes the logger so all entries reach the buffer.
	4️⃣ Parses the buffer line‑by‑line and asserts that the expected
	   fields (message text, formatted values, key/value pairs) appear.
	5️⃣ Confirms that the fatal methods still emit a log line
	   (the actual process‑exit is not intercepted because zap.Exit
	   isn’t exported in the bundled version).
*/
func TestSugarMethods(t *testing.T) {
	// ------------------------------------------------------------
	// 1️⃣ Build a logger that writes JSON to an in‑memory buffer.
	// ------------------------------------------------------------
	var buf bytes.Buffer
	logger, err := NewLogger(
		WithWriterProvider(&buf, JSONEncoder), // JSON makes string checks easy
		WithLevel(DebugLevel),                 // emit everything
	)
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}
	// Ensure we always clean up resources.
	defer func() {
		if cerr := logger.Close(); cerr != nil {
			t.Fatalf("logger.Close error: %v", cerr)
		}
	}()

	// ------------------------------------------------------------
	// 2️⃣ Exercise every sugar method once.
	// ------------------------------------------------------------
	logger.Debugf("debug %d %s", 1, "msg")
	logger.Infof("info %d %s", 2, "msg")
	logger.Warnf("warn %d %s", 3, "msg")
	logger.Errorf("error %d %s", 4, "msg")
	// Fatal* still logs before exiting; we only care about the log line.
	logger.Fatalf("fatal %d %s", 5, "msg")

	logger.Debugw("debugw", "k1", "v1", "k2", 2)
	logger.Infow("infow", "k1", "v1", "k2", 2)
	logger.Warnw("warnw", "k1", "v1", "k2", 2)
	logger.Errorw("errorw", "k1", "v1", "k2", 2)
	logger.Fatalw("fatalw", "k1", "v1", "k2", 2) // same note as Fatalf

	// ------------------------------------------------------------
	// 3️⃣ Flush the logger so all buffered entries are written.
	// ------------------------------------------------------------
	if err := logger.Sync(); err != nil {
		t.Fatalf("logger.Sync error: %v", err)
	}

	// ------------------------------------------------------------
	// 4️⃣ Inspect the output.
	// ------------------------------------------------------------
	output := buf.String()
	lines := strings.Split(strings.TrimSpace(output), "\n")

	// We expect ten lines – one per call above.
	const expectedLines = 10
	if len(lines) != expectedLines {
		t.Fatalf("expected %d log lines, got %d:\n%s", expectedLines, len(lines), output)
	}

	// Helper to assert that a line contains a particular substring.
	contains := func(substr, line string) {
		if !strings.Contains(line, substr) {
			t.Errorf("expected line to contain %q, got:\n%s", substr, line)
		}
	}

	// ----------- *f variants -----------
	contains(`"msg":"debug 1 msg"`, lines[0])
	contains(`"msg":"info 2 msg"`, lines[1])
	contains(`"msg":"warn 3 msg"`, lines[2])
	contains(`"msg":"error 4 msg"`, lines[3])
	contains(`"msg":"fatal 5 msg"`, lines[4]) // Fatalf still logs

	// ----------- *w variants -----------
	contains(`"msg":"debugw"`, lines[5])
	contains(`"k1":"v1"`, lines[5])
	contains(`"k2":2`, lines[5])

	contains(`"msg":"infow"`, lines[6])
	contains(`"k1":"v1"`, lines[6])
	contains(`"k2":2`, lines[6])

	contains(`"msg":"warnw"`, lines[7])
	contains(`"k1":"v1"`, lines[7])
	contains(`"k2":2`, lines[7])

	contains(`"msg":"errorw"`, lines[8])
	contains(`"k1":"v1"`, lines[8])
	contains(`"k2":2`, lines[8])

	contains(`"msg":"fatalw"`, lines[9])
	contains(`"k1":"v1"`, lines[9])
	contains(`"k2":2`, lines[9])

	// ------------------------------------------------------------
	// 5️⃣ (Optional) Verify that the logger still works after fatal calls.
	// ------------------------------------------------------------
	// Emit another entry to prove the logger wasn’t left in a broken state.
	logger.Info("post‑fatal check")
	if err := logger.Sync(); err != nil {
		t.Fatalf("sync after post‑fatal check failed: %v", err)
	}
	if !strings.Contains(buf.String(), `"msg":"post-fatal check"`) {
		t.Errorf("post‑fatal check message missing")
	}
}
