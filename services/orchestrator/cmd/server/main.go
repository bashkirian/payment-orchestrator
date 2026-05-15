package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/bashkirian/fintech-project/libs/observability"
	"github.com/bashkirian/fintech-project/services/orchestrator/internal/config"
	"github.com/bashkirian/fintech-project/services/orchestrator/internal/domain"
	grpcserver "github.com/bashkirian/fintech-project/services/orchestrator/internal/grpc"
	"github.com/bashkirian/fintech-project/services/orchestrator/internal/provider"
	mockprovider "github.com/bashkirian/fintech-project/services/orchestrator/internal/provider/mock"
	stripeprovider "github.com/bashkirian/fintech-project/services/orchestrator/internal/provider/stripe"
)

func main() {
	root := &cobra.Command{
		Use:           "orchestrator",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newStartCmd())

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func newStartCmd() *cobra.Command {
	var cfgFile string

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the orchestrator service",
		RunE: func(cmd *cobra.Command, args []string) error {
			return run(cfgFile)
		},
	}
	cmd.Flags().StringVar(&cfgFile, "config", "", "path to config file (required)")
	_ = cmd.MarkFlagRequired("config")
	return cmd
}

func run(cfgFile string) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return err
	}

	log, err := observability.NewLogger(observability.LogConfig{
		Env:      cfg.Env,
		LogLevel: cfg.LogLevel,
	})
	if err != nil {
		return err
	}
	defer func() { _ = log.Sync() }()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		log.Warn("database ping failed; continuing without DB", zap.Error(err))
	}

	registry := provider.NewRegistry()
	registry.Register(domain.RailCard, stripeprovider.New(stripeprovider.Config{
		APIKey:         cfg.Stripe.APIKey,
		MaxRetries:     cfg.Stripe.MaxRetries,
		TimeoutSeconds: cfg.Stripe.TimeoutSeconds,
	}))
	registry.Register(domain.RailCrypto, &mockprovider.Provider{})

	grpcSrv := grpcserver.New(log, pool, registry)

	grpcLis, err := net.Listen("tcp", cfg.GRPCAddr)
	if err != nil {
		return err
	}

	httpMux := chi.NewRouter()
	httpMux.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	httpSrv := &http.Server{
		Addr:    cfg.HTTPAddr,
		Handler: httpMux,
	}

	errCh := make(chan error, 2)

	go func() {
		if err := grpcSrv.Serve(grpcLis); err != nil {
			errCh <- err
		}
	}()

	go func() {
		log.Info("starting http health server", zap.String("addr", cfg.HTTPAddr))
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		log.Info("shutting down orchestrator")
		grpcSrv.GracefulStop()

		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		return httpSrv.Shutdown(shutdownCtx)

	case err := <-errCh:
		return err
	}
}
