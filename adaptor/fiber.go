package adaptor

import (
	"net/http"

	"github.com/vortechron/go-ws"

	"github.com/gofiber/fiber/v2"
	"github.com/valyala/fasthttp/fasthttpadaptor"
)

// FiberAdaptor creates a Fiber handler that serves WebSocket connections
// by adapting the request to the standard net/http handler `ws.ServeWS`.
func FiberAdaptor(hub ws.Hub, options *ws.Options) fiber.Handler {
	// Create an http.HandlerFunc that calls the original ServeWS
	httpHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		err := ws.ServeWS(hub, w, r, options)
		if err != nil {
			// Optionally log the error or handle it based on application needs
			// Using Fiber's error handler might be more appropriate here if integrated tightly.
			// For simplicity, we are just checking the error returned by ServeWS.
			if options.ErrorHandler != nil {
				options.ErrorHandler(err) // Use the configured error handler
			}
			// Consider writing an HTTP error response if the WebSocket upgrade fails within ServeWS
			// http.Error(w, "WebSocket upgrade failed", http.StatusInternalServerError)
		}
	})

	// Convert the net/http handler to a fasthttp request handler
	fastHTTPHandler := fasthttpadaptor.NewFastHTTPHandler(httpHandler)

	// Return a Fiber handler that uses the fasthttp handler
	return func(c *fiber.Ctx) error {
		fastHTTPHandler(c.Context())
		return nil // Return nil as the handler manages the response stream
	}
}
