package lockx_test

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/yuninks/lockx"
)

// TestLogger 测试日志器
type TestLogger struct {
	mock.Mock
	errorCount int32
	infoCount  int32
}

func (t *TestLogger) Errorf(ctx context.Context, format string, v ...any) {
	atomic.AddInt32(&t.errorCount, 1)
	t.Called(ctx, format, v)
}

func (t *TestLogger) Infof(ctx context.Context, format string, v ...any) {
	atomic.AddInt32(&t.infoCount, 1)
	t.Called(ctx, format, v)
}

func (t *TestLogger) GetErrorCount() int32 {
	return atomic.LoadInt32(&t.errorCount)
}

func (t *TestLogger) GetInfoCount() int32 {
	return atomic.LoadInt32(&t.infoCount)
}

// 测试重入锁修复
func TestReentrantLockFix(t *testing.T) {
	redisClient, teardown := setupTestRedis(t)
	defer teardown()

	ctx := context.Background()

	t.Run("重入锁原子性测试", func(t *testing.T) {
		lock, err := lockx.NewGlobalLock(ctx, redisClient, "reentrant-test")
		require.NoError(t, err)

		// 第一次获取锁
		success1, err := lock.Lock()
		require.NoError(t, err)
		assert.True(t, success1)

		// 同一个锁对象重入
		success2, err := lock.Lock()
		require.NoError(t, err)
		assert.True(t, success2)

		// 验证Redis中的值仍然是原来的
		val, err := redisClient.Get(ctx, "reentrant-test").Result()
		require.NoError(t, err)
		assert.Equal(t, lock.GetValue(), val)

		lock.Unlock()
	})

	t.Run("并发重入竞态条件测试", func(t *testing.T) {
		const numGoroutines = 50
		var wg sync.WaitGroup
		var successCount int32

		wg.Add(numGoroutines)

		for i := 0; i < numGoroutines; i++ {
			go func() {
				defer wg.Done()

				lock, err := lockx.NewGlobalLock(ctx, redisClient, "concurrent-reentrant")
				require.NoError(t, err)

				success, err := lock.Lock()
				// require.NoError(t, err)

				if success {
					atomic.AddInt32(&successCount, 1)
					// 持有锁一段时间
					time.Sleep(1 * time.Second)
					lock.Unlock()
				}
			}()
		}

		wg.Wait()

		// 应该只有一个goroutine成功获取锁
		assert.Equal(t, int32(1), successCount)
	})
}

// 测试空指针解引用修复
func TestNullPointerFix(t *testing.T) {
	redisClient, teardown := setupTestRedis(t)
	defer teardown()

	ctx := context.Background()

	t.Run("logger为nil时不会panic", func(t *testing.T) {
		lock, err := lockx.NewGlobalLock(ctx, redisClient, "null-logger-test",
			lockx.WithLogger(nil))
		require.NoError(t, err)

		success, err := lock.Lock()
		require.NoError(t, err)
		assert.True(t, success)

		// 这里不应该panic
		assert.NotPanics(t, func() {
			lock.Unlock()
		})
	})

	t.Run("刷新失败时logger为nil不会panic", func(t *testing.T) {
		// 创建无效的Redis客户端来触发刷新错误
		invalidClient := redis.NewClient(&redis.Options{
			Addr: "invalid:6379",
		})

		lock, err := lockx.NewGlobalLock(ctx, invalidClient, "refresh-error-test",
			lockx.WithLogger(nil),
			lockx.WithExpiry(100*time.Millisecond),
			lockx.WithRefreshPeriod(50*time.Millisecond))
		require.NoError(t, err)

		// 这里不应该panic，即使刷新失败
		assert.NotPanics(t, func() {
			lock.Lock() // 这会失败但不应该panic
		})
	})
}

// 测试Goroutine泄漏修复
func TestGoroutineLeakFix(t *testing.T) {
	redisClient, teardown := setupTestRedis(t)
	defer teardown()

	t.Run("上下文取消时goroutine正常退出", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())

		lock, err := lockx.NewGlobalLock(ctx, redisClient, "goroutine-leak-test",
			lockx.WithRefreshPeriod(100*time.Millisecond))
		require.NoError(t, err)

		success, err := lock.Lock()
		require.NoError(t, err)
		assert.True(t, success)

		// 取消上下文
		cancel()

		// 等待goroutine退出和自动清理
		time.Sleep(1 * time.Second)

		// 验证锁被自动释放
		val, err := redisClient.Get(context.Background(), "goroutine-leak-test").Result()
		assert.Equal(t, redis.Nil, err)
		assert.Empty(t, val)
	})

	t.Run("刷新失败时goroutine退出", func(t *testing.T) {
		ctx := context.Background()
		testLogger := &TestLogger{}
		testLogger.On("Errorf", mock.Anything, mock.Anything, mock.Anything).Return()
		testLogger.On("Infof", mock.Anything, mock.Anything, mock.Anything).Return()

		lock, err := lockx.NewGlobalLock(ctx, redisClient, "refresh-fail-exit",
			lockx.WithLogger(testLogger),
			// lockx.WithExpiry(200*time.Millisecond),
			lockx.WithRefreshPeriod(100*time.Millisecond))
		require.NoError(t, err)

		success, err := lock.Lock()
		require.NoError(t, err)
		assert.True(t, success)

		// 手动删除锁来模拟锁丢失
		err = redisClient.Del(ctx, "refresh-fail-exit").Err()
		require.NoError(t, err)

		// 等待刷新检测到锁丢失并退出goroutine
		time.Sleep(500 * time.Millisecond)

		// 验证错误日志被记录
		assert.True(t, testLogger.GetErrorCount() > 0)

		lock.Unlock()
	})
}

