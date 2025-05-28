package data

import (
	"context"
	"encoding/json"
	"time"
	"video/internal/biz"

	"github.com/IBM/sarama"
	"github.com/go-kratos/kratos/v2/log"
)

// KafkaProducer handles publishing events to Kafka
type KafkaProducer struct {
	producer sarama.SyncProducer
	log      *log.Helper
}

// Constants for Kafka topics
const (
	VideoCreatedTopic   = "video.created"
	VideoPublishedTopic = "video.published"
	VideoDeletedTopic   = "video.deleted"
)

// Event types for VideoEvent
const (
	VideoEventTypeCreated   = "created"
	VideoEventTypePublished = "published"
	VideoEventTypeDeleted   = "deleted"
)

// VideoEvent represents an event related to a video
type VideoEvent struct {
	EventType  string    `json:"event_type"`
	VideoID    int64     `json:"video_id"`
	UserID     int64     `json:"user_id"`
	Title      string    `json:"title,omitempty"`
	Status     string    `json:"status"`
	Visibility string    `json:"visibility,omitempty"`
	URL        string    `json:"url,omitempty"`
	Timestamp  time.Time `json:"timestamp"`
}

// NewKafkaProducer creates a new KafkaProducer
func NewKafkaProducer(data *Data, logger log.Logger) *KafkaProducer {
	return &KafkaProducer{
		producer: data.kafkaProducer,
		log:      log.NewHelper(logger),
	}
}

// PublishVideoCreated publishes a video.created event
func (k *KafkaProducer) PublishVideoCreated(ctx context.Context, video *biz.Video) error {
	event := VideoEvent{
		EventType:  VideoEventTypeCreated,
		VideoID:    video.ID,
		UserID:     video.UserID,
		Title:      video.Title,
		Status:     string(video.Status),
		Visibility: string(video.Visibility),
		Timestamp:  time.Now(),
	}

	return k.publishEvent(VideoCreatedTopic, event)
}

// PublishVideoPublished publishes a video.published event
func (k *KafkaProducer) PublishVideoPublished(ctx context.Context, video *biz.Video) error {
	// 确保至少有一个变体
	var url string
	if len(video.Variants) > 0 {
		url = video.Variants[0].StorageURL
	}

	event := VideoEvent{
		EventType:  VideoEventTypePublished,
		VideoID:    video.ID,
		UserID:     video.UserID,
		Title:      video.Title,
		Status:     string(video.Status),
		Visibility: string(video.Visibility),
		URL:        url,
		Timestamp:  time.Now(),
	}

	return k.publishEvent(VideoPublishedTopic, event)
}

// PublishVideoDeleted publishes a video.deleted event
func (k *KafkaProducer) PublishVideoDeleted(ctx context.Context, video *biz.Video) error {
	event := VideoEvent{
		EventType: VideoEventTypeDeleted,
		VideoID:   video.ID,
		UserID:    video.UserID,
		Status:    string(video.Status),
		Timestamp: time.Now(),
	}

	return k.publishEvent(VideoDeletedTopic, event)
}

// publishEvent is a helper function to publish events to Kafka
func (k *KafkaProducer) publishEvent(topic string, event interface{}) error {
	eventJSON, err := json.Marshal(event)
	if err != nil {
		k.log.Errorf("Failed to marshal event: %v", err)
		return err
	}

	msg := &sarama.ProducerMessage{
		Topic: topic,
		Value: sarama.ByteEncoder(eventJSON),
	}

	partition, offset, err := k.producer.SendMessage(msg)
	if err != nil {
		k.log.Errorf("Failed to publish event to topic %s: %v", topic, err)
		return err
	}

	k.log.Infof("Event published to topic %s, partition %d, offset %d", topic, partition, offset)
	return nil
}
