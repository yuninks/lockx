package lockx

import (
	"context"
	"log"
	"time"
)

type option struct {
	lockTimeout time.Duration // 锁的超时时间
	logger      Logger        // 日志
}

func defaultOption() *option {
	return &option{
		lockTimeout: time.Minute * 60,
		logger:      &print{},
	}
}

var opt *option

func init() {
	opt = defaultOption()
}

// 设置
func InitOption(opts ...Option) {
	for _, app := range opts {
		app(opt)
	}
}

type Option func(*option)

func SetTimeout(t time.Duration) Option {
	return func(o *option) {
		o.lockTimeout = t
	}
}

func SetLogger(logger Logger) Option {
	return func(o *option) {
		o.logger = logger
	}
}

type Logger interface {
	Errorf(ctx context.Context, format string, v ...any)
	Printf(ctx context.Context, format string, v ...any)
}

type print struct{}

func (*print) Errorf(ctx context.Context, format string, v ...any) {
	log.Printf(format, v...)
}

func (*print) Printf(ctx context.Context, format string, v ...any) {
	log.Printf(format, v...)
}
