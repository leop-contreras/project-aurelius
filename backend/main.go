package main

import (
	"database/sql"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"main/controllers"
	"main/dockerx"
	"main/events"
	"main/kafkax"
	"main/models"
	"main/proto_gen"

	consul "github.com/hashicorp/consul/api"
	_ "github.com/lib/pq"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

func registerWithConsul(client *consul.Client, instanceID string, role string) error {
	registration := &consul.AgentServiceRegistration{
		ID:      instanceID,
		Name:    "backend",
		Tags:    []string{role},
		Address: instanceID,
		Port:    50051,
		Check: &consul.AgentServiceCheck{
			GRPC:                           fmt.Sprintf("%s:50051", instanceID),
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
	challengeModel := &models.ChallengeModel{DB: db}
	dockerClient := dockerx.NewClient(os.Getenv("DOCKER_SOCKET"))

	log.Printf("Starting worker %v", debugWorkerID)

	grpcServer := grpc.NewServer()
	healthpb.RegisterHealthServer(grpcServer, health.NewServer())

	role := os.Getenv("ROLE")
	switch role {
	case "get":
		proto_gen.RegisterInstanceServiceServer(grpcServer, &controllers.ReadController{InstanceModel: instanceModel, ChallengeModel: challengeModel})
	case "post":
		maxContainers, err := strconv.Atoi(os.Getenv("MAX_CONTAINERS"))
		if err != nil {
			log.Fatalf("Invalid max containers: %v", err)
		}
		writer := kafkax.NewWriter([]string{os.Getenv("KAFKA_BROKER")}, events.TopicInstanceCreated)
		defer writer.Close()
		proto_gen.RegisterInstanceServiceServer(grpcServer, &controllers.WriteController{
			InstanceModel:  instanceModel,
			ChallengeModel: challengeModel,
			Docker:         dockerClient,
			MaxContainers:  maxContainers,
			Events:         writer,
		})
	case "put":
		conn, err := grpc.NewClient(os.Getenv("GATEWAY_GRPC_ADDR"), grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			log.Fatalf("Failed to create grpc client: %v", err)
		}
		defer conn.Close()
		writer := kafkax.NewWriter([]string{os.Getenv("KAFKA_BROKER")}, events.TopicInstanceStatusUpdated)
		defer writer.Close()
		updateController := &controllers.UpdateController{
			InstanceModel: instanceModel,
			Docker:        dockerClient,
			ReadClient:    proto_gen.NewInstanceServiceClient(conn),
			Events:        writer,
		}
		proto_gen.RegisterInstanceServiceServer(grpcServer, updateController)
		go updateController.AutoClean()
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

	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Printf("Shutting down worker %v", debugWorkerID)
		deregisterFromConsul(consulClient, instanceID)
		grpcServer.GracefulStop()
	}()

	log.Println("Server is running on port 50051...")
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatal(err)
	}
}
