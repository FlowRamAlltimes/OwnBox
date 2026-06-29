package rediska

import "github.com/redis/go-redis/v9"

var (
	rdb     *redis.Client
	fileRdb *redis.Client
)

func InitRedisForPasswords() *redis.Client {
	rdb = redis.NewClient(&redis.Options{
		Addr:     "file_cache_redis:6379",
		DB:       0,
		Password: "",
	})
	return rdb
}

func InitRedisForCache() *redis.Client {
	fileRdb = redis.NewClient(&redis.Options{
		Addr:     "file_cache_redis:6379",
		DB:       1,
		Password: "",
	})
	return fileRdb
}
