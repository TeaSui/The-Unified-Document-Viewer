package repository

import (
	"context"
	"encoding/json"
	"time"

	"github.com/segmentio/kafka-go"
	"github.com/tungnguyen/unified-document-viewer/internal/domain"
)

type KafkaAuditRepository struct {
	writer *kafka.Writer
}

func NewKafkaAuditRepository(brokers []string, topic string) *KafkaAuditRepository {
	writer := &kafka.Writer{
		Addr:                   kafka.TCP(brokers...),
		Topic:                  topic,
		Balancer:               &kafka.LeastBytes{},
		BatchTimeout:           10 * time.Millisecond,
		AllowAutoTopicCreation: true,
	}

	return &KafkaAuditRepository{writer: writer}
}

func (r *KafkaAuditRepository) Insert(ctx context.Context, entry domain.AuditEntry) error {
	value, err := json.Marshal(entry)
	if err != nil {
		return err
	}

	msg := kafka.Message{
		Key:   []byte(entry.RequestID),
		Value: value,
	}

	return r.writer.WriteMessages(ctx, msg)
}

func (r *KafkaAuditRepository) Close() error {
	return r.writer.Close()
}
