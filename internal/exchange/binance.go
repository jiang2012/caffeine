package exchange

import (
	"context"
	"errors"
	"fmt"
	"github.com/duke-git/lancet/v2/slice"
	"github.com/duke-git/lancet/v2/strutil"
	"github.com/jiang2012/caffeine/internal/client"
	"github.com/jiang2012/caffeine/internal/config"
	"github.com/jiang2012/caffeine/internal/helper"
	"github.com/jiang2012/caffeine/pkg/json"
	"github.com/jiang2012/caffeine/pkg/logging"
	"go.uber.org/zap"
	"strings"
	"sync"
)

const (
	bnChannelDepth5 = "depth5@0ms"
	bnChannelBBO    = "bookTicker"
	bnChannelTrades = "trade"
)

type binance struct {
	cfg *config.Binance

	wsClientManager *helper.WebSocketClientManager

	orderBooks map[string]*OrderBook

	// "symbol@channel" -> []process
	processes sync.Map

	mu sync.Mutex
}

type bnResponse struct {
	Stream string `json:"stream"`
}

type bnDepth struct {
	Data struct {
		EventType string     `json:"e"`
		EventTime int64      `json:"E"`
		TransTime int64      `json:"T"`
		UpdateId  int64      `json:"u"`
		Symbol    string     `json:"s"`
		Bids      [][]string `json:"b"`
		Asks      [][]string `json:"a"`
	} `json:"data"`
}

type bnBBO struct {
	Data struct {
		EventType string `json:"e"`
		EventTime int64  `json:"E"`
		TransTime int64  `json:"T"`
		UpdateId  int64  `json:"u"`
		Symbol    string `json:"s"`
		BidPrice  string `json:"b"`
		BidQty    string `json:"B"`
		AskPrice  string `json:"a"`
		AskQty    string `json:"A"`
	} `json:"data"`
}

type bnTrade struct {
	Data struct {
		EventType     string `json:"e"`
		EventTime     int64  `json:"E"`
		TradeTime     int64  `json:"T"`
		TradeId       int64  `json:"t"`
		Symbol        string `json:"s"`
		Price         string `json:"p"`
		Quantity      string `json:"q"`
		BuyerOrderId  int64  `json:"b"`
		SellerOrderId int64  `json:"a"`
		IsBuyerMaker  bool   `json:"m"`
	} `json:"data"`
}

var bnResponsePool sync.Pool
var bnDepthPool sync.Pool
var bnBBOPool sync.Pool
var bnTradePool sync.Pool

func newBnResponse() *bnResponse {
	if v := bnResponsePool.Get(); v != nil {
		res := v.(*bnResponse)
		res.reset()
		return res
	}
	return &bnResponse{}
}

func (r *bnResponse) reset() {
	r.Stream = ""
}

func newBnDepth() *bnDepth {
	if v := bnDepthPool.Get(); v != nil {
		d := v.(*bnDepth)
		d.reset()
		return d
	}
	return &bnDepth{}
}

func (d *bnDepth) reset() {
	d.Data.EventType = ""
	d.Data.EventTime = 0
	d.Data.TransTime = 0
	d.Data.UpdateId = 0
	d.Data.Symbol = ""

	for _, ask := range d.Data.Asks {
		ask[0] = ""
		ask[1] = ""
	}

	for _, bid := range d.Data.Bids {
		bid[0] = ""
		bid[1] = ""
	}
}

func newBnBBO() *bnBBO {
	if v := bnBBOPool.Get(); v != nil {
		b := v.(*bnBBO)
		b.reset()
		return b
	}
	return &bnBBO{}
}

func (b *bnBBO) reset() {
	b.Data.EventType = ""
	b.Data.EventTime = 0
	b.Data.TransTime = 0
	b.Data.UpdateId = 0
	b.Data.Symbol = ""
	b.Data.BidPrice = ""
	b.Data.BidQty = ""
	b.Data.AskPrice = ""
	b.Data.AskQty = ""
}

func newBnTrade() *bnTrade {
	if v := bnTradePool.Get(); v != nil {
		t := v.(*bnTrade)
		t.reset()
		return t
	}
	return &bnTrade{}
}

func (t *bnTrade) reset() {
	t.Data.EventType = ""
	t.Data.EventTime = 0
	t.Data.TradeTime = 0
	t.Data.TradeId = 0
	t.Data.Symbol = ""
	t.Data.Price = ""
	t.Data.Quantity = ""
	t.Data.BuyerOrderId = 0
	t.Data.SellerOrderId = 0
	t.Data.IsBuyerMaker = false
}

