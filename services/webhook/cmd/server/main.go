package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/bashkirian/fintech-project/libs/observability"
	"github.com/bashkirian/fintech-project/services/webhook/internal/config"
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

	handler := webhookhttp.NewRouter(log)
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
