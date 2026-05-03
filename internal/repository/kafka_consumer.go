package repository

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/segmentio/kafka-go"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"github.com/tungnguyen/unified-document-viewer/internal/domain"
)

type AuditRecord struct {
	RequestID  string          `gorm:"column:request_id;primaryKey"`
	VIN        string          `gorm:"column:vin;not null"`
	Ts         time.Time       `gorm:"column:ts;autoCreateTime"`
	HTTPStatus int             `gorm:"column:http_status;not null"`
	DurationMs int             `gorm:"column:duration_ms;not null"`
	Outcomes   json.RawMessage `gorm:"column:outcomes;type:jsonb;not null"`
}

func (AuditRecord) TableName() string {
	return "audit_request"
}

type AuditConsumer struct {
	reader *kafka.Reader
	db     *gorm.DB
}

func NewAuditConsumer(brokers []string, topic, groupID string, db *gorm.DB) *AuditConsumer {
	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers: brokers,
		Topic:   topic,
		GroupID: groupID,
	})

	return &AuditConsumer{reader: reader, db: db}
}

func (c *AuditConsumer) Run(ctx context.Context) {
	slog.Info("audit consumer started")
	for {
		msg, err := c.reader.ReadMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				slog.Info("audit consumer shutting down")
				return
			}
			slog.Warn("audit consumer read error", "error", err)
			continue
		}

		var entry domain.AuditEntry
		if err := json.Unmarshal(msg.Value, &entry); err != nil {
			slog.Warn("audit consumer: invalid message", "error", err)
			continue
		}

		outcomes, err := json.Marshal(entry.Outcomes)
		if err != nil {
			slog.Warn("audit consumer: marshal outcomes failed", "error", err)
			continue
		}

		record := AuditRecord{
			RequestID:  entry.RequestID,
			VIN:        entry.VIN,
			HTTPStatus: entry.HTTPStatus,
			DurationMs: entry.DurationMs,
			Outcomes:   outcomes,
		}

		result := c.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&record)
		if result.Error != nil {
			slog.Warn("audit consumer: db write failed", "error", result.Error, "request_id", entry.RequestID)
		}
	}
}

func (c *AuditConsumer) Close() error {
	return c.reader.Close()
}
