package ws

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"
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
	Client            struct {
		Logger Logger
	}
}

type Middleware interface {
	Handle(next http.Handler) http.Handler
}

// ServeWS handles WebSocket requests from clients.
func ServeWS(hub Hub, w http.ResponseWriter, r *http.Request, options *Options) error {
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

func handleWebSocket(hub Hub, w http.ResponseWriter, r *http.Request, options *Options) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		if options.ErrorHandler != nil {
			options.ErrorHandler(err)
		}
		return
	}

	// Create a new client with the userID and a unique clientID
	client := &Client{
		hub:         hub,
		conn:        conn,
		send:        make(chan []byte, hub.GetClientQueueSize()),
		ClientID:    uuid.New().String(),
		UserID:      options.GetUserID(r),
		Metadata:    make(map[string]interface{}),
		CreatedAt:   time.Now(),
		LastSeen:    time.Now(),
		authHandler: options.AuthHandler,
	}

	if options.Client.Logger != nil {
		client.SetLogger(options.Client.Logger)
	}

	if options.OnConnect != nil {
		options.OnConnect(client)
	}

	hub.IncrementConnections()

	go client.writePump()
	go client.readPump()
}

// Update the initialization
func NewWebSocketServer(ctx context.Context, redisAddr string, config Config) (*DefaultHub, error) {
	broker := NewRedisBroker(redisAddr)
	storage := NewRedisStorage(redisAddr)
	return NewHub(ctx, broker, storage, config), nil
}
