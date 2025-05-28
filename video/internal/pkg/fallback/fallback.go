package fallback

import (
	"context"

	"github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/go-kratos/kratos/v2/middleware"
	"github.com/go-kratos/kratos/v2/middleware/selector"
)

// FallbackFunc 定义降级处理函数类型
type FallbackFunc func(ctx context.Context, req interface{}) (interface{}, error)

// FallbackMatcher 定义降级条件匹配函数类型
type FallbackMatcher func(ctx context.Context, err error) bool

// Option 是降级器配置选项
type Option func(*options)

// options 保存降级器配置
type options struct {
	matcher  FallbackMatcher // 错误匹配器
	fallback FallbackFunc    // 降级处理函数
	logger   log.Logger      // 日志记录器
}

// WithMatcher 设置错误匹配器 (默认匹配器：如果错误为nil，则不降级，否则降级)
func WithMatcher(m FallbackMatcher) Option {
	return func(o *options) {
		o.matcher = m
	}
}

// WithLogger 设置日志记录器
func WithLogger(logger log.Logger) Option {
	return func(o *options) {
		o.logger = logger
	}
}

// DefaultMatcher 默认的错误匹配器(默认匹配器：如果错误为nil，则不降级，否则降级)
func DefaultMatcher(ctx context.Context, err error) bool {
	if err == nil {
		return false
	}

	// 检查错误码，对特定错误进行降级
	if e, ok := err.(*errors.Error); ok {
		// 对超时、限流和熔断错误进行降级处理
		switch e.Code {
		case 408, 429, 503, 504:
			return true
		}
	}

	return false
}

// Fallback 创建一个服务降级中间件
func Fallback(fallbackFunc FallbackFunc, opts ...Option) middleware.Middleware {
	o := &options{
		matcher:  DefaultMatcher,
		fallback: fallbackFunc,
		logger:   log.DefaultLogger,
	}

	for _, opt := range opts {
		opt(o)
	}

	return func(handler middleware.Handler) middleware.Handler {
		return func(ctx context.Context, req interface{}) (interface{}, error) {
			// 执行原始处理
			reply, err := handler(ctx, req)

			// 判断是否需要降级
			if err != nil && o.matcher(ctx, err) {
				// 记录降级日志
				log.WithContext(ctx, o.logger).Log(log.LevelInfo, "service degraded", "error", err)

				// 执行降级逻辑
				return o.fallback(ctx, req)
			}

			return reply, err
		}
	}
}

// PathFallback 为特定路径配置降级处理
func PathFallback(path string, fallbackFunc FallbackFunc, opts ...Option) middleware.Middleware {
	return selector.Server(
		Fallback(fallbackFunc, opts...),
	).Match(func(ctx context.Context, operation string) bool {
		return operation == path
	}).Build()
}
