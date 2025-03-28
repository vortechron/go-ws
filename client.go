package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Client represents a single WebSocket connection.
type Client struct {
	hub         *Hub
	conn        *websocket.Conn
	send        chan []byte
	UserID      string
	Metadata    map[string]interface{}
	CreatedAt   time.Time
	LastSeen    time.Time
	isClosing   bool
	mu          sync.RWMutex
	authHandler AuthorizationHandler
}

// PresenceMessage is how we'll communicate presence joins/leaves.
type PresenceMessage struct {
	Event     string                 `json:"event"`
	UserID    string                 `json:"user_id"`
	Timestamp time.Time              `json:"timestamp"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// readPump handles incoming messages from the WebSocket connection.
func (c *Client) readPump() {
	defer func() {
		c.hub.removeClientFromAllChannels(c)
		c.Close()
	}()

	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		c.updateLastSeen()
		return nil
	})

	for {
		var msg map[string]interface{}
		if err := c.conn.ReadJSON(&msg); err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("[ReadPump] error: %v", err)
			}
			break
		}

		c.hub.stats.incrementMessagesReceived()
		c.updateLastSeen()

		action, _ := msg["action"].(string)
		channel, _ := msg["channel"].(string)
		dataRaw := msg["data"]

		switch action {
		case "subscribe":
			if isPrivateChannel(channel) && !c.authHandler(c.UserID, channel) {
				fmt.Println("not authorized")
				continue
			}
			c.hub.subscribe <- Subscription{
				ChannelName: channel,
				Client:      c,
				Join:        true,
			}

		case "unsubscribe":
			c.hub.unsubscribe <- Subscription{
				ChannelName: channel,
				Client:      c,
				Join:        false,
			}

		case "broadcast":
			if data, err := json.Marshal(dataRaw); err == nil {
				c.hub.Broadcast <- Broadcast{
					ChannelName: channel,
					Data:        data,
					SenderID:    c.UserID,
					Timestamp:   time.Now(),
				}
			}
		}
	}
}

// writePump handles outgoing messages to the WebSocket connection.
func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)

			n := len(c.send)
			for i := 0; i < n; i++ {
				w.Write(newline)
				w.Write(<-c.send)
			}

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

// Close closes the WebSocket connection.
func (c *Client) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.isClosing {
		c.isClosing = true

		// Remove from Redis
		ctx := context.Background()
		if err := c.hub.storage.RemoveClient(ctx, c.UserID); err != nil {
			log.Printf("[Client] Error removing client from Redis: %v", err)
		}

		close(c.send)
		c.conn.Close()
	}
}

// updateLastSeen updates the last seen timestamp for the client.
func (c *Client) updateLastSeen() {
	c.mu.Lock()
	c.LastSeen = time.Now()
	c.mu.Unlock()

	ctx := context.Background()
	if err := c.hub.storage.UpdateClientLastSeen(ctx, c.UserID); err != nil {
		log.Printf("[Client] Error updating last seen: %v", err)
	}
}
