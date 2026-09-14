package metrics

import (
	"net/http"
	"strings"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

// ServerMiddleware records http.server.request.duration for everything the
// server answers. When metrics are off it hands back the handler it was given:
// the instrumentation does per-request work before it discovers the instrument
// records nothing, so the cheap path has to skip the wrapper entirely.
func (m *Metrics) ServerMiddleware(operation string) func(http.Handler) http.Handler {
	if !m.enabled {
		return func(next http.Handler) http.Handler { return next }
	}

	return func(next http.Handler) http.Handler {
		return otelhttp.NewHandler(next, operation,
			otelhttp.WithMeterProvider(m.provider),
			// Explicit no-ops rather than the OpenTelemetry globals: this
			// service injects what its components use, and a global would be
			// whatever some other package set last.
			otelhttp.WithTracerProvider(tracenoop.NewTracerProvider()),
			otelhttp.WithPropagators(propagation.NewCompositeTextMapPropagator()),
			otelhttp.WithFilter(func(r *http.Request) bool {
				// A kubelet polls these every few seconds and learns nothing
				// from their latency.
				return r.URL.Path != "/healthz" && r.URL.Path != "/readyz"
			}),
		)
	}
}

// RouteTag puts the matched pattern on the request's metrics.
//
// The instrumentation can read a route from http.Request.Pattern by itself, and
// ServeMux sets that field in place — but the middleware between them hands the
// mux a copy of the request, so the wrapper on the outside looks at a Pattern
// that was never filled in. The result is a metric that arrives complete and
// useless, with no route to group by and no test failing.
//
// The labeler lives in the context, which survives being copied, and is read
// after the handler returns. So this runs beside the mux, where the request it
// holds is the one the mux mutates.
func (m *Metrics) RouteTag(next http.Handler) http.Handler {
	if !m.enabled {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		labeler, ok := otelhttp.LabelerFromContext(r.Context())
		next.ServeHTTP(w, r)

		if ok && r.Pattern != "" {
			labeler.Add(semconv.HTTPRoute(routeOf(r.Pattern)))
		}
	})
}

// routeOf drops the method and host a Go pattern may carry, leaving the path —
// which is a literal an operator registered, so the attribute cannot grow
// without bound.
func routeOf(pattern string) string {
	if slash := strings.IndexByte(pattern, '/'); slash >= 0 {
		return pattern[slash:]
	}
	return pattern
}

// ClientTransport records http.client.request.duration for what this service
// calls out to. Like the server middleware, it is the identity function when
// metrics are off.
func (m *Metrics) ClientTransport(base http.RoundTripper) http.RoundTripper {
	if !m.enabled {
		return base
	}

	return otelhttp.NewTransport(base,
		otelhttp.WithMeterProvider(m.provider),
		otelhttp.WithTracerProvider(tracenoop.NewTracerProvider()),
		otelhttp.WithPropagators(propagation.NewCompositeTextMapPropagator()),
	)
}
