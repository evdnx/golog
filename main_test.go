package golog

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
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

type countingProvider struct {
	mu         sync.Mutex
	closeCalls int
}

func (p *countingProvider) newCore(level zapcore.Level) (zapcore.Core, error) {
	enc, err := buildEncoder(JSONEncoder)
	if err != nil {
		return nil, err
	}
	return zapcore.NewCore(enc, zapcore.AddSync(io.Discard), level), nil
}

func (p *countingProvider) close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closeCalls++
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

func TestLogger_CloseInvokesProvidersOnce(t *testing.T) {
	counter := &countingProvider{}

	logger, err := NewLogger(func(cfg *loggerConfig) {
		cfg.providers = append(cfg.providers, counter)
	})
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}

	if err := logger.Close(); err != nil {
		t.Fatalf("first Close returned error: %v", err)
	}
	if err := logger.Close(); err != nil {
		t.Fatalf("second Close returned error: %v", err)
	}

	counter.mu.Lock()
	defer counter.mu.Unlock()
	if counter.closeCalls != 1 {
		t.Fatalf("expected provider close() once, got %d", counter.closeCalls)
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
TestSugarMethods validates every *non‑fatal* sugar wrapper:

	Debugf / Infof / Warnf / Errorf
	Debugw / Infow / Warnw / Errorw

The fatal variants (`Fatalf`, `Fatalw`) are intentionally omitted from this
unit test because they invoke `os.Exit(1)` via zap’s internal exit handler,
which would abort the test process. If you need an end‑to‑end verification,
run those calls in a separate binary or use a subprocess harness.
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
	// Ensure resources are cleaned up even if the test fails.
	defer func() {
		if cerr := logger.Close(); cerr != nil {
			t.Fatalf("logger.Close error: %v", cerr)
		}
	}()

	// ------------------------------------------------------------
	// 2️⃣ Exercise every *non‑fatal* sugar method once.
	// ------------------------------------------------------------
	logger.Debugf("debug %d %s", 1, "msg")
	logger.Infof("info %d %s", 2, "msg")
	logger.Warnf("warn %d %s", 3, "msg")
	logger.Errorf("error %d %s", 4, "msg")
	// logger.Fatalf(...)   // ← omitted – would call os.Exit(1)

	logger.Debugw("debugw", "k1", "v1", "k2", 2)
	logger.Infow("infow", "k1", "v1", "k2", 2)
	logger.Warnw("warnw", "k1", "v1", "k2", 2)
	logger.Errorw("errorw", "k1", "v1", "k2", 2)
	// logger.Fatalw(...)   // ← omitted – would call os.Exit(1)

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

	// Expect eight lines – one per call above (four *f + four *w).
	const expectedLines = 8
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

	// ----------- *w variants -----------
	contains(`"msg":"debugw"`, lines[4])
	contains(`"k1":"v1"`, lines[4])
	contains(`"k2":2`, lines[4])

	contains(`"msg":"infow"`, lines[5])
	contains(`"k1":"v1"`, lines[5])
	contains(`"k2":2`, lines[5])

	contains(`"msg":"warnw"`, lines[6])
	contains(`"k1":"v1"`, lines[6])
	contains(`"k2":2`, lines[6])

	contains(`"msg":"errorw"`, lines[7])
	contains(`"k1":"v1"`, lines[7])
	contains(`"k2":2`, lines[7])

	// ------------------------------------------------------------
	// 5️⃣ (Optional) Verify the logger still works after the calls.
	// ------------------------------------------------------------
	logger.Info("post‑check")
	if err := logger.Sync(); err != nil {
		t.Fatalf("sync after post‑check failed: %v", err)
	}
	if !strings.Contains(buf.String(), `"msg":"post-check"`) && !strings.Contains(buf.String(), `"msg":"post‑check"`) {
		// The exact string depends on your Go version’s dash handling.
		t.Errorf("post‑check message missing")
	}
}

/*
	-------------------------------------------------------------
	  A very small thread‑safe buffer that satisfies io.Writer.
	  It forwards all operations to an embedded bytes.Buffer
	  while protecting them with a mutex.

-------------------------------------------------------------
*/
type concurrentBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (c *concurrentBuffer) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.b.Write(p)
}
func (c *concurrentBuffer) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.b.String()
}
func (c *concurrentBuffer) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.b.Len()
}

