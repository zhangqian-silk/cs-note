# Persistence

Redis 中数据持久化方案分为两种：

- RDB(Redis Database)：定期存储内存中的数据快照，恢复时直接复原即可
- AOF(Append Only File)：存储 Redis 的操作日志，恢复时按照指定顺序重新执行

最终的持久化策略，自然也可以分为四种：

- 不启用持久化方案
- 启用 RDB
- 启用 AOF
- 启用 RDB 与 AOF 的混合方案

用户需要根据业务特性，做对应的选择。

## RDB

## AOF

## Ref

- <https://redis.io/docs/latest/operate/oss_and_stack/management/persistence/>
- <https://xiaolincoding.com/redis/storage/aof.html#aof-%E6%97%A5%E5%BF%97>
- <https://xiaolincoding.com/redis/storage/rdb.html>
