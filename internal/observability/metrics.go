package observability

import (
	"fmt"
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/prometheus"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

func InitMeter() (*sdkmetric.MeterProvider, http.Handler, error) {
	exporter, err := prometheus.New()
	if err != nil {
		return nil, nil, fmt.Errorf("creating Prometheus exporter: %w", err)
	}

	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(exporter))
	otel.SetMeterProvider(mp)

	handler := promhttp.Handler()
	return mp, handler, nil
}
