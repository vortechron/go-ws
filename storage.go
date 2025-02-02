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
	GetClient(ctx context.Context, userID string) (*ClientInfo, error)
	RemoveClient(ctx context.Context, userID string) error
	UpdateClientLastSeen(ctx context.Context, userID string) error

	// Channel operations
	AddClientToChannel(ctx context.Context, channelName, userID string) error
	RemoveClientFromChannel(ctx context.Context, channelName, userID string) error
	GetChannelClients(ctx context.Context, channelName string) ([]string, error)

	// Cleanup
	CleanupStaleClients(ctx context.Context, timeout time.Duration) error
}

// ClientInfo represents client data stored in Redis
type ClientInfo struct {
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

func NewRedisStorage(redisAddr string) *RedisStorage {
	client := redis.NewClient(&redis.Options{
		Addr: redisAddr,
	})
	return &RedisStorage{client: client}
}

func (rs *RedisStorage) SaveClient(ctx context.Context, client *ClientInfo) error {
	data, err := json.Marshal(client)
	if err != nil {
		return fmt.Errorf("failed to marshal client: %w", err)
	}

	key := clientPrefix + client.UserID
	return rs.client.Set(ctx, key, data, clientExpiry).Err()
}

func (rs *RedisStorage) GetClient(ctx context.Context, userID string) (*ClientInfo, error) {
	key := clientPrefix + userID
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

func (rs *RedisStorage) RemoveClient(ctx context.Context, userID string) error {
	key := clientPrefix + userID
	return rs.client.Del(ctx, key).Err()
}

func (rs *RedisStorage) UpdateClientLastSeen(ctx context.Context, userID string) error {
	client, err := rs.GetClient(ctx, userID)
	if err != nil {
		return err
	}
	if client == nil {
		return fmt.Errorf("client not found: %s", userID)
	}

	client.LastSeen = time.Now()
	return rs.SaveClient(ctx, client)
}

func (rs *RedisStorage) AddClientToChannel(ctx context.Context, channelName, userID string) error {
	key := channelPrefix + channelName
	return rs.client.SAdd(ctx, key, userID).Err()
}

func (rs *RedisStorage) RemoveClientFromChannel(ctx context.Context, channelName, userID string) error {
	key := channelPrefix + channelName
	return rs.client.SRem(ctx, key, userID).Err()
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
			userID := key[len(clientPrefix):]
			rs.RemoveClient(ctx, userID)
		}
	}
	return iter.Err()
}
