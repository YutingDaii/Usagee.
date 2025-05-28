package server

import (
	"context"
	"os"

	u "user/api/user"
	"user/internal/conf"
	"user/internal/conf/metrix"
	"user/internal/service"

	"github.com/go-kratos/kratos/v2/log"
	jwtmw "github.com/go-kratos/kratos/v2/middleware/auth/jwt"
	"github.com/go-kratos/kratos/v2/middleware/logging"
	"github.com/go-kratos/kratos/v2/middleware/metrics"
	"github.com/go-kratos/kratos/v2/middleware/ratelimit"
	"github.com/go-kratos/kratos/v2/middleware/recovery"
	"github.com/go-kratos/kratos/v2/middleware/selector"
	"github.com/go-kratos/kratos/v2/middleware/tracing"
	"github.com/go-kratos/kratos/v2/transport/http"
	jwt "github.com/golang-jwt/jwt/v5"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// 令牌桶限流中间件，限制每秒钟 100 个请求

// 白名单
func NewWhiteListMatcher() selector.MatchFunc {
	skipAuthPaths := make(map[string]struct{})
	skipAuthPaths["/api.user.User/Login"] = struct{}{}
	skipAuthPaths["/api.user.User/SendVerificationCode"] = struct{}{}
	skipAuthPaths["/api.user.User/VerifyCode"] = struct{}{}
	return func(ctx context.Context, operation string) bool {
		if _, ok := skipAuthPaths[operation]; ok {
			return false
		}
		return true
	}
}

// NewHTTPServer new an HTTP server.
func NewHTTPServer(c *conf.Server, user *service.UserService, logger log.Logger) *http.Server {
	var opts = []http.ServerOption{
		http.Middleware(
			recovery.Recovery(),
			tracing.Server(),
			logging.Server(logger),
			ratelimit.Server(),
			metrics.Server(
				metrics.WithSeconds(metrix.MetricSeconds),
				metrics.WithRequests(metrix.MetricRequests),
			),
			//jwt middleware
			selector.Server(
				jwtmw.Server(func(token *jwt.Token) (interface{}, error) {
					return []byte(os.Getenv("JWT_SECRET")), nil
				},
					jwtmw.WithSigningMethod(jwt.SigningMethodHS256),
				),
			).Match(NewWhiteListMatcher()).Build(),
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
	//register user http service
	u.RegisterUserHTTPServer(srv, user)
	srv.Handle("/metrics",promhttp.Handler())
	return srv
}
