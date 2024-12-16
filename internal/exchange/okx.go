package exchange

import (
	"context"
	"errors"
	"fmt"
	"github.com/duke-git/lancet/v2/cryptor"
	"github.com/duke-git/lancet/v2/strutil"
	"github.com/jiang2012/caffeine/internal/client"
	"github.com/jiang2012/caffeine/internal/config"
	"github.com/jiang2012/caffeine/internal/helper"
	"github.com/jiang2012/caffeine/pkg/json"
	"github.com/jiang2012/caffeine/pkg/logging"
	"go.uber.org/zap"
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	key = "!lhqZ1NgK^z!bk^r"

	okxChannelDepth5 = "books5"
	okxChannelBBO    = "bbo-tbt"
	okxChannelTrades = "trades-all"
)

type okx struct {
	cfg *config.Okx

	apiKey     string
	secretKey  string
	passPhrase string

	publicWsClientManager   *helper.WebSocketClientManager
	privateWsClientManager  *helper.WebSocketClientManager
	businessWsClientManager *helper.WebSocketClientManager

	orderBooks map[string]*OrderBook

	// "symbol@channel" -> []process
	processes sync.Map

	mu sync.Mutex
}

type okxResponse struct {
	Arg struct {
		InstId  string `json:"instId"`
		Channel string `json:"channel"`
	} `json:"arg"`
}

type okxDepth struct {
	Data []*struct {
		Asks  [][]string `json:"asks"`
		Bids  [][]string `json:"bids"`
		Time  int64      `json:"ts,string"`
		SeqId int64      `json:"seqId"`
	} `json:"data"`
}

type okxTrade struct {
	Data []*struct {
		TradeId   int64  `json:"tradeId,string"`
		TradeTime int64  `json:"ts,string"`
		Price     string `json:"px"`
		Quantity  string `json:"sz"`
		Side      string `json:"side"`
	} `json:"data"`
}

type okxPosition struct {
	Symbol                     string       `json:"instId"`
	Mode                       MarginMode   `json:"mgnMode"`
	PosSide                    PositionSide `json:"posSide"`
	Leverage                   int          `json:"lever,string,omitempty"`
	Quantity                   float64      `json:"pos,string,omitempty"`
	EntryPrice                 float64      `json:"avgPx,string,omitempty"`
	MarkPrice                  float64      `json:"markPx,string,omitempty"`
	LiquidationPrice           float64      `json:"liqPx,string,omitempty"`
	UnrealizedPnl              float64      `json:"upl,string,omitempty"`
	RealizedPnl                float64      `json:"realizedPnl,string,omitempty"`
	HasTakeProfitStopLossOrder bool         `json:"autoCxl,omitempty"`
}

type okxFuturesLeverage struct {
	Symbol   string       `json:"instId"`
	Leverage int          `json:"lever,string"`
	Mode     MarginMode   `json:"mgnMode"`
	PosSide  PositionSide `json:"posSide,omitempty"`
}

type okxFuturesOrder struct {
	OrderId                string       `json:"ordId,omitempty"`
	Symbol                 string       `json:"instId"`
	Mode                   MarginMode   `json:"tdMode"`
	Type                   OrderType    `json:"ordType"`
	Side                   OrderSide    `json:"side"`
	PosSide                PositionSide `json:"posSide"`
	Price                  float64      `json:"px,string,omitempty"`
	Quantity               float64      `json:"sz,string,omitempty"`
	CloseFraction          float64      `json:"closeFraction,string,omitempty"`
	TakeProfitTriggerPrice float64      `json:"tpTriggerPx,string,omitempty"`
	TakeProfitPrice        float64      `json:"tpOrdPx,string,omitempty"`
	StopLossTriggerPrice   float64      `json:"slTriggerPx,string,omitempty"`
	StopLossPrice          float64      `json:"slOrdPx,string,omitempty"`
}

var okxResponsePool sync.Pool
var okxDepthPool sync.Pool
var okxTradePool sync.Pool

