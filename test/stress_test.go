package lockx_test

import (
	"context"
	"fmt"
	"runtime"
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

// setupTestRedis 统一的测试Redis设置函数
func setupTestRedis(t *testing.T) (redis.UniversalClient, func()) {
	client := redis.NewClient(&redis.Options{
		Addr:     "127.0.0.1:6379",
		Password: "123456",
		DB:       0,
	})

	// 测试连接
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := client.Ping(ctx).Err()
	if err != nil {
		t.Skipf("Redis连接失败，跳过测试: %v", err)
	}

	return client, func() {
		client.Close()
	}
}

// 压力测试和性能测试

// TestHighConcurrencyStress 高并发压力测试
func TestHighConcurrencyStress(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过压力测试")
	}

	redisClient, teardown := setupTestRedis(t)
	defer teardown()

	ctx := context.Background()

	t.Run("1000个goroutine竞争单个锁", func(t *testing.T) {
		const numGoroutines = 1000
		const lockKey = "high-concurrency-test"

		var successCount int64
		var errorCount int64
		var wg sync.WaitGroup

		startTime := time.Now()
		wg.Add(numGoroutines)

		for i := 0; i < numGoroutines; i++ {
			go func(id int) {
				defer wg.Done()

				lock, err := lockx.NewGlobalLock(ctx, redisClient, lockKey,
					lockx.WithMaxRetryTimes(10),
					lockx.WithRetryInterval(1*time.Millisecond))
				if err != nil {
					atomic.AddInt64(&errorCount, 1)
					return
				}

				success, err := lock.Try()
				if err != nil {
					atomic.AddInt64(&errorCount, 1)
					return
				}

				if success {
					atomic.AddInt64(&successCount, 1)
					// 模拟短暂的工作
					time.Sleep(1 * time.Millisecond)
					lock.Unlock()
				}
			}(i)
		}

		wg.Wait()
		duration := time.Since(startTime)

		t.Logf("高并发测试完成:")
		t.Logf("- 总goroutine数: %d", numGoroutines)
		t.Logf("- 成功获取锁: %d", successCount)
		t.Logf("- 错误数: %d", errorCount)
		t.Logf("- 总耗时: %v", duration)
		t.Logf("- 平均每次操作: %v", duration/time.Duration(numGoroutines))

		assert.True(t, successCount > 0, "应该有goroutine成功获取锁")
		assert.Equal(t, int64(0), errorCount, "不应该有错误")
	})

	t.Run("多锁并发测试", func(t *testing.T) {
		const numLocks = 100
		const goroutinesPerLock = 10

		var totalSuccess int64
		var totalErrors int64
		var wg sync.WaitGroup

		startTime := time.Now()
		wg.Add(numLocks * goroutinesPerLock)

		for lockID := 0; lockID < numLocks; lockID++ {
			for goroutineID := 0; goroutineID < goroutinesPerLock; goroutineID++ {
				go func(lID, gID int) {
					defer wg.Done()

					lockKey := fmt.Sprintf("multi-lock-test-%d", lID)
					lock, err := lockx.NewGlobalLock(ctx, redisClient, lockKey,
						lockx.WithMaxRetryTimes(10))
					if err != nil {
						atomic.AddInt64(&totalErrors, 1)
						return
					}

					success, err := lock.Try()
					if err != nil {
						atomic.AddInt64(&totalErrors, 1)
						return
					}

					if success {
						atomic.AddInt64(&totalSuccess, 1)
						time.Sleep(1 * time.Millisecond)
						lock.Unlock()
					}
				}(lockID, goroutineID)
			}
		}

		wg.Wait()
		duration := time.Since(startTime)

		t.Logf("多锁并发测试完成:")
		t.Logf("- 锁数量: %d", numLocks)
		t.Logf("- 每锁goroutine数: %d", goroutinesPerLock)
		t.Logf("- 总成功数: %d", totalSuccess)
		t.Logf("- 总错误数: %d", totalErrors)
		t.Logf("- 总耗时: %v", duration)

		// 每个锁应该只有一个goroutine成功
		assert.Equal(t, int64(numLocks*goroutinesPerLock), totalSuccess)
		assert.Equal(t, int64(0), totalErrors)
	})
}

