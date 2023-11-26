package lockx_test

import (
	"context"
	"fmt"
	"testing"

	"code.yun.ink/open/timer/lockx"
	"github.com/go-redis/redis/v8"
)

var Redis *redis.Client

// func TestMain(m *testing.M) {
// 	client := redis.NewClient(&redis.Options{
// 		Addr:     "127.0.0.1" + ":" + "6379",
// 		Password: "", // no password set
// 		DB:       0,  // use default DB
// 	})
// 	if client == nil {
// 		fmt.Println("redis init error")
// 		return
// 	}
// 	// fmt.Println("ffff")
// 	Redis = client
// }

func TestLockx(t *testing.T) {
	client := redis.NewClient(&redis.Options{
		Addr:     "127.0.0.1" + ":" + "6379",
		Password: "", // no password set
		DB:       0,  // use default DB
	})
	if client == nil {
		fmt.Println("redis init error")
		return
	}
	fmt.Println("begin")
	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	lock := lockx.NewGlobalLock(ctx, client, "lockx:test")

	if !lock.Lock() {
		fmt.Println("lock error")
	}
	defer lock.Unlock()

	fmt.Println("ssss")

}
