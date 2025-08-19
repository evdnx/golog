# Go Logger Package

A robust, extensible logging package for Go, built on top of Uber's Zap logger. It supports multiple logging providers (standard output, custom writer, Google Cloud Logging, and file logging with rotation) with a simple, dependency-free API for users. The package is designed to be flexible, allowing for easy addition of new providers in the future while hiding third-party dependencies from the end user.

## Features

- **Multiple Providers**: Log to standard output, custom writer (e.g., buffer), Google Cloud Logging, file with rotation, or any combination of these.
- **Structured Logging**: Supports key-value fields for structured logging without exposing Zap internals.
- **Customizable Log Levels**: Choose from `Debug`, `Info`, `Warn`, `Error`, or `Fatal` levels.
- **Log Rotation**: File provider supports log rotation with configurable size, backup count, age, and compression.
- **Dependency-Free API**: No need to import third-party packages like `go.uber.org/zap`, `cloud.google.com/go/logging`, or `gopkg.in/natefinch/lumberjack.v2`.
- **Extensible Design**: Easily add new logging providers internally without changing the public API.
- **High Performance**: Leverages Zap's performance optimizations under the hood.

## Installation

To use this package, add it to your Go project:

```bash
go get github.com/evdnx/golog
```

Replace `github.com/evdnx/golog` with the actual repository path where the package is hosted.

## Usage

The package provides a simple API for creating and using a logger. Below is an example of how to set up and use the logger with a custom writer, Google Cloud Logging, and file providers with rotation.

### Example

```go
package main

import (
	"bytes"
	"errors"
	"time"

	"github.com/evdnx/golog"
)

type Result struct {
	Value  interface{}
	Status string
}

func main() {
	// Set up a buffer to capture log output
	var buf bytes.Buffer

	// Create a logger with writer, GCP, and file providers
	log, err := golog.NewLogger(
		golog.WithWriterProvider(&buf, golog.ConsoleEncoder),
		golog.WithGCPProvider("my-project-id", "my-log-name"),
		golog.WithFileProvider("/var/log/myapp.log", 10, 3, 7, true), // 10MB, 3 backups, 7 days, compress
		golog.WithLevel(golog.DebugLevel),
	)
	if err != nil {
		panic(err)
	}
	defer log.Sync()

	// Log application start
	log.Info("Application started", golog.String("app", "my-app"), golog.Int("version", 1))

	// Simulate an operation with timing
	start := time.Now()
	time.Sleep(150 * time.Millisecond) // Simulate work
	duration := time.Since(start)

	// Log operation with duration
	log.Info("Operation completed",
		golog.String("operation", "example_task"),
		golog.Duration("elapsed", duration),
	)

	// Log a result with Any
	result := Result{
		Value:  map[string]int{"count": 42},
		Status: "Ok",
	}
	log.Info("Result", golog.String("status", result.Status), golog.Any("value", result.Value))

	// Log an error
	log.Error("Something went wrong", golog.Error(errors.New("example error")))

	// Log debug info
	log.Debug("Debugging info", golog.Float64("value", 42.5))

	// Print buffer contents (for demonstration)
	// fmt.Println(buf.String())
}
```

### Configuration Options

- **`WithStdOutProvider(encoderType EncoderType)`**: Configures logging to standard output. The `encoderType` can be `logger.JSONEncoder` for JSON output or `logger.ConsoleEncoder` for human-readable output.
- **`WithWriterProvider(writer io.Writer, encoderType EncoderType)`**: Configures logging to a custom `io.Writer` (e.g., a buffer or file). The `encoderType` can be `logger.JSONEncoder` or `logger.ConsoleEncoder`.
- **`WithGCPProvider(projectID, logName string)`**: Configures logging to Google Cloud Logging with the specified GCP project ID and log name.
- **`WithFileProvider(filename string, maxSize, maxBackups, maxAge int, compress bool)`**: Configures logging to a file with rotation. Parameters include:
  - `filename`: Path to the log file (e.g., `/var/log/myapp.log`).
  - `maxSize`: Maximum file size in megabytes before rotation.
  - `maxBackups`: Maximum number of old log files to retain.
  - `maxAge`: Maximum number of days to retain old log files.
  - `compress`: Whether to compress rotated log files using gzip.
