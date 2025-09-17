package lockx

import (
	"context"
	"log"
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

var globalOpts []Option

// 设置
func InitOption(opts ...Option) {
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
		o.Expiry = expiry
		if o.Expiry/3 < o.RefreshPeriod {
			o.RefreshPeriod = o.Expiry / 3
		}
	}
}

// 刷新间隔
func WithRefreshPeriod(period time.Duration) Option {
	return func(o *option) {
		o.RefreshPeriod = period
		if o.RefreshPeriod*3 > o.Expiry {
			o.Expiry = o.RefreshPeriod * 3
		}
	}
}

// 最大尝试次数
func WithMaxRetryTimes(times int) Option {
	return func(o *option) {
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
