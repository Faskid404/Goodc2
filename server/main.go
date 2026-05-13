package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func dbPath() string {
	if p := os.Getenv("DB_PATH"); p != "" {
		return p
	}
	return "c2.db"
}

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	initDB(dbPath())
	log.Println("[db] ready")

	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", HandleHealth)
	mux.HandleFunc("POST /api/auth/login", HandleLogin)

	mux.Handle("GET /ws/agent", middlewareAgentToken(http.HandlerFunc(HandleAgentWS)))
	mux.Handle("GET /ws/dashboard", middlewareJWT(http.HandlerFunc(HandleDashboardWS)))

	api := http.NewServeMux()
	api.HandleFunc("GET /agents", HandleListAgents)
	api.HandleFunc("GET /agents/{id}/metrics", HandleAgentMetrics)
	api.HandleFunc("GET /agents/{id}/events", HandleAgentEvents)
	api.HandleFunc("GET /agents/{id}/commands", HandleAgentCommands)
	api.HandleFunc("POST /agents/{id}/commands", HandleAgentCommands)
	api.HandleFunc("GET /events", HandleAllEvents)
	mux.Handle("/api/", middlewareJWT(http.StripPrefix("/api", api)))

	mux.Handle("/", http.FileServer(http.Dir("dashboard/dist")))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      withCORS(withLogger(mux)),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		log.Printf("[server] :%s", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("[server] fatal: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("[server] shutting down")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
	log.Println("[server] stopped")
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if o := r.Header.Get("Origin"); o != "" {
			w.Header().Set("Access-Control-Allow-Origin", o)
			w.Header().Set("Access-Control-Allow-Methods", "GET,POST,OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization,Content-Type,X-Agent-Token")
			w.Header().Set("Access-Control-Max-Age", "86400")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

type rw struct {
	http.ResponseWriter
	code int
}

func (r *rw) WriteHeader(c int) { r.code = c; r.ResponseWriter.WriteHeader(c) }

func withLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t := time.Now()
		wrapped := &rw{ResponseWriter: w, code: 200}
		next.ServeHTTP(wrapped, r)
		log.Printf("[http] %d %s %s %s", wrapped.code, r.Method, r.URL.Path, time.Since(t))
	})
}
