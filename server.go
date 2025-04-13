package ws

import (
	"context"
	"net/http"
	"time"
)

// AuthorizationHandler is a function type for checking channel authorization.
type AuthorizationHandler func(userID string, channelName string) bool
type GetUserIDHandler func(r *http.Request) string
type WhisperMiddlewareHandler func(whisper *WhisperEvent) bool

type Options struct {
	AuthHandler       AuthorizationHandler
	GetUserID         GetUserIDHandler
	ErrorHandler      func(error)
	Middleware        []Middleware
	OnConnect         func(*Client)
	OnDisconnect      func(*Client)
	EnableWhispers    bool
	WhisperMiddleware WhisperMiddlewareHandler
}

type Middleware interface {
	Handle(next http.Handler) http.Handler
}

// ServeWS handles WebSocket requests from clients.
func ServeWS(hub *DefaultHub, w http.ResponseWriter, r *http.Request, options *Options) error {
	// Set options on hub
	hub.SetOptions(options)

	// Apply middleware chain
	var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handleWebSocket(hub, w, r, options)
	})

	for i := len(options.Middleware) - 1; i >= 0; i-- {
		handler = options.Middleware[i].Handle(handler)
	}

	handler.ServeHTTP(w, r)
	return nil
}

func handleWebSocket(hub *DefaultHub, w http.ResponseWriter, r *http.Request, options *Options) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		if options.ErrorHandler != nil {
			options.ErrorHandler(err)
		}
		return
	}

	userID := options.GetUserID(r)

	client := &Client{
		hub:         hub,
		conn:        conn,
		send:        make(chan []byte, hub.clientQueueSize),
		UserID:      userID,
		Metadata:    make(map[string]interface{}),
		CreatedAt:   time.Now(),
		LastSeen:    time.Now(),
		authHandler: options.AuthHandler,
	}

	if options.OnConnect != nil {
		options.OnConnect(client)
	}

	hub.stats.mu.Lock()
	hub.stats.totalConnections++
	hub.stats.activeConnections++
	hub.stats.mu.Unlock()

	go client.writePump()
	go client.readPump()
}

// Update the initialization
func NewWebSocketServer(ctx context.Context, redisAddr string, config Config) (*DefaultHub, error) {
	broker := NewRedisBroker(redisAddr)
	storage := NewRedisStorage(redisAddr)
	return NewHub(ctx, broker, storage, config), nil
}
