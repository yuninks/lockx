package lockx

import (
	"context"
	"log"
	"sync"
	"time"
)

type option struct {
	lockTimeout time.Duration // 锁的超时时间

	MaxRetryTimes int           // 尝试次数
	RetryInterval time.Duration // 尝试间隔

	Expiry        time.Duration // 单次刷新有效时间
	RefreshPeriod time.Duration // 刷新间隔

	logger Logger // 日志
}

func defaultOption() *option {
	return &option{
		lockTimeout:   time.Minute * 60,
		Expiry:        5 * time.Second,
		RefreshPeriod: 1 * time.Second,
		MaxRetryTimes: 3,
		RetryInterval: 100 * time.Millisecond,
		logger:        &print{},
	}
}

var (
	globalOpts      []Option
	globalOptsMutex sync.RWMutex
)

// 设置
func InitOption(opts ...Option) {
	globalOptsMutex.Lock()
	defer globalOptsMutex.Unlock()
	globalOpts = opts
}

type Option func(*option)

// 最大锁定时间(默认1h)
func WithLockTimeout(t time.Duration) Option {
	return func(o *option) {
		o.lockTimeout = t
	}
}

// 日志
func WithLogger(logger Logger) Option {
	return func(o *option) {
		o.logger = logger
	}
}

// key有效时间(会自动刷新)
func WithExpiry(expiry time.Duration) Option {
	return func(o *option) {
		// 至少1秒
		if expiry < time.Second {
			expiry = time.Second
		}
		o.Expiry = expiry
		if o.Expiry/3 < o.RefreshPeriod {
			o.RefreshPeriod = o.Expiry / 3
		}
	}
}

// 刷新间隔
func WithRefreshPeriod(period time.Duration) Option {
	return func(o *option) {
		// 至少500毫秒
		if period < 500*time.Millisecond {
			period = 500 * time.Millisecond
		}
		o.RefreshPeriod = period
		if o.RefreshPeriod*3 > o.Expiry {
			o.Expiry = o.RefreshPeriod * 3
		}
	}
}

// 最大尝试次数(Try)
func WithMaxRetryTimes(times int) Option {
	return func(o *option) {
		// 至少1次
		if times < 1 {
			times = 1
		}
		o.MaxRetryTimes = times
	}
}

// 尝试间隔
func WithRetryInterval(interval time.Duration) Option {
	return func(o *option) {
		o.RetryInterval = interval
	}
}

type Logger interface {
	Errorf(ctx context.Context, format string, v ...any)
	Infof(ctx context.Context, format string, v ...any)
}

type print struct{}

func (*print) Errorf(ctx context.Context, format string, v ...any) {
	log.Printf(format, v...)
}

func (*print) Infof(ctx context.Context, format string, v ...any) {
	log.Printf(format, v...)
}
