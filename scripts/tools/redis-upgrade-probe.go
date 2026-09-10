// Standalone migration probe. Run from platform/ to use its vendored go-redis/v8.
// Only connects to loopback in the disposable Redis container's network namespace.
package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-redis/redis/v8"
)

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func main() {
	if len(os.Args) != 2 {
		panic("usage: probe seed|check|write|check-written")
	}
	ctx := context.Background()
	r := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379", Password: "isolated-upgrade-test"})
	defer r.Close()
	r1 := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379", Password: "isolated-upgrade-test", DB: 1})
	defer r1.Close()
	must(r.Ping(ctx).Err())
	const payload = `{"enabled":true,"name":"升級測試","secret":"synthetic-only"}`
	switch os.Args[1] {
	case "seed":
		// Refuse to seed any nonempty database: this tool is for synthetic fixtures only.
		for _, c := range []*redis.Client{r, r1} {
			n, err := c.DBSize(ctx).Result()
			must(err)
			if n != 0 {
				panic("refusing to seed nonempty database")
			}
		}
		must(r.HSet(ctx, "SRS_TEST_CONFIG", "global", payload).Err())
		must(r.Set(ctx, "SRS_TEST_TOKEN", "synthetic-token", 0).Err())
		must(r.HSet(ctx, "SRS_TEST_COUNTER", "publish", 41).Err())
		deadline := time.Now().Add(2 * time.Hour)
		must(r.Set(ctx, "SRS_TEST_EXPIRY", deadline.UnixMilli(), 0).Err())
		must(r.Set(ctx, "SRS_TEST_TTL", "temporary", 0).Err())
		must(r.PExpireAt(ctx, "SRS_TEST_TTL", deadline).Err())
		must(r1.Set(ctx, "db1", "preserved", 0).Err())
		must(r.Save(ctx).Err())
	case "write":
		v, err := r.HIncrBy(ctx, "SRS_TEST_COUNTER", "publish", 1).Result()
		must(err)
		if v != 42 {
			panic("counter mismatch")
		}
		must(r.HSet(ctx, "SRS_TEST_CONFIG", "temporary", "remove-me").Err())
		must(r.HDel(ctx, "SRS_TEST_CONFIG", "temporary").Err())
		must(r.Set(ctx, "delete-me", "value", 0).Err())
		must(r.Del(ctx, "delete-me").Err())
		must(r.Save(ctx).Err())
	case "check", "check-written":
	default:
		panic("unknown probe mode")
	}
	v, err := r.HGet(ctx, "SRS_TEST_CONFIG", "global").Result()
	must(err)
	if v != payload {
		panic("JSON hash value changed")
	}
	all, err := r.HGetAll(ctx, "SRS_TEST_CONFIG").Result()
	must(err)
	if len(all) != 1 || all["global"] != payload {
		panic("HGETALL mismatch")
	}
	items, cursor, err := r.HScan(ctx, "SRS_TEST_CONFIG", 0, "*", 100).Result()
	must(err)
	if cursor != 0 || len(items) != 2 {
		panic("HSCAN mismatch")
	}
	n, err := r.HLen(ctx, "SRS_TEST_CONFIG").Result()
	must(err)
	if n != 1 {
		panic("HLEN mismatch")
	}
	v, err = r.Get(ctx, "SRS_TEST_TOKEN").Result()
	must(err)
	if v != "synthetic-token" {
		panic("string value changed")
	}
	wantCounter := "41"
	if os.Args[1] == "write" || os.Args[1] == "check-written" {
		wantCounter = "42"
	}
	v, err = r.HGet(ctx, "SRS_TEST_COUNTER", "publish").Result()
	must(err)
	if v != wantCounter {
		panic("persisted counter mismatch")
	}
	_, err = r.Get(ctx, "absent").Result()
	if err != redis.Nil {
		panic("redis.Nil behavior changed")
	}
	v, err = r.Get(ctx, "SRS_TEST_EXPIRY").Result()
	must(err)
	deadline, err := strconv.ParseInt(v, 10, 64)
	must(err)
	ttl, err := r.PTTL(ctx, "SRS_TEST_TTL").Result()
	must(err)
	delta := time.Until(time.UnixMilli(deadline)) - ttl
	if ttl <= 0 || delta < -2*time.Second || delta > 2*time.Second {
		panic("expiry was lost or reset")
	}
	v, err = r1.Get(ctx, "db1").Result()
	must(err)
	if v != "preserved" {
		panic("database 1 lost")
	}
	for _, section := range []string{"server", "memory", "stats", "clients", "persistence", "keyspace"} {
		info, err := r.Info(ctx, section).Result()
		must(err)
		if !strings.Contains(info, "# ") {
			panic("INFO section missing: " + section)
		}
		if section == "server" {
			for _, line := range strings.Split(info, "\n") {
				if strings.HasPrefix(line, "redis_version:") || strings.HasPrefix(line, "valkey_version:") || strings.HasPrefix(line, "server_name:") {
					fmt.Println(strings.TrimSpace(line))
				}
			}
		}
	}
	fmt.Println("PASS", os.Args[1], "hash/string/counter/TTL/DB1/INFO/go-redis-v8")
}
