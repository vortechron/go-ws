package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"
)

type Hub interface {
	GetBroadcastChannel() chan Broadcast
	GetSubscribeChannel() chan Subscription
	GetUnsubscribeChannel() chan Subscription
	GetMessageBroker() MessageBroker
	GetStorageClient() StorageClient
	GetBrokerContexts() map[string]context.CancelFunc
	GetProcessedMessages() *sync.Map
	GetOptions() *Options
	SetOptions(options *Options)
	GetStats() *HubStats
	IncrementConnections()
	DecrementConnections()
	GetClientQueueSize() int
	SendWhisperToChannel(whisper *WhisperEvent)
	Run()
	HandleSubscribe(sub Subscription)
	HandleUnsubscribe(sub Subscription)
	HandleBroadcast(b Broadcast)
	HandleSlowClient(c *Client, channelName string)
}

// Hub manages all channels and routing events.
type DefaultHub struct {
	mu                 sync.RWMutex
	channels           map[string]map[string]*Client
	subscribe          chan Subscription
	unsubscribe        chan Subscription
	Broadcast          chan Broadcast
	broadcastQueueSize int
	clientQueueSize    int
	stats              HubStats
	messageBroker      MessageBroker
	storage            StorageClient
	brokerContexts     map[string]context.CancelFunc // Track broker subscriptions
	processedMessages  sync.Map
	options            *Options
	logger             Logger
	shouldLogStats     bool
}

// Implement Hub interface methods for DefaultHub
func (h *DefaultHub) GetBroadcastChannel() chan Broadcast {
	return h.Broadcast
}

func (h *DefaultHub) GetSubscribeChannel() chan Subscription {
	return h.subscribe
}

func (h *DefaultHub) GetUnsubscribeChannel() chan Subscription {
	return h.unsubscribe
}

func (h *DefaultHub) GetMessageBroker() MessageBroker {
	return h.messageBroker
}

func (h *DefaultHub) GetStorageClient() StorageClient {
	return h.storage
}

func (h *DefaultHub) GetBrokerContexts() map[string]context.CancelFunc {
	return h.brokerContexts
}

func (h *DefaultHub) GetProcessedMessages() *sync.Map {
	return &h.processedMessages
}

func (h *DefaultHub) GetOptions() *Options {
	return h.options
}

func (h *DefaultHub) GetStats() *HubStats {
	return &h.stats
}

func (h *DefaultHub) GetClientQueueSize() int {
	return h.clientQueueSize
}

func (h *DefaultHub) IncrementConnections() {
	h.stats.mu.Lock()
	h.stats.totalConnections++
	h.stats.activeConnections++
	h.stats.mu.Unlock()
}

func (h *DefaultHub) DecrementConnections() {
	h.stats.mu.Lock()
	h.stats.activeConnections--
	h.stats.totalConnections--
	h.stats.mu.Unlock()
}

func (h *DefaultHub) SendWhisperToChannel(whisper *WhisperEvent) {
	h.sendWhisperToChannel(whisper)
}

func (h *DefaultHub) HandleSubscribe(sub Subscription) {
	h.handleSubscribe(sub)
}

func (h *DefaultHub) HandleUnsubscribe(sub Subscription) {
	h.handleUnsubscribe(sub)
}

func (h *DefaultHub) HandleBroadcast(b Broadcast) {
	h.handleBroadcast(b)
}

func (h *DefaultHub) HandleSlowClient(c *Client, channelName string) {
	h.handleSlowClient(c, channelName)
}

// HubStats tracks important metrics
type HubStats struct {
	mu                sync.RWMutex
	totalConnections  int64
	activeConnections int64
	messagesSent      int64
	messagesReceived  int64
	errors            int64
}

// Subscription is a client's request to join or leave a channel.
type Subscription struct {
	ChannelName string
	Client      *Client
	Join        bool
}

// Broadcast is a message to all clients on a channel.
type Broadcast struct {
	ChannelName string    `json:"channel_name"`
	Event       string    `json:"event"`
	Data        []byte    `json:"data"`
	SenderID    string    `json:"sender_id"`
	Timestamp   time.Time `json:"timestamp"`
	FromBroker  bool      `json:"from_broker,omitempty"` // renamed field for generic broker
	MessageID   string    `json:"message_id"`
}

