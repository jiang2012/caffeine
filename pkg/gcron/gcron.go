package gcron

import (
	"context"
	"github.com/robfig/cron/v3"
	"go.uber.org/zap"
	"runtime/debug"
	"sync"
)

var c *cron.Cron
var l *logger
var runningMu sync.Mutex
var running bool

func Start(zapLogger *zap.Logger) {
	runningMu.Lock()
	defer runningMu.Unlock()
	if running {
		return
	}

	running = true

	l = &logger{zapLogger, false}
	c = cron.New(cron.WithLogger(l), cron.WithSeconds(), cron.WithChain(cron.Recover(l), cron.DelayIfStillRunning(l)))
	c.Start()
}

func AddFunc(spec string, cmd func()) (cron.EntryID, error) {
	if entryID, err := c.AddFunc(spec, cmd); err != nil {
		l.Error(err, "AddFunc", "spec", spec, "stack", debug.Stack())
		return 0, err
	} else {
		return entryID, nil
	}
}

func Remove(entryID cron.EntryID) {
	c.Remove(entryID)
}

func Stop(ctx context.Context) error {
	runningMu.Lock()
	defer runningMu.Unlock()
	if running {
		running = false
	}

	stopCtx := c.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-stopCtx.Done():
		return nil
	}
}

type logger struct {
	*zap.Logger
	logInfo bool
}

func (l *logger) Info(msg string, keysAndValues ...interface{}) {
	if l.logInfo {
		l.Sugar().Infow(msg, keysAndValues...)
	}
}

func (l *logger) Error(err error, msg string, keysAndValues ...interface{}) {
	l.Sugar().Errorw(msg, append(keysAndValues, "error", err.Error())...)
}
