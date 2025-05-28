// 文件：internal/data/kafka.go
package data

import (
	"context"
	"strconv"
	"user/internal/biz"
	"user/internal/conf"

	kafka "github.com/segmentio/kafka-go"
)

type KafkaProducer struct {
	followEventWriter   *kafka.Writer
    unfollowEventWriter *kafka.Writer
}

func NewKafkaProducer(c *conf.Data) *KafkaProducer {
	brokers := make([]string, len(c.Kafka.Brokers))
    for i, broker := range c.Kafka.Brokers {
        // 假设 KafkaBroker 有一个 Address 字段
        brokers[i] = broker.Address // 或者 broker.Host 或其他包含地址的字段
    }
    followWriter := kafka.Writer{
        Addr:     kafka.TCP(brokers...),
        Topic:    c.Kafka.Topics.FollowEvent,
        Balancer: &kafka.LeastBytes{},
        WriteTimeout: c.Kafka.WriteTimeout.AsDuration(),
        ReadTimeout: c.Kafka.ReadTimeout.AsDuration(),
    }

	// Unfollow event writer
	unfollowWriter := kafka.Writer{
		Addr:     kafka.TCP(brokers...),
		Topic:    c.Kafka.Topics.UnfollowEvent,
		Balancer: &kafka.LeastBytes{},
		WriteTimeout: c.Kafka.WriteTimeout.AsDuration(),
		ReadTimeout: c.Kafka.ReadTimeout.AsDuration(),
	}
	return &KafkaProducer{
		followEventWriter:   &followWriter,
		unfollowEventWriter: &unfollowWriter,
	}
} 

func (r *userRepo) SendFollowEvent(ctx context.Context, follow *biz.Follow) ( error) {
	msg := kafka.Message{
		Key:   []byte(strconv.FormatInt(follow.Follower_id, 10)),
		Value: []byte(strconv.FormatInt(follow.Followed_id, 10)),
	}
	err := r.data.Kafka.followEventWriter.WriteMessages(ctx, msg)
	if err != nil {
		return err
	}
	return nil
}
