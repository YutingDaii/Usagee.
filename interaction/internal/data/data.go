package data

import (
	"database/sql"
	"time"
	"video/internal/conf"

	"video/internal/biz"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/go-redis/redis/v8"
	_ "github.com/go-sql-driver/mysql" // MySQL驱动
	"github.com/google/wire"
)

// ProviderSet is data providers.
var ProviderSet = wire.NewSet(
	NewData,
	wire.Bind(new(biz.VideoRepo), new(*videoRepo)),
	NewVideoRepo,
)

// Data .
type Data struct {
	db            *sql.DB
	rdb           *redis.ClusterClient
}

// NewData .
func NewData(c *conf.Data, logger log.Logger) (*Data, func(), error) {
	log := log.NewHelper(logger)

	// Connect to VTGate
	db, err := sql.Open("mysql", c.Database.VtgateSource)
	if err != nil {
		log.Errorf("failed to connect to vtgate: %v", err)
		return nil, nil, err
	}

	// 设置连接池参数
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(time.Hour)

	// 验证连接
	if err := db.Ping(); err != nil {
		log.Errorf("failed to ping vtgate: %v", err)
		return nil, nil, err
	}

	// Connect to Rediscluster
	rdb := redis.NewClusterClient(&redis.ClusterOptions{
		Addrs:    c.Redis.Addresses, // 集群节点地址列表
		Password: c.Redis.Password,

		// 集群特定配置
		RouteByLatency: true,  // 根据延迟自动路由
		RouteRandomly:  false, // 不随机路由
		MaxRetries:     3,     // 最大重试次数

		// 连接池配置
		PoolSize:     50, // 每个节点的连接池大小
		MinIdleConns: 10, // 每个节点的最小空闲连接数
	})

	// 验证连接
	if err := rdb.Ping(rdb.Context()).Err(); err != nil {
		log.Errorf("failed to connect to redis cluster: %v", err)
		return nil, nil, err
	}


	d := &Data{
		db:            db,
		rdb:           rdb,
	}

	cleanup := func() {
		log.Info("closing the data resources")

		// Close MySQL connection
		if err := db.Close(); err != nil {
			log.Error(err)
		}

		// Close Redis connection
		if err := rdb.Close(); err != nil {
			log.Error(err)
		}
	}

	return d, cleanup, nil
}