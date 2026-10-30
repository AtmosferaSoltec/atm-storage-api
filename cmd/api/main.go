package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/AtmosferaSoltec/atm-storage-api/db"
	"github.com/AtmosferaSoltec/atm-storage-api/internal/auth"
	"github.com/AtmosferaSoltec/atm-storage-api/internal/config"
	"github.com/AtmosferaSoltec/atm-storage-api/internal/features/file"
	"github.com/AtmosferaSoltec/atm-storage-api/internal/platform/httpserver"
	storage "github.com/AtmosferaSoltec/atm-storage-api/internal/provider/s3"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/joho/godotenv"
	"github.com/pressly/goose/v3"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		logger.Error("unable to load .env")
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, logger, os.Args[1:]); err != nil {
		logger.Error("storage service stopped", "error", err.Error())
		os.Exit(1)
	}
}
func run(ctx context.Context, logger *slog.Logger, args []string) error {
	if len(args) > 0 && args[0] == "migrate" {
		if len(args) != 2 {
			return errors.New("usage: api migrate up|down|status")
		}
		return migrate(ctx, args[1])
	}
	if len(args) > 1 || (len(args) == 1 && args[0] != "serve") {
		return errors.New("usage: api [serve|migrate up|down|status]")
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	verifier, err := auth.New(cfg.AuthPublicKeyBase64, cfg.AuthIssuer)
	if err != nil {
		return err
	}
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return errors.New("invalid database configuration")
	}
	defer pool.Close()
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	err = pool.Ping(pingCtx)
	cancel()
	if err != nil {
		return errors.New("database unavailable")
	}
	store := storage.New(storage.Config{Endpoint: cfg.Endpoint, Region: cfg.Region, AccessKeyID: cfg.AccessKeyID, SecretAccessKey: cfg.SecretAccessKey, Bucket: cfg.Bucket})
	service := file.NewService(file.NewRepository(pool), store, file.Settings{Environment: cfg.Environment, Provider: cfg.Provider, Bucket: cfg.Bucket, MaxUploadBytes: cfg.MaxUploadBytes, MaxLogoBytes: cfg.MaxLogoBytes, DownloadTTL: cfg.DownloadTTL, PreviewTTL: cfg.PreviewTTL})
	gin.SetMode(gin.ReleaseMode)
	engine := gin.New()
	if err := engine.SetTrustedProxies(nil); err != nil {
		return err
	}
	// A custom recovery avoids Gin logging authorization headers on panic.
	engine.Use(func(c *gin.Context) {
		defer func() {
			if recover() != nil {
				logger.Error("request panic")
				httpserver.Error(c, 500, "internal_error", "Unable to complete the request.")
			}
		}()
		c.Next()
	}, httpserver.Middleware(cfg.CORSOrigins))
	type statusResponse struct {
		Status string `json:"status"`
	}
	engine.GET("/health", func(c *gin.Context) { c.JSON(200, statusResponse{Status: "ok"}) })
	engine.GET("/ready", func(c *gin.Context) {
		readyCtx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		if err := pool.Ping(readyCtx); err != nil {
			c.JSON(503, statusResponse{Status: "database_unavailable"})
			return
		}
		c.JSON(200, statusResponse{Status: "ready"})
	})
	file.RegisterRoutes(engine.Group("/v1", verifier.Middleware()), file.NewHandler(service, cfg.MaxUploadBytes, logger))
	engine.NoRoute(func(c *gin.Context) { httpserver.Error(c, 404, "not_found", "Route not found.") })
	server := &http.Server{Addr: ":" + cfg.HTTPPort, Handler: engine, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 90 * time.Second, WriteTimeout: 95 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	result := make(chan error, 1)
	go func() { result <- server.ListenAndServe() }()
	logger.Info("storage service started", "port", cfg.HTTPPort, "environment", cfg.Environment, "provider", cfg.Provider)
	select {
	case err := <-result:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("listen: %w", err)
	case <-ctx.Done():
	}
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		_ = server.Close()
		return errors.New("graceful shutdown timed out")
	}
	return nil
}
func migrate(ctx context.Context, command string) error {
	if command != "up" && command != "down" && command != "status" {
		return errors.New("usage: api migrate up|down|status")
	}
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return errors.New("DATABASE_URL is required")
	}
	conn, err := sql.Open("pgx", dsn)
	if err != nil {
		return errors.New("invalid database configuration")
	}
	defer conn.Close()
	goose.SetBaseFS(db.Migrations)
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	if err := goose.RunContext(ctx, command, conn, "migrations"); err != nil {
		return errors.New("database migration failed; inspect database migration status")
	}
	return nil
}