func newBinance(cfg *config.Config) *binance {
	return &binance{
		cfg:             cfg.GetBinance(),
		wsClientManager: helper.NewWebSocketClientManager(cfg.StreamNumLimitPerWsConn, helper.DeprecateAndNew),
		orderBooks:      make(map[string]*OrderBook),
	}
}

func (b *binance) close(ctx context.Context) error {
	return b.wsClientManager.Close(ctx)
}

func (b *binance) IsEnabled() bool {
	return b.cfg.Enabled
}

func (b *binance) GetName() string {
	return b.cfg.Name
}

func (b *binance) GetUniversalFuturesSymbols() []string {
	resBody, err := client.HttpGet(b.cfg.RestUrl + b.cfg.SymbolsUrlPath)
	if err != nil {
		logging.Error("Binance get futures symbols error", zap.Error(err))
		return nil
	}

	var res map[string]interface{}
	err = json.UnmarshalFromString(strutil.BytesToString(resBody), &res)
	if err != nil {
		logging.Error("Binance get futures symbols error", zap.Error(err))
		return nil
	}

	var symbols []string
	for _, d := range res["symbols"].([]interface{}) {
		m := d.(map[string]interface{})
		if strings.ToLower(m["status"].(string)) == "trading" &&
			strings.ToLower(m["contractType"].(string)) == "perpetual" &&
			strings.ToLower(m["quoteAsset"].(string)) == "usdt" {
			symbols = append(symbols, strings.ToLower(m["symbol"].(string)))
		}
	}

	logging.Info("GetUniversalFuturesSymbols", zap.Strings("binance", symbols))
	return symbols
}

func (b *binance) GetFuturesSymbol(universalSymbol string) string {
	return universalSymbol
}

// SubscribeToFuturesOrderBook depth5@0ms, bookTicker, trade
func (b *binance) SubscribeToFuturesOrderBook(universalSymbols []string, onUpdate func(book *OrderBook)) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	var streamNames []string
	channels := []string{bnChannelDepth5, bnChannelBBO, bnChannelTrades}
	for _, symbol := range universalSymbols {
		if _, ok := b.orderBooks[symbol]; !ok {
			b.orderBooks[symbol] = &OrderBook{
				Symbol: symbol,
			}
		}

		processWrappers := map[string]helper.ProcessWrapper{
			bnChannelDepth5: b.getOrderBookDepth5ProcessWrapper(symbol),
			bnChannelBBO:    b.getOrderBookBBOProcessWrapper(symbol),
			bnChannelTrades: b.getOrderBookTradesProcessWrapper(symbol),
		}

		for _, channel := range channels {
			streamName := getStreamName(b.GetFuturesSymbol(symbol), channel)
			ps, _ := b.processes.LoadOrStore(streamName, []helper.Process{})
			ps = append(ps.([]helper.Process), helper.NewChain(helper.RecoverFromPanic(), processWrappers[channel]).Then(func(interface{}) interface{} {
				onUpdate(b.orderBooks[symbol])
				return nil
			}))
			b.processes.Store(streamName, ps)

			streamNames = append(streamNames, getStreamName(symbol, channel))
		}
	}

	return b.subscribe(streamNames)
}

// UnsubscribeToFuturesOrderBook depth5@0ms, bookTicker, trade
func (b *binance) UnsubscribeToFuturesOrderBook(universalSymbols []string) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	var streamNames []string
	channels := []string{bnChannelDepth5, bnChannelBBO, bnChannelTrades}
	for _, symbol := range universalSymbols {
		for _, channel := range channels {
			streamNames = append(streamNames, getStreamName(symbol, channel))
		}
	}

	return b.unsubscribe(streamNames)
}

func (b *binance) GetFuturesPositions(universalSymbol string) ([]*FuturesPosition, error) {
	return nil, errors.New("not implemented")
}

func (b *binance) ClosePosition(universalSymbol string, mode MarginMode, posSide PositionSide) error {
	return errors.New("not implemented")
}

func (b *binance) PlaceFuturesOrder(order *FuturesOrder) error {
	return errors.New("not implemented")
}

