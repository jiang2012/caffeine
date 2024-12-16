package pprof

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	_ "net/http/pprof"
)

var httpServer *http.Server

func StartHttpServer(port int) {
	httpServer = &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: nil,
	}

	go func() {
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			panic(err)
		}
	}()
}

func StopHttpServer(ctx context.Context) error {
	return httpServer.Shutdown(ctx)
}
