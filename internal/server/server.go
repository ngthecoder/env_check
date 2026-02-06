package server

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"envcheck/internal/collector"

	"github.com/fsnotify/fsnotify"
)

type Server struct {
	collector *collector.Collector
	port      int
	interval  time.Duration
	webFS     fs.FS
	mu        sync.RWMutex
	clients   map[chan []byte]bool // SSE clients
}

type Config struct {
	Port     int
	Interval time.Duration
	WebFS    fs.FS
}

func New(cfg Config) *Server {
	if cfg.Port == 0 {
		cfg.Port = 8484
	}
	if cfg.Interval == 0 {
		cfg.Interval = 30 * time.Second
	}

	return &Server{
		collector: collector.New(),
		port:      cfg.Port,
		interval:  cfg.Interval,
		webFS:     cfg.WebFS,
		clients:   make(map[chan []byte]bool),
	}
}

func (s *Server) Run() error {
	// Initial collection
	log.Println("⚡ Running initial scan...")
	s.collector.Collect()
	log.Println("✅ Scan complete")

	// Set up HTTP routes
	mux := http.NewServeMux()

	// API
	mux.HandleFunc("GET /api/status", s.handleStatus)
	mux.HandleFunc("POST /api/rescan", s.handleRescan)
	mux.HandleFunc("GET /api/events", s.handleSSE)

	// Dashboard
	if s.webFS != nil {
		mux.Handle("/", http.FileServer(http.FS(s.webFS)))
	}

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", s.port),
		Handler: withCORS(mux),
	}

	// Start background scanner
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go s.periodicScan(ctx)
	go s.watchFiles(ctx)

	// Graceful shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Println("\n🛑 Shutting down...")
		cancel()
		srv.Shutdown(context.Background())
	}()

	log.Printf("🚀 envcheck server running on http://localhost:%d", s.port)
	log.Printf("   API:       http://localhost:%d/api/status", s.port)
	log.Printf("   Dashboard: http://localhost:%d/", s.port)
	log.Printf("   Scan interval: %s", s.interval)

	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		return err
	}
	return nil
}

// ──────────────────────────────────────────
// HTTP Handlers
// ──────────────────────────────────────────

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	data, err := s.collector.GetReportJSON()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

func (s *Server) handleRescan(w http.ResponseWriter, r *http.Request) {
	log.Println("🔄 Manual rescan triggered")
	report := s.collector.Collect()
	s.broadcast(report)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "ok",
		"timestamp": report.Timestamp,
	})
}

// SSE endpoint for real-time updates
func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", 500)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := make(chan []byte, 10)
	s.mu.Lock()
	s.clients[ch] = true
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.clients, ch)
		s.mu.Unlock()
		close(ch)
	}()

	// Send initial data
	if data, err := s.collector.GetReportJSON(); err == nil {
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}

	for {
		select {
		case msg, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", msg)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

// ──────────────────────────────────────────
// Background tasks
// ──────────────────────────────────────────

func (s *Server) periodicScan(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			log.Println("🔄 Periodic scan...")
			report := s.collector.Collect()
			s.broadcast(report)
			log.Println("✅ Scan complete")
		case <-ctx.Done():
			return
		}
	}
}

func (s *Server) watchFiles(ctx context.Context) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Printf("⚠️  File watcher unavailable: %v", err)
		return
	}
	defer watcher.Close()

	// Watch version files
	files := s.collector.WatchFiles()
	for _, f := range files {
		dir := filepath.Dir(f)
		if err := watcher.Add(dir); err != nil {
			log.Printf("⚠️  Cannot watch %s: %v", dir, err)
		}
	}

	// Watch cwd for new version files
	cwd, _ := os.Getwd()
	watcher.Add(cwd)

	watchedNames := map[string]bool{}
	for _, f := range files {
		watchedNames[filepath.Base(f)] = true
	}
	// Also watch for common version files that might be created
	for _, name := range []string{".python-version", ".go-version", ".node-version", ".tool-versions", "go.mod", "package.json", "Cargo.toml"} {
		watchedNames[name] = true
	}

	log.Printf("👁  Watching %d version files for changes", len(files))

	// Debounce: don't rescan more than once per 2 seconds
	var debounce *time.Timer

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			base := filepath.Base(event.Name)
			if watchedNames[base] && (event.Op&(fsnotify.Write|fsnotify.Create) != 0) {
				if debounce != nil {
					debounce.Stop()
				}
				debounce = time.AfterFunc(2*time.Second, func() {
					log.Printf("📝 %s changed, rescanning...", base)
					report := s.collector.Collect()
					s.broadcast(report)
				})
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			log.Printf("⚠️  Watch error: %v", err)
		case <-ctx.Done():
			return
		}
	}
}

func (s *Server) broadcast(report *collector.Report) {
	data, err := json.Marshal(report)
	if err != nil {
		return
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for ch := range s.clients {
		select {
		case ch <- data:
		default:
			// Skip slow clients
		}
	}
}

// ──────────────────────────────────────────
// Middleware
// ──────────────────────────────────────────

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == "OPTIONS" {
			w.WriteHeader(204)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// EmbedFS helper - wraps embed.FS for http serving
func SubFS(fsys embed.FS, dir string) (fs.FS, error) {
	return fs.Sub(fsys, dir)
}
