package client

import (
	"context"
	"github.com/gorilla/websocket"
	"github.com/jiang2012/caffeine/pkg/logging"
	"go.uber.org/zap"
	"net/http"
	"sync"
	"time"
)

/*
Concurrency:
Connections support one concurrent reader and one concurrent writer.

Applications are responsible for ensuring that
no more than one goroutine calls the write methods (NextWriter, SetWriteDeadline, WriteMessage, WriteJSON, EnableWriteCompression, SetCompressionLevel) concurrently
and that
no more than one goroutine calls the read methods (NextReader, SetReadDeadline, ReadMessage, ReadJSON, SetPongHandler, SetPingHandler) concurrently.

The Close and WriteControl methods can be called concurrently with all other methods.
*/

const (
	// Time allowed to write a message to the peer.
	defaultWriteWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer.
	defaultPongWait = 60 * time.Second

	// Send pings to peer with this period. Must be less than pongWait.
	defaultPingPeriod = (defaultPongWait * 9) / 10
)

type WsMessageHandler func(b []byte)

type WebSocketClient struct {
	url           string
	requestHeader http.Header
	msgHandler    WsMessageHandler

	conn       *websocket.Conn
	writeWait  time.Duration
	pongWait   time.Duration
	pingPeriod time.Duration

	send chan []byte

	writeLoopWaiter sync.WaitGroup
	connected       chan struct{}
	reconnected     chan struct{}
	close           chan struct{}
	closed          chan struct{}
}

func NewWebSocketClient(url string, requestHeader http.Header, msgHandler WsMessageHandler) *WebSocketClient {
	return &WebSocketClient{
		url:           url,
		requestHeader: requestHeader,
		msgHandler:    msgHandler,
		writeWait:     defaultWriteWait,
		pongWait:      defaultPongWait,
		pingPeriod:    defaultPingPeriod,
		send:          make(chan []byte),
		connected:     make(chan struct{}, 1),
		reconnected:   make(chan struct{}, 1),
		close:         make(chan struct{}, 1),
		closed:        make(chan struct{}, 1),
	}
}

// Connect see https://github.com/gorilla/websocket/issues/500
func (c *WebSocketClient) Connect() {
	go func() {
		for {
			logging.Info("WebSocket connecting...", zap.String("url", c.url))

			if err := c.connect(); err != nil {
				logging.Error("WebSocket connect error", zap.String("url", c.url), zap.Error(err))
				time.Sleep(5 * time.Second)
				continue
			}

			select {
			case <-c.close:
				c.closed <- struct{}{}
				return
			case c.connected <- struct{}{}:
			default:
			}

			logging.Info("WebSocket connected", zap.String("url", c.url))

			done := make(chan struct{})

			c.writeLoopWaiter.Add(1)
			go c.writeLoop(done)
			c.readLoop(done)
			c.writeLoopWaiter.Wait()

			select {
			case <-c.close:
				c.closed <- struct{}{}
				return

			default:
				select {
				case c.reconnected <- struct{}{}:
				default:
				}

				// read deadline, reconnect
				logging.Info("WebSocket prepare for reconnecting", zap.String("url", c.url))
				time.Sleep(5 * time.Second)
			}
		}
	}()

	<-c.connected
}

func (c *WebSocketClient) Close(ctx context.Context) error {
	c.close <- struct{}{}

	if c.conn != nil {
		_ = c.conn.Close()
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.closed:
		return nil
	}
}

func (c *WebSocketClient) Reconnected() <-chan struct{} {
	return c.reconnected
}

func (c *WebSocketClient) Send() chan<- []byte {
	return c.send
}

func (c *WebSocketClient) connect() error {
	var err error
	c.conn, _, err = websocket.DefaultDialer.Dial(c.url, c.requestHeader)
	return err
}

func (c *WebSocketClient) writeLoop(done chan struct{}) {
	defer c.writeLoopWaiter.Done()

	defer func(conn *websocket.Conn) {
		_ = conn.Close()
	}(c.conn)

	ticker := time.NewTicker(c.pingPeriod)
	defer ticker.Stop()

	defer logging.Info("WebSocket write loop exited")

	for {
		select {
		case <-done:
			return

		case s := <-c.send:
			logging.Info("WebSocket write", zap.ByteString("data", s))
			if err := c.conn.SetWriteDeadline(time.Now().Add(c.writeWait)); err != nil {
				logging.Error("WebSocket set write deadline error", zap.Error(err))
				return
			}

			if err := c.conn.WriteMessage(websocket.TextMessage, s); err != nil {
				logging.Error("WebSocket write error", zap.Error(err))
				return
			}

		case <-ticker.C:
			logging.Debug("WebSocket ping")
			if err := c.conn.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(c.writeWait)); err != nil {
				logging.Error("WebSocket ping error", zap.Error(err))
				return
			}
		}
	}
}

func (c *WebSocketClient) readLoop(done chan struct{}) {
	defer close(done)

	defer func(conn *websocket.Conn) {
		_ = conn.Close()
	}(c.conn)

	defer logging.Info("WebSocket read loop exited")

	if err := c.conn.SetReadDeadline(time.Now().Add(c.pongWait)); err != nil {
		logging.Error("WebSocket set read deadline error", zap.Error(err))
		return
	}

	c.conn.SetPongHandler(func(string) error {
		if err := c.conn.SetReadDeadline(time.Now().Add(c.pongWait)); err != nil {
			logging.Error("WebSocket set read deadline error", zap.Error(err))
			return err
		}

		logging.Debug("WebSocket pong received")
		return nil
	})

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			logging.Error("WebSocket read error", zap.Error(err))
			return
		}

		c.msgHandler(message)
	}
}
