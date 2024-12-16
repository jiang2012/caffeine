package service

import (
	"github.com/duke-git/lancet/v2/slice"
	"github.com/jiang2012/caffeine/internal/exchange"
	"github.com/jiang2012/caffeine/pkg/gcron"
	"github.com/jiang2012/caffeine/pkg/logging"
	"go.uber.org/zap"
)

var SymbolService = &symbolService{}

type SymbolAddedHandler func(symbols []string) error
type SymbolRemovedHandler func(symbols []string) error

type symbolService struct{}

var symbols []string

func (ss *symbolService) MonitorStatusChange(initialSymbolNumLimit int, addedHandler SymbolAddedHandler, removedHandler SymbolRemovedHandler) {
	ss.doMonitor(initialSymbolNumLimit, addedHandler, removedHandler)

	_, _ = gcron.AddFunc("0 */20 * * * ?", func() {
		ss.doMonitor(initialSymbolNumLimit, addedHandler, removedHandler)
	})
}

func (ss *symbolService) doMonitor(initialSymbolNumLimit int, addedHandler SymbolAddedHandler, removedHandler SymbolRemovedHandler) {
	newSymbols := ss.getSymbols()

	if len(symbols) == 0 {
		var s []string
		if initialSymbolNumLimit > 0 && len(newSymbols) > initialSymbolNumLimit {
			s = newSymbols[:initialSymbolNumLimit]
		} else {
			s = newSymbols
		}

		logging.Info("Symbols", zap.Strings("init", s), zap.Int("number", len(s)))
		if err := addedHandler(s); err != nil {
			logging.Error("SymbolAddedHandler", zap.Error(err))
		}
	} else {
		addedSymbols := slice.Difference(newSymbols, symbols)
		logging.Info("Symbols", zap.Strings("added", addedSymbols), zap.Int("number", len(addedSymbols)))
		if err := addedHandler(addedSymbols); err != nil {
			logging.Error("SymbolAddedHandler", zap.Error(err))
		}

		removedSymbols := slice.Difference(symbols, newSymbols)
		logging.Info("Symbols", zap.Strings("removed", removedSymbols), zap.Int("number", len(removedSymbols)))
		if err := removedHandler(removedSymbols); err != nil {
			logging.Error("SymbolRemovedHandler", zap.Error(err))
		}
	}

	symbols = newSymbols
	logging.Info("Symbols", zap.Strings("current", symbols), zap.Int("number", len(symbols)))
}

func (ss *symbolService) getSymbols() []string {
	var s []string

	for _, e := range exchange.Exchanges {
		if !e.IsEnabled() {
			continue
		}

		if len(s) == 0 {
			s = e.GetUniversalFuturesSymbols()
		} else {
			s = slice.Intersection(s, e.GetUniversalFuturesSymbols())
		}
	}
	slice.Sort(s)
	return s
}
