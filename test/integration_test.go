package lockx_test

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yuninks/lockx"
)

// 集成测试：模拟真实业务场景

// TestDistributedCounter 测试分布式计数器场景
func TestDistributedCounter(t *testing.T) {
	redisClient, teardown := setupTestRedis(t)
	defer teardown()

	ctx := context.Background()
	const numWorkers = 50
	const incrementsPerWorker = 20
	var counter int64
	var wg sync.WaitGroup

	wg.Add(numWorkers)

	for i := 0; i < numWorkers; i++ {
		go func(workerID int) {
			defer wg.Done()

			for j := 0; j < incrementsPerWorker; j++ {
				lock, err := lockx.NewGlobalLock(ctx, redisClient, "counter-lock",
					lockx.WithMaxRetryTimes(10),
					lockx.WithRetryInterval(50*time.Millisecond))
				require.NoError(t, err)

				success, err := lock.Try()
				require.NoError(t, err)

				if success {
					// 模拟业务逻辑：读取-修改-写入
					currentValue := atomic.LoadInt64(&counter)
					time.Sleep(1 * time.Millisecond) // 模拟处理时间
					atomic.StoreInt64(&counter, currentValue+1)

					lock.Unlock()
				} else {
					j-- // 重试
				}
			}
		}(i)
	}

	wg.Wait()

	expectedValue := int64(numWorkers * incrementsPerWorker)
	actualValue := atomic.LoadInt64(&counter)
	assert.Equal(t, expectedValue, actualValue,
		"分布式计数器结果不正确，期望: %d, 实际: %d", expectedValue, actualValue)
}

// TestOrderProcessing 测试订单处理场景
func TestOrderProcessing(t *testing.T) {
	redisClient, teardown := setupTestRedis(t)
	defer teardown()

	ctx := context.Background()

	// 模拟库存
	inventory := map[string]int{
		"product-1": 10,
		"product-2": 5,
	}
	var inventoryMutex sync.RWMutex

	// 模拟多个用户同时下单
	const numOrders = 20
	var successfulOrders int32
	var wg sync.WaitGroup

	wg.Add(numOrders)

	for i := 0; i < numOrders; i++ {
		go func(orderID int) {
			defer wg.Done()

			productID := fmt.Sprintf("product-%d", (orderID%2)+1)
			lockKey := fmt.Sprintf("inventory-lock-%s", productID)

			lock, err := lockx.NewGlobalLock(ctx, redisClient, lockKey,
				lockx.WithMaxRetryTimes(10),
				lockx.WithRetryInterval(50*time.Millisecond))
			require.NoError(t, err)

			success, err := lock.Try()
			require.NoError(t, err)

			if success {
				// 检查库存
				inventoryMutex.RLock()
				currentStock := inventory[productID]
				inventoryMutex.RUnlock()

				if currentStock > 0 {
					// 模拟订单处理时间
					time.Sleep(10 * time.Millisecond)

					// 减少库存
					inventoryMutex.Lock()
					inventory[productID]--
					inventoryMutex.Unlock()

					atomic.AddInt32(&successfulOrders, 1)
					t.Logf("订单 %d 成功处理，产品 %s", orderID, productID)
				} else {
					t.Logf("订单 %d 失败：产品 %s 库存不足", orderID, productID)
				}

				lock.Unlock()
			} else {
				t.Logf("订单 %d 获取锁失败", orderID)
			}
		}(i)
	}

	wg.Wait()

	// 验证结果
	inventoryMutex.RLock()
	finalInventory := make(map[string]int)
	for k, v := range inventory {
		finalInventory[k] = v
	}
	inventoryMutex.RUnlock()

	expectedSuccessfulOrders := 10 + 5 // 初始库存总和
	assert.Equal(t, int32(expectedSuccessfulOrders), successfulOrders,
		"成功订单数量不正确")
	assert.Equal(t, 0, finalInventory["product-1"], "product-1库存应为0")
	assert.Equal(t, 0, finalInventory["product-2"], "product-2库存应为0")
}

