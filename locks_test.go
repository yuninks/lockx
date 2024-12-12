package lockx_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/go-redis/redis/v8"
	"github.com/yuninks/lockx"
)

func TestSimpleLock(t *testing.T) {
	ctx := context.Background()
	client := redis.NewClient(&redis.Options{
		Addr:     "127.0.0.1" + ":" + "6379",
		Password: "123456", // no password set
		DB:       0,        // use default DB
	})
	if client == nil {
		fmt.Println("redis init error")
		return
	}
	lockx.Init(ctx, client)

	l, err := lockx.New(ctx, "lockx:test")
	if err != nil {
		t.Log(err)
		return
	}
	if l.Lock() {
		fmt.Println("lock success")
		l.Unlock()
	}
}
