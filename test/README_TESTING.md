# 测试用例说明

## 测试文件结构

### 1. `lockx_test.go` - 基础功能测试
- **NewGlobalLock测试**: 验证锁对象创建
- **Lock/Unlock测试**: 基本加锁解锁功能
- **Try方法测试**: 重试机制测试
- **并发测试**: 多goroutine竞争锁
- **上下文取消测试**: 超时和取消处理
- **自定义选项测试**: 配置参数验证
- **边界条件测试**: 异常输入处理

### 2. `comprehensive_test.go` - 漏洞修复验证测试
- **重入锁修复测试**: 验证竞态条件修复
- **空指针修复测试**: 验证logger nil检查
- **Goroutine泄漏修复测试**: 验证资源清理
- **错误计数器重置测试**: 验证刷新机制
- **全局变量线程安全测试**: 验证并发访问安全
- **边界条件测试**: 极端参数处理
- **错误恢复测试**: 故障恢复能力

### 3. `integration_test.go` - 集成测试
- **分布式计数器**: 模拟真实业务场景
- **订单处理**: 库存管理场景
- **缓存刷新**: 缓存更新竞争
- **领导者选举**: 分布式协调
- **任务调度**: 任务分发场景
- **故障转移**: 主备切换

### 4. `stress_test.go` - 压力测试
- **高并发压力测试**: 1000+ goroutine竞争
- **内存泄漏测试**: 长时间运行内存监控
- **长时间运行测试**: 30秒持续压力
- **刷新稳定性测试**: 高频刷新机制
- **网络分区测试**: 网络不稳定模拟
- **性能基准测试**: 各种操作的性能指标

### 5. `test_helper.go` - 测试辅助工具
- **MockRedisClient**: Redis模拟客户端
- **TestScenario**: 测试场景框架
- **LockTestHelper**: 锁测试辅助工具
- **并发测试工具**: 简化并发测试编写
- **条件等待工具**: 异步条件验证

## 运行测试

### 运行所有测试
```bash
go test -v ./...
```

### 运行特定测试文件
```bash
go test -v -run TestNewGlobalLock
go test -v -run TestReentrantLockFix
go test -v -run TestDistributedCounter
```

### 运行压力测试
```bash
go test -v -run TestHighConcurrencyStress
go test -v -run TestMemoryLeakStress
```

### 跳过长时间运行的测试
```bash
go test -v -short ./...
```

### 运行基准测试
```bash
go test -v -bench=. -benchmem
```

## 测试覆盖率

### 生成覆盖率报告
```bash
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out -o coverage.html
```

### 查看覆盖率统计
```bash
go test -cover ./...
```

## 测试场景覆盖

### 功能测试覆盖
- ✅ 基本锁操作（获取、释放、重试）
- ✅ 重入锁机制
- ✅ 自动刷新机制
- ✅ 上下文取消处理
- ✅ 配置选项验证
- ✅ 错误处理

### 并发测试覆盖
- ✅ 多goroutine竞争单个锁
- ✅ 多锁并发操作
- ✅ 高并发场景（1000+ goroutine）
- ✅ 长时间并发运行
- ✅ 竞态条件检测

### 稳定性测试覆盖
- ✅ 内存泄漏检测
- ✅ Goroutine泄漏检测
- ✅ 长时间运行稳定性
- ✅ 网络异常恢复
- ✅ Redis连接异常处理

### 业务场景测试覆盖
- ✅ 分布式计数器
- ✅ 库存管理
- ✅ 缓存更新
- ✅ 领导者选举
- ✅ 任务调度
- ✅ 故障转移

### 性能测试覆盖
- ✅ 单线程性能
- ✅ 并发性能
- ✅ 内存使用效率
- ✅ 锁竞争性能
- ✅ 网络延迟影响

## 测试环境要求

### Redis环境
- Redis 6.0+
- 地址: 127.0.0.1:6379
- 密码: 123456
- 数据库: 0

### Go环境
- Go 1.19+
- 测试依赖包:
  - github.com/stretchr/testify
  - github.com/redis/go-redis/v9

### 环境变量（可选）
```bash
export REDIS_ADDR=127.0.0.1:6379
export REDIS_PASSWORD=123456
export REDIS_DB=0
```

## 测试最佳实践

### 1. 测试隔离
- 每个测试使用不同的锁键名
- 测试结束后清理Redis数据
- 使用独立的上下文

### 2. 并发测试
- 使用sync.WaitGroup等待所有goroutine
- 使用原子操作统计结果
- 验证竞态条件

### 3. 错误处理
- 验证所有错误路径
- 测试异常恢复能力
- 检查资源清理

### 4. 性能测试
- 使用基准测试框架
- 监控内存使用
- 测试不同负载下的表现

### 5. 集成测试
- 模拟真实业务场景
- 测试多组件交互
- 验证端到端功能

## 常见问题

### Q: 测试失败"Redis连接失败"
A: 确保Redis服务运行在127.0.0.1:6379，密码为123456

### Q: 压力测试运行时间过长
A: 使用`-short`标志跳过长时间运行的测试

### Q: 内存泄漏测试不稳定
A: 运行前执行`runtime.GC()`强制垃圾回收

### Q: 并发测试偶尔失败
A: 增加等待时间或重试次数，考虑网络延迟影响

## 持续集成

### GitHub Actions示例
```yaml
name: Test
on: [push, pull_request]
jobs:
  test:
    runs-on: ubuntu-latest
    services:
      redis:
        image: redis:6
        options: >-
          --health-cmd "redis-cli ping"
          --health-interval 10s
          --health-timeout 5s
          --health-retries 5
    steps:
    - uses: actions/checkout@v2
    - uses: actions/setup-go@v2
      with:
        go-version: 1.19
    - run: go test -v -race -coverprofile=coverage.out ./...
    - run: go tool cover -func=coverage.out
```