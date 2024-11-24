# Redis

Redis 是一个基于内存的 KV 数据库，提供了极高的读写性能和丰富的数据类型，支持数据的持久化存储以及分布式部署。在命令执行上，以单线程的方式执行，所有操作都是原子性的，且支持通过 Lua 脚本来编写复杂操作。

## Data Type

[Data Type](data_type.md)

## Expiration Algorithm

[Expiration Algorithm](expiration_algorithm.md)

## Eviction Policy

当 Redis 的内存达到上限时，如果仍有新数据写入，则会因为内存不足而出现问题，此时可以根据淘汰策略的不同，Redis 命令的执行逻辑也会存在差异。

目前，Redis 7.0 共支持 8 种不同的淘汰策略，即 [maxmemory_policy_enum](https://github.com/redis/redis/blob/7.0.0/src/config.c#L49)，详细说明如下：

<table style="width:100%; text-align:center;">
    <tr>
        <th>Scope</th>
        <th>Policy</th>
        <th>Desc</th>
    </tr>
    <tr>
        <td>\</td>
        <td>noeviction</th>
        <td style="text-align:left;">禁止写入并报错</td>
    </tr>
    <tr>
        <td rowspan="4">设置了过期时间的键值</td>
        <td>volatile-random</td>
        <td style="text-align:left;">随机淘汰</td>
    </tr>
    <tr>
        <td>volatile-ttl</td>
        <td style="text-align:left;">过期时间最短</td>
    </tr>
    <tr>
        <td>volatile-lru</td>
        <td style="text-align:left;">最近最久未使用</td>
    </tr>
    <tr>
        <td>volatile-lfu</td>
        <td style="text-align:left;">最不常用</td>
    </tr>
    <tr>
        <td rowspan="3">Rides 中所有的键值</td>
        <td>allkeys-random</td>
        <td style="text-align:left;">随机淘汰</td>
    </tr>
    <tr>
        <td>allkeys-lru</td>
        <td style="text-align:left;">最近最久未使用</td>
    </tr>
    <tr>
        <td>allkeys-lfu</td>
        <td style="text-align:left;">最不常用</td>
    </tr>
</table>

## High Availability

- 持久化
  - 通过 AOF 和 RDB 两种方案，保障单机数据的高可用
- 主从架构
  - 多服务器对外提供服务，避免单机故障问题
  - 通过一主多从的架构，实现集群间的数据一致性
- 哨兵
  - 在主从架构的基础上，提供主从故障转移的能力

## Connect

> <https://redis.io/try-free/>

### Golang

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

## Ref

- <https://xiaolincoding.com/redis/module/strategy.html>
