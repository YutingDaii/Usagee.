package registry

import (
	"time"
	"video/internal/conf"

	"github.com/go-kratos/kratos/contrib/registry/etcd/v2"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/go-kratos/kratos/v2/registry"
	"github.com/google/wire"
	etcdv3 "go.etcd.io/etcd/client/v3"
)

var ProviderSet = wire.NewSet(NewRegistry, NewDiscovery)

// NewRegistry 创建etcd注册中心
func NewRegistry(c *conf.Registry, logger log.Logger) registry.Registrar {
	logs := log.NewHelper(logger)

	// 设置TTL
	ttl := int64(10)
	if c.Etcd.Ttl > 0 {
		ttl = c.Etcd.Ttl
	}

	// 设置超时时间
	timeout := 5 * time.Second
	if c.Etcd.Timeout != nil {
		timeout = c.Etcd.Timeout.AsDuration()
	}

	// 创建etcd客户端
	client, err := etcdv3.New(etcdv3.Config{
		Endpoints:   c.Etcd.Endpoints,
		DialTimeout: timeout,
	})
	if err != nil {
		logs.Fatalf("failed to create etcd client: %v", err)
		return nil
	}

	// 创建etcd注册中心
	r := etcd.New(client, etcd.Namespace(c.Etcd.Namespace))

	logs.Infof("etcd registry created with endpoints: %v, namespace: %s, ttl: %d",
		c.Etcd.Endpoints, c.Etcd.Namespace, ttl)

	return r
}

// NewDiscovery 创建etcd发现客户端
func NewDiscovery(c *conf.Registry, logger log.Logger) registry.Discovery {
	logs := log.NewHelper(logger)

	// 设置超时时间
	timeout := 5 * time.Second
	if c.Etcd.Timeout != nil {
		timeout = c.Etcd.Timeout.AsDuration()
	}

	// 创建etcd客户端
	client, err := etcdv3.New(etcdv3.Config{
		Endpoints:   c.Etcd.Endpoints,
		DialTimeout: timeout,
	})
	if err != nil {
		logs.Fatalf("failed to create etcd discovery client: %v", err)
		return nil
	}

	// 创建etcd发现客户端
	r := etcd.New(client, etcd.Namespace(c.Etcd.Namespace))

	logs.Infof("etcd discovery created with endpoints: %v, namespace: %s",
		c.Etcd.Endpoints, c.Etcd.Namespace)

	return r
}
