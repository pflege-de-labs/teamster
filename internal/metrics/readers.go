package metrics

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/otlptranslator"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	promexporter "go.opentelemetry.io/otel/exporters/prometheus"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"

	"github.com/pflege-de-labs/teamster/internal/config"
)

// cardinalityLimit bounds the series one process can grow. Route and template
// attributes come from configuration an operator edits, and a counter is
// cumulative for the life of the process: delete a route, create it again, and
// the old series lives on. Past the limit the SDK collapses the rest into one
// overflow series rather than growing without bound.
const cardinalityLimit = 2000

// prometheusHandler is the exporter and the registry it writes into, kept
// together because the handler is useless without the registry that owns it.
type prometheusHandler struct {
	registry *prometheus.Registry
	handler  http.Handler
	reader   *promexporter.Exporter
}

func build(cfg config.MetricsConfig) (*sdkmetric.MeterProvider, *prometheusHandler, error) {
	res, err := resource.Merge(resource.Default(), resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName(cfg.ServiceName),
	))
	if err != nil {
		return nil, nil, fmt.Errorf("metrics resource: %w", err)
	}

	options := []sdkmetric.Option{
		sdkmetric.WithResource(res),
		sdkmetric.WithView(histogramView),
		sdkmetric.WithCardinalityLimit(cardinalityLimit),
	}

	var handler *prometheusHandler
	if cfg.Prometheus {
		handler, err = prometheusReader()
		if err != nil {
			return nil, nil, err
		}
		options = append(options, sdkmetric.WithReader(handler.reader))
	}

	if cfg.OTLPEndpoint != "" {
		reader, err := otlpReader(cfg)
		if err != nil {
			return nil, nil, err
		}
		options = append(options, sdkmetric.WithReader(reader))
	}

	return sdkmetric.NewMeterProvider(options...), handler, nil
}

// histogramView is the whole reason a Prometheus scrape sees native histograms:
// the exporter renders only an exponential histogram as one, and the SDK
// aggregates into explicit buckets unless told otherwise.
//
// It is one function rather than several views because the SDK applies every
// view that matches and unions the streams — a kind-wide view plus a name-based
// drop would produce both, not one cancelling the other.
func histogramView(instrument sdkmetric.Instrument) (sdkmetric.Stream, bool) {
	if instrument.Kind != sdkmetric.InstrumentKindHistogram {
		return sdkmetric.Stream{}, false
	}

	// A hand-written view carries the identity itself; the SDK only fills these
	// in for the default view it would otherwise have used.
	stream := sdkmetric.Stream{
		Name:        instrument.Name,
		Description: instrument.Description,
		Unit:        instrument.Unit,
	}

	// Request and response sizes are recorded on the same attributes as the
	// duration, and answer no question this service has.
	if strings.HasSuffix(instrument.Name, ".body.size") {
		stream.Aggregation = sdkmetric.AggregationDrop{}
		return stream, true
	}

	// Matching on kind rather than on name means a histogram added later is
	// native without anyone remembering to come back here.
	stream.Aggregation = sdkmetric.AggregationBase2ExponentialHistogram{
		MaxSize:  160,
		MaxScale: 20,
		// Cumulative minima and maxima span the life of the process, so they
		// describe no window anybody is looking at.
		NoMinMax: true,
	}
	return stream, true
}

func prometheusReader() (*prometheusHandler, error) {
	// A registry of our own, not prometheus.DefaultRegisterer: that one is
	// process-global, arrives pre-populated with collectors this service did
	// not choose, and makes two instances in one process collide.
	registry := prometheus.NewRegistry()

	exporter, err := promexporter.New(
		promexporter.WithRegisterer(registry),
		// Pinned rather than left to the default, which the library documents
		// as changing: the alternative emits http.server.request.duration as a
		// dotted name and breaks every dashboard built on the underscored one.
		promexporter.WithTranslationStrategy(otlptranslator.UnderscoreEscapingWithSuffixes),
		promexporter.WithoutScopeInfo(),
	)
	if err != nil {
		return nil, fmt.Errorf("prometheus exporter: %w", err)
	}

	handler := promhttp.HandlerFor(registry, promhttp.HandlerOpts{
		// One data point the exporter cannot render must cost that data point
		// and not the whole scrape.
		ErrorHandling:     promhttp.ContinueOnError,
		EnableOpenMetrics: true,
	})

	return &prometheusHandler{registry: registry, handler: handler, reader: exporter}, nil
}

func otlpReader(cfg config.MetricsConfig) (sdkmetric.Reader, error) {
	var (
		exporter sdkmetric.Exporter
		err      error
	)

	switch cfg.OTLPProtocol {
	case "grpc":
		options := []otlpmetricgrpc.Option{otlpmetricgrpc.WithEndpoint(cfg.OTLPEndpoint)}
		if cfg.OTLPInsecure {
			options = append(options, otlpmetricgrpc.WithInsecure())
		}
		exporter, err = otlpmetricgrpc.New(context.Background(), options...)
	case "http", "":
		options := []otlpmetrichttp.Option{otlpmetrichttp.WithEndpoint(cfg.OTLPEndpoint)}
		if cfg.OTLPInsecure {
			options = append(options, otlpmetrichttp.WithInsecure())
		}
		exporter, err = otlpmetrichttp.New(context.Background(), options...)
	default:
		return nil, fmt.Errorf("otlp protocol %q is neither grpc nor http", cfg.OTLPProtocol)
	}
	if err != nil {
		return nil, fmt.Errorf("otlp exporter: %w", err)
	}

	return sdkmetric.NewPeriodicReader(exporter, sdkmetric.WithInterval(cfg.OTLPInterval)), nil
}
