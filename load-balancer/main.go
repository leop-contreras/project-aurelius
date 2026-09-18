package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
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

func NewLoadBalancer(targets []string, maxConcurrent int) *LoadBalancer {
	var backends []*Backend

	for _, t := range targets {
		target, err := url.Parse(t)
		if err != nil || target.Scheme == "" || target.Host == "" {
			log.Fatalf("invalid target %q: %v", t, err)
		}

		b := &Backend{
			URL:   target,
			Proxy: httputil.NewSingleHostReverseProxy(target),
		}
		b.healthy.Store(false)

		b.Proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
			b.healthy.Store(false)
			log.Printf("backend %s failed: %v", b.URL, err)
			http.Error(w, "backend unavailable", http.StatusBadGateway)
		}

		backends = append(backends, b)
	}

	return &LoadBalancer{
		backends: backends,
		sem:      make(chan struct{}, maxConcurrent),
	}
}

func (lb *LoadBalancer) next() *Backend {
	n := len(lb.backends)
	for i := 0; i < n; i++ {
		index := atomic.AddUint64(&lb.counter, 1) % uint64(n)
		if b := lb.backends[index]; b.healthy.Load() {
			return b
		}
	}
	return nil
}

func (lb *LoadBalancer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	lb.sem <- struct{}{}
	defer func() { <-lb.sem }()

	b := lb.next()
	if b == nil {
		http.Error(w, "no healthy backend available", http.StatusServiceUnavailable)
		return
	}

	log.Printf("%s %s -> %s", r.Method, r.URL.Path, b.URL)
	b.Proxy.ServeHTTP(w, r)
}

func checkHealth(target string) bool {
	client := http.Client{Timeout: 2 * time.Second}

	resp, err := client.Get(target + "/health")
	if err != nil {
		log.Printf("health check failed for %s: %v", target, err)
		return false
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	return resp.StatusCode == http.StatusOK
}

func (lb *LoadBalancer) doctorCheckup() {
	var wg sync.WaitGroup
	for _, b := range lb.backends {
		wg.Add(1)
		go func(b *Backend) {
			defer wg.Done()
			b.healthy.Store(checkHealth(b.URL.String()))
		}(b)
	}
	wg.Wait()
}

func (lb *LoadBalancer) startHealthMonitor(interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		lb.doctorCheckup()
		for range ticker.C {
			lb.doctorCheckup()
		}
	}()
}

func (lb *LoadBalancer) statusHandler(w http.ResponseWriter, r *http.Request) {
	type status struct {
		Target  string `json:"target"`
		Healthy bool   `json:"healthy"`
	}

	out := make([]status, 0, len(lb.backends))
	for _, b := range lb.backends {
		out = append(out, status{Target: b.URL.String(), Healthy: b.healthy.Load()})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
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

	for _, t := range targets {
		log.Printf("target: %s", t)
	}

	lb := NewLoadBalancer(targets, 10)
	log.Printf("pool: %d backends", len(lb.backends))
	lb.startHealthMonitor(2 * time.Second)

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})
	mux.HandleFunc("/status", lb.statusHandler)
	mux.Handle("/", lb)

	log.Fatal(http.ListenAndServe(":9000", mux))
}
