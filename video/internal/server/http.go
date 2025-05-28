package server

import (
	"context"
	"errors"
	v "video/api/video"
	"video/internal/conf"
	"video/internal/pkg/fallback"
	metric "video/internal/pkg/metrics"
	"video/internal/service"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/go-kratos/kratos/v2/middleware/circuitbreaker"
	"github.com/go-kratos/kratos/v2/middleware/metrics"
	"github.com/go-kratos/kratos/v2/middleware/ratelimit"
	"github.com/go-kratos/kratos/v2/middleware/recovery"
	"github.com/go-kratos/kratos/v2/middleware/tracing"
	"github.com/go-kratos/kratos/v2/transport/http"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// NewHTTPServer new an HTTP server.
func NewHTTPServer(c *conf.Server, video *service.VideoService, logger log.Logger) *http.Server {

	var opts = []http.ServerOption{
		http.Middleware(
			recovery.Recovery(),
			tracing.Server(),
			metrics.Server(
				metrics.WithRequests(metric.MetricRequests),
				metrics.WithSeconds(metric.MetricSeconds),
			),
			ratelimit.Server(), //限流
			circuitbreaker.Client(),
			// 添加全局降级处理 - 默认降级返回"服务繁忙，请稍后再试"
			fallback.Fallback(func(ctx context.Context, req interface{}) (interface{}, error) {
				return nil, errors.New("服务繁忙，请稍后再试")
			}),
		),
	}

	if c.Http.Network != "" {
		opts = append(opts, http.Network(c.Http.Network))
	}
	if c.Http.Addr != "" {
		opts = append(opts, http.Address(c.Http.Addr))
	}
	if c.Http.Timeout != nil {
		opts = append(opts, http.Timeout(c.Http.Timeout.AsDuration()))
	}
	srv := http.NewServer(opts...)
	v.RegisterVideoServiceHTTPServer(srv, video)

	// 暴露Prometheus指标端点
	srv.Handle("/metrics", promhttp.Handler())

	return srv
}
