# Redis

## connect

> <https://redis.io/try-free/>

### golang

首先导入 go-redis：

```shell
go get -u github.com/go-redis/redis
```

然后配置 redis 服务器相关参数，可以在[官网](https://redis.io/try-free/)免费运行云服务器

```go
client := redis.NewClient(&redis.Options{
    Addr:     "xxx:yyy",
    Password: "xxxxxxx",
})
```

连接后，简单进行测试：

```go
err := client.Set("key", "value", 0).Err()
if err != nil {
    fmt.Println(err)
}

val, err := client.Get("key").Result()
if err != nil {
    fmt.Println(err)
}
fmt.Println("key:", val)
```