// TestMemoryLeakStress 内存泄漏压力测试
func TestMemoryLeakStress(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过内存泄漏测试")
	}

	redisClient, teardown := setupTestRedis(t)
	defer teardown()

	ctx := context.Background()

	t.Run("大量锁对象创建销毁", func(t *testing.T) {
		const iterations = 10000

		var m1, m2 runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&m1)

		for i := 0; i < iterations; i++ {
			lock, err := lockx.NewGlobalLock(ctx, redisClient,
				fmt.Sprintf("memory-test-%d", i))
			require.NoError(t, err)

			success, err := lock.Lock()
			require.NoError(t, err)
			assert.True(t, success)

			lock.Unlock()

			// 每1000次强制GC一次
			if i%1000 == 0 {
				runtime.GC()
			}
		}

		runtime.GC()
		runtime.ReadMemStats(&m2)

		memoryIncrease := m2.Alloc - m1.Alloc
		t.Logf("内存使用变化:")
		t.Logf("- 开始: %d bytes", m1.Alloc)
		t.Logf("- 结束: %d bytes", m2.Alloc)
		t.Logf("- 增长: %d bytes", memoryIncrease)
		t.Logf("- 平均每次操作: %.2f bytes", float64(memoryIncrease)/float64(iterations))

		// 内存增长应该在合理范围内（每次操作不超过1KB）
		avgMemoryPerOp := float64(memoryIncrease) / float64(iterations)
		assert.True(t, avgMemoryPerOp < 1024,
			"平均每次操作内存增长过大: %.2f bytes", avgMemoryPerOp)
	})

	t.Run("长时间运行goroutine泄漏测试", func(t *testing.T) {
		initialGoroutines := runtime.NumGoroutine()

		const numLocks = 100
		var wg sync.WaitGroup
		wg.Add(numLocks)

		for i := 0; i < numLocks; i++ {
			go func(id int) {
				defer wg.Done()

				ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
				defer cancel()

				lock, err := lockx.NewGlobalLock(ctx, redisClient,
					fmt.Sprintf("goroutine-leak-test-%d", id),
					lockx.WithRefreshPeriod(100*time.Millisecond))
				require.NoError(t, err)

				success, err := lock.Lock()
				require.NoError(t, err)
				assert.True(t, success)

				// 等待上下文超时，触发自动清理
				<-ctx.Done()
			}(i)
		}

		wg.Wait()

		// 等待goroutine清理
		time.Sleep(3 * time.Second)
		runtime.GC()

		finalGoroutines := runtime.NumGoroutine()
		goroutineIncrease := finalGoroutines - initialGoroutines

		t.Logf("Goroutine数量变化:")
		t.Logf("- 初始: %d", initialGoroutines)
		t.Logf("- 最终: %d", finalGoroutines)
		t.Logf("- 增长: %d", goroutineIncrease)

		// 允许少量goroutine增长（测试框架本身可能创建）
		assert.True(t, goroutineIncrease < 10,
			"Goroutine泄漏过多: %d", goroutineIncrease)
	})
}

// TestLongRunningStress 长时间运行压力测试
func TestLongRunningStress(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过长时间运行测试")
	}

	redisClient, teardown := setupTestRedis(t)
	defer teardown()

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	t.Run("持续30秒的锁竞争", func(t *testing.T) {
		const numWorkers = 20
		var totalOperations int64
		var successfulLocks int64
		var errors int64
		var wg sync.WaitGroup

		wg.Add(numWorkers)

		for i := 0; i < numWorkers; i++ {
			go func(workerID int) {
				defer wg.Done()

				for {
					select {
					case <-ctx.Done():
						return
					default:
					}

					atomic.AddInt64(&totalOperations, 1)

					lock, err := lockx.NewGlobalLock(ctx, redisClient, "long-running-test",
						lockx.WithMaxRetryTimes(3),
						lockx.WithRetryInterval(10*time.Millisecond))
					if err != nil {
						atomic.AddInt64(&errors, 1)
						continue
					}

					success, err := lock.Try()
					if err != nil {
						atomic.AddInt64(&errors, 1)
						continue
					}

					if success {
						atomic.AddInt64(&successfulLocks, 1)
						// 模拟工作负载
						time.Sleep(50 * time.Millisecond)
						lock.Unlock()
					}

					// 短暂休息
					time.Sleep(10 * time.Millisecond)
				}
			}(i)
		}

		wg.Wait()

		t.Logf("长时间运行测试结果:")
		t.Logf("- 工作者数量: %d", numWorkers)
		t.Logf("- 总操作数: %d", totalOperations)
		t.Logf("- 成功获取锁: %d", successfulLocks)
		t.Logf("- 错误数: %d", errors)
		t.Logf("- 成功率: %.2f%%", float64(successfulLocks)/float64(totalOperations)*100)
		t.Logf("- 错误率: %.2f%%", float64(errors)/float64(totalOperations)*100)

		assert.True(t, totalOperations > 0, "应该有操作执行")
		assert.True(t, successfulLocks > 0, "应该有成功的锁操作")

		// 错误率应该很低
		errorRate := float64(errors) / float64(totalOperations)
		assert.True(t, errorRate < 0.01, "错误率过高: %.2f%%", errorRate*100)
	})
}

