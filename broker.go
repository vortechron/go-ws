package ws

import (
	"context"
	"log"

	"github.com/go-redis/redis/v8"
)

// MessageBroker defines the interface for a messaging broker.
type MessageBroker interface {
	Publish(ctx context.Context, channel string, payload []byte) error
	Subscribe(ctx context.Context, channel string) (<-chan []byte, error)
}

// RedisBroker implements the MessageBroker interface using Redis.
type RedisBroker struct {
	client *redis.Client
}

// NewRedisBroker returns a new instance of RedisBroker.
func NewRedisBroker(redisAddr string, options ...func(*redis.Options)) *RedisBroker {
	opt := newRedisConfig(options...)
	opt.Addr = redisAddr

	return &RedisBroker{client: redis.NewClient(opt)}
}

// Publish publishes the payload to the specified channel.
func (r *RedisBroker) Publish(ctx context.Context, channel string, payload []byte) error {
	// Add a prefix to distinguish WebSocket channels
	prefixedChannel := "ws:" + channel
	return r.client.Publish(ctx, prefixedChannel, payload).Err()
}

// Subscribe subscribes to the specified channel and returns a channel of messages.
func (r *RedisBroker) Subscribe(ctx context.Context, channel string) (<-chan []byte, error) {
	// Add a prefix to distinguish WebSocket channels
	prefixedChannel := "ws:" + channel
	pubsub := r.client.Subscribe(ctx, prefixedChannel)
	ch := make(chan []byte)

	go func() {
		defer func() {
			pubsub.Close()
			close(ch)
		}()

		for {
			select {
			case <-ctx.Done():
				return
			default:
				msg, err := pubsub.ReceiveMessage(ctx)
				if err != nil {
					log.Printf("[Broker] Error receiving message: %v", err)
					continue
				}
				ch <- []byte(msg.Payload)
			}
		}
	}()

	return ch, nil
}
