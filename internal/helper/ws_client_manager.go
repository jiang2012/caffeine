package helper

import (
	"context"
	"errors"
	"github.com/duke-git/lancet/v2/slice"
	"github.com/jiang2012/caffeine/internal/client"
	"github.com/jiang2012/caffeine/pkg/logging"
	"go.uber.org/zap"
	"sync"
	"time"
)

type NewWsClientForStreams func(streamNames []string) *client.WebSocketClient
type WsClientConnectedHandler func(*client.WebSocketClient) error
type WsClientStreamRemovedHandler func(wsClient *client.WebSocketClient, streamName string) error

// ReusePolicy 当对client添加或删除流时，直接复用 or 关闭重建
type ReusePolicy int

const (
	Reuse ReusePolicy = iota
	DeprecateAndNew
)

type WebSocketClientManager struct {
	maxStreamNumPerClient int
	reusePolicy           ReusePolicy

	streamName2ClientMap map[string]*client.WebSocketClient
	client2ParamMap      map[*client.WebSocketClient]*webSocketClientParam

	close  chan struct{}
	closed chan struct{}

	mu sync.Mutex
}

type webSocketClientParam struct {
	streamNames                    []string
	streamName2ConnectedHandlerMap map[string]WsClientConnectedHandler
}

func NewWebSocketClientManager(maxStreamNumPerClient int, reusePolicy ReusePolicy) *WebSocketClientManager {
	m := &WebSocketClientManager{
		maxStreamNumPerClient: maxStreamNumPerClient,
		reusePolicy:           reusePolicy,
		streamName2ClientMap:  make(map[string]*client.WebSocketClient),
		client2ParamMap:       make(map[*client.WebSocketClient]*webSocketClientParam),
		close:                 make(chan struct{}, 1),
		closed:                make(chan struct{}, 1),
	}

	go m.listenToWebSocketClient()
	return m
}

func (m *WebSocketClientManager) Close(ctx context.Context) error {
	m.close <- struct{}{}
	<-m.closed

	m.mu.Lock()
	defer m.mu.Unlock()

	var err error
	for c, _ := range m.client2ParamMap {
		err = errors.Join(err, c.Close(ctx))
	}

	return err
}

func (m *WebSocketClientManager) AddStream(streamName string, newWsClient NewWsClientForStreams, connectedHandler WsClientConnectedHandler) error {
	return m.AddStreams([]string{streamName}, newWsClient, map[string]WsClientConnectedHandler{streamName: connectedHandler})
}

// AddStreams 若该streamName已存在，则什么都不做；否则优先选择未达流数量上限的client，若都已达上限则新建一个client
func (m *WebSocketClientManager) AddStreams(streamNames []string, newWsClient NewWsClientForStreams, connectedHandlers map[string]WsClientConnectedHandler) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var newStreamNames []string
	for _, streamName := range streamNames {
		if _, ok := m.streamName2ClientMap[streamName]; !ok {
			newStreamNames = append(newStreamNames, streamName)
		}
	}

	tempClient2StreamNames := make(map[*client.WebSocketClient][]string)
	remainStartIndex := 0
	remainEndIndex := len(newStreamNames)
	for c, p := range m.client2ParamMap {
		if remainStartIndex >= remainEndIndex {
			break
		}

		if len(p.streamNames) >= m.maxStreamNumPerClient {
			continue
		}

		if m.maxStreamNumPerClient-len(p.streamNames) >= remainEndIndex-remainStartIndex {
			tempClient2StreamNames[c] = newStreamNames[remainStartIndex:remainEndIndex]
			remainStartIndex = remainEndIndex
		} else {
			endIndex := remainStartIndex + m.maxStreamNumPerClient - len(p.streamNames)
			tempClient2StreamNames[c] = newStreamNames[remainStartIndex:endIndex]
			remainStartIndex = endIndex
		}
	}

	for c, s := range tempClient2StreamNames {
		newC := c
		if m.reusePolicy == DeprecateAndNew {
			if err := c.Close(context.Background()); err != nil {
				return err
			}
			newC = newWsClient(append(m.client2ParamMap[c].streamNames, s...))
			newC.Connect()

			for _, h := range m.client2ParamMap[c].streamName2ConnectedHandlerMap {
				if err := h(newC); err != nil {
					return err
				}
			}

			m.client2ParamMap[newC] = m.client2ParamMap[c]
			delete(m.client2ParamMap, c)
			for _, sn := range m.client2ParamMap[newC].streamNames {
				m.streamName2ClientMap[sn] = newC
			}
		}

		for _, streamName := range s {
			if h, ok := connectedHandlers[streamName]; ok && h != nil {
				if err := h(newC); err != nil {
					return err
				}

				m.client2ParamMap[newC].streamName2ConnectedHandlerMap[streamName] = h
			}

			m.client2ParamMap[newC].streamNames = append(m.client2ParamMap[newC].streamNames, streamName)
			m.streamName2ClientMap[streamName] = newC
		}
	}

	for {
		if remainStartIndex >= remainEndIndex {
			break
		}

		var tempStreamNames []string
		if m.maxStreamNumPerClient >= remainEndIndex-remainStartIndex {
			tempStreamNames = newStreamNames[remainStartIndex:remainEndIndex]
			remainStartIndex = remainEndIndex
		} else {
			endIndex := remainStartIndex + m.maxStreamNumPerClient
			tempStreamNames = newStreamNames[remainStartIndex:endIndex]
			remainStartIndex = endIndex
		}

		newC := newWsClient(tempStreamNames)
		newC.Connect()

		m.client2ParamMap[newC] = &webSocketClientParam{
			streamName2ConnectedHandlerMap: make(map[string]WsClientConnectedHandler),
		}

		for _, streamName := range tempStreamNames {
			if h, ok := connectedHandlers[streamName]; ok && h != nil {
				if err := h(newC); err != nil {
					return err
				}

				m.client2ParamMap[newC].streamName2ConnectedHandlerMap[streamName] = h
			}

			m.client2ParamMap[newC].streamNames = append(m.client2ParamMap[newC].streamNames, streamName)
			m.streamName2ClientMap[streamName] = newC
		}
	}

	return nil
}

