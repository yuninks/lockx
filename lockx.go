package lockx

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/google/uuid"
)

// 全局锁
type GlobalLock struct {
	redis           redis.UniversalClient
	ctx             context.Context
	cancel          context.CancelFunc
	uniqueKey       string
	value           string
	isClosed        bool
	closeLock       sync.RWMutex
	options         *option
	refreshErrCount int64 // 刷新错误次数
}

func NewGlobalLock(ctx context.Context, red redis.UniversalClient, uniqueKey string, opts ...Option) (*GlobalLock, error) {
	if uniqueKey == "" {
		return nil, fmt.Errorf("uniqueKey is empty")
	}
	if red == nil {
		return nil, fmt.Errorf("redis is nil")
	}

	options := defaultOption()
	for _, opt := range opts {
		opt(options)
	}

	ctx, cancel := context.WithTimeout(ctx, options.lockTimeout)

	u, err := uuid.NewV7()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to generate UUID: %w", err)
	}

	return &GlobalLock{
		redis:     red,
		ctx:       ctx,
		cancel:    cancel,
		uniqueKey: uniqueKey,
		value:     u.String(),
		options:   options,
	}, nil
}

// 获取上下文
func (l *GlobalLock) GetCtx() context.Context {
	return l.ctx
}

// 获取锁
func (g *GlobalLock) Lock() (bool, error) {
	script := `
	if redis.call('set', KEYS[1], ARGV[1], 'NX', 'EX', ARGV[2]) then
		return 'OK'
	else
		local current_val = redis.call('get', KEYS[1])
		if current_val == ARGV[1] then
			redis.call('expire', KEYS[1], ARGV[2])
			return 'OK'
		else
			return 'ERROR'
		end
	end
	`

	resp, err := g.redis.Eval(g.ctx, script, []string{g.uniqueKey}, g.value, int(g.options.Expiry.Seconds())).Result()
	if err != nil {
		if g.options.logger != nil {
			g.options.logger.Errorf(g.ctx, "global lock failed: %v, key: %s, value: %s", err, g.uniqueKey, g.value)
		}
		return false, err
	}

	if resp == "OK" {
		g.startRefresh()
		return true, nil
	}

	return false, nil
}

// 尝试获取锁
func (g *GlobalLock) Try() (bool, error) {
	for i := 0; i < g.options.MaxRetryTimes; i++ {
		success, err := g.Lock()
		if err != nil {
			return false, err
		}
		if success {
			return true, nil
		}

		select {
		case <-time.After(g.options.RetryInterval):
			continue
		case <-g.ctx.Done():
			return false, g.ctx.Err()
		}
	}
	return false, nil
}

// 删除锁
func (g *GlobalLock) Unlock() error {
	// 已经关闭就不需要重复关闭
	if g.setOrGetClose() {
		// g.options.logger.Infof(g.ctx, "global lock already closed, key: %s, value: %s", g.uniqueKey, g.value)
		return nil
	}

	script := `
	if redis.call('get', KEYS[1]) == ARGV[1] then
		return redis.call('del', KEYS[1])
	else
		return 0
	end
	`

	// 避免上游已取消执行不了
	ctx := context.WithoutCancel(g.ctx)

	resp, err := g.redis.Eval(ctx, script, []string{g.uniqueKey}, g.value).Result()
	if err != nil {
		if g.options.logger != nil {
			g.options.logger.Infof(g.ctx, "global unlock may have failed: %v, key: %s, value: %s", err, g.uniqueKey, g.value)
		}
		// 即使删除失败也继续执行，因为锁可能会自动过期
	}

	if delCount, ok := resp.(int64); ok && delCount == 1 {
		return nil
	}
	g.options.logger.Infof(g.ctx, "global unlock may have failed: %v, key: %s, value: %s", err, g.uniqueKey, g.value)
	return fmt.Errorf("lock was already released or owned by another client")
}

// 启动刷新goroutine
func (g *GlobalLock) startRefresh() {

	go func() {

		ticker := time.NewTicker(g.options.RefreshPeriod)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				g.refreshExec() 
			case <-g.ctx.Done():
				// g.options.logger.Infof(g.ctx, "global lock refresh canceled, key: %s, value: %s", g.uniqueKey, g.value)
				g.Unlock()
				return
			}
		}
	}()
}

// 返回原来的
func (l *GlobalLock) setOrGetClose() (readyClosed bool) {
	l.closeLock.Lock()
	defer l.closeLock.Unlock()

	if l.isClosed {
		return true
	}

	l.isClosed = true

	l.cancel()

	return false
}

// 执行刷新操作
func (g *GlobalLock) refreshExec() bool {
	if g.IsClosed() {
		g.options.logger.Infof(g.ctx, "global lock already closed, key: %s, value: %s", g.uniqueKey, g.value)
		return false
	}

	script := `
	if redis.call('get', KEYS[1]) == ARGV[1] then
		redis.call('expire', KEYS[1], ARGV[2])
		return 1
	else
		return 0
	end
	`

	resp, err := g.redis.Eval(g.ctx, script, []string{g.uniqueKey}, g.value, int(g.options.Expiry.Seconds())).Result()
	if err != nil {
		if g.options.logger != nil {
			g.options.logger.Errorf(g.ctx, "global refresh failed: %v, key: %s, value: %s", err, g.uniqueKey, g.value)
		}

		newCount := atomic.AddInt64(&g.refreshErrCount, 1)
		if newCount >= 3 {
			// 关闭锁
			g.setOrGetClose()
			g.options.logger.Errorf(g.ctx, "global refresh failed, lock may be lost 3 times, key: %s, value: %s", g.uniqueKey, g.value)
		}
		return false
	}

	if refreshed, ok := resp.(int64); !ok || refreshed != 1 {
		if g.options.logger != nil {
			g.options.logger.Errorf(g.ctx, "global refresh failed, lock may be lost, key: %s, value: %s", g.uniqueKey, g.value)
		}
		// 关闭锁
		g.setOrGetClose()
		return false
	}

	return true
}

// 检查锁是否已关闭
func (g *GlobalLock) IsClosed() bool {
	g.closeLock.RLock()
	defer g.closeLock.RUnlock()
	return g.isClosed
}

func (l *GlobalLock) GetValue() string {
	return l.value
}
