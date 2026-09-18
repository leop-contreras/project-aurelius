package main

import (
	"log"
	"net/http"
)

type Route struct {
	Prefix string `json:"prefix"`
	Target string `json:"target"`
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	log.Fatal(http.ListenAndServe(":8000", mux))
}
