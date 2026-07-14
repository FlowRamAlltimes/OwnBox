package rediska

import "github.com/redis/go-redis/v9"

var (
	fileRdb *redis.Client
)

func InitRedisForCache(addr string) *redis.Client {
	fileRdb = redis.NewClient(&redis.Options{
		Addr:     addr,
		DB:       0,
		Password: "",
	})
	return fileRdb
}
