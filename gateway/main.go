package main

import (
	"encoding/json"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

type Route struct {
	Prefix string `json:"prefix"`
	Target string `json:"target"`
}

type Gateway struct {
	routes   []Route
	proxies  map[string]*httputil.ReverseProxy
	health   map[string]bool
	healthMu sync.RWMutex
	sem      chan struct{}
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

func NewGateway(routes []Route, maxConcurrent int) *Gateway {
	proxies := make(map[string]*httputil.ReverseProxy)
	healths := make(map[string]bool)
	for _, route := range routes {
		path, err := url.Parse(route.Target)
		if err != nil {
			log.Fatalf("Failed to parse target URL: %s", route.Target)
		}
		proxies[route.Prefix] = httputil.NewSingleHostReverseProxy(path)
		healths[route.Prefix] = false
	}
	return &Gateway{
		routes:   routes,
		proxies:  proxies,
		health:   healths,
		healthMu: sync.RWMutex{},
		sem:      make(chan struct{}, maxConcurrent),
	}
}

func heartbeatMonitor(interval time.Duration, gw *Gateway) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for range ticker.C {
			wg := sync.WaitGroup{}
			for _, route := range gw.routes {
				wg.Add(1)
				go func(r Route) {
					defer wg.Done()
					ok := checkHealth(r.Target)
					gw.healthMu.Lock()
					gw.health[r.Prefix] = ok
					gw.healthMu.Unlock()
				}(route)
			}
			wg.Wait()
		}
	}()
}

func checkHealth(target string) bool {
	client := http.Client{
		Timeout: 2 * time.Second,
	}
	resp, err := client.Get(target + "/health")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	g.sem <- struct{}{}
	defer func() { <-g.sem }()
	for _, route := range g.routes {
		if strings.HasPrefix(r.URL.Path, route.Prefix) {
			log.Printf("Proxying request for %s to %s", r.URL.Path, route.Target)
			g.proxies[route.Prefix].ServeHTTP(w, r)
			return
		}
	}
	http.Error(w, "Not Found", http.StatusNotFound)
}

func (g *Gateway) statusHandler(w http.ResponseWriter, r *http.Request) {
	g.healthMu.RLock()
	defer g.healthMu.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(g.health)
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Expose-Headers", "X-Worker-ID")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func main() {
	paths, err := loadRoutes("routes.json")
	if err != nil {
		log.Fatal(err)
	}
	for _, route := range paths {
		log.Printf("route: %s | %s", route.Prefix, route.Target)
	}

	gw := NewGateway(paths, 50)
	heartbeatMonitor(10*time.Second, gw)

	mux := http.NewServeMux()
	mux.HandleFunc("/status", gw.statusHandler)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	mux.Handle("/", gw)
	log.Fatal(http.ListenAndServe(":8000", withCORS(mux)))
}
