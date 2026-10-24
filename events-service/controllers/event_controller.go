package controllers

import (
	"context"
	"encoding/json"
	"log"
	"main/events"
	"main/models"

	"github.com/segmentio/kafka-go"
)

type AuditController struct{}

type HistoryController struct {
	HistoryModel *models.HistoryModel
}

func decode(msg kafka.Message) (*events.InstanceEvent, bool) {
	var event events.InstanceEvent
	if err := json.Unmarshal(msg.Value, &event); err != nil {
		log.Printf("Skipping invalid event on %s partition=%d offset=%d: %v", msg.Topic, msg.Partition, msg.Offset, err)
		return nil, false
	}
	return &event, true
}

func (c *AuditController) Handle(_ context.Context, msg kafka.Message) error {
	event, ok := decode(msg)
	if !ok {
		return nil
	}

	log.Printf("[audit] %s instance=%d name=%q status=%s by worker %s (partition=%d offset=%d)",
		event.Type, event.InstanceID, event.Name, event.Status, event.WorkerID, msg.Partition, msg.Offset)
	return nil
}

func (c *HistoryController) Handle(_ context.Context, msg kafka.Message) error {
	event, ok := decode(msg)
	if !ok {
		return nil
	}

	recorded, err := c.HistoryModel.Record(event)
	if err != nil {
		return err
	}
	if !recorded {
		log.Printf("[history] duplicate event %s ignored", event.EventID)
		return nil
	}
	log.Printf("[history] recorded %s for instance %d (partition=%d offset=%d)", event.Type, event.InstanceID, msg.Partition, msg.Offset)
	return nil
}
