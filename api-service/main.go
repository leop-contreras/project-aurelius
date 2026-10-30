package main

import (
	"log"
	"net/http"
	"os"

	"main/controllers"
	"main/proto_gen"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	conn, err := grpc.NewClient(os.Getenv("GATEWAY_GRPC_ADDR"), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("Failed to create grpc client: %v", err)
	}
	defer conn.Close()

	instanceController := &controllers.InstanceController{Client: proto_gen.NewInstanceServiceClient(conn)}

	http.HandleFunc("GET /instance", instanceController.GetInstanceHandler)
	http.HandleFunc("GET /instances", instanceController.ListInstancesHandler)
	http.HandleFunc("POST /instance/create", instanceController.CreateInstanceHandler)
	http.HandleFunc("PUT /instance/update-status", instanceController.UpdateStatusHandler)
	http.HandleFunc("PUT /instance/renew", instanceController.RenewInstanceHandler)
	http.HandleFunc("GET /challenges", instanceController.ListChallengesHandler)

	log.Println("Server is running on port 8080...")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
