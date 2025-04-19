package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-redis/redis/v8"
)

// StorageClient defines the interface for client/channel storage
type StorageClient interface {
	// Client operations
	SaveClient(ctx context.Context, client *ClientInfo) error
	GetClient(ctx context.Context, clientID string) (*ClientInfo, error)
	RemoveClient(ctx context.Context, clientID string) error
	UpdateClientLastSeen(ctx context.Context, clientID string) error

	// Channel operations
	AddClientToChannel(ctx context.Context, channelName, clientID string) error
	RemoveClientFromChannel(ctx context.Context, channelName, clientID string) error
	GetChannelClients(ctx context.Context, channelName string) ([]string, error)

	// Cleanup
	CleanupStaleClients(ctx context.Context, timeout time.Duration) error
}

// ClientInfo represents client data stored in Redis
type ClientInfo struct {
	ClientID  string                 `json:"client_id"`
	UserID    string                 `json:"user_id"`
	Metadata  map[string]interface{} `json:"metadata"`
	CreatedAt time.Time              `json:"created_at"`
	LastSeen  time.Time              `json:"last_seen"`
}

// RedisStorage implements StorageClient using Redis
type RedisStorage struct {
	client *redis.Client
}

const (
	clientPrefix  = "ws:client:"
	channelPrefix = "ws:channel:"
	clientExpiry  = 24 * time.Hour
	channelExpiry = 24 * time.Hour
)

func NewRedisStorage(redisAddr string, options ...func(*redis.Options)) *RedisStorage {
	opt := newRedisConfig(options...)
	opt.Addr = redisAddr

	return &RedisStorage{client: redis.NewClient(opt)}
}

func (rs *RedisStorage) SaveClient(ctx context.Context, client *ClientInfo) error {
	data, err := json.Marshal(client)
	if err != nil {
		return fmt.Errorf("failed to marshal client: %w", err)
	}

	key := clientPrefix + client.ClientID
	return rs.client.Set(ctx, key, data, clientExpiry).Err()
}

func (rs *RedisStorage) GetClient(ctx context.Context, clientID string) (*ClientInfo, error) {
	key := clientPrefix + clientID
	data, err := rs.client.Get(ctx, key).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, nil
		}
		return nil, err
	}

	var client ClientInfo
	if err := json.Unmarshal(data, &client); err != nil {
		return nil, fmt.Errorf("failed to unmarshal client: %w", err)
	}
	return &client, nil
}

func (rs *RedisStorage) RemoveClient(ctx context.Context, clientID string) error {
	key := clientPrefix + clientID
	return rs.client.Del(ctx, key).Err()
}

func (rs *RedisStorage) UpdateClientLastSeen(ctx context.Context, clientID string) error {
	client, err := rs.GetClient(ctx, clientID)
	if err != nil {
		return err
	}
	if client == nil {
		return fmt.Errorf("client not found: %s", clientID)
	}

	client.LastSeen = time.Now()
	return rs.SaveClient(ctx, client)
}

func (rs *RedisStorage) AddClientToChannel(ctx context.Context, channelName, clientID string) error {
	key := channelPrefix + channelName
	return rs.client.SAdd(ctx, key, clientID).Err()
}

func (rs *RedisStorage) RemoveClientFromChannel(ctx context.Context, channelName, clientID string) error {
	key := channelPrefix + channelName
	return rs.client.SRem(ctx, key, clientID).Err()
}

func (rs *RedisStorage) GetChannelClients(ctx context.Context, channelName string) ([]string, error) {
	key := channelPrefix + channelName
	return rs.client.SMembers(ctx, key).Result()
}

func (rs *RedisStorage) CleanupStaleClients(ctx context.Context, timeout time.Duration) error {
	// Implement cleanup logic for stale clients
	pattern := clientPrefix + "*"
	iter := rs.client.Scan(ctx, 0, pattern, 0).Iterator()

	for iter.Next(ctx) {
		key := iter.Val()
		data, err := rs.client.Get(ctx, key).Bytes()
		if err != nil {
			continue
		}

		var client ClientInfo
		if err := json.Unmarshal(data, &client); err != nil {
			continue
		}

		if time.Since(client.LastSeen) > timeout {
			clientID := key[len(clientPrefix):]
			rs.RemoveClient(ctx, clientID)
		}
	}
	return iter.Err()
}