func newOkxResponse() *okxResponse {
	if v := okxResponsePool.Get(); v != nil {
		res := v.(*okxResponse)
		res.reset()
		return res
	}
	return &okxResponse{}
}

func (r *okxResponse) reset() {
	r.Arg.InstId = ""
	r.Arg.Channel = ""
}

func newOkxDepth() *okxDepth {
	if v := okxDepthPool.Get(); v != nil {
		d := v.(*okxDepth)
		d.reset()
		return d
	}
	return &okxDepth{}
}

func (d *okxDepth) reset() {
	for _, data := range d.Data {
		for _, ask := range data.Asks {
			ask[0] = ""
			ask[1] = ""
		}

		for _, bid := range data.Bids {
			bid[0] = ""
			bid[1] = ""
		}

		data.Time = 0
		data.SeqId = 0
	}
}

func newOkxTrade() *okxTrade {
	if v := okxTradePool.Get(); v != nil {
		t := v.(*okxTrade)
		t.reset()
		return t
	}
	return &okxTrade{}
}

func (t *okxTrade) reset() {
	for _, data := range t.Data {
		data.TradeId = 0
		data.TradeTime = 0
		data.Price = ""
		data.Quantity = ""
		data.Side = ""
	}
}

func newOkx(cfg *config.Config) *okx {
	return &okx{
		cfg:                     cfg.GetOkx(),
		apiKey:                  strutil.BytesToString(decode(cfg.GetOkx().ApiKey, strutil.StringToBytes(key))),
		secretKey:               strutil.BytesToString(decode(cfg.GetOkx().SecretKey, strutil.StringToBytes(key))),
		passPhrase:              strutil.BytesToString(decode(cfg.GetOkx().PassPhrase, strutil.StringToBytes(key))),
		publicWsClientManager:   helper.NewWebSocketClientManager(cfg.StreamNumLimitPerWsConn, helper.Reuse),
		privateWsClientManager:  helper.NewWebSocketClientManager(cfg.StreamNumLimitPerWsConn, helper.Reuse),
		businessWsClientManager: helper.NewWebSocketClientManager(cfg.StreamNumLimitPerWsConn, helper.Reuse),
		orderBooks:              make(map[string]*OrderBook),
	}
}

func (o *okx) close(ctx context.Context) error {
	var err error
	for _, m := range []*helper.WebSocketClientManager{o.publicWsClientManager, o.privateWsClientManager, o.businessWsClientManager} {
		err = errors.Join(err, m.Close(ctx))
	}

	return err
}

func (o *okx) IsEnabled() bool {
	return o.cfg.Enabled
}

func (o *okx) GetName() string {
	return o.cfg.Name
}

func (o *okx) GetUniversalFuturesSymbols() []string {
	res, err := o.doGetRequest(o.cfg.SymbolsUrlPath)
	if err != nil {
		logging.Error("OKX get futures symbols error", zap.Error(err))
		return nil
	}

	var symbols []string
	for _, d := range res {
		m := d.(map[string]interface{})
		if strings.ToLower(m["state"].(string)) == "live" && strings.ToLower(m["settleCcy"].(string)) == "usdt" {
			symbols = append(symbols, strings.ReplaceAll(strings.ToLower(m["instId"].(string)), "-usdt-swap", "usdt"))
		}
	}

	logging.Info("GetUniversalFuturesSymbols", zap.Strings("okx", symbols))
	return symbols
}

func (o *okx) GetFuturesSymbol(universalSymbol string) string {
	return strings.ToUpper(strings.ReplaceAll(universalSymbol, "usdt", "-usdt-swap"))
}

