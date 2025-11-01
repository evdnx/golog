# Go Logger Package  
A robust, extensible logging package for Go built on top of Uber’s **Zap** logger.  
It supports multiple logging providers (standard output, custom writer, Google Cloud Logging, and file logging with rotation) via a simple, dependency‑free public API while keeping third‑party packages hidden from the consumer.

## Features  
- **Multiple Providers** – Log to stdout, any `io.Writer`, Google Cloud Logging, or a rotating file (or any combination thereof).  
- **Structured Logging** – Key‑value fields without exposing Zap internals.  
- **Custom Log Levels** – `Debug`, `Info`, `Warn`, `Error`, `Fatal`.  
- **Log Rotation** – Size‑based rotation with configurable backups, age, and optional gzip compression.  
- **Zero‑Dependency API** – Users import only `github.com/evdnx/golog`; the package internally manages Zap, GCP, and Lumberjack.  
- **Extensible Design** – New providers can be added by implementing the internal `provider` interface.  
- **High Performance** – Leverages Zap’s low‑overhead core.
- **Graceful Shutdown** – `Close()` flushes providers exactly once and ignores benign stdout/stderr sync errors.

## Installation  
```bash
go get github.com/evdnx/golog  
```

## Getting Started  
```go
package main

import (
	"bytes"
	"errors"
	"time"

	"github.com/evdnx/golog"
)

func main() {
	var buf bytes.Buffer

	// Create a logger that writes to a buffer, GCP, and a rotating file.
	logger, err := golog.NewLogger(
		golog.WithWriterProvider(&buf, golog.ConsoleEncoder),
		golog.WithGCPProvider("my-project-id", "my-log-name"),
		golog.WithFileProvider("/var/log/myapp.log", 10, 3, 7, true), // 10 MiB, 3 backups, 7 days, gzip
		golog.WithLevel(golog.DebugLevel),
	)
	if err != nil {
		panic(err)
	}
	defer logger.Close() // flushes and releases resources

	// Close is idempotent and safe to call multiple times (e.g. in deferred cleanup helpers).

	logger.Info("Application started",
		golog.String("app", "my-app"),
		golog.Int("version", 1),
	)

	// Example of logging a timed operation
	start := time.Now()
	time.Sleep(150 * time.Millisecond) // simulate work
	logger.Info("Operation completed",
		golog.String("operation", "example_task"),
		golog.Duration("elapsed", time.Since(start)),
	)

	// Log a structured result
	type Result struct {
		Value  interface{}
		Status string
	}
	res := Result{
		Value:  map[string]int{"count": 42},
		Status: "OK",
	}
	logger.Info("Result",
		golog.String("status", res.Status),
		golog.Any("value", res.Value),
	)

	// Log an error
	logger.Error("Something went wrong", golog.Error(errors.New("example error")))
}
```

## Configuration Options  

| Option                                 | Description                                                                                                    |
|----------------------------------------|----------------------------------------------------------------------------------------------------------------|
| `WithStdOutProvider(encoder EncoderType)` | Sends logs to `os.Stdout`. `encoder` can be `golog.JSONEncoder` (machine‑readable) or `golog.ConsoleEncoder` (human‑readable). |
| `WithWriterProvider(w io.Writer, encoder EncoderType)` | Sends logs to any `io.Writer` (e.g., a `bytes.Buffer`).                                                       |
| `WithGCPProvider(projectID, logName string)` | Sends logs to Google Cloud Logging under the given project and log name.                                        |
| `WithFileProvider(path string, maxSize, maxBackups, maxAge int, compress bool)` | Writes logs to a file with rotation. See **Log Rotation** below for parameter meanings.                         |
| `WithLevel(l Level)`                   | Sets the minimum level that will be emitted (`DebugLevel` … `FatalLevel`).                                      |

### Log Rotation Details  

| Parameter      | Meaning                                                                                     |
|----------------|---------------------------------------------------------------------------------------------|
| `maxSize` (MiB) | Rotate when the file exceeds this size.                                                     |
| `maxBackups`   | Maximum number of rotated files to keep (e.g., `myapp.log.1`, `myapp.log.2`, …).          |
| `maxAge` (days) | Delete rotated files older than this many days.                                            |
| `compress`     | If `true`, rotated files are gzipped (`*.gz`).                                             |

> **Tip:** The underlying implementation uses **lumberjack**; all values are passed straight through, so the semantics match the lumberjack documentation.

