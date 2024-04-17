package lockx

import "time"

type option struct {
	lockTimeout time.Duration // 锁的超时时间
}

func defaultOption() *option {
	return &option{
		lockTimeout: time.Minute * 60,
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
