package rediska

import "github.com/redis/go-redis/v9"

var (
	rdb     *redis.Client
	fileRdb *redis.Client
)

func InitRedisForPasswords(addr string) *redis.Client {
	rdb = redis.NewClient(&redis.Options{
		Addr:     addr,
		DB:       0,
		Password: "",
	})
	return rdb
}

func InitRedisForCache(addr string) *redis.Client {
	fileRdb = redis.NewClient(&redis.Options{
		Addr:     addr,
		DB:       1,
		Password: "",
	})
	return fileRdb
}