- **`WithLevel(level Level)`**: Sets the minimum log level (`DebugLevel`, `InfoLevel`, `WarnLevel`, `ErrorLevel`, or `FatalLevel`).

### Logging Methods

The `Logger` type provides the following methods:

- `Debug(msg string, fields ...Field)`: Logs a message at the Debug level.
- `Info(msg string, fields ...Field)`: Logs a message at the Info level.
- `Warn(msg string, fields ...Field)`: Logs a message at the Warn level.
- `Error(msg string, fields ...Field)`: Logs a message at the Error level.
- `Fatal(msg string, fields ...Field)`: Logs a message at the Fatal level and exits the program.
- `Sync()`: Flushes any buffered log entries.

### Structured Logging Fields

Use the following helper functions to create structured logging fields:

- `String(key, value string)`: Adds a string field.
- `Int(key string, value int)`: Adds an integer field.
- `Float64(key string, value float64)`: Adds a float64 field.
- `Error(err error)`: Adds an error field.
- `Duration(key string, value time.Duration)`: Adds a duration field (e.g., for logging elapsed time).
- `Any(key string, value interface{})`: Adds a field with an arbitrary value (e.g., structs, maps, or slices).

### Log Rotation

The file provider supports log rotation using the following settings:
- **MaxSize**: Rotates the log file when it exceeds the specified size (in megabytes).
- **MaxBackups**: Limits the number of old log files retained.
- **MaxAge**: Deletes old log files after the specified number of days.
- **Compress**: Compresses rotated log files using gzip to save disk space.

For example, `WithFileProvider("/var/log/myapp.log", 10, 3, 7, true)` configures the logger to:
- Write logs to `/var/log/myapp.log`.
- Rotate when the file exceeds 10MB.
- Keep up to 3 backup files (e.g., `myapp.log.1`, `myapp.log.2`, etc.).
- Delete backups older than 7 days.
- Compress rotated files.

## Running Tests

The package includes a comprehensive test suite to verify its functionality. To run the tests, navigate to the package directory and execute:

```bash
go test -v ./...
```

The tests cover:
- Logger creation with different providers (stdout, writer, file, GCP).
- Logging at various levels (`Debug`, `Info`, `Warn`, `Error`).
- Structured logging fields (`String`, `Int`, `Float64`, `Error`, `Duration`, `Any`).
- Configuration options and error handling.

Note: Tests for the GCP provider use a mock to avoid real API calls, so no GCP credentials are required.

## Releases

This project uses Semantic Versioning (SemVer) with tags in the format `vX.Y.Z` (e.g., `v1.0.0`). Pre-release versions may include suffixes like `-alpha.1`, `-beta.2`, or `-rc.1`. Tags are annotated with release notes for clarity. For example, `v0.0.0` may be used for early prototypes, while `v1.0.0` indicates a stable release.

## Design Principles

- **Encapsulation**: Hides third-party dependencies (`go.uber.org/zap`, `cloud.google.com/go/logging`, `gopkg.in/natefinch/lumberjack.v2`) to simplify the user experience.
- **Extensibility**: Uses an internal provider interface to allow easy addition of new logging backends without modifying the public API.
- **Simplicity**: Provides a clean, minimal API focused on common logging use cases.
- **Performance**: Builds on Zap's high-performance logging core for efficient logging.

## Adding New Providers

To add a new logging provider (e.g., for a different cloud service or output format), implement the internal `provider` interface within the package. The interface requires a `newCore(level zapcore.Level) (zapcore.Core, error)` method. Then, add a new `With<ProviderName>Provider` function to the public API to allow users to configure it.

## Dependencies

The package internally depends on:
- `go.uber.org/zap`: For high-performance logging.
- `cloud.google.com/go/logging`: For Google Cloud Logging integration.
- `gopkg.in/natefinch/lumberjack.v2`: For log rotation in the file provider.

Users do not need to import these dependencies directly, as the package encapsulates all functionality.

## License

This package is licensed under the MIT License. See the `LICENSE` file for details.

## Contributing

Contributions are welcome! Please submit a pull request or open an issue on the repository to suggest improvements or report bugs.
