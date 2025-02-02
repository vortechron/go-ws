package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"
)

// Hub manages all channels and routing events.
type Hub struct {
	mu                 sync.RWMutex
	channels           map[string]map[*Client]bool
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
	Data        []byte    `json:"data"`
	SenderID    string    `json:"sender_id"`
	Timestamp   time.Time `json:"timestamp"`
	FromBroker  bool      `json:"from_broker,omitempty"` // renamed field for generic broker
	MessageID   string    `json:"message_id"`
}

// NewHub initializes a new Hub instance.
func NewHub(ctx context.Context, broker MessageBroker, storage StorageClient, config Config) *Hub {
	hub := &Hub{
		channels:           make(map[string]map[*Client]bool),
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
	}

	return hub
}

// Run starts the Hub's event loop.
func (h *Hub) Run() {
	log.Println("[Run] Starting Hub event loop")
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

// handleSubscribe processes subscription requests.
func (h *Hub) handleSubscribe(sub Subscription) {
	// Store client info in Redis
	clientInfo := &ClientInfo{
		UserID:    sub.Client.UserID,
		Metadata:  sub.Client.Metadata,
		CreatedAt: sub.Client.CreatedAt,
		LastSeen:  time.Now(),
	}

	if err := h.storage.SaveClient(context.Background(), clientInfo); err != nil {
		log.Printf("[Hub] Error saving client: %v", err)
		return
	}

	if err := h.storage.AddClientToChannel(context.Background(), sub.ChannelName, sub.Client.UserID); err != nil {
		log.Printf("[Hub] Error adding client to channel: %v", err)
		return
	}

	// Add to local map for active connections
	h.mu.Lock()
	if _, exists := h.channels[sub.ChannelName]; !exists {
		h.channels[sub.ChannelName] = make(map[*Client]bool)

		// Set up broker subscription if needed
		ctx, cancel := context.WithCancel(context.Background())
		h.brokerContexts[sub.ChannelName] = cancel

		msgCh, err := h.messageBroker.Subscribe(ctx, sub.ChannelName)
		if err != nil {
			log.Printf("[Hub] Error subscribing to broker channel %s: %v", sub.ChannelName, err)
		} else {
			go h.handleBrokerMessages(sub.ChannelName, msgCh)
		}
	}
	h.channels[sub.ChannelName][sub.Client] = true
	h.mu.Unlock()

	if isPresenceChannel(sub.ChannelName) {
		h.broadcastPresenceJoin(sub)
	}
}

// handleBrokerMessages processes messages from the broker for a specific channel
func (h *Hub) handleBrokerMessages(channelName string, msgCh <-chan []byte) {
	for msg := range msgCh {
		var broadcast Broadcast
		if err := json.Unmarshal(msg, &broadcast); err != nil {
			log.Printf("[Hub] Error unmarshalling broker message: %v", err)
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

		// Skip rebroadcasting presence messages that originated from this instance
		// if broadcast.SenderID == "system" {
		// 	var presenceMsg PresenceMessage
		// 	if err := json.Unmarshal(broadcast.Data, &presenceMsg); err == nil {
		// 		if presenceMsg.Event == "presence:join" || presenceMsg.Event == "presence:leave" {
		// 			continue
		// 		}
		// 	}
		// }

		// Get all active clients for this channel from Redis
		ctx := context.Background()
		clientIDs, err := h.storage.GetChannelClients(ctx, channelName)
		if err != nil {
			log.Printf("[Hub] Error getting channel clients: %v", err)
			continue
		}

		// Send to local active connections
		h.mu.RLock()
		clients := h.channels[channelName]
		for client := range clients {
			// Skip if client is closing
			if client.isClosing {
				continue
			}

			// Only send if client is still subscribed according to Redis
			for _, id := range clientIDs {
				if id == client.UserID {
					// Use a separate goroutine to send with timeout
					go func(c *Client) {
						select {
						case c.send <- broadcast.Data:
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

// handleUnsubscribe processes unsubscription requests.
func (h *Hub) handleUnsubscribe(sub Subscription) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Remove client from Redis channel first
	if err := h.storage.RemoveClientFromChannel(context.Background(), sub.ChannelName, sub.Client.UserID); err != nil {
		log.Printf("[Hub] Error removing client from Redis channel: %v", err)
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
func (h *Hub) handleBroadcast(b Broadcast) {
	// Generate a unique message ID if not present
	if b.MessageID == "" {
		b.MessageID = fmt.Sprintf("%s-%d", b.SenderID, time.Now().UnixNano())
	}

	payload, err := json.Marshal(b)
	if err != nil {
		log.Printf("[Hub] Error marshalling broadcast: %v", err)
		return
	}

	if err := h.messageBroker.Publish(context.Background(), b.ChannelName, payload); err != nil {
		log.Printf("[Hub] Error publishing to broker: %v", err)
	}
}

// handleSlowClient removes slow clients from a channel.
func (h *Hub) handleSlowClient(c *Client, channelName string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Check if client is already closing
	if c.isClosing {
		return
	}

	log.Printf("[Hub] Removing slow client %s from channel %s", c.UserID, channelName)
	h.unsubscribeClientFromChannel(c, channelName)

	// Remove from Redis
	if err := h.storage.RemoveClientFromChannel(context.Background(), channelName, c.UserID); err != nil {
		log.Printf("[Hub] Error removing client from Redis channel: %v", err)
	}

	c.Close()
	h.stats.mu.Lock()
	h.stats.activeConnections--
	h.stats.mu.Unlock()
}

// cleanupStaleConnections removes clients that have been inactive for too long.
func (h *Hub) cleanupStaleConnections() {
	h.mu.Lock()
	defer h.mu.Unlock()

	staleTimeout := 5 * time.Minute
	now := time.Now()

	for channelName, clients := range h.channels {
		for client := range clients {
			if now.Sub(client.LastSeen) > staleTimeout {
				log.Printf("[Cleanup] Removing stale client %s", client.UserID)
				h.unsubscribeClientFromChannel(client, channelName)
				client.Close()
				h.stats.mu.Lock()
				h.stats.activeConnections--
				h.stats.mu.Unlock()
			}
		}
	}
}

// logStats logs the current statistics of the Hub.
func (h *Hub) logStats() {
	h.stats.mu.RLock()
	defer h.stats.mu.RUnlock()

	log.Printf("[Stats] Active: %d, Total: %d, Sent: %d, Received: %d, Errors: %d",
		h.stats.activeConnections,
		h.stats.totalConnections,
		h.stats.messagesSent,
		h.stats.messagesReceived,
		h.stats.errors)
}

// unsubscribeClientFromChannel removes a client from a specific channel.
func (h *Hub) unsubscribeClientFromChannel(client *Client, channelName string) {
	if clients, exists := h.channels[channelName]; exists {
		if _, ok := clients[client]; ok {
			delete(clients, client)

			// Remove client from Redis channel storage
			if err := h.storage.RemoveClientFromChannel(context.Background(), channelName, client.UserID); err != nil {
				log.Printf("[Hub] Error removing client from Redis channel: %v", err)
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
func (h *Hub) broadcastPresenceJoin(sub Subscription) {
	// Check if client is already in the channel according to Redis
	// ctx := context.Background()
	// clients, err := h.storage.GetChannelClients(ctx, sub.ChannelName)
	// if err != nil {
	// 	log.Printf("[Hub] Error checking channel clients: %v", err)
	// 	return
	// }

	// Check if client is already in the channel
	// for _, id := range clients {
	// 	if id == sub.Client.UserID {
	// 		fmt.Println("Client already joined, skip presence broadcast")
	// 		// Client already joined, skip presence broadcast
	// 		return
	// 	}
	// }

	joinMsg := PresenceMessage{
		Event:     "presence:join",
		UserID:    sub.Client.UserID,
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
func (h *Hub) broadcastPresenceLeave(client *Client, channelName string) {
	leaveMsg := PresenceMessage{
		Event:     "presence:leave",
		UserID:    client.UserID,
		Timestamp: time.Now(),
		Metadata:  client.Metadata,
	}

	if payload, err := json.Marshal(leaveMsg); err == nil {
		h.Broadcast <- Broadcast{
			ChannelName: channelName,
			Data:        payload,
			SenderID:    "system",
			Timestamp:   time.Now(),
			MessageID:   fmt.Sprintf("presence-leave-%s-%d", client.UserID, time.Now().UnixNano()),
		}
	}
}

// removeClientFromAllChannels removes a client from all channels.
func (h *Hub) removeClientFromAllChannels(c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()

	for channelName, clients := range h.channels {
		if _, ok := clients[c]; ok {
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
