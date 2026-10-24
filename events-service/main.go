package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"main/controllers"
	"main/events"
	"main/kafkax"
	"main/models"

	_ "github.com/lib/pq"
	"github.com/segmentio/kafka-go"
)

func connectDB() *sql.DB {
	connStr := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		os.Getenv("DB_HOST"), os.Getenv("DB_PORT"), os.Getenv("DB_USER"), os.Getenv("DB_PASSWORD"), os.Getenv("DB_NAME"))

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	if err := db.Ping(); err != nil {
		log.Fatalf("Failed to ping database: %v", err)
	}
	return db
}

func main() {
	broker := os.Getenv("KAFKA_BROKER")
	topics := []string{events.TopicInstanceCreated, events.TopicInstanceStatusUpdated}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	for _, topic := range topics {
		if err := kafkax.EnsureTopic(broker, topic, 3); err != nil && !errors.Is(err, kafka.TopicAlreadyExists) {
			log.Fatalf("Failed to create topic %s: %v", topic, err)
		}
	}

	var group string
	var handle kafkax.Handler

	role := os.Getenv("ROLE")
	switch role {
	case "audit":
		group = events.GroupAudit
		handle = (&controllers.AuditController{}).Handle
	case "history":
		db := connectDB()
		defer db.Close()
		group = events.GroupHistory
		handle = (&controllers.HistoryController{HistoryModel: &models.HistoryModel{DB: db}}).Handle
	default:
		log.Fatalf("Unknown role: %s", role)
	}

	log.Printf("Starting %s consumer in group %s", role, group)
	if err := kafkax.Consume(ctx, []string{broker}, topics, group, handle); err != nil {
		log.Fatal(err)
	}
	log.Printf("Stopped %s consumer", role)
}