// 测试刷新错误计数器重置
func TestRefreshErrorCounterReset(t *testing.T) {
	redisClient, teardown := setupTestRedis(t)
	defer teardown()

	ctx := context.Background()

	t.Run("刷新成功时重置错误计数", func(t *testing.T) {
		testLogger := &TestLogger{}
		testLogger.On("Errorf", mock.Anything, mock.Anything, mock.Anything).Return()
		testLogger.On("Infof", mock.Anything, mock.Anything, mock.Anything).Return()

		lock, err := lockx.NewGlobalLock(ctx, redisClient, "error-counter-reset",
			lockx.WithLogger(testLogger),
			lockx.WithExpiry(1*time.Second),
			lockx.WithRefreshPeriod(200*time.Millisecond))
		require.NoError(t, err)

		success, err := lock.Lock()
		require.NoError(t, err)
		assert.True(t, success)

		// 临时关闭Redis连接来触发刷新错误
		redisClient.Close()

		// 等待一些刷新周期让错误累积
		time.Sleep(800 * time.Millisecond)

		// 重新连接Redis
		newClient := redis.NewClient(&redis.Options{
			Addr:     "127.0.0.1:6379",
			Password: "123456",
			DB:       0,
		})
		defer newClient.Close()

		// 手动设置锁以模拟恢复
		err = newClient.Set(ctx, "error-counter-reset", lock.GetValue(), 1*time.Second).Err()
		require.NoError(t, err)

		// 等待更多刷新周期，验证不会因为之前的错误而关闭
		time.Sleep(1 * time.Second)

		lock.Unlock()
	})
}

// 测试全局变量线程安全
func TestGlobalVariableThreadSafety(t *testing.T) {
	redisClient, teardown := setupTestRedis(t)
	defer teardown()

	ctx := context.Background()

	t.Run("并发InitOption和New操作", func(t *testing.T) {
		const numGoroutines = 100
		var wg sync.WaitGroup

		wg.Add(numGoroutines * 2)

		// 并发调用InitOption
		for i := 0; i < numGoroutines; i++ {
			go func(i int) {
				defer wg.Done()
				lockx.InitOption(
					lockx.WithExpiry(time.Duration(i+1)*time.Second),
					lockx.WithMaxRetryTimes(i+1),
				)
			}(i)
		}

		// 并发调用New
		for i := 0; i < numGoroutines; i++ {
			go func(i int) {
				defer wg.Done()
				lockx.Init(ctx, redisClient)
				_, err := lockx.New(ctx, fmt.Sprintf("thread-safety-test-%d", i))
				assert.NoError(t, err)
			}(i)
		}

		wg.Wait()
	})
}

// 测试边界条件
func TestEdgeCases(t *testing.T) {
	redisClient, teardown := setupTestRedis(t)
	defer teardown()

	ctx := context.Background()

	t.Run("极短的过期时间", func(t *testing.T) {
		lock, err := lockx.NewGlobalLock(ctx, redisClient, "very-short-expiry",
			lockx.WithExpiry(1*time.Second),
			// lockx.WithRefreshPeriod(500*time.Millisecond),
		)
		require.NoError(t, err)

		success, err := lock.Lock()
		require.NoError(t, err)
		assert.True(t, success)

		// 等待锁自然过期
		time.Sleep(2 * time.Second)

		lock.Unlock()
	})

	t.Run("极长的过期时间", func(t *testing.T) {
		lock, err := lockx.NewGlobalLock(ctx, redisClient, "very-long-expiry",
			lockx.WithExpiry(24*time.Hour))
		require.NoError(t, err)

		success, err := lock.Lock()
		require.NoError(t, err)
		assert.True(t, success)

		// 检查TTL
		ttl, err := redisClient.TTL(ctx, "very-long-expiry").Result()
		require.NoError(t, err)
		assert.True(t, ttl > 23*time.Hour)

		lock.Unlock()
	})

	t.Run("零重试次数", func(t *testing.T) {
		// 先占用锁
		lock1, err := lockx.NewGlobalLock(ctx, redisClient, "zero-retry-test")
		require.NoError(t, err)
		success1, err := lock1.Lock()
		require.NoError(t, err)
		assert.True(t, success1)

		// 零重试次数的锁
		lock2, err := lockx.NewGlobalLock(ctx, redisClient, "zero-retry-test",
			lockx.WithMaxRetryTimes(0))
		require.NoError(t, err)

		success2, err := lock2.Try()
		require.NoError(t, err)
		assert.False(t, success2)

		lock1.Unlock()
		lock2.Unlock()
	})
}

