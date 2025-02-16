# Cluster

Redis Cluster 是 Redis 官方提供的分布式解决方案，主要用于解决单节点 Redis 在高并发、大数据量场景下的性能瓶颈和可用性问题。其核心目标是通过数据分片（Sharding）、高可用性和自动故障转移来实现水平扩展和容错能力。

## 初始化

### 集群初始化

在启动程序时，即 [`main()`](https://github.com/redis/redis/blob/7.0.0/src/server.c#L6832) 函数中，会调用 [`initServer()`](https://github.com/redis/redis/blob/7.0.0/src/server.c#L2374) 函数来初始化服务器相关配置。如果配置了 [`cluster-enabled`](https://github.com/redis/redis/blob/7.0.0/src/config.c#L2934) 字段，则会调用 [`clusterInit()`](https://github.com/redis/redis/blob/7.0.0/src/cluster.c#L606) 函数，来执行 Cluster 相关配置。

```c
int main(int argc, char **argv) {
    ...
    initServer();
    ...
}

standardConfig static_configs[] = {
    ...
    createBoolConfig("cluster-enabled", NULL, IMMUTABLE_CONFIG, server.cluster_enabled, 0, NULL, NULL),
    ...
}

void initServer(void) {
    ...
    if (server.cluster_enabled) clusterInit();
    ...
}
```

[`clusterInit()`](https://github.com/redis/redis/blob/7.0.0/src/cluster.c#L606) 函数的执行逻辑如下所示：

- **初始化集群状态结构体**
  - 分配内存给 `server.cluster`，并初始化其核心字段
  - 当前节点 `myself` 设为 `NULL`，集群状态初始为 `CLUSTER_FAIL`（不可用）
  - 创建存储节点和黑名单节点的字典（`nodes` 和 `nodes_black_list`）
  - 初始化槽位，并调用 [`clusterCloseAllSlots()`](https://github.com/redis/redis/blob/7.0.0/src/cluster.c#L4353) 函数重置槽位状态

    ```c
    void clusterInit(void) {
        int saveconf = 0;

        server.cluster = zmalloc(sizeof(clusterState));
        server.cluster->myself = NULL;
        server.cluster->currentEpoch = 0;
        server.cluster->state = CLUSTER_FAIL;
        server.cluster->size = 1;
        server.cluster->todo_before_sleep = 0;
        server.cluster->nodes = dictCreate(&clusterNodesDictType);
        server.cluster->nodes_black_list =
            dictCreate(&clusterNodesBlackListDictType);
        server.cluster->failover_auth_time = 0;
        server.cluster->failover_auth_count = 0;
        server.cluster->failover_auth_rank = 0;
        server.cluster->failover_auth_epoch = 0;
        server.cluster->cant_failover_reason = CLUSTER_CANT_FAILOVER_NONE;
        server.cluster->lastVoteEpoch = 0;

        /* Initialize stats */
        for (int i = 0; i < CLUSTERMSG_TYPE_COUNT; i++) {
            server.cluster->stats_bus_messages_sent[i] = 0;
            server.cluster->stats_bus_messages_received[i] = 0;
        }
        server.cluster->stats_pfail_nodes = 0;
        server.cluster->stat_cluster_links_buffer_limit_exceeded = 0;

        memset(server.cluster->slots,0, sizeof(server.cluster->slots));
        clusterCloseAllSlots();
        ...
    }
    ```

- **配置文件处理**
  - 对集群配置文件加锁，防止多进程冲突
  - 加载现有集群配置，若失败则创建新的配置文件
    - 通过 [`createClusterNode()`](https://github.com/redis/redis/blob/7.0.0/src/cluster.c#L950) 函数创建新节点，节点名称设置为 `NULL`，由函数内部随机生成
    - 调用 [`clusterAddNode()`](https://github.com/redis/redis/blob/7.0.0/src/cluster.c#L1150) 函数将节点添加至集群中
    - 通过 [`clusterSaveConfigOrDie()`](https://github.com/redis/redis/blob/7.0.0/src/cluster.c#L454) 持久化新创建的配置

    ```c
    void clusterInit(void) {
        ...
        /* Lock the cluster config file to make sure every node uses
        * its own nodes.conf. */
        server.cluster_config_file_lock_fd = -1;
        if (clusterLockConfig(server.cluster_configfile) == C_ERR)
            exit(1);

        /* Load or create a new nodes configuration. */
        if (clusterLoadConfig(server.cluster_configfile) == C_ERR) {
            /* No configuration found. We will just use the random name provided
            * by the createClusterNode() function. */
            myself = server.cluster->myself =
                createClusterNode(NULL,CLUSTER_NODE_MYSELF|CLUSTER_NODE_MASTER);
            serverLog(LL_NOTICE,"No cluster configuration found, I'm %.40s",
                myself->name);
            clusterAddNode(myself);
            saveconf = 1;
        }
        if (saveconf) clusterSaveConfigOrDie(1);
        ...
    }
    ```

- **端口与网络初始化**
  - 检查端口与绑定地址的合法性
  - 绑定集群通信端口（默认 Redis 端口 + 10000），启动监听。
  - 注册 socket 处理函数 [`clusterAcceptHandler()`](https://github.com/redis/redis/blob/7.0.0/src/cluster.c#L846)，用于处理节点间连接请求。

    ```c
    void clusterInit(void) {
        ...
        /* We need a listening TCP port for our cluster messaging needs. */
        server.cfd.count = 0;

        /* Port sanity check II
        * The other handshake port check is triggered too late to stop
        * us from trying to use a too-high cluster port number. */
        int port = server.tls_cluster ? server.tls_port : server.port;
        if (!server.cluster_port && port > (65535-CLUSTER_PORT_INCR)) {
            serverLog(LL_WARNING, "Redis port number too high. "
                    "Cluster communication port is 10,000 port "
                    "numbers higher than your Redis port. "
                    "Your Redis port number must be 55535 or less.");
            exit(1);
        }
        if (!server.bindaddr_count) {
            serverLog(LL_WARNING, "No bind address is configured, but it is required for the Cluster bus.");
            exit(1);
        }
        int cport = server.cluster_port ? server.cluster_port : port + CLUSTER_PORT_INCR;
        if (listenToPort(cport, &server.cfd) == C_ERR ) {
            /* Note: the following log text is matched by the test suite. */
            serverLog(LL_WARNING, "Failed listening on port %u (cluster), aborting.", cport);
            exit(1);
        }
        
        if (createSocketAcceptHandler(&server.cfd, clusterAcceptHandler) != C_OK) {
            serverPanic("Unrecoverable error creating Redis Cluster socket accept handler.");
        }
        ...
    }
    ```

- **数据结构和节点信息初始化**
  - 调用 [`slotToKeyInit()`](https://github.com/redis/redis/blob/7.0.0/src/cluster.c#L6959) 函数初始化槽位到键的映射
  - 创建基数树 `slots_to_channels`，用于槽位与发布 / 订阅频道的关联。
  - 设置当前节点的端口信息，通过后续 MEET 消息交换 IP 地址。
  - 重置手动故障转移状态
  - 更新节点标志、IP 和主机名。

    ```c
    void clusterInit(void) {
        /* Initialize data for the Slot to key API. */
        slotToKeyInit(server.db);

        /* The slots -> channels map is a radix tree. Initialize it here. */
        server.cluster->slots_to_channels = raxNew();

        /* Set myself->port/cport/pport to my listening ports, we'll just need to
        * discover the IP address via MEET messages. */
        deriveAnnouncedPorts(&myself->port, &myself->pport, &myself->cport);

        server.cluster->mf_end = 0;
        server.cluster->mf_slave = NULL;
        resetManualFailover();
        clusterUpdateMyselfFlags();
        clusterUpdateMyselfIp();
        clusterUpdateMyselfHostname();
    }
    ```

## 数据分片

1. 数据分片（Sharding）
作用：将数据分散存储在多个节点上，突破单机内存和性能限制。
实现方式：
Redis Cluster 将数据划分为 16384 个哈希槽（Hash Slot），每个键通过 CRC16 算法计算出一个哈希值，再对 16384 取模，确定其所属的槽位。
集群中的每个主节点负责一部分哈希槽（例如：节点 A 负责槽 0-5000，节点 B 负责槽 5001-10000，依此类推）。
优势：
支持水平扩展：通过增加节点，可以动态分配槽位，提升存储容量和吞吐量。
数据分布均衡：槽位分配均匀时，负载会分散到各个节点。

### 智能路由

3. 客户端智能路由
作用：客户端可直接连接正确的节点执行操作，无需代理层。
实现方式：
客户端首次连接集群时，会获取一份 槽位-节点 映射表（Slot Map）。
执行命令时，客户端根据键计算槽位，直接发送请求到对应的主节点。
如果槽位不在当前节点（例如节点迁移或客户端缓存过期），节点会返回 MOVED 或 ASK 重定向指令，引导客户端连接正确节点。
优势：
减少中间层开销：相比代理模式（如 Twemproxy），性能更高。
支持多键操作：如果所有键属于同一槽位（通过 Hash Tag 强制指定），可执行事务、Lua 脚本等。

## 定时任务

## 主从复制

## 故障转移

2. 高可用性与自动故障转移
主从复制：
每个主节点（Master）可以有多个从节点（Slave），主节点负责写入，从节点异步复制数据。
当主节点宕机时，从节点会自动升级为主节点，继续提供服务。
故障检测与恢复：
节点间通过 Gossip 协议定期交换状态信息，检测节点是否存活。
若主节点故障，其他主节点会通过投票机制（类似 Raft 算法）触发故障转移，提升从节点为新主节点。
优势：
服务持续可用：故障转移通常在秒级完成，对客户端透明。
无需人工干预：集群自动处理节点故障。

## 弹性扩容

4. 线性扩展能力
动态扩缩容：
新增节点时，集群支持重新分配槽位（通过 CLUSTER ADDSLOTS 或工具自动迁移）。
删除节点时，其槽位会被转移到其他节点。
数据迁移：
槽位迁移过程中，数据会逐步从旧节点复制到新节点，期间集群仍可正常服务。
优势：
灵活应对业务增长：按需增减节点，避免资源浪费。

## 数据迁移

## Ref

- [Scale with Redis Cluster](https://redis.io/docs/latest/operate/oss_and_stack/management/scaling/)
- [redis源码解析 pdf redis cluster 源码]()
