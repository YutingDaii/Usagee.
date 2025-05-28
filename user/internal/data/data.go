package data

import (
	"user/internal/conf"
	models "user/internal/data/model"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/go-redis/redis/v8"
	_ "github.com/go-sql-driver/mysql"
	"github.com/google/wire"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// ProviderSet is data providers.
var ProviderSet = wire.NewSet(NewData, NewUserRepo,NewKafkaProducer)



// Data .
type Data struct {
	 RDb *redis.Client
	 Db *gorm.DB
	 //kafka
	Kafka *KafkaProducer
}

// NewData .
func NewData(c *conf.Data, logger log.Logger) (*Data, func(), error) {
	log:=log.NewHelper(logger)
	//MySQL
	db ,err := gorm.Open(mysql.Open(c.Database.Source), &gorm.Config{})
	if err != nil {
		return nil, nil, err
	}
	db.AutoMigrate(&models.User{},&models.Follow{})
	//Redis
	rdb := redis.NewClient(&redis.Options{
		Addr:     c.Redis.Addr,
		Network: c.Redis.Network,
		DialTimeout: c.Redis.DialTimeout.AsDuration(),
		ReadTimeout: c.Redis.ReadTimeout.AsDuration(),
		WriteTimeout: c.Redis.WriteTimeout.AsDuration(),
	})

	d:=&Data{
		Db: db,
		RDb: rdb,
	}

	cleanup := func() {
		log.Info("closing the data resources")
		sqlDB,_:=db.DB()
		if err := sqlDB.Close(); err != nil {
			log.Errorf("failed to close database: %v", err)
		}
		if err := d.RDb.Close(); err != nil {
			log.Errorf("failed to close redis: %v", err)
		}	
	}
	return d, cleanup, nil
}
