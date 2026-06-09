package ws

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	logpkg "nft-market-backend/internal/log"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMessageSize = 512
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

// Message is a JSON-structured WebSocket push.
type Message struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// Client represents a single WebSocket connection subscribed to one or more collections.
type Client struct {
	hub         *Hub
	conn        *websocket.Conn
	send        chan []byte
	collections map[string]bool
}

// readPump drains incoming messages (client → server). The v1 protocol is
// server-push only, so messages from the client are discarded.
func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()
	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})
	for {
		_, _, err := c.conn.ReadMessage()
		if err != nil {
			break
		}
	}
}

// writePump pumps messages from the hub to the client.
func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()
	for {
		select {
		case msg, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(msg)
			if err := w.Close(); err != nil {
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// Hub manages all WebSocket connections and broadcasts.
type Hub struct {
	clients       map[*Client]bool
	byCollection  map[string]map[*Client]bool
	register      chan *Client
	unregister    chan *Client
	mu            sync.RWMutex
}

// NewHub creates a new Hub.
func NewHub() *Hub {
	return &Hub{
		clients:      make(map[*Client]bool),
		byCollection: make(map[string]map[*Client]bool),
		register:     make(chan *Client),
		unregister:   make(chan *Client),
	}
}

// Run starts the hub's event loop. Must be called in a goroutine.
func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			for col := range client.collections {
				if h.byCollection[col] == nil {
					h.byCollection[col] = make(map[*Client]bool)
				}
				h.byCollection[col][client] = true
			}
			h.mu.Unlock()

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				for col := range client.collections {
					if clients, ok := h.byCollection[col]; ok {
						delete(clients, client)
						if len(clients) == 0 {
							delete(h.byCollection, col)
						}
					}
				}
				close(client.send)
			}
			h.mu.Unlock()
		}
	}
}

// Broadcast sends a message to all clients subscribed to a given collection.
func (h *Hub) Broadcast(collection string, msg Message) {
	data, err := json.Marshal(msg)
	if err != nil {
		logpkg.Logger.Error("ws: marshal broadcast failed", zap.Error(err))
		return
	}

	h.mu.RLock()
	clients := h.byCollection[collection]
	h.mu.RUnlock()

	for client := range clients {
		select {
		case client.send <- data:
		default:
			// Client's send buffer is full; drop the message and disconnect.
			go func(c *Client) {
				h.unregister <- c
				c.conn.Close()
			}(client)
		}
	}
}

// Upgrade upgrades an HTTP connection to WebSocket and registers the client
// for the given collections.
func (h *Hub) Upgrade(w http.ResponseWriter, r *http.Request, collections []string) error {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return err
	}

	colMap := make(map[string]bool, len(collections))
	for _, col := range collections {
		colMap[col] = true
	}

	client := &Client{
		hub:         h,
		conn:        conn,
		send:        make(chan []byte, 64),
		collections: colMap,
	}
	h.register <- client

	go client.writePump()
	go client.readPump()
	return nil
}
