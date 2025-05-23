package ws

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

func NewClient(hub Hub, conn *websocket.Conn, send chan []byte, userID string, metadata map[string]interface{}, createdAt time.Time, lastSeen time.Time, authHandler AuthorizationHandler) *Client {
	// Generate UUID if userID is empty
	if userID == "" {
		userID = uuid.NewString()
	}

	// Always generate a new ClientID for each connection
	clientID := uuid.NewString()

	// Create a logger with client context
	logger := DefaultLogger()

	return &Client{
		hub:         hub,
		conn:        conn,
		send:        send,
		ClientID:    clientID,
		UserID:      userID,
		Metadata:    metadata,
		CreatedAt:   createdAt,
		LastSeen:    lastSeen,
		authHandler: authHandler,
		logger:      logger,
	}
}

// Client represents a single WebSocket connection.
type Client struct {
	hub         Hub
	conn        *websocket.Conn
	send        chan []byte
	ClientID    string // Unique identifier for each connection
	UserID      string // User identifier (can be shared across connections)
	Metadata    map[string]interface{}
	CreatedAt   time.Time
	LastSeen    time.Time
	isClosing   bool
	mu          sync.RWMutex
	authHandler AuthorizationHandler
	logger      Logger
}

// SetLogger sets the logger for the client
func (c *Client) SetLogger(logger Logger) {
	c.logger = logger
}

// PresenceMessage is how we'll communicate presence joins/leaves.
type PresenceMessage struct {
	Event     string                 `json:"event"`
	ClientID  string                 `json:"client_id"` // Unique identifier for the connection
	UserID    string                 `json:"user_id"`   // User identifier (can be shared)
	Timestamp time.Time              `json:"timestamp"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// WhisperEvent represents a client-emitted event on a channel.
type WhisperEvent struct {
	Action      string                 `json:"action"`
	ChannelName string                 `json:"channel_name"`
	FromID      string                 `json:"from"`
	Event       string                 `json:"event"`
	Data        map[string]interface{} `json:"data"`
	Timestamp   time.Time              `json:"timestamp"`
}

// readPump handles incoming messages from the WebSocket connection.
func (c *Client) readPump() {
	defer func() {
		c.hub.HandleUnsubscribe(Subscription{
			ChannelName: "",
			Client:      c,
			Join:        false,
		})
		c.Close()
	}()

	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.logger.Debug("Received pong message")
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		c.updateLastSeen()
		return nil
	})

	hubOptions := c.hub.GetOptions()
	hubStats := c.hub.GetStats()
	hubSubscribeChannel := c.hub.GetSubscribeChannel()
	hubUnsubscribeChannel := c.hub.GetUnsubscribeChannel()
	hubBroadcastChannel := c.hub.GetBroadcastChannel()

	for {
		var msg map[string]interface{}
		if err := c.conn.ReadJSON(&msg); err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				c.logger.Error("WebSocket read error: %v", err)
			}
			break
		}

		hubStats.incrementMessagesReceived()
		c.updateLastSeen()

		action, _ := msg["action"].(string)
		channel, _ := msg["channel"].(string)
		dataRaw := msg["data"]

		c.logger.Debug("Received message: action=%s, channel=%s", action, channel)

		switch action {
		case "subscribe":
			if shouldAuthenticate(channel) && !c.authHandler(c.UserID, channel) {
				c.logger.Warn("Unauthorized subscription attempt to channel %s", channel)
				continue
			}
			hubSubscribeChannel <- Subscription{
				ChannelName: channel,
				Client:      c,
				Join:        true,
			}

		case "unsubscribe":
			hubUnsubscribeChannel <- Subscription{
				ChannelName: channel,
				Client:      c,
				Join:        false,
			}

		case "broadcast":
			if data, err := json.Marshal(dataRaw); err == nil {
				hubBroadcastChannel <- Broadcast{
					ChannelName: channel,
					Data:        data,
					SenderID:    c.ClientID,
					Timestamp:   time.Now(),
				}
			} else {
				c.logger.Error("Failed to marshal broadcast data: %v", err)
			}

		case "whisper":
			event, _ := msg["event"].(string)

			// Validate channel and event
			if channel == "" || event == "" {
				if hubOptions != nil && hubOptions.ErrorHandler != nil {
					hubOptions.ErrorHandler(&WhisperError{Message: "Invalid whisper: missing channel or event"})
				}
				c.logger.Warn("Invalid whisper: missing channel or event")
				continue
			}

			// Check if whispers are enabled
			if hubOptions == nil || !hubOptions.EnableWhispers {
				c.logger.Debug("Whisper ignored: whispers not enabled")
				continue
			}

			// Create whisper event
			whisperEvt := &WhisperEvent{
				Action:      "whisper",
				ChannelName: channel,
				FromID:      c.ClientID,
				Event:       event,
				Timestamp:   time.Now(),
			}

			// Extract data if present
			if dataRaw != nil {
				if dataMap, ok := dataRaw.(map[string]interface{}); ok {
					whisperEvt.Data = dataMap
				}
			}

			whisperEvt.Data["event"] = event

			// Apply whisper middleware if configured
			if c.hub.GetOptions().WhisperMiddleware != nil {
				if !c.hub.GetOptions().WhisperMiddleware(whisperEvt) {
					c.logger.Debug("Whisper blocked by middleware")
					continue
				}
			}

			// Send the whisper to the channel
			c.hub.SendWhisperToChannel(whisperEvt)
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
		c.logger.Info("Closing connection")

		// Remove from Redis
		ctx := context.Background()
		if err := c.hub.GetStorageClient().RemoveClient(ctx, c.ClientID); err != nil {
			c.logger.Error("Error removing client from Redis: %v", err)
		}

		close(c.send)
		c.conn.Close()
	}
}

// updateLastSeen updates the last seen timestamp for the client.
func (c *Client) updateLastSeen() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.logger.Debug("Updating last seen timestamp")
	c.LastSeen = time.Now()

	// Update in Redis
	ctx := context.Background()
	if err := c.hub.GetStorageClient().UpdateClientLastSeen(ctx, c.ClientID); err != nil {
		c.logger.Error("Error updating last seen: %v", err)
	}
}
