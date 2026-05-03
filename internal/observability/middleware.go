package observability

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

var tracer = otel.Tracer("unified-document-viewer")

func Tracer() trace.Tracer {
	return tracer
}

func TracingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, span := tracer.Start(r.Context(), r.Method+" "+r.URL.Path)
		defer span.End()

		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r.WithContext(ctx))

		span.SetAttributes(
			attribute.Int("http.status_code", ww.Status()),
			attribute.String("http.method", r.Method),
			attribute.String("http.route", r.URL.Path),
		)
	})
}

func LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

		next.ServeHTTP(ww, r.WithContext(r.Context()))

		requestID := middleware.GetReqID(r.Context())
		spanCtx := trace.SpanFromContext(r.Context()).SpanContext()
		traceID := spanCtx.TraceID().String()

		slog.Info("request completed",
			"request_id", requestID,
			"trace_id", traceID,
			"method", r.Method,
			"path", r.URL.Path,
			"status", ww.Status(),
			"duration_ms", time.Since(start).Milliseconds(),
		)
	})
}

func MaskVIN(vin string) string {
	if len(vin) <= 6 {
		return vin
	}
	return "***" + vin[len(vin)-6:]
}
