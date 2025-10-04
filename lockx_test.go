package lockx_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/yuninks/lockx"
)

var Redis *redis.Client

// func TestMain(m *testing.M) {
// 	client := redis.NewClient(&redis.Options{
// 		Addr:     "127.0.0.1" + ":" + "6379",
// 		Password: "", // no password set
// 		DB:       0,  // use default DB
// 	})
// 	if client == nil {
// 		fmt.Println("redis init error")
// 		return
// 	}
// 	// fmt.Println("ffff")
// 	Redis = client
// }

// func TestLockx(t *testing.T) {
// 	client := redis.NewClient(&redis.Options{
// 		Addr:     "127.0.0.1" + ":" + "6379",
// 		Password: "123456", // no password set
// 		DB:       0,        // use default DB
// 	})
// 	if client == nil {
// 		fmt.Println("redis init error")
// 		return
// 	}
// 	fmt.Println("begin")
// 	ctx := context.Background()
// 	ctx, cancel := context.WithCancel(ctx)
// 	defer cancel()

// 	wg := sync.WaitGroup{}

// 	for i := 0; i < 20; i++ {
// 		wg.Add(1)
// 		go func(i int) {
// 			defer wg.Done()
// 			lock, _ := lockx.NewGlobalLock(ctx, client, "lockx:test")
// 			if b, _ := lock.Lock(); !b {
// 				fmt.Println("lock error", i)
// 				return
// 			}
// 			defer lock.Unlock()

// 			fmt.Println("ssss2", i)

// 			time.Sleep(time.Second * 2)
// 		}(i)
// 		time.Sleep(time.Second)
// 	}

// 	wg.Wait()
// }

// MockLogger 用于测试的模拟日志器
type MockLogger struct {
	mock.Mock
}

func (m *MockLogger) Errorf(ctx context.Context, format string, args ...interface{}) {
	m.Called(ctx, format, args)
}

func (m *MockLogger) Infof(ctx context.Context, format string, args ...interface{}) {
	m.Called(ctx, format, args)
}

// 测试工具函数
func setupTestRedis(t *testing.T) (redis.UniversalClient, func()) {

	client := redis.NewClient(&redis.Options{
		Addr:     "127.0.0.1" + ":" + "6379",
		Password: "123456", // no password set
		DB:       0,        // use default DB
	})

	return client, func() {
		client.Close()
	}
}

func TestNewGlobalLock(t *testing.T) {
	redisClient, teardown := setupTestRedis(t)
	defer teardown()

	t.Run("成功创建锁", func(t *testing.T) {
		ctx := context.Background()
		lock, err := lockx.NewGlobalLock(ctx, redisClient, "test-key")

		require.NoError(t, err)
		assert.NotEmpty(t, lock.GetValue())
		assert.False(t, lock.IsClosed())
	})

	t.Run("创建锁时UUID生成失败", func(t *testing.T) {
		// 这个测试需要模拟uuid.NewV7()失败，在实际中较难触发
		// 通常可以跳过或者使用mock来测试
		t.Skip("很难模拟UUID生成失败的情况")
	})
}

func TestGlobalLock_Lock(t *testing.T) {
	redisClient, teardown := setupTestRedis(t)
	defer teardown()

	ctx := context.Background()

	t.Run("成功获取锁", func(t *testing.T) {
		lock, err := lockx.NewGlobalLock(ctx, redisClient, "test-key-success")
		require.NoError(t, err)

		success, err := lock.Lock()
		require.NoError(t, err)
		assert.True(t, success)

		defer lock.Unlock()
	})

	t.Run("获取已存在的锁失败", func(t *testing.T) {
		// 第一个客户端获取锁
		lock1, err := lockx.NewGlobalLock(ctx, redisClient, "test-key-conflict")
		require.NoError(t, err)

		success1, err := lock1.Lock()
		require.NoError(t, err)
		assert.True(t, success1)

		defer lock1.Unlock()

		// 第二个客户端尝试获取同一个锁
		lock2, err := lockx.NewGlobalLock(ctx, redisClient, "test-key-conflict")
		require.NoError(t, err)

		success2, err := lock2.Lock()
		require.NoError(t, err)
		assert.False(t, success2)

		defer lock2.Unlock()
	})

	t.Run("Redis连接失败", func(t *testing.T) {
		// 创建无效的Redis客户端
		invalidClient := redis.NewClient(&redis.Options{
			Addr: "invalid:6379",
		})

		_, err := lockx.NewGlobalLock(ctx, invalidClient, "test-key-fail")
		require.NoError(t, err)

		// 设置短超时
		ctxShort, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		defer cancel()

		lockWithCtx, err := lockx.NewGlobalLock(ctxShort, invalidClient, "test-key-fail")
		require.NoError(t, err)

		success, err := lockWithCtx.Lock()
		assert.Error(t, err)
		assert.False(t, success)
	})
}

