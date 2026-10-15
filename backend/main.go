package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"main/controllers"
	"main/models"

	consul "github.com/hashicorp/consul/api"
	_ "github.com/lib/pq"
)

func registerWithConsul(client *consul.Client, instanceID string, role string) error {
	registration := &consul.AgentServiceRegistration{
		ID:      instanceID,
		Name:    "backend",
		Tags:    []string{role},
		Address: instanceID,
		Port:    8080,
		Check: &consul.AgentServiceCheck{
			HTTP:                           fmt.Sprintf("http://%s:8080/health", instanceID),
			Interval:                       "5s",
			Timeout:                        "2s",
			DeregisterCriticalServiceAfter: "1m",
		},
	}

	var err error
	for attempt := 1; attempt <= 10; attempt++ {
		err = client.Agent().ServiceRegister(registration)
		if err == nil {
			log.Printf("Registered %s in consul", instanceID)
			return nil
		}
		time.Sleep(time.Duration(attempt) * time.Second)
	}
	return fmt.Errorf("giving up registering with consul: %w", err)
}

func deregisterFromConsul(client *consul.Client, instanceID string) {
	if err := client.Agent().ServiceDeregister(instanceID); err != nil {
		log.Printf("Failed to deregister %s from consul: %v", instanceID, err)
		return
	}
	log.Printf("Deregistered %s from consul", instanceID)
}

func main() {
	dbHost := os.Getenv("DB_HOST")
	dbUser := os.Getenv("DB_USER")
	dbPass := os.Getenv("DB_PASSWORD")
	dbName := os.Getenv("DB_NAME")
	dbPort := os.Getenv("DB_PORT")

	debugWorkerID := os.Getenv("DEBUG_WORKER_ID")

	instanceID := os.Getenv("INSTANCE_ID")
	if instanceID == "" {
		log.Fatal("No instance ID configured")
	}

	connStr := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		dbHost, dbPort, dbUser, dbPass, dbName)

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	defer db.Close()

	if err := db.Ping(); err != nil {
		log.Fatalf("Failed to ping database: %v", err)
	}

	instanceModel := &models.InstanceModel{DB: db}
	instanceController := &controllers.InstanceController{InstanceModel: instanceModel}

	log.Printf("Starting worker %v", debugWorkerID)

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		role := os.Getenv("ROLE")
		if role == "" {
			http.Error(w, "no role configured", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("X-Worker-ID", debugWorkerID)
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "OK %s", role)
	})

	role := os.Getenv("ROLE")
	switch role {
	case "get":
		http.HandleFunc("GET /instance", instanceController.GetInstanceHandler)
	case "post":
		http.HandleFunc("POST /instance/create", instanceController.CreateInstanceHandler)
	case "put":
		http.HandleFunc("PUT /instance/update-status", instanceController.UpdateStatusHandler)
	default:
		log.Fatalf("Unknown role: %s", role)
	}

	consulClient, err := consul.NewClient(consul.DefaultConfig())
	if err != nil {
		log.Fatalf("Failed to create consul client: %v", err)
	}

	if err := registerWithConsul(consulClient, instanceID, role); err != nil {
		log.Printf("WARNING: %v", err)
	}

	srv := &http.Server{Addr: ":8080"}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Printf("Shutting down worker %v", debugWorkerID)
		deregisterFromConsul(consulClient, instanceID)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(ctx)
	}()

	log.Println("Server is running on port 8080...")
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
