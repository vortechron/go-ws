package ws

import (
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

const (
	writeWait = 10 * time.Second
	pongWait  = 60 * time.Second
	// pingPeriod     = (pongWait * 9) / 10
	pingPeriod     = 5 * time.Second
	maxMessageSize = 512 * 1024 // 512KB
)

var (
	newline  = []byte{'\n'}
	upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}
)

// isPrivateChannel checks if a channel is private.
func isPrivateChannel(channelName string) bool {
	return strings.HasPrefix(channelName, "private-")
}

// isPresenceChannel checks if a channel is a presence channel.
func isPresenceChannel(channelName string) bool {
	return strings.HasPrefix(channelName, "presence-")
}

func shouldAuthenticate(channelName string) bool {
	return isPrivateChannel(channelName) || isPresenceChannel(channelName)
}