func TestGlobalLock_Try(t *testing.T) {
	redisClient, teardown := setupTestRedis(t)
	defer teardown()

	ctx := context.Background()

	t.Run("尝试获取可用锁成功", func(t *testing.T) {
		_, err := lockx.NewGlobalLock(ctx, redisClient, "test-try-success")
		require.NoError(t, err)

		// 使用自定义选项
		customLock, err := lockx.NewGlobalLock(ctx, redisClient, "test-try-custom",
			lockx.WithMaxRetryTimes(2),
			lockx.WithRetryInterval(50*time.Millisecond),
		)
		require.NoError(t, err)

		success, err := customLock.Try()
		require.NoError(t, err)
		assert.True(t, success)

		defer customLock.Unlock()
	})

	t.Run("尝试获取被占用的锁失败", func(t *testing.T) {
		// 先获取锁
		lock1, err := lockx.NewGlobalLock(ctx, redisClient, "test-try-busy")
		require.NoError(t, err)

		success1, err := lock1.Lock()
		require.NoError(t, err)
		assert.True(t, success1)

		// 另一个客户端尝试获取
		lock2, err := lockx.NewGlobalLock(ctx, redisClient, "test-try-busy",
			lockx.WithMaxRetryTimes(2),
			lockx.WithRetryInterval(10*time.Millisecond),
		)
		require.NoError(t, err)

		success2, err := lock2.Try()
		require.NoError(t, err)
		assert.False(t, success2)

		lock1.Unlock()
		lock2.Unlock()
	})

	t.Run("上下文取消", func(t *testing.T) {
		ctxCancel, cancel := context.WithCancel(ctx)
		lock, err := lockx.NewGlobalLock(ctxCancel, redisClient, "test-try-cancel")
		require.NoError(t, err)

		cancel() // 立即取消

		success, err := lock.Try()
		assert.True(t, errors.Is(err, context.Canceled))
		assert.False(t, success)

		err = lock.Unlock()
		assert.Error(t, err)
	})
}

func TestGlobalLock_Unlock(t *testing.T) {
	redisClient, teardown := setupTestRedis(t)
	defer teardown()

	ctx := context.Background()

	t.Run("成功释放锁", func(t *testing.T) {
		lock, err := lockx.NewGlobalLock(ctx, redisClient, "test-unlock-success")
		require.NoError(t, err)

		success, err := lock.Lock()
		require.NoError(t, err)
		assert.True(t, success)

		err = lock.Unlock()
		assert.NoError(t, err)
		assert.True(t, lock.IsClosed())

		// 验证锁确实被释放了
		val, err := redisClient.Get(ctx, "test-unlock-success").Result()
		assert.Equal(t, redis.Nil, err)
		assert.Empty(t, val)
	})

	t.Run("重复释放锁", func(t *testing.T) {
		lock, err := lockx.NewGlobalLock(ctx, redisClient, "test-unlock-twice")
		require.NoError(t, err)

		success, err := lock.Lock()
		require.NoError(t, err)
		assert.True(t, success)

		err = lock.Unlock()
		assert.NoError(t, err)

		// 再次释放应该不会报错
		err = lock.Unlock()
		assert.NoError(t, err)
	})

	t.Run("释放其他客户端的锁", func(t *testing.T) {
		lock1, err := lockx.NewGlobalLock(ctx, redisClient, "test-unlock-other")
		require.NoError(t, err)

		success, err := lock1.Lock()
		require.NoError(t, err)
		assert.True(t, success)

		// 另一个客户端尝试释放（使用不同的value）
		lock2, err := lockx.NewGlobalLock(ctx, redisClient, "test-unlock-other")
		require.NoError(t, err)

		err = lock2.Unlock()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "lock was already released or owned by another client")

		lock1.Unlock()
		lock2.Unlock()
	})
}

