package main

import (
	"context"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"path"
	"strings"
	"time"

	"codeswitch/services"
)

type webServer struct {
	addr   string
	server *http.Server
}

func newWebServer(
	addr string,
	assets fs.FS,
	registry *rpcRegistry,
	events *eventHub,
	relay *services.ProviderRelayService,
) *webServer {
	mux := http.NewServeMux()
	mux.Handle("/api/rpc", registry)
	mux.Handle("/api/events", events)
	mux.HandleFunc("/api/info", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"version":       AppVersion,
			"web_address":   addr,
			"relay_address": relay.Addr(),
		})
	})
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, _ *http.Request) {
		relayRunning := len(relay.BoundAddresses()) > 0
		status := http.StatusOK
		state := "ok"
		if !relayRunning {
			status = http.StatusServiceUnavailable
			state = "degraded"
		}
		writeJSON(w, status, map[string]any{
			"status":        state,
			"relay_running": relayRunning,
		})
	})
	mux.Handle("/", staticWebUI(assets))

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		mux.ServeHTTP(w, r)
	})

	return &webServer{
		addr: addr,
		server: &http.Server{
			Addr:              addr,
			Handler:           handler,
			ReadHeaderTimeout: 10 * time.Second,
			IdleTimeout:       90 * time.Second,
		},
	}
}

func (s *webServer) Start() error {
	listener, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", s.addr, err)
	}
	go func() {
		if err := s.server.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Printf("WebUI server: %v", err)
		}
	}()
	return nil
}

func (s *webServer) Stop(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

func staticWebUI(assets fs.FS) http.Handler {
	if assets == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				http.NotFound(w, r)
				return
			}
			http.Error(w, "WebUI assets are not built", http.StatusServiceUnavailable)
		})
	}

	files := http.FileServer(http.FS(assets))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cleanPath := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if cleanPath == "." || cleanPath == "" {
			cleanPath = "index.html"
		}
		if _, err := fs.Stat(assets, cleanPath); err != nil {
			cleanPath = "index.html"
		}
		if cleanPath == "index.html" {
			w.Header().Set("Cache-Control", "no-cache")
		} else if strings.HasPrefix(cleanPath, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		clone := r.Clone(r.Context())
		// FileServer redirects explicit /index.html paths to ./; serving the SPA
		// shell through / avoids an empty redirect response for both the root and
		// client-side routes.
		if cleanPath == "index.html" {
			clone.URL.Path = "/"
		} else {
			clone.URL.Path = "/" + cleanPath
		}
		files.ServeHTTP(w, clone)
	})
}