func (b *binance) subscribe(streamNames []string) error {
	if len(streamNames) > 0 {
		return b.wsClientManager.AddStreams(streamNames, func(streamNames []string) *client.WebSocketClient {
			var futuresSymbols []string
			for _, streamName := range streamNames {
				futuresSymbols = append(futuresSymbols, getStreamName(b.GetFuturesSymbol(strutil.Before(streamName, "@")), strutil.After(streamName, "@")))
			}

			if len(futuresSymbols) > 1 {
				return client.NewWebSocketClient(fmt.Sprintf(b.cfg.CombinedStreamsUrl, slice.Join(futuresSymbols, "/")), nil, b.getWsMessageHandler())
			} else {
				return client.NewWebSocketClient(fmt.Sprintf(b.cfg.SingleStreamUrl, futuresSymbols[0]), nil, b.getWsMessageHandler())
			}
		}, nil)
	}

	return nil
}

func (b *binance) unsubscribe(streamNames []string) error {
	if len(streamNames) > 0 {
		return b.wsClientManager.RemoveStreams(streamNames, func(streamNames []string) *client.WebSocketClient {
			var futuresSymbols []string
			for _, streamName := range streamNames {
				futuresSymbols = append(futuresSymbols, getStreamName(b.GetFuturesSymbol(strutil.Before(streamName, "@")), strutil.After(streamName, "@")))
			}

			if len(futuresSymbols) > 1 {
				return client.NewWebSocketClient(fmt.Sprintf(b.cfg.CombinedStreamsUrl, slice.Join(futuresSymbols, "/")), nil, b.getWsMessageHandler())
			} else {
				return client.NewWebSocketClient(fmt.Sprintf(b.cfg.SingleStreamUrl, futuresSymbols[0]), nil, b.getWsMessageHandler())
			}
		}, nil)
	}

	return nil
}

func (b *binance) getWsMessageHandler() client.WsMessageHandler {
	return func(bt []byte) {
		res := newBnResponse()
		defer bnResponsePool.Put(res)

		_ = json.UnmarshalFromString(strutil.BytesToString(bt), res)

		if ps, ok := b.processes.Load(res.Stream); ok {
			for _, p := range ps.([]helper.Process) {
				p(bt)
			}
		}
	}
}

func (b *binance) getOrderBookDepth5ProcessWrapper(symbol string) helper.ProcessWrapper {
	return func(p helper.Process) helper.Process {
		return func(d interface{}) interface{} {
			depth := newBnDepth()
			defer bnDepthPool.Put(depth)

			_ = json.UnmarshalFromString(strutil.BytesToString(d.([]byte)), depth)

			if depth.Data.EventTime == 0 {
				logging.Info("Binance message data is nil", zap.ByteString("message", d.([]byte)))
				return nil
			}

			b.orderBooks[symbol].updateDepth(depth.Data.Asks, depth.Data.Bids, depth.Data.UpdateId, depth.Data.EventTime)

			return p(d)
		}
	}
}

func (b *binance) getOrderBookBBOProcessWrapper(symbol string) helper.ProcessWrapper {
	return func(p helper.Process) helper.Process {
		return func(d interface{}) interface{} {
			bbo := newBnBBO()
			defer bnBBOPool.Put(bbo)

			_ = json.UnmarshalFromString(strutil.BytesToString(d.([]byte)), bbo)

			if bbo.Data.EventTime == 0 {
				logging.Info("Binance message data is nil", zap.ByteString("message", d.([]byte)))
				return nil
			}

			b.orderBooks[symbol].updateBBO([][]string{{bbo.Data.AskPrice, bbo.Data.AskQty}}, [][]string{{bbo.Data.BidPrice, bbo.Data.BidQty}}, bbo.Data.UpdateId, bbo.Data.EventTime)

			return p(d)
		}
	}
}

func (b *binance) getOrderBookTradesProcessWrapper(symbol string) helper.ProcessWrapper {
	return func(p helper.Process) helper.Process {
		return func(d interface{}) interface{} {
			trade := newBnTrade()
			defer bnTradePool.Put(trade)

			_ = json.UnmarshalFromString(strutil.BytesToString(d.([]byte)), trade)

			if trade.Data.TradeTime == 0 {
				logging.Info("Binance message data is nil", zap.ByteString("message", d.([]byte)))
				return nil
			}

			b.orderBooks[symbol].updateTrade(&Trade{
				Symbol:       symbol,
				TradeId:      trade.Data.TradeId,
				TradeTime:    trade.Data.TradeTime,
				Price:        trade.Data.Price,
				Quantity:     trade.Data.Quantity,
				IsBuyerMaker: trade.Data.IsBuyerMaker,
			})

			return p(d)
		}
	}
}