func TestGlobalLock_Refresh(t *testing.T) {
	redisClient, teardown := setupTestRedis(t)
	defer teardown()

	ctx := context.Background()

	t.Run("自动刷新保持锁", func(t *testing.T) {
		lock, err := lockx.NewGlobalLock(ctx, redisClient, "test-refresh",
			lockx.WithExpiry(2*time.Second),
			lockx.WithRefreshPeriod(500*time.Millisecond),
		)
		require.NoError(t, err)

		success, err := lock.Lock()
		require.NoError(t, err)
		assert.True(t, success)

		// 等待一段时间，确保刷新机制工作
		time.Sleep(3 * time.Second)

		// 检查锁仍然存在
		val, err := redisClient.Get(ctx, "test-refresh").Result()
		require.NoError(t, err)
		assert.Equal(t, lock.GetValue(), val)

		// 检查TTL应该还在
		ttl, err := redisClient.TTL(ctx, "test-refresh").Result()
		require.NoError(t, err)
		assert.True(t, ttl > 0 && ttl <= 2*time.Second)

		lock.Unlock()
	})

	t.Run("锁丢失时停止刷新", func(t *testing.T) {
		mockLogger := new(MockLogger)
		mockLogger.On("Errorf", mock.Anything, mock.Anything, mock.Anything).Return()
		mockLogger.On("Infof", mock.Anything, mock.Anything, mock.Anything).Return()

		lock, err := lockx.NewGlobalLock(ctx, redisClient, "test-refresh-lost",
			lockx.WithLogger(mockLogger),
			lockx.WithExpiry(1*time.Second),
		)
		require.NoError(t, err)

		success, err := lock.Lock()
		require.NoError(t, err)
		assert.True(t, success)

		// 手动删除锁来模拟锁丢失
		err = redisClient.Del(ctx, "test-refresh-lost").Err()
		require.NoError(t, err)

		// 等待刷新周期
		time.Sleep(1500 * time.Millisecond)

		// 验证日志被调用
		mockLogger.AssertCalled(t, "Errorf", mock.Anything, mock.Anything, mock.Anything)

		// <-lock.GetCtx().Done()

		lock.Unlock()
	})
}

func TestGlobalLock_Concurrency(t *testing.T) {
	redisClient, teardown := setupTestRedis(t)
	defer teardown()

	ctx := context.Background()

	t.Run("多 goroutine 竞争锁", func(t *testing.T) {
		const numGoroutines = 100
		var successCount int
		var wg sync.WaitGroup
		var mu sync.Mutex

		wg.Add(numGoroutines)

		for i := 0; i < numGoroutines; i++ {
			go func(id int) {
				defer wg.Done()

				lock, err := lockx.NewGlobalLock(ctx, redisClient, "concurrent-test")
				require.NoError(t, err)

				success, err := lock.Lock()
				require.NoError(t, err)

				if success {
					mu.Lock()
					successCount++
					mu.Unlock()

					// 持有锁一段时间
					time.Sleep(100 * time.Millisecond)
					lock.Unlock()
				}
			}(i)
		}

		wg.Wait()

		// 应该只有一个goroutine成功获取锁
		assert.Equal(t, 1, successCount)
	})
}

func TestGlobalLock_ContextCancellation(t *testing.T) {
	redisClient, teardown := setupTestRedis(t)
	defer teardown()

	t.Run("上下文超时", func(t *testing.T) {
		ctxShort, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		lock, err := lockx.NewGlobalLock(ctxShort, redisClient, "test-context-timeout")
		require.NoError(t, err)

		// 等待上下文超时
		time.Sleep(200 * time.Millisecond)

		// 尝试操作应该失败
		success, err := lock.Lock()
		assert.Error(t, err)
		assert.False(t, success)
	})

	t.Run("上下文取消时自动释放锁", func(t *testing.T) {
		ctxCancel, cancel := context.WithCancel(context.Background())
		lock, err := lockx.NewGlobalLock(ctxCancel, redisClient, "test-context-cancel")
		require.NoError(t, err)

		success, err := lock.Lock()
		require.NoError(t, err)
		assert.True(t, success)

		// 取消上下文
		cancel()

		// lock.Unlock()

		// 等待自动清理
		time.Sleep(5 * time.Second)

		// 检查锁应该被释放
		val, err := redisClient.Get(context.Background(), "test-context-cancel").Result()
		assert.Equal(t, redis.Nil, err)
		assert.Empty(t, val)
	})
}

