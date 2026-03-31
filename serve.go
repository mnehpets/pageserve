package pageserve

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// Serve starts one goroutine per listener in cfg.Server.Listeners, each
// running either HTTP or HTTPS depending on whether TLS is configured.
// All listeners share the same srv handler.
//
// Serve blocks until ctx is cancelled or any listener returns an error.
// On exit it initiates a graceful shutdown of all listeners (5 s timeout)
// and waits for all goroutines to finish before returning.
//
// A context cancellation is treated as a normal shutdown; Serve returns nil
// in that case. Any other error is returned as-is.
func Serve(ctx context.Context, cfg Config, srv *Server) error {
	n := len(cfg.Server.Listeners)
	if n == 0 {
		return fmt.Errorf("pageserve: no listeners configured")
	}

	errCh := make(chan error, n)
	servers := make([]*http.Server, n)

	for i, l := range cfg.Server.Listeners {
		hs := &http.Server{Addr: l.Address, Handler: srv}
		servers[i] = hs
		go func(hs *http.Server, l ListenerConfig) {
			var err error
			if l.TLS != nil {
				err = hs.ListenAndServeTLS(l.TLS.CertFile, l.TLS.KeyFile)
			} else {
				err = hs.ListenAndServe()
			}
			if err == http.ErrServerClosed {
				err = nil
			}
			errCh <- err
		}(hs, l)
	}

	// Wait for the first listener error or context cancellation.
	var firstErr error
	select {
	case err := <-errCh:
		firstErr = err
		n-- // one result already consumed
	case <-ctx.Done():
		// Normal shutdown — don't propagate context.Canceled.
	}

	// Shut down all listeners gracefully.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, s := range servers {
		s.Shutdown(shutdownCtx) //nolint:errcheck
	}

	// Drain remaining goroutine results.
	for ; n > 0; n-- {
		<-errCh
	}

	return firstErr
}
