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

	http.HandleFunc("GET /instance/{id}", instanceController.GetInstanceHandler)
	http.HandleFunc("POST /instance/create", instanceController.CreateInstanceHandler)
	http.HandleFunc("PUT /instance/{id}/status", instanceController.UpdateStatusHandler)

	log.Println("Server is running on port 8080...")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
