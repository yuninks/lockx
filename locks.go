package lockx

import (
	"context"
	"fmt"

	"github.com/go-redis/redis/v8"
)

// 简单使用

var redisConn redis.UniversalClient

// 初始化redis连接
func Init(ctx context.Context, redis redis.UniversalClient, opts ...Option) error {
	redisConn = redis
	InitOption(opts...)
	return nil
}

// 新起一个锁对象
// 先Init后New再Lock
func New(ctx context.Context, uniqueKey string) (*GlobalLock, error) {
	if redisConn == nil {
		return nil, fmt.Errorf("redis client is nil")
	}
	return NewGlobalLock(ctx, redisConn, uniqueKey, globalOpts...)
}