func TestGlobalLock_CustomOptions(t *testing.T) {
	redisClient, teardown := setupTestRedis(t)
	defer teardown()

	ctx := context.Background()

	t.Run("自定义配置选项", func(t *testing.T) {
		mockLogger := new(MockLogger)
		mockLogger.On("Errorf", mock.Anything, mock.Anything, mock.Anything).Return()
		mockLogger.On("Infof", mock.Anything, mock.Anything, mock.Anything).Return()

		lock, err := lockx.NewGlobalLock(ctx, redisClient, "test-custom-options",
			lockx.WithLockTimeout(5*time.Second),
			lockx.WithExpiry(10*time.Second),
			lockx.WithRefreshPeriod(2*time.Second),
			lockx.WithMaxRetryTimes(5),
			lockx.WithRetryInterval(200*time.Millisecond),
			lockx.WithLogger(mockLogger),
		)
		require.NoError(t, err)

		success, err := lock.Lock()
		require.NoError(t, err)
		assert.True(t, success)

		// 检查TTL是否正确设置
		ttl, err := redisClient.TTL(ctx, "test-custom-options").Result()
		require.NoError(t, err)
		assert.True(t, ttl > 8*time.Second && ttl <= 10*time.Second)

		lock.Unlock()
	})
}

func TestGlobalLock_EdgeCases(t *testing.T) {
	redisClient, teardown := setupTestRedis(t)
	defer teardown()

	ctx := context.Background()

	t.Run("空键名", func(t *testing.T) {
		_, err := lockx.NewGlobalLock(ctx, redisClient, "")
		require.Error(t, err)

	})

	t.Run("非常长的键名", func(t *testing.T) {
		longKey := string(make([]byte, 1000)) // 很长的键名
		lock, err := lockx.NewGlobalLock(ctx, redisClient, longKey)
		require.NoError(t, err)

		success, err := lock.Lock()
		require.NoError(t, err)
		assert.True(t, success)

		lock.Unlock()
	})
}

// 基准测试
func BenchmarkGlobalLock(b *testing.B) {
	t := testing.T{}
	redisClient, teardown := setupTestRedis(&t)
	defer teardown()

	ctx := context.Background()

	b.Run("获取释放锁", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			lock, err := lockx.NewGlobalLock(ctx, redisClient, fmt.Sprintf("benchmark-%d", i))
			if err != nil {
				b.Fatal(err)
			}

			success, err := lock.Lock()
			if err != nil {
				b.Fatal(err)
			}
			if !success {
				b.Fatal("Failed to acquire lock")
			}

			err = lock.Unlock()
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

func TestTimeout(t *testing.T) {
	redisClient, teardown := setupTestRedis(t)
	defer teardown()

	ctx := context.Background()

	begin := time.Now()

	lock, err := lockx.NewGlobalLock(ctx, redisClient, "test-timeout", lockx.WithLockTimeout(time.Second))
	require.NoError(t, err)
	success, err := lock.Lock()
	require.NoError(t, err)
	assert.True(t, success)

	<-lock.GetCtx().Done()
	t.Log("Done", time.Since(begin))

}

func TestRedisClose(t *testing.T) {
	redisClient, teardown := setupTestRedis(t)
	defer teardown()

	ctx := context.Background()

	begin := time.Now()

	lock, err := lockx.NewGlobalLock(ctx, redisClient, "test-timeout", lockx.WithLockTimeout(time.Hour))
	require.NoError(t, err)
	success, err := lock.Lock()
	require.NoError(t, err)
	assert.True(t, success)

	redisClient.Close()

	<-lock.GetCtx().Done()
	t.Log("Done", time.Since(begin))

}
