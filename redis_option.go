package ws

import "github.com/go-redis/redis/v8"

func WithPassword(password string) func(*redis.Options) {
	return func(rs *redis.Options) {
		rs.Password = password
	}
}

func WithDB(db int) func(*redis.Options) {
	return func(rs *redis.Options) {
		rs.DB = db
	}
}

func newRedisConfig(options ...func(*redis.Options)) *redis.Options {
	redisConfig := &redis.Options{
		Addr:     "localhost:6379",
		Password: "",
		DB:       0,
	}
	for _, option := range options {
		option(redisConfig)
	}
	return redisConfig
}
