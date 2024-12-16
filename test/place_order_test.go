package test

import (
	"github.com/hzauccg/omega/internal/exchange"
	"testing"
)

func TestPlaceOrder(t *testing.T) {
	defer setupTest(t)(t)

	okxTest(t)
}

func okxTest(t *testing.T) {
	for _, e := range exchange.Exchanges {
		if e.GetName() == "okx" {
			if err := e.PlaceFuturesOrder(&exchange.FuturesOrder{
				Symbol:          "dogeusdt",
				Mode:            exchange.Cross,
				Type:            exchange.Limit,
				Side:            exchange.Sell,
				PosSide:         exchange.Short,
				Leverage:        5,
				Price:           0.166,
				Quantity:        0.1,
				TakeProfitPrice: 0.158,
				StopLossPrice:   0.17,
			}); err != nil {
				t.Errorf("OKX place futures order error: %+v", err)
			} else {
				t.Logf("OKX place futures order success")
			}
		}
	}
}
