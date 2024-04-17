package lockx

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/go-redis/redis/v8"
)

// 全局锁
type globalLock struct {
	redis     *redis.Client
	ctx       context.Context
	cancel    context.CancelFunc
	uniqueKey string
	value     string
}

type option struct {
	lockTimeout time.Time // 锁的超时时间
}

func defaultOption() *option {
	return &option{
		lockTimeout: time.Minute * 10 // 默认是10分钟
	}
}

var opt *option

func init() {
	opt = defaultOption()
}

// 设置
func InitOption(opts ...Option) {
	for _,app := range opts {
		app(opt)
	}
}

type Option func(*option)

func SetTimeout(t time.Time) Option {
	return func(o *option) {
		o.lockTimeout = t
	}
}


func NewGlobalLock(ctx context.Context, red *redis.Client, uniqueKey string) *globalLock {
	ctx, cancel := context.WithTimeout(ctx, opt.lockTimeout)
	return &globalLock{
		redis:     red,
		ctx:       ctx,
		cancel:    cancel,
		uniqueKey: uniqueKey,
		value:     fmt.Sprintf("%d", time.Now().UnixNano()),
	}
}

// 获取锁
func (g *globalLock) Lock() bool {

	script := `
	local token = redis.call('get',KEYS[1])
	if token == false
	then
		return redis.call('set',KEYS[1],ARGV[1],'EX',ARGV[2])
	end
		return 'ERROR'
	`

	resp, err := g.redis.Eval(g.ctx, script, []string{g.uniqueKey}, g.value, 5).Result()
	if resp != "OK" {
		_ = err
		log.Println("globalLock Lock", resp, err, g.uniqueKey, g.value)
	}
	if resp == "OK" {
		g.refresh()
		return true
	}
	return false
}

// 尝试获取锁
func (g *globalLock) Try(limitTimes int) bool {
	for i := 0; i < limitTimes; i++ {
		if g.Lock() {
			return true
		}
		time.Sleep(time.Millisecond * 100)
	}
	return false
}

// 删除锁
func (g *globalLock) Unlock() bool {

	script := `
	local token = redis.call('get',KEYS[1])
	if token == ARGV[1]
	then
		redis.call('del',KEYS[1])
		return 'OK'
	end
		return 'ERROR'
	`

	resp, err := g.redis.Eval(g.ctx, script, []string{g.uniqueKey}, g.value).Result()
	if resp != "OK" {
		log.Println("globalLock Unlock", resp, err, g.uniqueKey, g.value)
	}
	g.cancel()
	return true
}

// 刷新锁
func (g *globalLock) refresh() {
	go func() {
		t := time.NewTicker(time.Second)
		for {
			select {
			case <-t.C:
				g.refreshExec()
			case <-g.ctx.Done():
				t.Stop()
				return
			}
		}
	}()
}

func (g *globalLock) refreshExec() bool {
	script := `
	local token = redis.call('get',KEYS[1])
	if token == ARGV[1]
	then
		redis.call('set',KEYS[1],ARGV[1],'EX',ARGV[2])
		return 'OK'
	end
		return 'ERROR'
	`

	resp, err := g.redis.Eval(g.ctx, script, []string{g.uniqueKey}, g.value, 5).Result()
	if resp != "OK" {
		log.Println("globalLock refresh", resp, err, g.uniqueKey, g.value)
	}
	return resp == "OK"
}
