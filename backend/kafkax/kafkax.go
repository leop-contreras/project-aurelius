package kafkax

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/segmentio/kafka-go"
)

func NewWriter(brokers []string, topic string) *kafka.Writer {
	return &kafka.Writer{
		Addr:         kafka.TCP(brokers...),
		Topic:        topic,
		Balancer:     &kafka.Hash{},
		RequiredAcks: kafka.RequireAll,
		BatchTimeout: 10 * time.Millisecond,
		Async:        true,
		Completion: func(messages []kafka.Message, err error) {
			if err != nil {
				log.Printf("Failed to publish %d event(s) to %s: %v", len(messages), topic, err)
			}
		},
	}
}

func Publish(w *kafka.Writer, key string, event any) {
	payload, err := json.Marshal(event)
	if err != nil {
		log.Printf("Failed to encode event: %v", err)
		return
	}
	if err := w.WriteMessages(context.Background(), kafka.Message{Key: []byte(key), Value: payload}); err != nil {
		log.Printf("Failed to publish event to %s: %v", w.Topic, err)
	}
}
