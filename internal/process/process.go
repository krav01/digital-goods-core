package process

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/krav01/digital-goods-core/internal/config"
)

func Main(run func(context.Context, *slog.Logger) error) {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx, logger)
	stop()
	if err != nil {
		logger.Error("process stopped", "error", err)
		os.Exit(1)
	}
}

func Serve(ctx context.Context, cfg config.Config, handler http.Handler, logger *slog.Logger) error {
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", cfg.HTTPAddress)
	if err != nil {
		return fmt.Errorf("listen HTTP: %w", err)
	}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	logger.Info("HTTP listening", "address", listener.Addr().String())
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cfg.ShutdownTimeout)
	defer cancel()
	err = server.Shutdown(shutdownCtx)
	if err != nil {
		err = errors.Join(err, server.Close())
	}
	serveErr := <-done
	if errors.Is(serveErr, http.ErrServerClosed) {
		serveErr = nil
	}
	logger.Info("HTTP stopped")
	return errors.Join(err, serveErr)
}