// NewHub initializes a new Hub instance.
func NewHub(ctx context.Context, broker MessageBroker, storage StorageClient, config Config) *DefaultHub {
	logger := NewLogger(config.LogLevel, nil)

	hub := &DefaultHub{
		channels:           make(map[string]map[string]*Client),
		subscribe:          make(chan Subscription, 1000),
		unsubscribe:        make(chan Subscription, 1000),
		Broadcast:          make(chan Broadcast, config.WriteBufferSize),
		broadcastQueueSize: config.WriteBufferSize,
		clientQueueSize:    config.ReadBufferSize,
		stats:              HubStats{},
		messageBroker:      broker,
		storage:            storage,
		brokerContexts:     make(map[string]context.CancelFunc),
		processedMessages:  sync.Map{},
		logger:             logger,
		shouldLogStats:     config.ShouldLogStats,
	}

	// Set the default logger with the configured level
	SetLogLevel(config.LogLevel)

	return hub
}

// SetOptions sets the options for the hub
func (h *DefaultHub) SetOptions(options *Options) {
	h.options = options
}

// SetLogger sets the logger for the hub
func (h *DefaultHub) SetLogger(logger Logger) {
	h.logger = logger
}

// sendWhisperToChannel sends a whisper event to all clients on a channel
func (h *DefaultHub) sendWhisperToChannel(whisper *WhisperEvent) {
	channelName := whisper.ChannelName

	// Verify the channel exists
	h.mu.RLock()
	clients, channelExists := h.channels[channelName]
	h.mu.RUnlock()

	if !channelExists {
		if h.options != nil && h.options.ErrorHandler != nil {
			h.options.ErrorHandler(&WhisperError{
				Message: fmt.Sprintf("Channel %s not found", channelName),
			})
		}
		return
	}

	message, err := json.Marshal(whisper)
	if err != nil {
		if h.options != nil && h.options.ErrorHandler != nil {
			h.options.ErrorHandler(&WhisperError{
				Message: fmt.Sprintf("Failed to marshal whisper: %v", err),
			})
		}
		return
	}

	// Get all active clients for this channel from Redis
	ctx := context.Background()
	clientIDs, err := h.storage.GetChannelClients(ctx, channelName)
	if err != nil {
		log.Println("Error getting channel clients", "error", err, "channel", channelName)
		return
	}

	// Send to all clients in the channel
	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, client := range clients {
		// Skip the sender (optional, based on your requirements)
		if client.UserID == whisper.FromID || client.ClientID == whisper.FromID {
			continue
		}

		// Skip if client is closing
		if client.isClosing {
			continue
		}

		// Only send if client is still subscribed according to Redis
		for _, id := range clientIDs {
			if id == client.ClientID {
				// Send the message in a non-blocking way
				select {
				case client.send <- message:
					h.stats.incrementMessagesSent()
				default:
					// Handle slow client or buffer full
					go h.handleSlowClient(client, channelName)
				}
				break
			}
		}
	}
}

// Run starts the Hub's event loop.
func (h *DefaultHub) Run() {
	h.logger.Info("Starting Hub event loop")
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case sub := <-h.subscribe:
			h.handleSubscribe(sub)
		case sub := <-h.unsubscribe:
			h.handleUnsubscribe(sub)
		case b := <-h.Broadcast:
			h.handleBroadcast(b)
		case <-ticker.C:
			h.cleanupStaleConnections()
			h.logStats()
		}
	}
}

// handleSubscribe handles a client subscription
func (h *DefaultHub) handleSubscribe(sub Subscription) {
	// Store client info in Redis
	clientInfo := &ClientInfo{
		UserID:    sub.Client.UserID,
		ClientID:  sub.Client.ClientID,
		Metadata:  sub.Client.Metadata,
		CreatedAt: sub.Client.CreatedAt,
		LastSeen:  time.Now(),
	}

	if err := h.storage.SaveClient(context.Background(), clientInfo); err != nil {
		log.Println("Error saving client", "error", err, "clientID", sub.Client.ClientID)
		return
	}

	if err := h.storage.AddClientToChannel(context.Background(), sub.ChannelName, sub.Client.ClientID); err != nil {
		log.Println("Error adding client to channel", "error", err, "clientID", sub.Client.ClientID, "channel", sub.ChannelName)
		return
	}

	// Add to local map for active connections
	h.mu.Lock()
	if _, exists := h.channels[sub.ChannelName]; !exists {
		h.channels[sub.ChannelName] = make(map[string]*Client)

		// Set up broker subscription if needed
		ctx, cancel := context.WithCancel(context.Background())
		h.brokerContexts[sub.ChannelName] = cancel

		msgCh, err := h.messageBroker.Subscribe(ctx, sub.ChannelName)
		if err != nil {
			log.Println("Error subscribing to broker channel", "error", err, "channel", sub.ChannelName)
			cancel()
			delete(h.brokerContexts, sub.ChannelName)
			return
		}
		go h.handleBrokerMessages(sub.ChannelName, msgCh)
	}
	h.channels[sub.ChannelName][sub.Client.ClientID] = sub.Client
	h.mu.Unlock()

	if isPresenceChannel(sub.ChannelName) {
		h.broadcastPresenceJoin(sub)
	}
}

