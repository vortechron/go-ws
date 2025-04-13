# Go WebSocket Server

A simple WebSocket server implementation in Go with support for pub/sub channels, presence channels, and private channels inspired from Laravel Echo

## Features

- WebSocket server with pub/sub capabilities
- Support for private channels with authorization
- Presence channels with join/leave events
- Client-to-client whisper events within channels
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
       ShouldLogStats: true,
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
       
       // Enable whisper functionality
       EnableWhispers: true,
       
       // Optional whisper middleware for filtering or transforming whispers
       WhisperMiddleware: func(whisper *ws.WhisperEvent) bool {
           // Process the whisper event here
           // Return false to block the whisper
           return true
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
           case "whisper":
               console.log(`Whisper from ${data.from}, event: ${data.event}, channel: ${data.channel}`);
               console.log("Whisper data:", data.data);
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

## Using Whisper Events

Whisper events allow clients to send events directly to other clients who are subscribed to the same channel without going through your application server. This is useful for features like typing indicators, read receipts, or any temporary client state that doesn't need to be stored.

1. Send a whisper to a channel:

   ```javascript
   // Send a whisper to a channel
   socket.send(JSON.stringify({
       action: "whisper",
       channel: "chat-room-1",  // Channel to whisper on
       event: "typing",         // Custom event name
       data: {                  // Custom data payload
           isTyping: true
       }
   }));
   ```

2. Listen for whispers on the client:

   ```javascript
   // Using the onmessage handler from above
   socket.onmessage = function(event) {
       const data = JSON.parse(event.data);
       
       if (data.type === "whisper") {
           // Handle specific whisper events
           switch(data.event) {
               case "typing":
                   const isTyping = data.data.isTyping;
                   console.log(`User ${data.from} is ${isTyping ? 'typing' : 'stopped typing'} in ${data.channel}`);
                   updateTypingIndicator(data.from, isTyping, data.channel);
                   break;
                   
               case "read":
                   console.log(`User ${data.from} read your message in ${data.channel}`);
                   updateReadStatus(data.from, data.channel);
                   break;
                   
               default:
                   console.log(`Received whisper from ${data.from} with event: ${data.event} in ${data.channel}`);
                   console.log("Data:", data.data);
           }
       }
   };
   ```

3. Server-side configuration (Go):

   ```go
   // Enable whisper functionality in options
   options.EnableWhispers = true
   
   // Optional: Add whisper middleware for filtering or transforming whispers
   options.WhisperMiddleware = func(whisper *ws.WhisperEvent) bool {
       // You can modify the whisper event here
       // Return false to block the whisper
       
       // Example: Log all whispers
       log.Printf("Whisper from %s on channel %s: %s", 
           whisper.FromID, whisper.ChannelName, whisper.Event)
       
       // Example: Block whispers with certain events
       if whisper.Event == "blocked-event" {
           return false
       }
       
       return true
   }
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
    case *ws.WhisperError:
        // Handle whisper-related errors
        log.Printf("Whisper error: %s", e.Message)
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

## Using with Fiber Framework

If you're using the [Fiber web framework](https://github.com/gofiber/fiber/v2), you can use the provided Fiber adapter:

1. First, install the required dependencies:

  ```
  go get github.com/gofiber/fiber/v2
  go get github.com/gofiber/websocket/v2
  ```

2. Create a new Fiber app and set up the WebSocket endpoint:

  ```go
  package main

  import (
    "context"
    "time"

    "github.com/gofiber/fiber/v2"
    "github.com/vortechron/go-ws"
    "github.com/vortechron/go-ws/adaptor"
  )

  func main() {
    // Create WebSocket hub
    ctx := context.Background()
    redisAddr := "localhost:6379"
    config := ws.Config{
      PingInterval:    30 * time.Second,
      WriteBufferSize: 1024,
      ReadBufferSize:  1024,
    }
    
    hub, _ := ws.NewWebSocketServer(ctx, redisAddr, config)
    go hub.Run()

    // Create WebSocket options
    options := &ws.Options{
      // Authorization handler
      AuthHandler: func(userID string, channelName string) bool {
        // Implement your authorization logic
        return true
      },

      // Get user ID from request
      GetUserID: func(r *http.Request) string {
        // Get user ID from request
        return "user-123"
      },
    }

    // Create Fiber app
    app := fiber.New()
    
    // Create WebSocket route
    app.Use("/ws", adaptor.FiberHandler(hub, options))
    
    // Start server
    app.Listen(":8080")
  }
  ```

3. Connect from the client side as shown in the earlier examples.