## Logging Methods
| Method | Signature | Example |
|--------|-----------|---------|
| `Debug(msg string, fields …Field)` | `Debug(msg string, fields …Field)` | `logger.Debug("starting job", golog.String("jobID", id))` |
| `Info(msg string, fields …Field)` | `Info(msg string, fields …Field)` | `logger.Info("user logged in", golog.String("user", name))` |
| `Warn(msg string, fields …Field)` | `Warn(msg string, fields …Field)` | `logger.Warn("disk space low", golog.Int("percent", 5))` |
| `Error(msg string, fields …Field)` | `Error(msg string, fields …Field)` | `logger.Error("request failed", golog.Error(err))` |
| `Fatal(msg string, fields …Field)` | `Fatal(msg string, fields …Field)` | `logger.Fatal("unrecoverable error", golog.Error(err))` |
| `Sync() error` | `Sync() error` | `if err := logger.Sync(); err != nil { … }` |
| `Close() error` | `Close() error` | `defer logger.Close()` |
| **Sugared (formatted) methods** | | |
| `Debugf(format string, args …interface{})` | `Debugf(format string, args …interface{})` | `logger.Debugf("processing %d items", n)` |
| `Infof(format string, args …interface{})` | `Infof(format string, args …interface{})` | `logger.Infof("user %s logged in", username)` |
| `Warnf(format string, args …interface{})` | `Warnf(format string, args …interface{})` | `logger.Warnf("retry %d of %d", attempt, maxAttempts)` |
| `Errorf(format string, args …interface{})` | `Errorf(format string, args …interface{})` | `logger.Errorf("failed to open %s: %v", path, err)` |
| `Fatalf(format string, args …interface{})` | `Fatalf(format string, args …interface{})` | `logger.Fatalf("cannot start: %v", err)` |
| **Sugared (key/value) methods** | | |
| `Debugw(msg string, keysAndValues …interface{})` | `Debugw(msg string, keysAndValues …interface{})` | `logger.Debugw("cache miss", "key", k, "ttl", ttl)` |
| `Infow(msg string, keysAndValues …interface{})` | `Infow(msg string, keysAndValues …interface{})` | `logger.Infow("order placed", "orderID", id, "amount", amt)` |
| `Warnw(msg string, keysAndValues …interface{})` | `Warnw(msg string, keysAndValues …interface{})` | `logger.Warnw("high latency", "endpoint", url, "ms", ms)` |
| `Errorw(msg string, keysAndValues …interface{})` | `Errorw(msg string, keysAndValues …interface{})` | `logger.Errorw("db error", "query", q, "err", err)` |
| `Fatalw(msg string, keysAndValues …interface{})` | `Fatalw(msg string, keysAndValues …interface{})` | `logger.Fatalw("service crash", "reason", r)` |


## Structured Field Helpers  

| Helper   | Signature                              | Example                                   |
|----------|----------------------------------------|-------------------------------------------|
| `String` | `String(key, value string) Field`      | `golog.String("user", "alice")`          |
| `Int`    | `Int(key string, value int) Field`    | `golog.Int("attempts", 3)`               |
| `Float64`| `Float64(key string, value float64) Field` | `golog.Float64("ratio", 0.75)`          |
| `Error`  | `Error(err error) Field`               | `golog.Error(err)`                       |
| `Duration`| `Duration(key string, d time.Duration) Field` | `golog.Duration("latency", 120*time.Millisecond)` |
| `Any`    | `Any(key string, v interface{}) Field` | `golog.Any("payload", myStruct)`         |

## Running the Test Suite  
```bash
go test -v ./...
```

The suite covers:

- Creation of a logger with each provider (stdout, writer, GCP mock, file).  
- Emission at every log level.  
- Correct encoding of all field helpers.  
- Validation of rotation parameters and graceful cleanup.  

> **Note:** The GCP tests use a mock client, so no credentials are required.

## Release Policy  

We follow **Semantic Versioning** (`MAJOR.MINOR.PATCH`). Pre‑releases use suffixes such as `-alpha.1`, `-beta.2`, or `-rc.1`. Tag examples:

- `v0.0.0` – early prototype.  
- `v1.0.0` – first stable release.

Release notes are attached to each tag.

## Extending the Package  

To add a new provider (e.g., a custom cloud service):

1. Implement the internal `provider` interface:

    ```go
    type myProvider struct { /* config fields */ }
    func (p *myProvider) newCore(level zapcore.Level) (zapcore.Core, error) { … }
    func (p *myProvider) close() error { … } // optional
    ```

2. Export a functional option:

    ```go
    func WithMyProvider(arg1 string, arg2 int) LoggerOption {
        return func(cfg *loggerConfig) {
            cfg.providers = append(cfg.providers, &myProvider{…})
        }
    }
    ```

3. Add documentation and, optionally, tests.

## License  

MIT-0 (no attrib) – see the `LICENSE` file.
