package lockx

import (
	"context"
	"log"
	"time"
)

type option struct {
	lockTimeout   time.Duration // 锁的超时时间
	Expiry        time.Duration // 单次刷新有效时间
	MaxRetryTimes int           // 尝试次数
	RetryInterval time.Duration // 尝试间隔
	RefreshPeriod time.Duration // 刷新间隔
	logger        Logger        // 日志
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

var globalOpts []Option

// 设置
func InitOption(opts ...Option) {
	globalOpts = opts
}

type Option func(*option)

func WithLockTimeout(t time.Duration) Option {
	return func(o *option) {
		o.lockTimeout = t
	}
}

func WithLogger(logger Logger) Option {
	return func(o *option) {
		o.logger = logger
	}
}

func WithExpiry(expiry time.Duration) Option {
	return func(o *option) {
		o.Expiry = expiry
	}
}

func WithRefreshPeriod(period time.Duration) Option {
	return func(o *option) {
		o.RefreshPeriod = period
	}
}

func WithMaxRetryTimes(times int) Option {
	return func(o *option) {
		o.MaxRetryTimes = times
	}
}

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
