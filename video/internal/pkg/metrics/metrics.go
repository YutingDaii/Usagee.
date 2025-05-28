package metrics

import (
	"github.com/go-kratos/kratos/v2/middleware/metrics"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

// 全局指标变量
var (
	MetricRequests metric.Int64Counter
	MetricSeconds  metric.Float64Histogram
)

// Init 初始化指标
func Init() {
	// 初始化Prometheus导出器(将指标数据导出到Prometheus)
	exporter, err := prometheus.New()
	if err != nil {
		panic(err)
	}

	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(exporter))
	// 创建指标
	meter := provider.Meter("video-metrics")

	// 创建请求计数器指标
	MetricRequests, err = metrics.DefaultRequestsCounter(meter, metrics.DefaultServerRequestsCounterName)
	if err != nil {
		panic(err)
	}

	// 创建响应时间直方图指标
	MetricSeconds, err = metrics.DefaultSecondsHistogram(meter, metrics.DefaultServerSecondsHistogramName)
	if err != nil {
		panic(err)
	}
}