// SubscribeToFuturesOrderBook books5, bbo-tbt, trades-all
func (o *okx) SubscribeToFuturesOrderBook(universalSymbols []string, onUpdate func(book *OrderBook)) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	channels := []string{okxChannelDepth5, okxChannelBBO, okxChannelTrades}
	for _, symbol := range universalSymbols {
		if _, ok := o.orderBooks[symbol]; !ok {
			o.orderBooks[symbol] = &OrderBook{
				Symbol: symbol,
			}
		}

		processWrappers := map[string]helper.ProcessWrapper{
			okxChannelDepth5: o.getOrderBookDepth5ProcessWrapper(symbol),
			okxChannelBBO:    o.getOrderBookBBOProcessWrapper(symbol),
			okxChannelTrades: o.getOrderBookTradesProcessWrapper(symbol),
		}

		for _, channel := range channels {
			streamName := getStreamName(o.GetFuturesSymbol(symbol), channel)
			ps, _ := o.processes.LoadOrStore(streamName, []helper.Process{})
			ps = append(ps.([]helper.Process), helper.NewChain(helper.RecoverFromPanic(), processWrappers[channel]).Then(func(interface{}) interface{} {
				onUpdate(o.orderBooks[symbol])
				return nil
			}))
			o.processes.Store(streamName, ps)

			handler := func(c *client.WebSocketClient) error {
				if d, err := json.Marshal(map[string]interface{}{
					"op":   "subscribe",
					"args": []map[string]string{{"channel": channel, "instId": o.GetFuturesSymbol(symbol)}},
				}); err != nil {
					return err
				} else {
					c.Send() <- d
					return nil
				}
			}

			if err := o.subscribe(symbol, channel, handler); err != nil {
				return err
			}
		}
	}

	return nil
}

// UnsubscribeToFuturesOrderBook books5, bbo-tbt, trades-all
func (o *okx) UnsubscribeToFuturesOrderBook(universalSymbols []string) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	channels := []string{okxChannelDepth5, okxChannelBBO, okxChannelTrades}
	for _, symbol := range universalSymbols {
		for _, channel := range channels {
			handler := func(wsClient *client.WebSocketClient, streamName string) error {
				if d, err := json.Marshal(map[string]interface{}{
					"op":   "unsubscribe",
					"args": []map[string]string{{"channel": channel, "instId": o.GetFuturesSymbol(symbol)}},
				}); err != nil {
					return err
				} else {
					wsClient.Send() <- d
					return nil
				}
			}

			if err := o.unsubscribe(symbol, channel, handler); err != nil {
				return err
			}
		}
	}

	return nil
}

func (o *okx) GetFuturesPositions(universalSymbol string) ([]*FuturesPosition, error) {
	symbol := o.GetFuturesSymbol(universalSymbol)

	res, err := o.doGetRequest(o.cfg.GetPositionsUrlPath, "instType", "SWAP", "instId", symbol)
	if err != nil {
		logging.Error("OKX get futures positions error", zap.String("symbol", symbol), zap.Error(err))
		return nil, err
	}

	var positions []*FuturesPosition
	for _, p := range res {
		d, err := json.Marshal(p)
		if err != nil {
			continue
		}

		var position okxPosition
		err = json.UnmarshalFromString(strutil.BytesToString(d), &position)
		if err != nil {
			continue
		}

		positions = append(positions, &FuturesPosition{
			Symbol:           universalSymbol,
			Mode:             position.Mode,
			PosSide:          position.PosSide,
			Leverage:         position.Leverage,
			Quantity:         position.Quantity,
			EntryPrice:       position.EntryPrice,
			MarkPrice:        position.MarkPrice,
			LiquidationPrice: position.LiquidationPrice,
			UnrealizedPnl:    position.UnrealizedPnl,
			RealizedPnl:      position.RealizedPnl,
		})
	}

	return positions, nil
}

func (o *okx) ClosePosition(universalSymbol string, mode MarginMode, posSide PositionSide) error {
	symbol := o.GetFuturesSymbol(universalSymbol)

	position := &okxPosition{
		Symbol:                     symbol,
		Mode:                       mode,
		PosSide:                    posSide,
		HasTakeProfitStopLossOrder: true,
	}

	_, err := o.doPostRequest(position, o.cfg.ClosePositionUrlPath, "closePosition")
	if err != nil {
		logging.Error("OKX close position error", zap.String("symbol", symbol), zap.Error(err))
	}
	return err
}

