package kafkax

import (
	"context"
	"log"
	"net"
	"strconv"
	"time"

	"github.com/segmentio/kafka-go"
)

func EnsureTopic(broker, topic string, partitions int) error {
	conn, err := kafka.Dial("tcp", broker)
	if err != nil {
		return err
	}
	defer conn.Close()

	controller, err := conn.Controller()
	if err != nil {
		return err
	}
	ctrl, err := kafka.Dial("tcp", net.JoinHostPort(controller.Host, strconv.Itoa(controller.Port)))
	if err != nil {
		return err
	}
	defer ctrl.Close()

	return ctrl.CreateTopics(kafka.TopicConfig{
		Topic:             topic,
		NumPartitions:     partitions,
		ReplicationFactor: 1,
	})
}

type Handler func(ctx context.Context, msg kafka.Message) error

func Consume(ctx context.Context, brokers []string, topics []string, groupID string, handle Handler) error {
	backoff := time.Second
	for {
		err := consumeOnce(ctx, brokers, topics, groupID, handle)
		if ctx.Err() != nil {
			return nil
		}

		log.Printf("Consumer stopped: %v, reconnecting in %s", err, backoff)
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff):
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

func consumeOnce(ctx context.Context, brokers []string, topics []string, groupID string, handle Handler) error {
	r := kafka.NewReader(kafka.ReaderConfig{
		Brokers:     brokers,
		GroupTopics: topics,
		GroupID:     groupID,
		StartOffset: kafka.FirstOffset,
		MinBytes:    1,
		MaxBytes:    10e6,
	})
	defer r.Close()

	for {
		msg, err := r.FetchMessage(ctx)
		if err != nil {
			return err
		}

		if err := processWithRetry(ctx, handle, msg, 3); err != nil {
			log.Printf("Giving up on %s partition=%d offset=%d: %v", msg.Topic, msg.Partition, msg.Offset, err)
		}

		if err := r.CommitMessages(ctx, msg); err != nil {
			return err
		}
	}
}

func processWithRetry(ctx context.Context, h Handler, msg kafka.Message, attempts int) error {
	var err error
	for i := 1; i <= attempts; i++ {
		if err = h(ctx, msg); err == nil {
			return nil
		}
		log.Printf("Attempt %d/%d failed: %v", i, attempts, err)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(i) * 500 * time.Millisecond):
		}
	}
	return err
}
