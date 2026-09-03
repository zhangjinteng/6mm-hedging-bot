package lock

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const unlockScript = `
if redis.call("get", KEYS[1]) == ARGV[1] then
	return redis.call("del", KEYS[1])
end
return 0
`

type RedisLocker struct {
	client *redis.Client
}

func NewRedisLocker(client *redis.Client) *RedisLocker {
	return &RedisLocker{client: client}
}

func (l *RedisLocker) TryLock(ctx context.Context, key string, ttl time.Duration) (func(context.Context) error, bool, error) {
	if l == nil || l.client == nil {
		return func(context.Context) error { return nil }, true, nil
	}
	token, err := randomToken()
	if err != nil {
		return nil, false, err
	}
	ok, err := l.client.SetNX(ctx, key, token, ttl).Result()
	if err != nil {
		return nil, false, fmt.Errorf("acquire redis lock %s: %w", key, err)
	}
	if !ok {
		return nil, false, nil
	}
	release := func(ctx context.Context) error {
		if err := l.client.Eval(ctx, unlockScript, []string{key}, token).Err(); err != nil {
			return fmt.Errorf("release redis lock %s: %w", key, err)
		}
		return nil
	}
	return release, true, nil
}

func randomToken() (string, error) {
	var data [16]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "", fmt.Errorf("generate lock token: %w", err)
	}
	return hex.EncodeToString(data[:]), nil
}
