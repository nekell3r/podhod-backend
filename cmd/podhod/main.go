package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
	"podhod/server/internal/authn"
	"podhod/server/internal/httpapi"
	mcptransport "podhod/server/internal/mcp"
	"podhod/server/internal/service"
	"podhod/server/internal/storage"
)

func main() {
	if err := run(); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()
	base := strings.TrimRight(os.Getenv("PUBLIC_BASE_URL"), "/")
	issuer := os.Getenv("OIDC_ISSUER")
	if base == "" || issuer == "" {
		return fmt.Errorf("PUBLIC_BASE_URL and OIDC_ISSUER are required")
	}
	dsn := os.Getenv("DATABASE_URL")
	if filename := os.Getenv("DATABASE_URL_FILE"); filename != "" {
		data, err := os.ReadFile(filename)
		if err != nil {
			return fmt.Errorf("read database configuration: %w", err)
		}
		dsn = strings.TrimSpace(string(data))
	}
	if dsn == "" {
		return fmt.Errorf("database configuration is required")
	}
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return fmt.Errorf("invalid database configuration")
	}
	config.MaxConns = 8
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return fmt.Errorf("create database pool: %w", err)
	}
	defer pool.Close()
	repo := storage.New(pool)
	if err := repo.Migrate(ctx); err != nil {
		return err
	}
	apiVerifier, err := authn.NewVerifier(ctx, issuer, "podhod-api")
	if err != nil {
		return err
	}
	mcpVerifier, err := authn.NewVerifier(ctx, issuer, base+"/mcp")
	if err != nil {
		return err
	}
	training := service.New(repo)
	metadataURL := base + "/.well-known/oauth-protected-resource"
	scopes := []string{"schedule:read", "schedule:write", "workout:read", "workout:write"}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		probe, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := pool.Ping(probe); err != nil {
			http.Error(w, "unhealthy", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte("ok\n")); err != nil {
			slog.Warn("health response failed", "error", err)
		}
	})
	mux.HandleFunc("GET /v1/config", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		configuration := map[string]any{
			"issuer":   issuer,
			"clientID": "podhod-ios",
			"scopes":   append([]string{"openid", "offline_access"}, scopes...),
		}
		if err := json.NewEncoder(w).Encode(configuration); err != nil {
			slog.Warn("config response failed", "error", err)
		}
	})
	metadata := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		value := map[string]any{"resource": base + "/mcp", "authorization_servers": []string{issuer},
			"scopes_supported": scopes, "bearer_methods_supported": []string{"header"}, "resource_name": "Подход"}
		if err := json.NewEncoder(w).Encode(value); err != nil {
			slog.Warn("metadata response failed", "error", err)
		}
	}
	mux.HandleFunc("GET /.well-known/oauth-protected-resource", metadata)
	mux.HandleFunc("GET /.well-known/oauth-protected-resource/mcp", metadata)
	authenticateAPI := sdkauth.RequireBearerToken(
		apiVerifier,
		&sdkauth.RequireBearerTokenOptions{ResourceMetadataURL: metadataURL},
	)
	api := authenticateAPI(httpapi.New(training))
	mux.Handle("/v1/", http.TimeoutHandler(api, 15*time.Second, "request timed out"))
	mcp := sdkauth.RequireBearerToken(mcpVerifier, &sdkauth.RequireBearerTokenOptions{ResourceMetadataURL: metadataURL})(
		mcptransport.NewHandler(training, metadataURL))
	mux.Handle("/mcp", mcp)
	address := os.Getenv("LISTEN_ADDR")
	if address == "" {
		address = ":8080"
	}
	server := &http.Server{
		Addr:              address,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    32 << 10,
	}
	stopped := make(chan error, 1)
	go func() { stopped <- server.ListenAndServe() }()
	slog.Info("podhod server listening", "address", address)
	select {
	case err := <-stopped:
		if !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve HTTP: %w", err)
		}
	case <-ctx.Done():
		shutdown, stop := context.WithTimeout(context.Background(), 15*time.Second)
		defer stop()
		if err := server.Shutdown(shutdown); err != nil {
			return fmt.Errorf("graceful shutdown: %w", err)
		}
	}
	return nil
}
