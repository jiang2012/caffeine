package main

import (
	"context"
	"github.com/jiang2012/caffeine/internal/config"
	"github.com/jiang2012/caffeine/internal/exchange"
	"github.com/jiang2012/caffeine/internal/service"
	"github.com/jiang2012/caffeine/pkg/gcron"
	"github.com/jiang2012/caffeine/pkg/logging"
	"github.com/jiang2012/caffeine/pkg/pprof"
	"go.uber.org/zap"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

func main() {
	cfg := config.Init()

	// logger
	logging.Init(cfg.Log)
	defer logging.Sync()

	// cron
	gcron.Start(logging.Logger())

	// exchange
	exchange.Init(cfg)

	service.SymbolService.MonitorStatusChange(cfg.SymbolNumLimit, func(symbols []string) error {
		return service.OrderBookService.Subscribe(symbols)
	}, func(symbols []string) error {
		return service.OrderBookService.Unsubscribe(symbols)
	})

	// pprof
	pprof.StartHttpServer(cfg.HttpServer.PprofPort)

	logging.Info("Env", zap.String("GOGC", os.Getenv("GOGC")))
	awaitShutdown()
}

func awaitShutdown() {
	done := make(chan os.Signal)
	signal.Notify(done, syscall.SIGINT, syscall.SIGTERM)

	<-done
	logging.Info("Shutting down...")

	ctx, cancelFunc := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelFunc()

	shutdown := func(module string, ctx context.Context, f func(ctx context.Context) error) {
		logging.Info("Shutting down module: " + module)
		if err := f(ctx); err != nil {
			logging.Error("Error while shutting down module: "+module, zap.Error(err))
		} else {
			logging.Info(module + " exited")
		}
	}

	wg := sync.WaitGroup{}

	wg.Add(1)
	go func() {
		defer wg.Done()
		shutdown("cron", ctx, gcron.Stop)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		shutdown("exchange", ctx, exchange.Close)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		shutdown("pprof", ctx, pprof.StopHttpServer)
	}()

	wg.Wait()
}
