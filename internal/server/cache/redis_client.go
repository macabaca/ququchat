package cache

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisOptions struct {
	Addr           string
	Password       string
	DB             int
	KeyPrefix      string
	DialTimeoutMs  int
	ReadTimeoutMs  int
	WriteTimeoutMs int
}

type RedisClient struct {
	raw       *redis.Client
	keyPrefix string
}

func NewRedisClient(opts RedisOptions) *RedisClient {
	addr := strings.TrimSpace(opts.Addr)
	if addr == "" {
		addr = "127.0.0.1:6379"
	}
	dialTimeout := 2 * time.Second
	if opts.DialTimeoutMs > 0 {
		dialTimeout = time.Duration(opts.DialTimeoutMs) * time.Millisecond
	}
	readTimeout := 1 * time.Second
	if opts.ReadTimeoutMs > 0 {
		readTimeout = time.Duration(opts.ReadTimeoutMs) * time.Millisecond
	}
	writeTimeout := 1 * time.Second
	if opts.WriteTimeoutMs > 0 {
		writeTimeout = time.Duration(opts.WriteTimeoutMs) * time.Millisecond
	}
	keyPrefix := strings.TrimSpace(opts.KeyPrefix)
	if keyPrefix == "" {
		keyPrefix = DefaultKeyPrefix
	}
	raw := redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     opts.Password,
		DB:           opts.DB,
		DialTimeout:  dialTimeout,
		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
	})
	return &RedisClient{
		raw:       raw,
		keyPrefix: keyPrefix,
	}
}

func (c *RedisClient) Ping(ctx context.Context) error {
	return c.raw.Ping(ctx).Err()
}

func (c *RedisClient) Close() error {
	return c.raw.Close()
}

func (c *RedisClient) BuildKey(parts ...string) string {
	filtered := make([]string, 0, len(parts)+1)
	filtered = append(filtered, c.keyPrefix)
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed == "" {
			continue
		}
		filtered = append(filtered, trimmed)
	}
	return strings.Join(filtered, ":")
}

func (c *RedisClient) GetString(ctx context.Context, key string) (string, bool, error) {
	val, err := c.raw.Get(ctx, strings.TrimSpace(key)).Result()
	if err == redis.Nil {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return val, true, nil
}

func (c *RedisClient) SetString(ctx context.Context, key string, value string, ttl time.Duration) error {
	return c.raw.Set(ctx, strings.TrimSpace(key), value, ttl).Err()
}

func (c *RedisClient) GetJSON(ctx context.Context, key string, out interface{}) (bool, error) {
	val, ok, err := c.GetString(ctx, key)
	if err != nil || !ok {
		return ok, err
	}
	if err := json.Unmarshal([]byte(val), out); err != nil {
		return false, err
	}
	return true, nil
}

func (c *RedisClient) SetJSON(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return c.SetString(ctx, key, string(b), ttl)
}

func (c *RedisClient) Del(ctx context.Context, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	return c.raw.Del(ctx, keys...).Err()
}
