package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"

	"main/controllers"
	"main/models"

	_ "github.com/lib/pq"
)

func main() {
	dbHost := os.Getenv("DB_HOST")
	dbUser := os.Getenv("DB_USER")
	dbPass := os.Getenv("DB_PASSWORD")
	dbName := os.Getenv("DB_NAME")
	dbPort := os.Getenv("DB_PORT")

	debugWorkerID := os.Getenv("DEBUG_WORKER_ID")

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

	switch role := os.Getenv("ROLE"); role {
	case "get":
		http.HandleFunc("GET /instance", instanceController.GetInstanceHandler)
	case "post":
		http.HandleFunc("POST /instance/create", instanceController.CreateInstanceHandler)
	case "put":
		http.HandleFunc("PUT /instance/update-status", instanceController.UpdateStatusHandler)
	}

	log.Println("Server is running on port 8080...")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