// handleBrokerMessages processes messages from the broker for a specific channel
func (h *DefaultHub) handleBrokerMessages(channelName string, msgCh <-chan []byte) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-msgCh:
			var broadcast Broadcast
			if err := json.Unmarshal(msg, &broadcast); err != nil {
				h.logger.Error("Error unmarshalling broker message: %v", err)
				continue
			}

			// Check if we've already processed this message
			if _, exists := h.processedMessages.LoadOrStore(broadcast.MessageID, true); exists {
				continue
			}

			// Clean up old message IDs after a delay
			go func(messageID string) {
				time.Sleep(5 * time.Second)
				h.processedMessages.Delete(messageID)
			}(broadcast.MessageID)

			// Get all active clients for this channel from Redis
			ctx := context.Background()
			clientIDs, err := h.storage.GetChannelClients(ctx, channelName)
			if err != nil {
				log.Println("Error getting channel clients", "error", err, "channel", channelName)
				continue
			}

			// Send to local active connections
			h.mu.RLock()
			clients := h.channels[channelName]
			for _, client := range clients {
				// Skip if client is closing
				if client.isClosing {
					continue
				}

				// Only send if client is still subscribed according to Redis
				for _, id := range clientIDs {
					if id == client.ClientID {
						// Use a separate goroutine to send with timeout
						go func(c *Client) {
							broadcastBytes, err := json.Marshal(map[string]interface{}{
								"channel_name": broadcast.ChannelName,
								"data":         json.RawMessage(broadcast.Data),
								"sender_id":    broadcast.SenderID,
								"timestamp":    broadcast.Timestamp.Format(time.RFC3339),
								"message_id":   broadcast.MessageID,
								"event":        broadcast.Event,
							})
							if err != nil {
								log.Println("Error marshalling broadcast", "error", err)
								return
							}
							select {
							case c.send <- broadcastBytes:
								h.stats.incrementMessagesSent()
							case <-time.After(writeWait):
								// If we timeout, handle as slow client
								go h.handleSlowClient(c, channelName)
							}
						}(client)
						break
					}
				}
			}
			h.mu.RUnlock()
		}
	}
}

// handleUnsubscribe processes unsubscription requests.
func (h *DefaultHub) handleUnsubscribe(sub Subscription) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Remove client from Redis channel first
	if err := h.storage.RemoveClientFromChannel(context.Background(), sub.ChannelName, sub.Client.ClientID); err != nil {
		log.Println("Error removing client from Redis channel", "error", err, "clientID", sub.Client.ClientID, "channel", sub.ChannelName)
	}

	h.unsubscribeClientFromChannel(sub.Client, sub.ChannelName)

	// If no more clients in channel, cancel broker subscription
	if clients, exists := h.channels[sub.ChannelName]; exists && len(clients) == 0 {
		if cancel, ok := h.brokerContexts[sub.ChannelName]; ok {
			cancel()
			delete(h.brokerContexts, sub.ChannelName)
		}
		delete(h.channels, sub.ChannelName)
	}
}

// handleBroadcast sends messages to the broker.
func (h *DefaultHub) handleBroadcast(b Broadcast) {
	// Generate a unique message ID if not present
	if b.MessageID == "" {
		b.MessageID = fmt.Sprintf("%s-%d", b.SenderID, time.Now().UnixNano())
	}

	payload, err := json.Marshal(b)
	if err != nil {
		log.Println("Error marshalling broadcast", "error", err)
		return
	}

	if err := h.messageBroker.Publish(context.Background(), b.ChannelName, payload); err != nil {
		log.Println("Error publishing to broker", "error", err, "channel", b.ChannelName)
	}
}

// handleSlowClient removes slow clients from a channel.
func (h *DefaultHub) handleSlowClient(c *Client, channelName string) {
	h.logger.Debug("Removing slow client from channel", "clientID", c.ClientID, "channel", channelName)

	h.mu.Lock()
	defer h.mu.Unlock()

	// Check if client is already closing
	if c.isClosing {
		return
	}

	h.unsubscribeClientFromChannel(c, channelName)

	// Remove from Redis
	if err := h.storage.RemoveClientFromChannel(context.Background(), c.ClientID, channelName); err != nil {
		h.logger.Error("Error removing client from Redis channel", "error", err, "clientID", c.ClientID, "channel", channelName)
	}

	c.Close()
	h.stats.mu.Lock()
	h.stats.activeConnections--
	h.stats.mu.Unlock()
}