func (m *WebSocketClientManager) RemoveStream(streamName string, newWsClient NewWsClientForStreams, streamRemovedHandler WsClientStreamRemovedHandler) error {
	return m.RemoveStreams([]string{streamName}, newWsClient, map[string]WsClientStreamRemovedHandler{streamName: streamRemovedHandler})
}

func (m *WebSocketClientManager) RemoveStreams(streamNames []string, newWsClient NewWsClientForStreams, streamRemovedHandlers map[string]WsClientStreamRemovedHandler) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	client2StreamNamesMap := make(map[*client.WebSocketClient][]string)
	for _, streamName := range streamNames {
		if c, ok := m.streamName2ClientMap[streamName]; ok {
			client2StreamNamesMap[c] = append(client2StreamNamesMap[c], streamName)
		}
	}

	for c, s := range client2StreamNamesMap {
		for _, streamName := range s {
			if h, ok := streamRemovedHandlers[streamName]; ok && h != nil {
				if err := h(c, streamName); err != nil {
					return err
				}
			}

			delete(m.streamName2ClientMap, streamName)

			idx := slice.IndexOf(m.client2ParamMap[c].streamNames, streamName)
			if idx >= 0 {
				m.client2ParamMap[c].streamNames = slice.DeleteAt(m.client2ParamMap[c].streamNames, idx)
			}

			if _, ok := m.client2ParamMap[c].streamName2ConnectedHandlerMap[streamName]; ok {
				delete(m.client2ParamMap[c].streamName2ConnectedHandlerMap, streamName)
			}
		}

		if len(m.client2ParamMap[c].streamNames) == 0 {
			if err := c.Close(context.Background()); err != nil {
				return err
			}
			delete(m.client2ParamMap, c)
		} else if m.reusePolicy == DeprecateAndNew {
			if err := c.Close(context.Background()); err != nil {
				return err
			}
			newC := newWsClient(m.client2ParamMap[c].streamNames)
			newC.Connect()

			for _, h := range m.client2ParamMap[c].streamName2ConnectedHandlerMap {
				if err := h(newC); err != nil {
					return err
				}
			}

			m.client2ParamMap[newC] = m.client2ParamMap[c]
			delete(m.client2ParamMap, c)
			for _, sn := range m.client2ParamMap[newC].streamNames {
				m.streamName2ClientMap[sn] = newC
			}
		}
	}

	return nil
}

func (m *WebSocketClientManager) listenToWebSocketClient() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-m.close:
			m.closed <- struct{}{}
			return

		case <-ticker.C:
			func() {
				m.mu.Lock()
				defer m.mu.Unlock()

				for c, p := range m.client2ParamMap {
					select {
					case <-c.Reconnected():
						for _, h := range p.streamName2ConnectedHandlerMap {
							if err := h(c); err != nil {
								logging.Error("Reconnected handler error", zap.Error(err))
								return
							}
						}
					default:
					}
				}
			}()
		}
	}
}
