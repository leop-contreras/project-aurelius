package main

import (
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
)

type Backend struct {
	URL     *url.URL
	Proxy   *httputil.ReverseProxy
	healthy atomic.Bool
}

type LoadBalancer struct {
	backends []*Backend
	counter  uint64
	sem      chan struct{}
}

func main() {
	targetsraw := os.Getenv("TARGETS")

	var targets []string
	for _, t := range strings.Split(targetsraw, ",") {
		t = strings.TrimSpace(t)
		if t != "" {
			targets = append(targets, t)
		}
	}
	if len(targets) == 0 {
		log.Fatal("no usable targets")
	}

	listen := os.Getenv("LISTEN")
	if listen == "" {
		listen = ":9000"
	}

	for i, t := range targets {
		log.Printf("target %d: %s", i, t)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	log.Printf("load balancer listening on %s", listen)
	log.Fatal(http.ListenAndServe(listen, mux))
}