func (o *okx) PlaceFuturesOrder(order *FuturesOrder) error {
	// 1. 设置杠杆
	if err := o.setFuturesLeverage(order.Symbol, order.Leverage, order.Mode, order.PosSide); err != nil {
		return err
	}

	// 2. 下单
	symbol := o.GetFuturesSymbol(order.Symbol)
	okxOrder := &okxFuturesOrder{
		Symbol:   symbol,
		Mode:     order.Mode,
		Type:     order.Type,
		Side:     order.Side,
		PosSide:  order.PosSide,
		Price:    order.Price,
		Quantity: order.Quantity,
	}

	res, err := o.doPostRequest(okxOrder, o.cfg.PlaceOrderUrlPath, "placeFuturesOrder")
	if err != nil {
		logging.Error("placeFuturesOrder", zap.String("symbol", symbol), zap.Error(err))
		return err
	}

	okxOrder.OrderId = res["data"].([]interface{})[0].(map[string]interface{})["ordId"].(string)
	okxOrder.TakeProfitTriggerPrice = order.TakeProfitPrice
	okxOrder.StopLossTriggerPrice = order.StopLossPrice

	logging.Info("placeFuturesTPSLOrder...", zap.String("symbol", symbol))
	go func(okxOrder *okxFuturesOrder) {
		for {
			// 3. 等待全部成交
			res, err := o.getOrderInfo(okxOrder.Symbol, okxOrder.OrderId)
			if err == nil {
				orderState := res["state"].(string)
				if orderState == "filled" {
					// 4. 下止盈止损单
					okxTPSLOrder := &okxFuturesOrder{
						Symbol: okxOrder.Symbol,
						Mode:   okxOrder.Mode,
						Type:   OrderType("oco"),
						Side: func(side OrderSide) OrderSide {
							if side == Buy {
								return Sell
							}
							return Buy
						}(okxOrder.Side),
						PosSide:                okxOrder.PosSide,
						CloseFraction:          1,
						TakeProfitTriggerPrice: okxOrder.TakeProfitTriggerPrice,
						TakeProfitPrice:        -1,
						StopLossTriggerPrice:   okxOrder.StopLossTriggerPrice,
						StopLossPrice:          -1,
					}

					if _, err := o.doPostRequest(okxTPSLOrder, o.cfg.PlaceTakeProfitStopLossOrderUrlPath, "placeFuturesTPSLOrder"); err != nil {
						logging.Error("placeFuturesTPSLOrder", zap.String("symbol", okxOrder.Symbol), zap.Error(err))
					} else {
						logging.Info("placeFuturesTPSLOrder success", zap.String("symbol", okxOrder.Symbol))
						break
					}
				}
			}

			time.Sleep(time.Second)
		}
	}(okxOrder)

	return nil
}

func (o *okx) getOrderInfo(symbol, orderId string) (map[string]interface{}, error) {
	res, err := o.doGetRequest(o.cfg.GetOrderInfoUrlPath, "instId", symbol, "ordId", orderId)
	if err != nil {
		logging.Error("getOrderInfo", zap.String("symbol", symbol), zap.Error(err))
		return nil, err
	}

	return res[0].(map[string]interface{}), nil
}

func (o *okx) setFuturesLeverage(universalSymbol string, leverage int, mode MarginMode, posSide PositionSide) error {
	symbol := o.GetFuturesSymbol(universalSymbol)

	okxLeverage := &okxFuturesLeverage{
		Symbol:   symbol,
		Leverage: leverage,
		Mode:     mode,
	}
	if mode == Isolated {
		okxLeverage.PosSide = posSide
	}

	_, err := o.doPostRequest(okxLeverage, o.cfg.SetLeverageUrlPath, "setFuturesLeverage")
	if err != nil {
		logging.Error("setFuturesLeverage", zap.String("symbol", symbol), zap.Error(err))
	}
	return err
}

