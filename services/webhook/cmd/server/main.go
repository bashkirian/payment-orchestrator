package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/redis/rueidis"
	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/bashkirian/fintech-project/libs/observability"
	"github.com/bashkirian/fintech-project/services/webhook/internal/config"
	"github.com/bashkirian/fintech-project/services/webhook/internal/grpc"
	webhookhttp "github.com/bashkirian/fintech-project/services/webhook/internal/http"
)

func main() {
	root := &cobra.Command{
		Use:           "webhook",
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
		Short: "Start the webhook service",
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

	// Initialize Redis client
	var redisClient rueidis.Client
	if cfg.RedisAddr != "" {
		redisClient, err = rueidis.NewClient(rueidis.ClientOption{
			InitAddress: []string{cfg.RedisAddr},
			Password:    cfg.RedisPassword,
		})
		if err != nil {
			return err
		}
		defer redisClient.Close()
		log.Info("connected to redis", zap.String("addr", cfg.RedisAddr))
	}

	// Initialize orchestrator gRPC client
	orchestratorClient, err := grpc.NewOrchestratorClient(cfg.OrchestratorAddr)
	if err != nil {
		return err
	}
	defer func() {
		if err := orchestratorClient.Close(); err != nil {
			log.Error("close orchestrator client", zap.Error(err))
		}
	}()
	log.Info("connected to orchestrator", zap.String("addr", cfg.OrchestratorAddr))

	handler := webhookhttp.NewRouter(webhookhttp.Dependencies{
		Log:          log,
		RedisClient:  redisClient,
		Orchestrator: orchestratorClient,
		StripeSecret: cfg.StripeWebhookSecret,
	})
	srv := &http.Server{
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
		log.Sugar().Infof("starting webhook http server on %s", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		log.Info("shutting down webhook")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		return srv.Shutdown(shutdownCtx)

	case err := <-errCh:
		return err
	}
}