/*
	-------------------------------------------------------------
	  Helper that builds a logger wired to the safe buffer.

-------------------------------------------------------------
*/
func newSafeBufferLogger(t *testing.T, lvl Level) (*Logger, *concurrentBuffer) {
	var buf concurrentBuffer
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
	-------------------------------------------------------------
	  The actual test – now deterministic.

-------------------------------------------------------------
*/
func TestLogger_ConcurrentWrites(t *testing.T) {
	const workers = 50
	const msgsPerWorker = 200 // 50 × 200 = 10 000 total messages

	logger, buf := newSafeBufferLogger(t, DebugLevel)
	// Ensure everything is flushed before we inspect the buffer.
	defer func() {
		if err := logger.Sync(); err != nil {
			t.Fatalf("final Sync failed: %v", err)
		}
		logger.Close()
	}()

	var wg sync.WaitGroup
	wg.Add(workers)

	for i := 0; i < workers; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < msgsPerWorker; j++ {
				// Make the message unique so Zap’s sampler (if any) won’t drop it.
				msg := fmt.Sprintf("concurrent msg w%02d seq%04d", id, j)
				logger.Info(msg,
					String("worker", fmt.Sprintf("w%d", id)),
					Int("seq", j),
				)
			}
		}(i)
	}
	wg.Wait()

	// Force Zap to write any buffered data.
	if err := logger.Sync(); err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	// Count the lines that actually made it into the buffer.
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	expected := workers * msgsPerWorker
	if len(lines) != expected {
		t.Fatalf("expected %d log lines, got %d", expected, len(lines))
	}
}

func TestFileProvider_InvalidRotationParams(t *testing.T) {
	_, err := NewLogger(
		WithFileProvider("/tmp/bad.log", -1, 0, 0, false), // maxSize negative
		WithLevel(DebugLevel),
	)
	if err == nil {
		t.Fatalf("expected error for negative maxSize, got nil")
	}
	if !strings.Contains(err.Error(), "rotation parameters must be non‑negative") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestBuildEncoder_UnsupportedFallback(t *testing.T) {
	enc, err := buildEncoder(EncoderType("xml")) // deliberately unsupported
	if err == nil {
		t.Fatalf("expected error for unknown encoder")
	}
	if !strings.Contains(err.Error(), "unsupported encoder type") {
		t.Fatalf("unexpected error text: %v", err)
	}
	// The function still returns a JSON encoder so the logger can continue.
	if enc == nil {
		t.Fatalf("expected a non‑nil encoder fallback")
	}
}

func TestLogger_WithContext(t *testing.T) {
	ctx := context.WithValue(context.Background(), "trace_id", "abc123")
	logger, buf := newBufferLogger(t, InfoLevel)
	defer logger.Close()

	// Suppose you add a method `InfoCtx(ctx, ...)` that pulls fields from ctx.
	// For now we can simulate it manually:
	logger.Info("ctx test", Any("trace_id", ctx.Value("trace_id")))

	if !strings.Contains(buf.String(), `"trace_id":"abc123"`) {
		t.Errorf("expected trace_id field in log output")
	}
}

func TestLogger_CloseIdempotent(t *testing.T) {
	logger, _ := newBufferLogger(t, InfoLevel)

	if err := logger.Close(); err != nil {
		t.Fatalf("first Close failed: %v", err)
	}
	// Second call – should be a no‑op.
	if err := logger.Close(); err != nil {
		t.Fatalf("second Close returned error: %v", err)
	}
}

func TestIgnoreSyncError(t *testing.T) {
	err := &os.PathError{
		Op:   "sync",
		Path: "/dev/stdout",
		Err:  syscall.ENOTTY,
	}
	if ignoreSyncError(err) != nil {
		t.Fatalf("expected ENOTTY path errors to be ignored")
	}
	if ignoreSyncError(syscall.EIO) == nil {
		t.Fatalf("non-ignorable errors should be returned")
	}
}