func (o *okx) doGetRequest(urlPath string, paramKeysAndValues ...string) ([]interface{}, error) {
	if len(paramKeysAndValues)%2 != 0 {
		return nil, errors.New("invalid param keys and values")
	}

	var params []string
	for i := 0; i < len(paramKeysAndValues); i += 2 {
		params = append(params, strings.Join([]string{paramKeysAndValues[i], paramKeysAndValues[i+1]}, "="))
	}

	if len(params) > 0 {
		urlPath = strings.Join([]string{urlPath, strings.Join(params, "&")}, "?")
	}

	resBody, err := client.HttpGet(o.cfg.RestUrl+urlPath, o.buildAuthHeaders(client.MethodGet, urlPath, "")...)
	if err != nil {
		return nil, err
	}

	var res map[string]interface{}
	err = json.UnmarshalFromString(strutil.BytesToString(resBody), &res)
	if err != nil {
		return nil, err
	}

	if res["code"].(string) != "0" {
		return nil, fmt.Errorf("OKX get failed, response: %+v", res)
	}

	return res["data"].([]interface{}), nil
}

func (o *okx) doPostRequest(body interface{}, urlPath, reqDesc string) (map[string]interface{}, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	res, err := client.HttpPostJson(o.cfg.RestUrl+urlPath, b, o.buildAuthHeaders(client.MethodPost, urlPath, strutil.BytesToString(b))...)
	logging.Info("OKX post", zap.String("request", reqDesc), zap.ByteString("body", b))
	if err != nil {
		return nil, err
	}

	if res["code"].(string) != "0" {
		return nil, fmt.Errorf("OKX post failed, response: %+v", res)
	}

	return res, nil
}

func (o *okx) buildAuthHeaders(method, urlPath, body string) []string {
	utc := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	sign := cryptor.HmacSha256WithBase64(strings.Join([]string{utc, method, urlPath, body}, ""), o.secretKey)

	return []string{
		"OK-ACCESS-KEY", o.apiKey,
		"OK-ACCESS-SIGN", sign,
		"OK-ACCESS-TIMESTAMP", utc,
		"OK-ACCESS-PASSPHRASE", o.passPhrase,
	}
}

func (o *okx) subscribe(symbol, channel string, connectedHandler helper.WsClientConnectedHandler) error {
	streamName := getStreamName(symbol, channel)

	switch {
	case slices.Contains(o.cfg.PublicStreamChannels, channel):
		return o.publicWsClientManager.AddStream(streamName, func(streamNames []string) *client.WebSocketClient {
			return client.NewWebSocketClient(o.cfg.PublicStreamUrl, nil, o.getWsMessageHandler())
		}, connectedHandler)
	case slices.Contains(o.cfg.PrivateStreamChannels, channel):
		return o.privateWsClientManager.AddStream(streamName, func(streamNames []string) *client.WebSocketClient {
			return client.NewWebSocketClient(o.cfg.PrivateStreamUrl, nil, o.getWsMessageHandler())
		}, connectedHandler)
	case slices.Contains(o.cfg.BusinessStreamChannels, channel):
		return o.businessWsClientManager.AddStream(streamName, func(streamNames []string) *client.WebSocketClient {
			return client.NewWebSocketClient(o.cfg.BusinessStreamUrl, nil, o.getWsMessageHandler())
		}, connectedHandler)
	default:
		return fmt.Errorf("can not determine public/private/business channel: %s", channel)
	}
}

