package service

import (
	"github.com/jiang2012/caffeine/internal/exchange"
	"github.com/jiang2012/caffeine/pkg/logging"
	"go.uber.org/zap"
	"strconv"
)

var OrderBookService = &orderBookService{}

type orderBookService struct{}

func (obs *orderBookService) Subscribe(symbols []string) error {
	for _, e := range exchange.Exchanges {
		if !e.IsEnabled() {
			continue
		}

		if err := e.SubscribeToFuturesOrderBook(symbols, func(ob *exchange.OrderBook) {
			if len(ob.Asks) == 0 || len(ob.Bids) == 0 {
				return
			}

			ob.Mu.Lock()
			ask1, _ := strconv.ParseFloat(ob.Asks[0][0], 64)
			bid1, _ := strconv.ParseFloat(ob.Bids[0][0], 64)
			ob.Mu.Unlock()

			logging.Info("BBO", zap.String("exchange", e.GetName()), zap.String("symbol", ob.Symbol), zap.Float64("ask1", ask1), zap.Float64("bid1", bid1))
		}); err != nil {
			return err
		}
	}

	return nil
}

func (obs *orderBookService) Unsubscribe(symbols []string) error {
	for _, e := range exchange.Exchanges {
		if !e.IsEnabled() {
			continue
		}
		if err := e.UnsubscribeToFuturesOrderBook(symbols); err != nil {
			return err
		}
	}

	return nil
}
