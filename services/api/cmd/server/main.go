package main

import (
	"context"
	"errors"
	"net/http"
	"os/signal"
	"syscall"

	"github.com/bashkirian/fintech-project/services/api/internal/config"
	apigrpc "github.com/bashkirian/fintech-project/services/api/internal/grpc"
	apihttp "github.com/bashkirian/fintech-project/services/api/internal/http"
	"github.com/bashkirian/fintech-project/services/api/internal/logger"
)

func main() {
	if err := run(); err != nil {
		panic(err)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	log, err := logger.New(cfg)
	if err != nil {
		return err
	}
	defer func() {
		_ = log.Sync()
	}()

	orchestratorClient, err := apigrpc.NewOrchestratorClient(cfg.OrchestratorAddr)
	if err != nil {
		return err
	}
	defer func() { _ = orchestratorClient.Close() }()

	handler := apihttp.NewRouter(log, orchestratorClient)
	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
		ReadTimeout:       cfg.ReadTimeout,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		IdleTimeout:       cfg.IdleTimeout,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		log.Info("starting http server", logger.FieldString("addr", cfg.HTTPAddr))
		if serveErr := server.ListenAndServe(); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			errCh <- serveErr
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()

		log.Info("shutting down http server")
		if err := server.Shutdown(shutdownCtx); err != nil {
			return err
		}
		return nil
	case err := <-errCh:
		return err
	}
}