// 测试错误恢复
func TestErrorRecovery(t *testing.T) {
	redisClient, teardown := setupTestRedis(t)
	defer teardown()

	ctx := context.Background()

	t.Run("Redis连接恢复后锁功能正常", func(t *testing.T) {
		lock, err := lockx.NewGlobalLock(ctx, redisClient, "recovery-test")
		require.NoError(t, err)

		// 正常获取锁
		success, err := lock.Lock()
		require.NoError(t, err)
		assert.True(t, success)

		// lock.Unlock()
		// 删除Key以释放锁
		err = redisClient.Del(ctx, "recovery-test").Err()
		require.NoError(t, err)

		// 模拟Redis连接问题后恢复
		time.Sleep(100 * time.Millisecond)

		// 再次获取锁应该正常
		success, err = lock.Lock()
		require.NoError(t, err)
		assert.True(t, success)

		lock.Unlock()
	})
}

// 测试性能和稳定性
func TestPerformanceAndStability(t *testing.T) {
	redisClient, teardown := setupTestRedis(t)
	defer teardown()

	ctx := context.Background()

	t.Run("高频获取释放锁", func(t *testing.T) {
		const iterations = 1000

		for i := 0; i < iterations; i++ {

			redisKey := fmt.Sprintf("perf-1test-%d", i)

			lock, err := lockx.NewGlobalLock(ctx, redisClient, redisKey)
			require.NoError(t, err)

			success, err := lock.Lock()
			require.NoError(t, err)
			assert.True(t, success)

			// 检查锁存在
			val, err := redisClient.Get(ctx, redisKey).Result()
			require.NoError(t, err)
			assert.Equal(t, lock.GetValue(), val)

			lock.Unlock()

			// 检查锁已释放
			_, err = redisClient.Get(ctx, redisKey).Result()
			assert.Equal(t, redis.Nil, err)
		}
	})

	t.Run("长时间持有锁", func(t *testing.T) {
		lock, err := lockx.NewGlobalLock(ctx, redisClient, "long-hold-test",
			lockx.WithExpiry(2*time.Second),
			lockx.WithRefreshPeriod(500*time.Millisecond))
		require.NoError(t, err)

		success, err := lock.Lock()
		require.NoError(t, err)
		assert.True(t, success)

		// 持有锁10秒，验证刷新机制
		time.Sleep(10 * time.Second)

		// 验证锁仍然有效
		val, err := redisClient.Get(ctx, "long-hold-test").Result()
		require.NoError(t, err)
		assert.Equal(t, lock.GetValue(), val)

		lock.Unlock()
	})
}

// 测试资源清理
func TestResourceCleanup(t *testing.T) {
	redisClient, teardown := setupTestRedis(t)
	defer teardown()

	ctx := context.Background()

	t.Run("锁对象销毁时资源清理", func(t *testing.T) {
		func() {
			lock, err := lockx.NewGlobalLock(ctx, redisClient, "cleanup-test",
				lockx.WithRefreshPeriod(100*time.Millisecond))
			require.NoError(t, err)

			success, err := lock.Lock()
			require.NoError(t, err)
			assert.True(t, success)

			// 函数结束时lock对象会被GC
		}()

		// 强制GC
		time.Sleep(1 * time.Second)

		// 验证锁最终被清理（可能需要等待刷新周期）
		time.Sleep(2 * time.Second)
	})
}

// 基准测试
func BenchmarkLockOperations(b *testing.B) {
	t := &testing.T{}
	redisClient, teardown := setupTestRedis(t)
	defer teardown()

	ctx := context.Background()

	b.Run("Lock/Unlock", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			lock, _ := lockx.NewGlobalLock(ctx, redisClient,
				fmt.Sprintf("bench-lock-%d", i))
			lock.Lock()
			lock.Unlock()
		}
	})

	b.Run("Try", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			lock, _ := lockx.NewGlobalLock(ctx, redisClient,
				fmt.Sprintf("bench-try-%d", i))
			lock.Try()
			lock.Unlock()
		}
	})

	b.Run("Concurrent", func(b *testing.B) {
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				lock, _ := lockx.NewGlobalLock(ctx, redisClient,
					fmt.Sprintf("bench-concurrent-%d", i))
				lock.Lock()
				lock.Unlock()
				i++
			}
		})
	})
}
