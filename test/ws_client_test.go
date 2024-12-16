package test

import (
	"context"
	"github.com/hzauccg/omega/internal/client"
	"github.com/hzauccg/omega/pkg/json"
	"testing"
	"time"
)

func TestWebSocketClient_Connect(t *testing.T) {
	defer setupTest(t)(t)

	//binanceWsTest(t)
	okxWsTest(t)
}

func binanceWsTest(t *testing.T) {
	url := "wss://fstream.binance.com/stream?streams=ondousdt@depth5@0ms/ondousdt@bookTicker/ondousdt@trade"

	c := client.NewWebSocketClient(url, nil, func(msg []byte) {
		t.Logf("Received message: %s", msg)
	})

	defer func(c *client.WebSocketClient, ctx context.Context) {
		_ = c.Close(ctx)
	}(c, context.Background())

	c.Connect()

	time.Sleep(10 * time.Second)
}

func okxWsTest(t *testing.T) {
	url := "wss://ws.okx.com:8443/ws/v5/business"
	symbol := "ONDO-USDT-SWAP"
	subscription := map[string]interface{}{
		"op":   "subscribe",
		"args": []map[string]string{{"channel": "trades-all", "instId": symbol}},
	}
	d, _ := json.Marshal(subscription)

	c := client.NewWebSocketClient(url, nil, func(msg []byte) {
		t.Logf("Received message: %s", msg)
	})

	defer func(c *client.WebSocketClient, ctx context.Context) {
		_ = c.Close(ctx)
	}(c, context.Background())

	c.Connect()
	c.Send() <- d

	time.Sleep(10 * time.Second)
}
