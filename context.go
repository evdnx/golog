package golog

import "context"

// ContextKey describes the keys we store structured values under when
// enriching a context for downstream logging.
type ContextKey string

const (
	CorrelationIDKey ContextKey = "correlation_id"
	RequestIDKey     ContextKey = "request_id"
	UserIDKey        ContextKey = "user_id"
	TraceIDKey       ContextKey = "trace_id"
	SpanIDKey        ContextKey = "span_id"
)

// WithCorrelationID attaches a correlation identifier to the context so it can
// later be surfaced in log fields.
func WithCorrelationID(ctx context.Context, id string) context.Context {
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, CorrelationIDKey, id)
}

// WithRequestID records a request identifier on the context.
func WithRequestID(ctx context.Context, id string) context.Context {
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, RequestIDKey, id)
}

// WithUserID records a user identifier on the context.
func WithUserID(ctx context.Context, id string) context.Context {
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, UserIDKey, id)
}

// WithTraceID records a trace identifier on the context.
func WithTraceID(ctx context.Context, id string) context.Context {
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, TraceIDKey, id)
}

// WithSpanID records a span identifier on the context.
func WithSpanID(ctx context.Context, id string) context.Context {
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, SpanIDKey, id)
}

// FieldsFromContext converts known context values into structured logging
// fields. Missing values are ignored, allowing the result to be appended
// directly to a log call: logger.Info("...", FieldsFromContext(ctx)...).
func FieldsFromContext(ctx context.Context) []Field {
	if ctx == nil {
		return nil
	}

	var fields []Field
	if v, _ := ctx.Value(CorrelationIDKey).(string); v != "" {
		fields = append(fields, String(string(CorrelationIDKey), v))
	}
	if v, _ := ctx.Value(RequestIDKey).(string); v != "" {
		fields = append(fields, String(string(RequestIDKey), v))
	}
	if v, _ := ctx.Value(UserIDKey).(string); v != "" {
		fields = append(fields, String(string(UserIDKey), v))
	}
	if v, _ := ctx.Value(TraceIDKey).(string); v != "" {
		fields = append(fields, String(string(TraceIDKey), v))
	}
	if v, _ := ctx.Value(SpanIDKey).(string); v != "" {
		fields = append(fields, String(string(SpanIDKey), v))
	}
	return fields
}
