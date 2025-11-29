package lockx

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/mock"
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

// 测试辅助工具

// TestRedisConfig Redis测试配置
type TestRedisConfig struct {
	Addr     string
	Password string
	DB       int
}

// DefaultTestRedisConfig 默认测试Redis配置
func DefaultTestRedisConfig() *TestRedisConfig {
	return &TestRedisConfig{
		Addr:     "127.0.0.1:6379",
		Password: "123456",
		DB:       0,
	}
}

// SetupTestRedisWithConfig 使用自定义配置设置测试Redis
func SetupTestRedisWithConfig(t *testing.T, config *TestRedisConfig) (redis.UniversalClient, func()) {
	client := redis.NewClient(&redis.Options{
		Addr:     config.Addr,
		Password: config.Password,
		DB:       config.DB,
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

// MockRedisClient 模拟Redis客户端用于单元测试
type MockRedisClient struct {
	mock.Mock
	data map[string]string
	ttl  map[string]time.Time
	mu   sync.RWMutex
}

func NewMockRedisClient() *MockRedisClient {
	return &MockRedisClient{
		data: make(map[string]string),
		ttl:  make(map[string]time.Time),
	}
}

func (m *MockRedisClient) Eval(ctx context.Context, script string, keys []string, args ...interface{}) *redis.Cmd {
	m.mu.Lock()
	defer m.mu.Unlock()

	result := m.Called(ctx, script, keys, args)

	// 简单模拟Lua脚本执行
	if len(keys) > 0 {
		key := keys[0]

		// 检查TTL
		if expiry, exists := m.ttl[key]; exists && time.Now().After(expiry) {
			delete(m.data, key)
			delete(m.ttl, key)
		}

		// 模拟SET NX操作
		if _, exists := m.data[key]; !exists && len(args) >= 2 {
			m.data[key] = args[0].(string)
			if len(args) >= 3 {
				if expiry, ok := args[2].(int); ok {
					m.ttl[key] = time.Now().Add(time.Duration(expiry) * time.Second)
				}
			}
			return redis.NewCmdResult("OK", nil)
		}

		// 模拟重入检查
		if val, exists := m.data[key]; exists && val == args[0].(string) {
			if len(args) >= 3 {
				if expiry, ok := args[2].(int); ok {
					m.ttl[key] = time.Now().Add(time.Duration(expiry) * time.Second)
				}
			}
			return redis.NewCmdResult("OK", nil)
		}
	}

	return redis.NewCmdResult(result.Get(0), result.Error(1))
}

func (m *MockRedisClient) Get(ctx context.Context, key string) *redis.StringCmd {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// 检查TTL
	if expiry, exists := m.ttl[key]; exists && time.Now().After(expiry) {
		delete(m.data, key)
		delete(m.ttl, key)
		return redis.NewStringResult("", redis.Nil)
	}

	if val, exists := m.data[key]; exists {
		return redis.NewStringResult(val, nil)
	}
	return redis.NewStringResult("", redis.Nil)
}

func (m *MockRedisClient) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) *redis.StatusCmd {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.data[key] = fmt.Sprintf("%v", value)
	if expiration > 0 {
		m.ttl[key] = time.Now().Add(expiration)
	}
	return redis.NewStatusResult("OK", nil)
}

func (m *MockRedisClient) Del(ctx context.Context, keys ...string) *redis.IntCmd {
	m.mu.Lock()
	defer m.mu.Unlock()

	deleted := int64(0)
	for _, key := range keys {
		if _, exists := m.data[key]; exists {
			delete(m.data, key)
			delete(m.ttl, key)
			deleted++
		}
	}
	return redis.NewIntResult(deleted, nil)
}

func (m *MockRedisClient) TTL(ctx context.Context, key string) *redis.DurationCmd {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if expiry, exists := m.ttl[key]; exists {
		remaining := time.Until(expiry)
		if remaining > 0 {
			return redis.NewDurationResult(remaining, nil)
		}
	}
	return redis.NewDurationResult(-1, nil)
}

func (m *MockRedisClient) Close() error {
	return nil
}

func (m *MockRedisClient) Ping(ctx context.Context) *redis.StatusCmd {
	return redis.NewStatusResult("PONG", nil)
}

// TestScenario 测试场景结构
type TestScenario struct {
	Name        string
	Description string
	Setup       func(t *testing.T) (redis.UniversalClient, func())
	Test        func(t *testing.T, client redis.UniversalClient)
}

// RunTestScenarios 运行多个测试场景
func RunTestScenarios(t *testing.T, scenarios []TestScenario) {
	for _, scenario := range scenarios {
		t.Run(scenario.Name, func(t *testing.T) {
			if scenario.Description != "" {
				t.Logf("测试描述: %s", scenario.Description)
			}

			client, teardown := scenario.Setup(t)
			defer teardown()

			scenario.Test(t, client)
		})
	}
}

// LockTestHelper 锁测试辅助工具
type LockTestHelper struct {
	client redis.UniversalClient
	ctx    context.Context
}

func NewLockTestHelper(client redis.UniversalClient) *LockTestHelper {
	return &LockTestHelper{
		client: client,
		ctx:    context.Background(),
	}
}

// CreateLock 创建测试锁
func (h *LockTestHelper) CreateLock(key string, opts ...lockx.Option) (*lockx.GlobalLock, error) {
	return lockx.NewGlobalLock(h.ctx, h.client, key, opts...)
}

// CreateLockWithContext 使用指定上下文创建锁
func (h *LockTestHelper) CreateLockWithContext(ctx context.Context, key string, opts ...lockx.Option) (*lockx.GlobalLock, error) {
	return lockx.NewGlobalLock(ctx, h.client, key, opts...)
}

// VerifyLockExists 验证锁是否存在于Redis中
func (h *LockTestHelper) VerifyLockExists(t *testing.T, key string, expectedValue string) {
	val, err := h.client.Get(h.ctx, key).Result()
	if expectedValue == "" {
		if err != redis.Nil {
			t.Errorf("期望锁不存在，但实际存在: %s", val)
		}
	} else {
		if err != nil {
			t.Errorf("期望锁存在但获取失败: %v", err)
		} else if val != expectedValue {
			t.Errorf("锁值不匹配，期望: %s, 实际: %s", expectedValue, val)
		}
	}
}

// VerifyLockTTL 验证锁的TTL
func (h *LockTestHelper) VerifyLockTTL(t *testing.T, key string, expectedMin, expectedMax time.Duration) {
	ttl, err := h.client.TTL(h.ctx, key).Result()
	if err != nil {
		t.Errorf("获取TTL失败: %v", err)
		return
	}

	if ttl < expectedMin || ttl > expectedMax {
		t.Errorf("TTL不在期望范围内，实际: %v, 期望范围: [%v, %v]", ttl, expectedMin, expectedMax)
	}
}

// ConcurrentLockTest 并发锁测试辅助函数
func (h *LockTestHelper) ConcurrentLockTest(t *testing.T, lockKey string, numGoroutines int, workDuration time.Duration) (successCount int, errorCount int) {
	var wg sync.WaitGroup
	var successCounter, errorCounter int32
	var mu sync.Mutex

	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()

			lock, err := h.CreateLock(lockKey)
			if err != nil {
				mu.Lock()
				errorCounter++
				mu.Unlock()
				return
			}

			success, err := lock.Lock()
			if err != nil {
				mu.Lock()
				errorCounter++
				mu.Unlock()
				return
			}

			if success {
				mu.Lock()
				successCounter++
				mu.Unlock()

				// 模拟工作
				time.Sleep(workDuration)
				lock.Unlock()
			}
		}(i)
	}

	wg.Wait()
	return int(successCounter), int(errorCounter)
}

// WaitForCondition 等待条件满足
func WaitForCondition(t *testing.T, condition func() bool, timeout time.Duration, message string) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("等待条件超时: %s", message)
}

// AssertEventuallyTrue 断言条件最终为真
func AssertEventuallyTrue(t *testing.T, condition func() bool, timeout time.Duration, message string) {
	WaitForCondition(t, condition, timeout, message)
}

// CleanupRedisKeys 清理Redis测试键
func CleanupRedisKeys(client redis.UniversalClient, pattern string) error {
	ctx := context.Background()
	keys, err := client.Keys(ctx, pattern).Result()
	if err != nil {
		return err
	}

	if len(keys) > 0 {
		return client.Del(ctx, keys...).Err()
	}
	return nil
}