// TestCacheRefresh 测试缓存刷新场景
func TestCacheRefresh(t *testing.T) {
	redisClient, teardown := setupTestRedis(t)
	defer teardown()

	ctx := context.Background()

	// 模拟缓存数据
	var cacheData string
	var cacheVersion int64
	var cacheMutex sync.RWMutex

	// 初始化缓存
	cacheData = "initial-data"
	cacheVersion = 1

	const numReaders = 30
	const numWriters = 3
	var wg sync.WaitGroup

	// 启动读取者
	wg.Add(numReaders)
	for i := 0; i < numReaders; i++ {
		go func(readerID int) {
			defer wg.Done()

			for j := 0; j < 10; j++ {
				cacheMutex.RLock()
				data := cacheData
				version := cacheVersion
				cacheMutex.RUnlock()

				t.Logf("读取者 %d: 数据=%s, 版本=%d", readerID, data, version)
				time.Sleep(50 * time.Millisecond)
			}
		}(i)
	}

	// 启动写入者（缓存刷新）
	wg.Add(numWriters)
	for i := 0; i < numWriters; i++ {
		go func(writerID int) {
			defer wg.Done()

			for j := 0; j < 3; j++ {
				lock, err := lockx.NewGlobalLock(ctx, redisClient, "cache-refresh-lock",
					lockx.WithExpiry(5*time.Second),
					lockx.WithMaxRetryTimes(3))
				require.NoError(t, err)

				success, err := lock.Try()
				require.NoError(t, err)

				if success {
					// 模拟缓存刷新
					t.Logf("写入者 %d 开始刷新缓存", writerID)

					// 模拟从数据库获取新数据的时间
					time.Sleep(200 * time.Millisecond)

					cacheMutex.Lock()
					cacheData = fmt.Sprintf("data-updated-by-writer-%d-round-%d", writerID, j)
					cacheVersion++
					cacheMutex.Unlock()

					t.Logf("写入者 %d 完成缓存刷新，新版本: %d", writerID, cacheVersion)

					lock.Unlock()
				} else {
					t.Logf("写入者 %d 获取锁失败，跳过刷新", writerID)
				}

				time.Sleep(1 * time.Second)
			}
		}(i)
	}

	wg.Wait()

	// 验证最终状态
	cacheMutex.RLock()
	finalVersion := cacheVersion
	cacheMutex.RUnlock()

	assert.True(t, finalVersion > 1, "缓存版本应该被更新")
	t.Logf("最终缓存版本: %d", finalVersion)
}

// TestLeaderElection 测试领导者选举场景
func TestLeaderElection(t *testing.T) {
	redisClient, teardown := setupTestRedis(t)
	defer teardown()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	const numCandidates = 10
	var currentLeader string
	var leaderChanges int32
	var leaderMutex sync.RWMutex
	var wg sync.WaitGroup

	wg.Add(numCandidates)

	for i := 0; i < numCandidates; i++ {
		go func(candidateID int) {
			defer wg.Done()

			candidateName := fmt.Sprintf("candidate-%d", candidateID)

			for {
				select {
				case <-ctx.Done():
					return
				default:
				}

				lock, err := lockx.NewGlobalLock(ctx, redisClient, "leader-election",
					lockx.WithExpiry(2*time.Second),
					lockx.WithRefreshPeriod(500*time.Millisecond),
					lockx.WithMaxRetryTimes(1))
				require.NoError(t, err)

				success, err := lock.Try()
				if err != nil {
					continue
				}

				if success {
					// 成为领导者
					leaderMutex.Lock()
					if currentLeader != candidateName {
						currentLeader = candidateName
						atomic.AddInt32(&leaderChanges, 1)
						t.Logf("%s 成为新领导者", candidateName)
					}
					leaderMutex.Unlock()

					// 执行领导者任务
					select {
					case <-ctx.Done():
						lock.Unlock()
						return
					case <-time.After(1 * time.Second):
						// 模拟领导者工作
					}

					lock.Unlock()
				} else {
					// 等待一段时间再尝试
					time.Sleep(100 * time.Millisecond)
				}
			}
		}(i)
	}

	wg.Wait()

	leaderMutex.RLock()
	finalLeader := currentLeader
	changes := atomic.LoadInt32(&leaderChanges)
	leaderMutex.RUnlock()

	assert.NotEmpty(t, finalLeader, "应该选出一个领导者")
	assert.True(t, changes >= 1, "应该至少有一次领导者变更")
	t.Logf("最终领导者: %s, 总变更次数: %d", finalLeader, changes)
}

