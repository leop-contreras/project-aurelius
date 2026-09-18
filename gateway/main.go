package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
)

type Route struct {
	Prefix string `json:"prefix"`
	Target string `json:"target"`
}

func loadRoutes(path string) ([]Route, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var routes []Route
	err = json.Unmarshal(data, &routes)
	if err != nil {
		return nil, err
	}
	return routes, nil
}

func main() {
	paths, err := loadRoutes("routes.json")
	if err != nil {
		log.Fatal(err)
	}
	for _, route := range paths {
		log.Printf("route: %s | %s", route.Prefix, route.Target)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	log.Fatal(http.ListenAndServe(":8000", mux))
}
