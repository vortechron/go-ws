# Go WebSocket Server

A production-ready, scalable WebSocket server implementation in Go with support for pub/sub channels, presence channels, and private channels.

## Features

- WebSocket server with pub/sub capabilities
- Support for private channels with authorization
- Presence channels with join/leave events
- Redis backend for scaling across multiple nodes
- Automatic connection management and cleanup
- Built-in statistics tracking
- Extensible message broker and storage interfaces
- Context-based graceful shutdown
- Comprehensive security features
- Configurable rate limiting
- Middleware support
- Customizable connection settings

## Installation

1. Install the package using `go get`:

   ```
   go get github.com/vortechron/go-ws
   ```

2. Import the package in your Go code:

   ```go
   import "github.com/vortechron/go-ws"
   ```

## Usage

1. Create a configuration:

   ```go
   config := ws.Config{
       PingInterval:    30 * time.Second,
       WriteBufferSize: 1024,
       ReadBufferSize:  1024,
       RateLimit: ws.RateLimit{
           Messages: 100,
           Interval: time.Minute,
       },
   }
   ```

2. Create a new WebSocket server instance with context for graceful shutdown:

   ```go
   ctx := context.Background()
   redisAddr := "localhost:6379"
   hub := ws.NewHub(
       ctx,
       ws.NewRedisBroker(redisAddr),
       ws.NewRedisStorage(redisAddr),
       config,
   )

   go hub.Run()
   ```

3. Define your WebSocket options including middleware, auth handlers, and error handlers:

   ```go
   options := &ws.Options{
       // Authorization handler
       AuthHandler: func(userID string, channelName string) bool {
           // Implement your authorization logic here
           return true
       },

       // User identification
       GetUserID: func(r *http.Request) string {
           // Extract user ID from JWT token
           token := jwt.ParseFromRequest(r)
           return token.Claims.UserID
       },

       // Error handling
       ErrorHandler: func(err error) {
           log.Printf("WebSocket error: %v", err)
       },

       // Middleware chain
       Middleware: []ws.Middleware{
           // ws.RateLimitMiddleware(), example
       },

       // Connection events
       OnConnect: func(client *ws.Client) {
           log.Printf("Client connected: %s", client.ID)
       },
       OnDisconnect: func(client *ws.Client) {
           log.Printf("Client disconnected: %s", client.ID)
       },
   }
   ```

4. Start the WebSocket server with graceful shutdown:

   ```go
   // Create server with context
   server := &http.Server{
       Addr: ":8080",
   }

   // Handle WebSocket connections
   http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
       if err := ws.ServeWS(hub, w, r, options); err != nil {
           log.Printf("Failed to serve WebSocket: %v", err)
       }
   })

   // Start server
   go func() {
       if err := server.ListenAndServeTLS("cert.pem", "key.pem"); err != nil {
           log.Printf("Server error: %v", err)
       }
   }()

   // Graceful shutdown
   stop := make(chan os.Signal, 1)
   signal.Notify(stop, os.Interrupt)
   <-stop

   shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
   defer cancel()

   if err := server.Shutdown(shutdownCtx); err != nil {
       log.Printf("Error during shutdown: %v", err)
   }
   ```

5. Connect and use from client side:

   ```javascript
   const socket = new WebSocket("wss://localhost:8080/ws");

   // Handle connection
   socket.onopen = function() {
       // Subscribe to channels
       socket.send(JSON.stringify({
           action: "subscribe",
           channel: "my-channel"
       }));

       // Subscribe to presence channel
       socket.send(JSON.stringify({
           action: "subscribe",
           channel: "presence-room-1"
       }));
   };

   // Handle incoming messages
   socket.onmessage = function(event) {
       const data = JSON.parse(event.data);
       
       // Handle different message types
       switch(data.type) {
           case "message":
               console.log("New message:", data.message);
               break;
           case "presence":
               console.log("Presence update:", data.users);
               break;
           case "error":
               console.error("Error:", data.error);
               break;
       }
   };

   // Handle errors
   socket.onerror = function(error) {
       console.error("WebSocket error:", error);
   };

   // Handle disconnection
   socket.onclose = function() {
       console.log("Connection closed");
       // Implement reconnection logic here
   };
   ```

## Error Handling

The package provides comprehensive error handling:

```go
// Custom error handling
options.ErrorHandler = func(err error) {
    switch e := err.(type) {
    case *ws.AuthError:
        // Handle authentication errors
    case *ws.RateLimitError:
        // Handle rate limiting errors
    case *ws.ConnectionError:
        // Handle connection errors
    default:
        // Handle other errors
    }
}
```

## Scaling Considerations

When scaling across multiple nodes:

1. Ensure Redis cluster is properly configured
2. Configure appropriate connection limits per node
3. Monitor memory usage and connection counts
4. Use health checks for load balancing

```go
// Configure connection limits
config.MaxConnections = 10000
config.MaxConnectionsPerIP = 100
```

## Security Best Practices

1. Always use SSL/TLS in production
2. Implement rate limiting
3. Validate origin headers
4. Use token-based authentication
5. Sanitize all input messages
6. Set appropriate timeouts

## Extending Functionality

The package provides interfaces for extending functionality:

- `MessageBroker`: Implement this interface to use a different message broker
- `StorageClient`: Implement this interface to use a different storage backend
- `Middleware`: Implement this interface to add custom middleware
- `AuthProvider`: Implement this interface to add custom authentication

## Production Checklist

- [ ] Set up monitoring and alerting
- [ ] Configure appropriate timeouts
- [ ] Implement rate limiting
- [ ] Set up error tracking
- [ ] Configure logging
- [ ] Set up health checks
- [ ] Plan for scaling
- [ ] Implement reconnection strategy
- [ ] Set up backup and recovery procedures

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## License

This project is licensed under the MIT License - see the LICENSE file for details.