func (o *okx) unsubscribe(symbol, channel string, removedHandler helper.WsClientStreamRemovedHandler) error {
	streamName := getStreamName(symbol, channel)

	switch {
	case slices.Contains(o.cfg.PublicStreamChannels, channel):
		return o.publicWsClientManager.RemoveStream(streamName, func(streamNames []string) *client.WebSocketClient {
			return client.NewWebSocketClient(o.cfg.PublicStreamUrl, nil, o.getWsMessageHandler())
		}, removedHandler)
	case slices.Contains(o.cfg.PrivateStreamChannels, channel):
		return o.privateWsClientManager.RemoveStream(streamName, func(streamNames []string) *client.WebSocketClient {
			return client.NewWebSocketClient(o.cfg.PrivateStreamUrl, nil, o.getWsMessageHandler())
		}, removedHandler)
	case slices.Contains(o.cfg.BusinessStreamChannels, channel):
		return o.businessWsClientManager.RemoveStream(streamName, func(streamNames []string) *client.WebSocketClient {
			return client.NewWebSocketClient(o.cfg.BusinessStreamUrl, nil, o.getWsMessageHandler())
		}, removedHandler)
	default:
		return fmt.Errorf("can not determine public/private/business channel: %s", channel)
	}
}

func (o *okx) getWsMessageHandler() client.WsMessageHandler {
	return func(b []byte) {
		res := newOkxResponse()
		defer okxResponsePool.Put(res)

		_ = json.UnmarshalFromString(strutil.BytesToString(b), res)

		if ps, ok := o.processes.Load(getStreamName(res.Arg.InstId, res.Arg.Channel)); ok {
			for _, p := range ps.([]helper.Process) {
				p(b)
			}
		}
	}
}

func (o *okx) getOrderBookDepth5ProcessWrapper(symbol string) helper.ProcessWrapper {
	return func(p helper.Process) helper.Process {
		return func(d interface{}) interface{} {
			depth := newOkxDepth()
			defer okxDepthPool.Put(depth)

			_ = json.UnmarshalFromString(strutil.BytesToString(d.([]byte)), depth)

			if len(depth.Data) == 0 || depth.Data[0].Time == 0 {
				logging.Info("OKX message data is nil", zap.ByteString("message", d.([]byte)))
				return nil
			}

			o.orderBooks[symbol].updateDepth(depth.Data[0].Asks, depth.Data[0].Bids, depth.Data[0].SeqId, depth.Data[0].Time)

			return p(d)
		}
	}
}

func (o *okx) getOrderBookBBOProcessWrapper(symbol string) helper.ProcessWrapper {
	return func(p helper.Process) helper.Process {
		return func(d interface{}) interface{} {
			depth := newOkxDepth()
			defer okxDepthPool.Put(depth)

			_ = json.UnmarshalFromString(strutil.BytesToString(d.([]byte)), depth)

			if len(depth.Data) == 0 || depth.Data[0].Time == 0 {
				logging.Info("OKX message data is nil", zap.ByteString("message", d.([]byte)))
				return nil
			}

			o.orderBooks[symbol].updateBBO(depth.Data[0].Asks, depth.Data[0].Bids, depth.Data[0].SeqId, depth.Data[0].Time)

			return p(d)
		}
	}
}

func (o *okx) getOrderBookTradesProcessWrapper(symbol string) helper.ProcessWrapper {
	return func(p helper.Process) helper.Process {
		return func(d interface{}) interface{} {
			trade := newOkxTrade()
			defer okxTradePool.Put(trade)

			_ = json.UnmarshalFromString(strutil.BytesToString(d.([]byte)), trade)

			if len(trade.Data) == 0 || trade.Data[0].TradeTime == 0 {
				logging.Info("OKX message data is nil", zap.ByteString("message", d.([]byte)))
				return nil
			}

			o.orderBooks[symbol].updateTrade(&Trade{
				Symbol:    symbol,
				TradeId:   trade.Data[0].TradeId,
				TradeTime: trade.Data[0].TradeTime,
				Price:     trade.Data[0].Price,
				Quantity:  trade.Data[0].Quantity,
				IsBuyerMaker: func(side string) bool {
					return side == "sell"
				}(trade.Data[0].Side),
			})

			return p(d)
		}
	}
}
