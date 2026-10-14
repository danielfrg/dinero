package server

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"
)

// Serve runs until the listener fails or ctx is canceled, then drains active requests.
func Serve(ctx context.Context, listener net.Listener, handler http.Handler, shutdownTimeout time.Duration) error {
	srv := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
	served := make(chan error, 1)
	go func() { served <- srv.Serve(listener) }()
	select {
	case err := <-served:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			srv.Close()
			<-served
			return err
		}
		err := <-served
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