// cleanupStaleConnections removes clients that have been inactive for too long.
func (h *DefaultHub) cleanupStaleConnections() {
	h.logger.Debug("Cleaning up stale connections", "channels", h.channels)

	now := time.Now()
	staleTimeout := now.Add(-5 * time.Minute)
	// staleTimeout := now.Add(-10 * time.Second)

	for channelName, clients := range h.channels {
		for _, client := range clients {
			h.logger.Debug("Checking client last seen", "clientID", client.ClientID, "lastSeen", client.LastSeen, "staleTimeout", staleTimeout)
			if client.LastSeen.Before(staleTimeout) {
				h.logger.Debug("Removing stale client", "clientID", client.ClientID)
				h.handleSlowClient(client, channelName)
			}
		}
	}
}

// logStats logs the current statistics of the Hub.
func (h *DefaultHub) logStats() {
	if !h.shouldLogStats {
		return
	}

	h.stats.mu.RLock()
	defer h.stats.mu.RUnlock()

	h.logger.Info("Hub stats",
		"activeConnections", h.stats.activeConnections,
		"totalConnections", h.stats.totalConnections,
		"messagesSent", h.stats.messagesSent,
		"messagesReceived", h.stats.messagesReceived,
		"errors", h.stats.errors)
}

// unsubscribeClientFromChannel removes a client from a specific channel.
func (h *DefaultHub) unsubscribeClientFromChannel(client *Client, channelName string) {
	if clients, exists := h.channels[channelName]; exists {
		if _, ok := clients[client.ClientID]; ok {
			delete(clients, client.ClientID)

			// Remove client from Redis channel storage
			if err := h.storage.RemoveClientFromChannel(context.Background(), channelName, client.ClientID); err != nil {
				log.Println("Error removing client from Redis channel", "error", err, "clientID", client.ClientID, "channel", channelName)
			}

			if isPresenceChannel(channelName) {
				h.broadcastPresenceLeave(client, channelName)
			}

			if len(clients) == 0 {
				delete(h.channels, channelName)
			}
		}
	}
}

// broadcastPresenceJoin notifies all clients of a new presence join.
func (h *DefaultHub) broadcastPresenceJoin(sub Subscription) {
	joinMsg := PresenceMessage{
		Event:     "presence:join",
		UserID:    sub.Client.UserID,
		ClientID:  sub.Client.ClientID,
		Timestamp: time.Now(),
		Metadata:  sub.Client.Metadata,
	}

	if payload, err := json.Marshal(joinMsg); err == nil {
		h.Broadcast <- Broadcast{
			ChannelName: sub.ChannelName,
			Data:        payload,
			SenderID:    "system",
			Timestamp:   time.Now(),
			MessageID:   fmt.Sprintf("presence-join-%s-%d", sub.Client.UserID, time.Now().UnixNano()),
		}
	}
}

// broadcastPresenceLeave notifies all clients of a presence leave.
func (h *DefaultHub) broadcastPresenceLeave(client *Client, channelName string) {
	leaveMsg := PresenceMessage{
		Event:     "presence:leave",
		UserID:    client.UserID,
		ClientID:  client.ClientID,
		Timestamp: time.Now(),
		Metadata:  client.Metadata,
	}

	if payload, err := json.Marshal(leaveMsg); err == nil {
		h.Broadcast <- Broadcast{
			ChannelName: channelName,
			Data:        payload,
			SenderID:    "system",
			Timestamp:   time.Now(),
			MessageID:   fmt.Sprintf("presence-leave-%s-%d", client.ClientID, time.Now().UnixNano()),
		}
	}
}

// removeClientFromAllChannels removes a client from all channels.
func (h *DefaultHub) removeClientFromAllChannels(c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()

	for channelName, clients := range h.channels {
		if _, ok := clients[c.ClientID]; ok {
			h.unsubscribeClientFromChannel(c, channelName)
		}
	}
	h.stats.mu.Lock()
	h.stats.activeConnections--
	h.stats.mu.Unlock()
}

// incrementMessagesSent increments the count of sent messages.
func (h *HubStats) incrementMessagesSent() {
	h.mu.Lock()
	h.messagesSent++
	h.mu.Unlock()
}

// incrementMessagesReceived increments the count of received messages.
func (h *HubStats) incrementMessagesReceived() {
	h.mu.Lock()
	h.messagesReceived++
	h.mu.Unlock()
}