// TestTaskScheduling 测试任务调度场景
func TestTaskScheduling(t *testing.T) {
	redisClient, teardown := setupTestRedis(t)
	defer teardown()

	ctx := context.Background()

	// 模拟任务队列
	tasks := []string{
		"task-1", "task-2", "task-3", "task-4", "task-5",
		"task-6", "task-7", "task-8", "task-9", "task-10",
	}

	var completedTasks []string
	var completedMutex sync.Mutex
	const numWorkers = 5
	var wg sync.WaitGroup

	wg.Add(numWorkers)

	for i := 0; i < numWorkers; i++ {
		go func(workerID int) {
			defer wg.Done()

			for _, task := range tasks {
				lockKey := fmt.Sprintf("task-lock-%s", task)

				lock, err := lockx.NewGlobalLock(ctx, redisClient, lockKey,
					lockx.WithMaxRetryTimes(1))
				require.NoError(t, err)

				success, err := lock.Try()
				require.NoError(t, err)

				if success {
					// 执行任务
					t.Logf("工作者 %d 开始执行 %s", workerID, task)

					// 模拟任务执行时间
					time.Sleep(100 * time.Millisecond)

					completedMutex.Lock()
					completedTasks = append(completedTasks, task)
					completedMutex.Unlock()

					t.Logf("工作者 %d 完成 %s", workerID, task)

					lock.Unlock()
				}
			}
		}(i)
	}

	wg.Wait()

	// 验证所有任务都被执行且只执行一次
	completedMutex.Lock()
	completed := make([]string, len(completedTasks))
	copy(completed, completedTasks)
	completedMutex.Unlock()

	assert.Equal(t, len(tasks), len(completed), "所有任务都应该被完成")

	// 检查是否有重复执行的任务
	taskCount := make(map[string]int)
	for _, task := range completed {
		taskCount[task]++
	}

	for task, count := range taskCount {
		assert.Equal(t, 1, count, "任务 %s 应该只被执行一次，实际执行了 %d 次", task, count)
	}
}

// TestFailoverScenario 测试故障转移场景
func TestFailoverScenario(t *testing.T) {
	redisClient, teardown := setupTestRedis(t)
	defer teardown()

	ctx := context.Background()

	t.Run("主节点故障模拟", func(t *testing.T) {
		// 主节点获取锁
		primaryLock, err := lockx.NewGlobalLock(ctx, redisClient, "failover-test",
			lockx.WithExpiry(3*time.Second),
			lockx.WithRefreshPeriod(1*time.Second))
		require.NoError(t, err)

		success, err := primaryLock.Lock()
		require.NoError(t, err)
		assert.True(t, success)

		// 模拟主节点工作
		time.Sleep(1 * time.Second)

		// 备用节点尝试获取锁（应该失败）
		backupLock, err := lockx.NewGlobalLock(ctx, redisClient, "failover-test",
			lockx.WithMaxRetryTimes(1))
		require.NoError(t, err)

		success, err = backupLock.Try()
		require.NoError(t, err)
		assert.False(t, success, "备用节点不应该获取到锁")

		// 模拟主节点故障（停止刷新）
		primaryLock.Unlock() // 主动释放模拟故障

		// 等待一小段时间
		time.Sleep(100 * time.Millisecond)

		// 备用节点现在应该能获取锁
		success, err = backupLock.Try()
		require.NoError(t, err)
		assert.True(t, success, "备用节点应该能获取到锁")

		backupLock.Unlock()
	})
}
