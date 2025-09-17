package lockx

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
)

// 全局锁
type GlobalLock struct {
	redis       redis.UniversalClient
	ctx         context.Context
	cancel      context.CancelFunc
	uniqueKey   string
	value       string
	isClosed    bool
	closeLock   sync.RWMutex
	options     *option
	stopRefresh chan struct{}
	wg          sync.WaitGroup
}

func NewGlobalLock(ctx context.Context, red redis.UniversalClient, uniqueKey string, opts ...Option) (*GlobalLock, error) {
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
		redis:       red,
		ctx:         ctx,
		cancel:      cancel,
		uniqueKey:   uniqueKey,
		value:       u.String(),
		stopRefresh: make(chan struct{}),
		options:     options,
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
	g.closeLock.Lock()
	defer g.closeLock.Unlock()

	if g.isClosed {
		return nil
	}
	g.isClosed = true

	// 停止刷新goroutine
	close(g.stopRefresh)
	g.wg.Wait()

	script := `
	if redis.call('get', KEYS[1]) == ARGV[1] then
		return redis.call('del', KEYS[1])
	else
		return 0
	end
	`

	resp, err := g.redis.Eval(g.ctx, script, []string{g.uniqueKey}, g.value).Result()
	if err != nil {
		if g.options.logger != nil {
			g.options.logger.Infof(g.ctx, "global unlock may have failed: %v, key: %s, value: %s", err, g.uniqueKey, g.value)
		}
		// 即使删除失败也继续执行，因为锁可能会自动过期
	}

	g.cancel()

	if delCount, ok := resp.(int64); ok && delCount == 1 {
		return nil
	}
	return fmt.Errorf("lock was already released or owned by another client")
}

// 启动刷新goroutine
func (g *GlobalLock) startRefresh() {
	g.wg.Add(1)
	go func() {
		defer g.wg.Done()

		ticker := time.NewTicker(g.options.RefreshPeriod)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if !g.refreshExec() {
					return
				}
			case <-g.stopRefresh:
				return
			case <-g.ctx.Done():
				g.Unlock()
				return
			}
		}
	}()
}

// 执行刷新操作
func (g *GlobalLock) refreshExec() bool {
	g.closeLock.RLock()
	defer g.closeLock.RUnlock()

	if g.isClosed {
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
		return false
	}

	if refreshed, ok := resp.(int64); !ok || refreshed != 1 {
		if g.options.logger != nil {
			g.options.logger.Errorf(g.ctx, "global refresh failed, lock may be lost, key: %s, value: %s", g.uniqueKey, g.value)
		}
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
