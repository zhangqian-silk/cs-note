# Replication

## 主从架构

为了提高服务的可用性，避免单机故障导致服务不可用，Redis 也提供了集群部署的能力，其中最基础的模式为主从模式。

在 Redis 中，主从架构一般为一主多从，且读写分离。所有的写操作仅在主服务器进行，并由主服务器同步至从服务器，主要包含如下三种场景：

- 全量复制：当主从服务器连接时，如果数据差异过大，比如首次进行数据同步，主服务器会将当前所有数据创建一个快照，然后发送给从服务器
- 命令传播：当主服务器中数据发生变更时，会将命令同步发送给从服务器
- 增量复制：当主从服务器连接时，如果数据差异过小，比如偶发的网络中断，主服务器会根据从服务器给的索引，将新增数据发送给从服务器

此外，采用读写分离的方案，也能显著减少在高并发的场景下，保障数据一致性所需的成本。

在集群中从服务器数量过多时，主服务器向所有从服务器发送指令也会变成性能问题，此时可采用主从从的架构方案，数据从主服务器同步至少部分从服务器，再由这部分从服务器继续同步给其他从服务器。

## 建连

在主从服务器建立连接的阶段，从服务器的状态，即 [repl_state](https://github.com/redis/redis/blob/7.0.0/src/server.h#L392)，可能存在如下几种情况：

> capa 是 capabilities 的缩写，capa 请求目的是告诉主服务器支持的主从复制能力

| Enum | Desc |
| :--: | :--: |
| REPL_STATE_NONE | 与主从复制无关，普通实例 |
| REPL_STATE_CONNECT | 需要连接主服务器，待发起连接 |
| REPL_STATE_CONNECTING | 正在连接主服务器 |
| REPL_STATE_RECEIVE_PING_REPLY | 已发送 ping 请求，等待回复 |
| REPL_STATE_SEND_HANDSHAKE | 发送握手请求 |
| REPL_STATE_RECEIVE_AUTH_REPLY | 已发送 auth 请求，等待回复 |
| REPL_STATE_RECEIVE_PORT_REPLY | 已发送 port 请求，等待回复 |
| REPL_STATE_RECEIVE_IP_REPLY | 已发送 ip 请求，等待回复 |
| REPL_STATE_RECEIVE_CAPA_REPLY | 已发送 capa 请求，等待回复 |
| REPL_STATE_SEND_PSYNC | 待发送 psync 命令 |
| REPL_STATE_RECEIVE_PSYNC_REPLY | 已发送 psync 请求，等待回复 |
| REPL_STATE_TRANSFER | 正在从主服务器接收 .rdb 文件 |
| REPL_STATE_CONNECTED | 与主服务器连接成功 |

## Ref

- <https://redis.io/docs/latest/operate/oss_and_stack/management/replication/>
