package test

import (
	"github.com/hzauccg/omega/pkg/logging"
	"testing"
)

func setupTest(tb testing.TB) func(tb testing.TB) {
	// setup code here
	logging.Init(&logging.Config{
		Path:       "../logs",
		FileName:   "test.log",
		Level:      "info",
		JsonFormat: true,
		MaxSize:    5,
		MaxAge:     1,
		MaxBackups: 1,
		Compress:   false,
		Stdout:     true,
	})

	return func(tb testing.TB) {
		// teardown code here
		logging.Sync()
	}
}
