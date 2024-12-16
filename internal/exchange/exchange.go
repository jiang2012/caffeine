package exchange

import (
	"context"
	"encoding/hex"
	"errors"
	"github.com/duke-git/lancet/v2/cryptor"
	"github.com/jiang2012/caffeine/internal/config"
	"strings"
	"sync"
)

var Exchanges []Exchange

type Exchange interface {
	close(ctx context.Context) error

	IsEnabled() bool
	GetName() string
	GetUniversalFuturesSymbols() []string                                                        // 获取所有合约交易对，统一格式
	GetFuturesSymbol(universalSymbol string) string                                              // 获取合约交易对，内部格式
	SubscribeToFuturesOrderBook(universalSymbols []string, onUpdate func(book *OrderBook)) error // 订阅合约交易对订单簿
	UnsubscribeToFuturesOrderBook(universalSymbols []string) error                               // 取消订阅合约交易对订单簿
	GetFuturesPositions(universalSymbol string) ([]*FuturesPosition, error)                      // 获取合约仓位
	ClosePosition(universalSymbol string, mode MarginMode, posSide PositionSide) error           // 市价仓位全平
	PlaceFuturesOrder(order *FuturesOrder) error                                                 // 合约下单
}

type OrderBook struct {
	Symbol              string
	Asks                [][]string // [][price, quantity]
	Bids                [][]string // [][price, quantity]
	Trades              []*Trade
	LastDepthSeqId      int64 // depth seqId
	LastDepthUpdateTime int64 // depth update time
	LastTradeTime       int64 // trade time
	Mu                  sync.Mutex
}

type Trade struct {
	Symbol       string
	TradeId      int64
	TradeTime    int64
	Price        string
	Quantity     string
	IsBuyerMaker bool // true: sell, false: buy
}

type MarginMode string
type OrderType string
type OrderSide string
type PositionSide string

const (
	Isolated MarginMode = "isolated" // 逐仓
	Cross    MarginMode = "cross"    // 全仓

	Market OrderType = "market" // 市价单
	Limit  OrderType = "limit"  // 限价单

	Buy  OrderSide = "buy"
	Sell OrderSide = "sell"

	Long  PositionSide = "long"
	Short PositionSide = "short"
)

type FuturesOrder struct {
	OrderId         string
	Symbol          string
	Mode            MarginMode
	Type            OrderType
	Side            OrderSide
	PosSide         PositionSide
	Leverage        int
	Price           float64
	Quantity        float64
	TakeProfitPrice float64
	StopLossPrice   float64
}

type FuturesPosition struct {
	Symbol           string
	Mode             MarginMode
	PosSide          PositionSide
	Leverage         int
	Quantity         float64
	EntryPrice       float64
	MarkPrice        float64
	LiquidationPrice float64
	UnrealizedPnl    float64
	RealizedPnl      float64
}

func Init(cfg *config.Config) {
	Exchanges = []Exchange{newBinance(cfg), newOkx(cfg)}
}

func Close(ctx context.Context) error {
	var err error
	for _, exchange := range Exchanges {
		err = errors.Join(err, exchange.close(ctx))
	}

	return err
}

func (ob *OrderBook) updateDepth(asks, bids [][]string, seqId, time int64) {}

func (ob *OrderBook) updateBBO(asks, bids [][]string, seqId, time int64) {
	ob.Mu.Lock()
	defer ob.Mu.Unlock()

	if time < ob.LastDepthUpdateTime {
		//logging.Info("updateBBO", zap.Int64("LastDepthUpdateTime", ob.LastDepthUpdateTime), zap.Int64("time", time))
		return
	}

	if len(ob.Asks) == 0 {
		ob.Asks = [][]string{{"", ""}}
	}

	if len(ob.Bids) == 0 {
		ob.Bids = [][]string{{"", ""}}
	}

	ob.Asks[0][0] = asks[0][0]
	ob.Asks[0][1] = asks[0][1]
	ob.Bids[0][0] = bids[0][0]
	ob.Bids[0][1] = bids[0][1]

	ob.LastDepthSeqId = seqId
	ob.LastDepthUpdateTime = time
}

func (ob *OrderBook) updateTrade(trade *Trade) {}

func getStreamName(symbol, channel string) string {
	return strings.Join([]string{symbol, channel}, "@")
}

func decode(encoded string, key []byte) []byte {
	b, _ := hex.DecodeString(encoded)
	return cryptor.AesCbcDecrypt(b, key)
}
