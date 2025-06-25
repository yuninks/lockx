package lockx_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/go-redis/redis/v8"
	"github.com/yuninks/lockx"
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
		Password: "123456", // no password set
		DB:       0,        // use default DB
	})
	if client == nil {
		fmt.Println("redis init error")
		return
	}
	fmt.Println("begin")
	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	wg := sync.WaitGroup{}

	for i := 0; i < 10000; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			lock := lockx.NewGlobalLock(ctx, client, "lockx:test")
			defer lock.Unlock()
			if !lock.Lock() {
				fmt.Println("lock error", i)
				return
			}
			fmt.Println("ssss", i)
		}(i)
	}

	wg.Wait()
}