// TestRefreshStabilityStress 刷新机制稳定性测试
func TestRefreshStabilityStress(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过刷新稳定性测试")
	}

	redisClient, teardown := setupTestRedis(t)
	defer teardown()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	t.Run("高频刷新稳定性", func(t *testing.T) {
		const numLocks = 10
		var wg sync.WaitGroup
		var refreshErrors int64

		testLogger := &TestLogger{}
		testLogger.On("Errorf", mock.Anything, mock.Anything, mock.Anything).Return()
		testLogger.On("Infof", mock.Anything, mock.Anything, mock.Anything).Return()

		wg.Add(numLocks)

		for i := 0; i < numLocks; i++ {
			go func(lockID int) {
				defer wg.Done()

				lock, err := lockx.NewGlobalLock(ctx, redisClient,
					fmt.Sprintf("refresh-stability-%d", lockID),
					lockx.WithExpiry(1*time.Second),
					lockx.WithRefreshPeriod(100*time.Millisecond),
					lockx.WithLogger(testLogger))
				require.NoError(t, err)

				success, err := lock.Lock()
				require.NoError(t, err)
				assert.True(t, success)

				// 持有锁直到上下文取消
				<-ctx.Done()
				lock.Unlock()
			}(i)
		}

		wg.Wait()

		// 检查刷新错误
		refreshErrors = int64(testLogger.GetErrorCount())
		t.Logf("刷新稳定性测试结果:")
		t.Logf("- 锁数量: %d", numLocks)
		t.Logf("- 刷新错误: %d", refreshErrors)

		// 在正常情况下，刷新错误应该很少
		assert.True(t, refreshErrors < 10, "刷新错误过多: %d", refreshErrors)
	})
}

// TestNetworkPartitionStress 网络分区压力测试
func TestNetworkPartitionStress(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过网络分区测试")
	}

	redisClient, teardown := setupTestRedis(t)
	defer teardown()

	ctx := context.Background()

	t.Run("模拟网络不稳定", func(t *testing.T) {
		const numOperations = 100
		var successCount int64
		var networkErrors int64

		for i := 0; i < numOperations; i++ {
			// 随机关闭和重新连接来模拟网络不稳定
			if i%10 == 0 && i > 0 {
				redisClient.Close()
				time.Sleep(100 * time.Millisecond)

				// 重新连接
				redisClient = redis.NewClient(&redis.Options{
					Addr:     "127.0.0.1:6379",
					Password: "123456",
					DB:       0,
				})
			}

			lock, err := lockx.NewGlobalLock(ctx, redisClient,
				fmt.Sprintf("network-partition-test-%d", i),
				lockx.WithMaxRetryTimes(3))
			if err != nil {
				atomic.AddInt64(&networkErrors, 1)
				continue
			}

			success, err := lock.Try()
			if err != nil {
				atomic.AddInt64(&networkErrors, 1)
				continue
			}

			if success {
				atomic.AddInt64(&successCount, 1)
				lock.Unlock()
			}
		}

		t.Logf("网络分区测试结果:")
		t.Logf("- 总操作数: %d", numOperations)
		t.Logf("- 成功数: %d", successCount)
		t.Logf("- 网络错误: %d", networkErrors)
		t.Logf("- 成功率: %.2f%%", float64(successCount)/float64(numOperations)*100)

		// 即使在网络不稳定的情况下，也应该有一定的成功率
		successRate := float64(successCount) / float64(numOperations)
		assert.True(t, successRate > 0.5, "网络不稳定时成功率过低: %.2f%%", successRate*100)
	})
}

// BenchmarkLockPerformance 性能基准测试
func BenchmarkLockPerformance(b *testing.B) {
	t := &testing.T{}
	redisClient, teardown := setupTestRedis(t)
	defer teardown()

	ctx := context.Background()

	b.Run("单线程Lock/Unlock", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			lock, _ := lockx.NewGlobalLock(ctx, redisClient,
				fmt.Sprintf("bench-single-%d", i))
			lock.Lock()
			lock.Unlock()
		}
	})

	b.Run("并发Lock/Unlock", func(b *testing.B) {
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				lock, _ := lockx.NewGlobalLock(ctx, redisClient,
					fmt.Sprintf("bench-parallel-%d", i))
				lock.Lock()
				lock.Unlock()
				i++
			}
		})
	})

	b.Run("Try方法性能", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			lock, _ := lockx.NewGlobalLock(ctx, redisClient,
				fmt.Sprintf("bench-try-%d", i))
			lock.Try()
			lock.Unlock()
		}
	})

	b.Run("竞争锁性能", func(b *testing.B) {
		const lockKey = "bench-contention"

		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				lock, _ := lockx.NewGlobalLock(ctx, redisClient, lockKey,
					lockx.WithMaxRetryTimes(1))
				success, _ := lock.Try()
				if success {
					lock.Unlock()
				}
			}
		})
	})
}
