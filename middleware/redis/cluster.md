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
  - 加载现有集群配置，包括各节点的主从关系、哈希槽分配、节点的连接状态、心跳包时间等
  - 若失败则创建新的配置文件
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

### 创建集群

当客户端执行 `CLUSTER CREATE` 命令时，[`clusterManagerCommandCreate()`](https://github.com/redis/redis/blob/7.0.0/src/redis-cli.c#L6108) 函数会将指定的节点创建为一个集群，包括哈希槽的分配、主从关系的绑定、节点握手等操作。

```bash
redis-cli --cluster create \
  127.0.0.1:7000 127.0.0.1:7001 127.0.0.1:7002 \
  127.0.0.1:7003 127.0.0.1:7004 127.0.0.1:7005 \
  --cluster-replicas 1
```

核心流程如下所示：

- **节点初始化**
  - 遍历入参，解析节点地址，并创建
  - 校验节点连接是否正常
  - 校验节点是否为集群模式
  - 加载节点信息
  - 校验节点是否为空
  - 将节点加入列表中

    ```c
    static int clusterManagerCommandCreate(int argc, char **argv) {
        ...
        cluster_manager.nodes = listCreate();
        for (i = 0; i < argc; i++) {
            ...
            char *ip = addr;
            int port = atoi(++c);
            clusterManagerNode *node = clusterManagerNewNode(ip, port);
            if (!clusterManagerNodeConnect(node)) {
                ...
            }
            char *err = NULL;
            if (!clusterManagerNodeIsCluster(node, &err)) {
                ...
            }
            err = NULL;
            if (!clusterManagerNodeLoadInfo(node, 0, &err)) {
                ...
            }
            err = NULL;
            if (!clusterManagerNodeIsEmpty(node, &err)) {
                ...
            }
            listAddNodeTail(cluster_manager.nodes, node);
        }
        ...
    }
    ```

- **校验节点数量**
  - 根据传入的所有节点数量以及主从配置，计算主节点数量
    - 例如传入 6 个节点，主从配置为 1，则最终为 3 主 3 从
  - 限制主节点数量（`masters_count`）大于等于 3 才能构成集群

    ```c
    static int clusterManagerCommandCreate(int argc, char **argv) {
        ...
        int node_len = cluster_manager.nodes->len;
        int replicas = config.cluster_manager_command.replicas;
        int masters_count = CLUSTER_MANAGER_MASTERS_COUNT(node_len, replicas);
        if (masters_count < 3) {
            clusterManagerLogErr(
                "*** ERROR: Invalid configuration for cluster creation.\n"
                "*** Redis Cluster requires at least 3 master nodes.\n"
                "*** This is not possible with %d nodes and %d replicas per node.",
                node_len, replicas);
            clusterManagerLogErr("\n*** At least %d nodes are required.\n",
                                3 * (replicas + 1));
            return 0;
        }
        ...
    }
    ```

- **节点 IP 分组**
  - 将节点按照 IP 进行分组

    ```c
    static int clusterManagerCommandCreate(int argc, char **argv) {
        ...
        while ((ln = listNext(&li)) != NULL) {
            clusterManagerNode *n = ln->value;
            int found = 0;
            for (i = 0; i < ip_count; i++) {
                char *ip = ips[i];
                if (!strcmp(ip, n->ip)) {
                    found = 1;
                    break;
                }
            }
            if (!found) {
                ips[ip_count++] = n->ip;
            }
            clusterManagerNodeArray *node_array = &(ip_nodes[i]);
            if (node_array->nodes == NULL)
                clusterManagerNodeArrayInit(node_array, node_len);
            clusterManagerNodeArrayAdd(node_array, n);
        }
        ...
    }
    ```

  - 按照 IP，重新将节点进行排序
  - 在绑定主从关系时，会优先用 `interleaved` 前半部分的节点作为主节点，该操作保障主节点仅可能不在同一 IP

    ```c
    static int clusterManagerCommandCreate(int argc, char **argv) {
        ...
        while (interleaved_len < node_len) {
            for (i = 0; i < ip_count; i++) {
                clusterManagerNodeArray *node_array = &(ip_nodes[i]);
                if (node_array->count > 0) {
                    clusterManagerNode *n = NULL;
                    clusterManagerNodeArrayShift(node_array, &n);
                    interleaved[interleaved_len++] = n;
                }
            }
        }
        ...
    }
    ```

- **分配哈希槽**
  - 将 16384 个槽平均分配给主节点（`slots_per_node`）
  - 在分配时，通过 `first` 和 `last` 计算每一个节点对应的槽位窗口
  - 节点以 bitmap 的方式存储自己对应的槽位信息（`master->slots`），并存储槽位总数（`master->slots_count`）
  - 在计算 `last` 时，通过 `lround` 函数向下取整，并通过 `cursor` 字段累计浮点值，避免误差
  - 对于最后一个节点或 `last` 超出槽位范围，强制将 `last` 设置为最后一个槽位
  - 最后会将主节点设置为脏数据（`master->dirty`），后续将其配置持久化存储

    ```c
    #define CLUSTER_MANAGER_SLOTS               16384

    static int clusterManagerCommandCreate(int argc, char **argv) {
        ...
        float slots_per_node = CLUSTER_MANAGER_SLOTS / (float) masters_count;
        long first = 0;
        float cursor = 0.0f;
        for (i = 0; i < masters_count; i++) {
            clusterManagerNode *master = masters[i];
            long last = lround(cursor + slots_per_node - 1);
            if (last > CLUSTER_MANAGER_SLOTS || i == (masters_count - 1))
                last = CLUSTER_MANAGER_SLOTS - 1;
            if (last < first) last = first;
            printf("Master[%d] -> Slots %ld - %ld\n", i, first, last);
            master->slots_count = 0;
            for (j = first; j <= last; j++) {
                master->slots[j] = 1;
                master->slots_count++;
            }
            master->dirty = 1;
            first = last + 1;
            cursor += slots_per_node;
        }
        ...
    }
    ```

- **主从配置**
  - 划分节点：`interleaved` 列表中，前半部分作为主节点，后半部分作为从节点

    ```c
    static int clusterManagerCommandCreate(int argc, char **argv) {
        ...
        clusterManagerNode **masters = interleaved;
        interleaved += masters_count;
        interleaved_len -= masters_count;
        ...
    }
    ```

  - 旋转从节点列表
  - 之前对节点重排序时，按照 IP 进行排序，该操作可减小主节点与从节点位于同一 IP 的概率

    ```c
    static int clusterManagerCommandCreate(int argc, char **argv) {
        ...
        /* Rotating the list sometimes helps to get better initial
        * anti-affinity before the optimizer runs. */
        clusterManagerNode *first_node = interleaved[0];
        for (i = 0; i < (interleaved_len - 1); i++)
            interleaved[i] = interleaved[i + 1];
        interleaved[interleaved_len - 1] = first_node;
        ...
    }
    ```

  - 绑定主从关系，为每一个主节点分配满足数量的从节点
  - 分配时，优先分配 ip 不同的从节点，确认分配后，将其置空
  - 如果未找到 ip 不同的从节点，则使用遍历到的第一个从节点（`firstNodeIdx`），并修改 `interleaved` 指针位置及剩余节点长度 `interleaved_len`
  - 该函数仅在从节点内部存储主节点的必要信息（`slave->replicate`），然后将其标记为脏数据（`slave->dirty`），后续将其配置持久化存储

    ```c
    static int clusterManagerCommandCreate(int argc, char **argv) {
        ...
        int assign_unused = 0, available_count = interleaved_len;
    assign_replicas:
        for (i = 0; i < masters_count; i++) {
            clusterManagerNode *master = masters[i];
            int assigned_replicas = 0;
            while (assigned_replicas < replicas) {
                if (available_count == 0) break;
                clusterManagerNode *found = NULL, *slave = NULL;
                int firstNodeIdx = -1;
                for (j = 0; j < interleaved_len; j++) {
                    clusterManagerNode *n = interleaved[j];
                    if (n == NULL) continue;
                    if (strcmp(n->ip, master->ip)) {
                        found = n;
                        interleaved[j] = NULL;
                        break;
                    }
                    if (firstNodeIdx < 0) firstNodeIdx = j;
                }
                if (found) slave = found;
                else if (firstNodeIdx >= 0) {
                    slave = interleaved[firstNodeIdx];
                    interleaved_len -= (firstNodeIdx + 1);
                    interleaved += (firstNodeIdx + 1);
                }
                if (slave != NULL) {
                    assigned_replicas++;
                    available_count--;
                    if (slave->replicate) sdsfree(slave->replicate);
                    slave->replicate = sdsnew(master->name);
                    slave->dirty = 1;
                } else break;
                printf("Adding replica %s:%d to %s:%d\n", slave->ip, slave->port,
                    master->ip, master->port);
                if (assign_unused) break;
            }
        }
        ...
    }
    ```

  - 如果主从分配未完成（`available_count > 0`），则再次执行一次

    ```c
    static int clusterManagerCommandCreate(int argc, char **argv) {
        ...
        int assign_unused = 0, available_count = interleaved_len;
    assign_replicas:
        for (i = 0; i < masters_count; i++) {
            ...
        }
        if (!assign_unused && available_count > 0) {
            assign_unused = 1;
            printf("Adding extra replicas...\n");
            goto assign_replicas;
        }
        ...
    }
    ```

  - 重置 IP 对应的节点数组
  - 调用 [`clusterManagerOptimizeAntiAffinity()`](https://github.com/redis/redis/blob/7.0.0/src/redis-cli.c#L3553) 函数，优化主从节点的分布情况，仅可能避免同一主节点的副本在同一 IP 下

    ```c
    static int clusterManagerCommandCreate(int argc, char **argv) {
        ...
        for (i = 0; i < ip_count; i++) {
            clusterManagerNodeArray *node_array = ip_nodes + i;
            clusterManagerNodeArrayReset(node_array);
        }
        clusterManagerOptimizeAntiAffinity(ip_nodes, ip_count);
        clusterManagerShowNodes();
        ...
    }
    ```

- **处理节点配置**
  - 遍历所有节点，调用 [`clusterManagerFlushNodeConfig()`](https://github.com/redis/redis/blob/7.0.0/src/redis-cli.c#L4482) 函数刷新其配置
  - 对于从节点，会执行 `CLUSTER REPLICATE` 命令，绑定主从关系
  - 对于主节点，会调用 [`clusterManagerAddSlots()`](https://github.com/redis/redis/blob/7.0.0/src/redis-cli.c#L3879) 函数执行 `CLUSTER ADDSLOTS` 命令，按照之前的配置，添加哈希槽至当前节点

    ```c
    static int clusterManagerCommandCreate(int argc, char **argv) {
        ...
        int ignore_force = 0;
        if (confirmWithYes("Can I set the above configuration?", ignore_force)) {
            ...
    l      istRewind(cluster_manager.nodes, &li);
            while ((ln = listNext(&li)) != NULL) {
                clusterManagerNode *node = ln->value;
                char *err = NULL;
                int flushed = clusterManagerFlushNodeConfig(node, &err);
                ...
            }
            ...
        }
        clusterManagerLogInfo(">>> Nodes configuration updated\n");
        ...
    }

    static int clusterManagerFlushNodeConfig(clusterManagerNode *node, char **err) {
        if (!node->dirty) return 0;
        redisReply *reply = NULL;
        int is_err = 0, success = 1;
        if (err != NULL) *err = NULL;
        if (node->replicate != NULL) {
            reply = CLUSTER_MANAGER_COMMAND(node, "CLUSTER REPLICATE %s",
                                            node->replicate);
            if (reply == NULL || (is_err = (reply->type == REDIS_REPLY_ERROR))) {
                if (is_err && err != NULL) {
                    *err = zmalloc((reply->len + 1) * sizeof(char));
                    strcpy(*err, reply->str);
                }
                success = 0;
                /* If the cluster did not already joined it is possible that
                * the slave does not know the master node yet. So on errors
                * we return ASAP leaving the dirty flag set, to flush the
                * config later. */
                goto cleanup;
            }
        } else {
            int added = clusterManagerAddSlots(node, err);
            if (!added || *err != NULL) success = 0;
        }
        node->dirty = 0;
    cleanup:
        if (reply != NULL) freeReplyObject(reply);
        return success;
    }
    ```

  - 为所有节点更新配置版本
  - 不同的节点配置，通过递增来处理，避免版本冲突

    ```c
    static int clusterManagerCommandCreate(int argc, char **argv) {
        ...
        int ignore_force = 0;
        if (confirmWithYes("Can I set the above configuration?", ignore_force)) {
            ...
    l       clusterManagerLogInfo(">>> Assign a different config epoch to "
                                "each node\n");
            int config_epoch = 1;
            listRewind(cluster_manager.nodes, &li);
            while ((ln = listNext(&li)) != NULL) {
                clusterManagerNode *node = ln->value;
                redisReply *reply = NULL;
                reply = CLUSTER_MANAGER_COMMAND(node,
                                                "cluster set-config-epoch %d",
                                                config_epoch++);
                if (reply != NULL) freeReplyObject(reply);
            }
            ...
        }
        ...
    }
    ```

- **节点握手**
  - 选择第一个节点，通过 `CLUSTER MEET` 命令，与其他节点互联，形成集群
  
    ```c
    static int clusterManagerCommandCreate(int argc, char **argv) {
        ...
        int ignore_force = 0;
        if (confirmWithYes("Can I set the above configuration?", ignore_force)) {
            ...
            clusterManagerLogInfo(">>> Sending CLUSTER MEET messages to join "
                                "the cluster\n");
            clusterManagerNode *first = NULL;
            char first_ip[NET_IP_STR_LEN]; /* first->ip may be a hostname */
            listRewind(cluster_manager.nodes, &li);
            while ((ln = listNext(&li)) != NULL) {
                clusterManagerNode *node = ln->value;
                if (first == NULL) {
                    first = node;
                    ...
                    continue;
                }
                redisReply *reply = NULL;
                reply = CLUSTER_MANAGER_COMMAND(node, "cluster meet %s %d",
                                                first_ip, first->port);
                ...
            }
            ...
        }
        ...
    }
    ```

  - 等待节点握手完成，再次调用 [`clusterManagerFlushNodeConfig()`](https://github.com/redis/redis/blob/7.0.0/src/redis-cli.c#L4482) 函数刷新其配置

    ```c
    static int clusterManagerCommandCreate(int argc, char **argv) {
        ...
        int ignore_force = 0;
        if (confirmWithYes("Can I set the above configuration?", ignore_force)) {
            ...
            /* Give one second for the join to start, in order to avoid that
            * waiting for cluster join will find all the nodes agree about
            * the config as they are still empty with unassigned slots. */
            sleep(1);
            clusterManagerWaitForClusterJoin();
            /* Useful for the replicas */
            listRewind(cluster_manager.nodes, &li);
            while ((ln = listNext(&li)) != NULL) {
                clusterManagerNode *node = ln->value;
                if (!node->dirty) continue;
                char *err = NULL;
                int flushed = clusterManagerFlushNodeConfig(node, &err);
                ...
            }
            ...
        }
        ...
    }
    ```

  - 清空节点列表，保留第一个节点并释放其他节点
  - 调用 [`clusterManagerLoadInfoFromNode()`](https://github.com/redis/redis/blob/7.0.0/src/redis-cli.c#L4753) 函数从第一个节点中，重新加载集群节点信息

    ```c
    static int clusterManagerCommandCreate(int argc, char **argv) {
        ...
        int ignore_force = 0;
        if (confirmWithYes("Can I set the above configuration?", ignore_force)) {
            ...
            // Reset Nodes
            listRewind(cluster_manager.nodes, &li);
            clusterManagerNode *first_node = NULL;
            while ((ln = listNext(&li)) != NULL) {
                clusterManagerNode *node = ln->value;
                if (!first_node) first_node = node;
                else freeClusterManagerNode(node);
            }
            listEmpty(cluster_manager.nodes);
            if (!clusterManagerLoadInfoFromNode(first_node)) {
                success = 0;
                goto cleanup;
            }
            clusterManagerCheckCluster(0);
        }
        ...
    }
    ```

### REPLICATE

在 [`clusterCommand()`](https://github.com/redis/redis/blob/7.0.0/src/cluster.c#L5165) 函数中，针对 `CLUSTER REPLICATE` 命令做一些异常判断，然后调用 [`clusterSetMaster()`](https://github.com/redis/redis/blob/7.0.0/src/cluster.c#L4545) 函数去执行主从关系的绑定。

```c
void clusterCommand(client *c) {
    ...
    if (!strcasecmp(c->argv[1]->ptr,"replicate") && c->argc == 3) {
        /* CLUSTER REPLICATE <NODE ID> */
        /* Lookup the specified node in our table. */
        clusterNode *n = clusterLookupNode(c->argv[2]->ptr, sdslen(c->argv[2]->ptr));
        if (!n) {
            addReplyErrorFormat(c,"Unknown node %s", (char*)c->argv[2]->ptr);
            return;
        }

        /* I can't replicate myself. */
        if (n == myself) {
            addReplyError(c,"Can't replicate myself");
            return;
        }

        /* Can't replicate a slave. */
        if (nodeIsSlave(n)) {
            addReplyError(c,"I can only replicate a master, not a replica.");
            return;
        }

        /* If the instance is currently a master, it should have no assigned
         * slots nor keys to accept to replicate some other node.
         * Slaves can switch to another master without issues. */
        if (nodeIsMaster(myself) &&
            (myself->numslots != 0 || dictSize(server.db[0].dict) != 0)) {
            addReplyError(c,
                "To set a master the node must be empty and "
                "without assigned slots.");
            return;
        }

        /* Set the master. */
        clusterSetMaster(n);
        clusterDoBeforeSleep(CLUSTER_TODO_UPDATE_STATE|CLUSTER_TODO_SAVE_CONFIG);
        addReply(c,shared.ok);
    } 
    ...
}
```

[`clusterSetMaster()`](https://github.com/redis/redis/blob/7.0.0/src/cluster.c#L4545) 函数内部，针对当前节点是主节点以及当前节点已绑定其他主节点的两种情况做了数据清理工作，然后更新当前配置，最终调用 [`replicationSetMaster()`](https://github.com/redis/redis/blob/7.0.0/src/replication.c#L2908) 函数绑定主从关系。

```c
/* Set the specified node 'n' as master for this node.
 * If this node is currently a master, it is turned into a slave. */
void clusterSetMaster(clusterNode *n) {
    serverAssert(n != myself);
    serverAssert(myself->numslots == 0);

    if (nodeIsMaster(myself)) {
        myself->flags &= ~(CLUSTER_NODE_MASTER|CLUSTER_NODE_MIGRATE_TO);
        myself->flags |= CLUSTER_NODE_SLAVE;
        clusterCloseAllSlots();
    } else {
        if (myself->slaveof)
            clusterNodeRemoveSlave(myself->slaveof,myself);
    }
    myself->slaveof = n;
    clusterNodeAddSlave(n,myself);
    replicationSetMaster(n->ip, n->port);
    resetManualFailover();
}
```

### MEET

在 [`clusterCommand()`](https://github.com/redis/redis/blob/7.0.0/src/cluster.c#L5165) 函数中，针对 `CLUSTER MEET` 命令做一些异常判断，然后调用 [`clusterStartHandshake()`](https://github.com/redis/redis/blob/7.0.0/src/cluster.c#L15705) 函数去启动握手操作。

```c
void clusterCommand(client *c) {
    ...
    if (!strcasecmp(c->argv[1]->ptr,"meet") && (c->argc == 4 || c->argc == 5)) {
        /* CLUSTER MEET <ip> <port> [cport] */
        long long port, cport;

        if (getLongLongFromObject(c->argv[3], &port) != C_OK) {
            addReplyErrorFormat(c,"Invalid TCP base port specified: %s",
                                (char*)c->argv[3]->ptr);
            return;
        }

        if (c->argc == 5) {
            if (getLongLongFromObject(c->argv[4], &cport) != C_OK) {
                addReplyErrorFormat(c,"Invalid TCP bus port specified: %s",
                                    (char*)c->argv[4]->ptr);
                return;
            }
        } else {
            cport = port + CLUSTER_PORT_INCR;
        }

        if (clusterStartHandshake(c->argv[2]->ptr,port,cport) == 0 &&
            errno == EINVAL)
        {
            addReplyErrorFormat(c,"Invalid node address specified: %s:%s",
                            (char*)c->argv[2]->ptr, (char*)c->argv[3]->ptr);
        } else {
            addReply(c,shared.ok);
        }
    }
    ...
}
```

[`clusterStartHandshake()`](https://github.com/redis/redis/blob/7.0.0/src/cluster.c#L15705) 函数会将目标节点添加至集群中，并添加 `HANDSHAKE` 与 `MEET` 标志位，后续会在定时任务中触发建连操作

- 针对于 `MEET` 标记，会发送 `Meet` 消息给目标节点，取代 `Ping` 消息，如 [`clusterLinkConnectHandler()`](https://github.com/redis/redis/blob/7.0.0/src/cluster.c#L2618) 函数所示
- 针对于 `HANDSHAKE` 标记，会在节点间首次完成 `Ping/Pong` 交互后，取消标记

    ```c
    int clusterStartHandshake(char *ip, int port, int cport) {
        ...
        /* Add the node with a random address (NULL as first argument to
        * createClusterNode()). Everything will be fixed during the
        * handshake. */
        n = createClusterNode(NULL,CLUSTER_NODE_HANDSHAKE|CLUSTER_NODE_MEET);
        memcpy(n->ip,norm_ip,sizeof(n->ip));
        n->port = port;
        n->cport = cport;
        clusterAddNode(n);
        return 1;
    }

    void clusterLinkConnectHandler(connection *conn) {
        ...
        /* Queue a PING in the new connection ASAP: this is crucial
        * to avoid false positives in failure detection.
        *
        * If the node is flagged as MEET, we send a MEET message instead
        * of a PING one, to force the receiver to add us in its node
        * table. */
        clusterSendPing(link, node->flags & CLUSTER_NODE_MEET ?
                CLUSTERMSG_TYPE_MEET : CLUSTERMSG_TYPE_PING);
        ...
    }
    ```

## 数据分片

Cluster 使用了基于虚拟槽分区的一致性哈希作为分片方式，将所有数据划分为 16384 个哈希槽（Hash Slot），每个键通过 CRC16 算法计算出一个哈希值，再对 16384 取模，确定其所属的槽位，进而确定所在节点。

集群中的每个主节点负责一部分哈希槽（例如：节点 A 负责槽 0-5000，节点 B 负责槽 5001-10000，依此类推），哈希槽可以在创建节点时，由 Redis 平均进行分配，也可以由运维人员手动进行配置。

相较于传统哈希，一致性哈希在数据迁移时，有较大优势：

- 传统哈希会按照节点数量来取模，这会导致在增加或删除节点数量时，绝大部分数据需要重新计算哈希值，进而引发大规模缓存失效
  - 假设之前的节点数量为 n，扩容后的节点数量为 m，那么仅有 $\frac{n}{lcm(n,m)}$ （lcm 为最小公倍数）的数据，取模结果不发生改变，其他元素都需要重新进行分配

- 一致性哈希会按照固定的哈希槽的数量来取模，然后再将哈希槽分配给所有主节点，在增加或删除节点数量时，动态调整哈希槽和节点的绑定关系即可
  - 在迁移时，以哈希槽为单位进行迁移，且在调整槽和节点的关系时，可以通过一定的算法逻辑，保障影响的槽位仅可能的少
  - 在迁移过程中，可以保持先保持槽位和原本节点的绑定关系不变，迁移完成后再进行修改，可以保障在扩容期间，用户可以正常访问数据，保障服务的高可用

### 智能路由

在集群环境下，数据会分片进行存储，所以客户端在执行命令时，需要根据键值计算槽位，然后根据本地缓存的槽位和主节点的映射表，发送请求到对应的主节点。

如果是发生数据迁移导致的查询未命中，所请求的节点会返回 MOVED 或 ASK 重定向指令，引导客户端连接正确节点。在高版本的 redis 中，会自动重定向至新的节点，将 MOVED 或 ASK 重定向指令和重定向后的请求结果，一起返回给客户端。

Redis 服务器在处理命令时，即 [`processCommand()`](https://github.com/redis/redis/blob/7.0.0/src/server.c#L3565) 函数中，如果是集群模式，会调用 [`getNodeByQuery()`](https://github.com/redis/redis/blob/7.0.0/src/cluster.c#L6580) 函数来获取集群节点：

- 如果获取失败，或不是当前节点，会调用 [`clusterRedirectClient()`](https://github.com/redis/redis/blob/7.0.0/src/cluster.c#L6792) 函数封装错误信息返回给客户端
- 如果是当前节点，则会继续走其他校验逻辑，直至最终执行该命令

    ```c
    int processCommand(client *c) {
        ...
        /* If cluster is enabled perform the cluster redirection here.
        * However we don't perform the redirection if:
        * 1) The sender of this command is our master.
        * 2) The command has no key arguments. */
        if (server.cluster_enabled &&
            !mustObeyClient(c) &&
            !(!(c->cmd->flags&CMD_MOVABLE_KEYS) && c->cmd->key_specs_num == 0 &&
            c->cmd->proc != execCommand))
        {
            int error_code;
            clusterNode *n = getNodeByQuery(c,c->cmd,c->argv,c->argc,
                                            &c->slot,&error_code);
            if (n == NULL || n != server.cluster->myself) {
                if (c->cmd->proc == execCommand) {
                    discardTransaction(c);
                } else {
                    flagTransaction(c);
                }
                clusterRedirectClient(c,n,c->slot,error_code);
                c->cmd->rejected_calls++;
                return C_OK;
            }
        }
        ...
    }
    ```

对于 redis-cli 来说，在 [`cliReadReply()`](https://github.com/redis/redis/blob/7.0.0/src/redis-cli.c#L1640) 函数中，会针对性的判断 MOVED 或 ASK 重定向指令并解析相关参数，然后更新 `config.cluster_reissue_command` 状态位，后续客户端会按照新的节点地址，重试执行命令。

```c
static int cliReadReply(int output_raw_strings) {
    ...
    /* Check if we need to connect to a different node and reissue the
     * request. */
    if (config.cluster_mode && reply->type == REDIS_REPLY_ERROR &&
        (!strncmp(reply->str,"MOVED ",6) || !strncmp(reply->str,"ASK ",4)))
    {
        char *p = reply->str, *s;
        int slot;

        output = 0;
        /* Comments show the position of the pointer as:
         *
         * [S] for pointer 's'
         * [P] for pointer 'p'
         */
        s = strchr(p,' ');      /* MOVED[S]3999 127.0.0.1:6381 */
        p = strchr(s+1,' ');    /* MOVED[S]3999[P]127.0.0.1:6381 */
        *p = '\0';
        slot = atoi(s+1);
        s = strrchr(p+1,':');    /* MOVED 3999[P]127.0.0.1[S]6381 */
        *s = '\0';
        sdsfree(config.conn_info.hostip);
        config.conn_info.hostip = sdsnew(p+1);
        config.conn_info.hostport = atoi(s+1);
        if (config.interactive)
            printf("-> Redirected to slot [%d] located at %s:%d\n",
                slot, config.conn_info.hostip, config.conn_info.hostport);
        config.cluster_reissue_command = 1;
        if (!strncmp(reply->str,"ASK ",4)) {
            config.cluster_send_asking = 1;
        }
        cliRefreshPrompt();
    }
    ...
}
```

### 获取实际节点

[`getNodeByQuery()`](https://github.com/redis/redis/blob/7.0.0/src/cluster.c#L6580) 函数用于在集群模式下，确定该命令具体由哪个节点进行处理，确保在分布式场景下，能够重定向至正确的节点，并处理异常状态。

关键设计要点

- 槽一致性：确保多键命令的所有键属于同一槽，否则拒绝。
- 槽状态感知：处理迁移/导入中的槽，动态决策重定向类型（ASK 或 MOVED）。
- 集群容错：在集群异常时，根据配置限制操作，保证数据安全。
- 性能优化：允许从节点处理只读请求，降低主节点负载。

核心逻辑如下所示：

- **前置检查**
  - 模块禁用重定向：若模块标记 `CLUSTER_MODULE_FLAG_NO_REDIRECTION`，直接返回本地节点（`myself`），不进行任何重定向。

    ```c
    clusterNode *getNodeByQuery(client *c, struct redisCommand *cmd, robj **argv, int argc, int *hashslot, int *error_code) {
        ...
        /* Allow any key to be set if a module disabled cluster redirections. */
        if (server.cluster_module_flags & CLUSTER_MODULE_FLAG_NO_REDIRECTION)
            return myself;
        ...
    }
    ```

- **校验槽位**
- 遍历所有命令的键：通过getKeysFromCommand解析每个命令涉及的键。

    ```c
    clusterNode *getNodeByQuery(client *c, struct redisCommand *cmd, robj **argv, int argc, int *hashslot, int *error_code) {
        ...
        for (i = 0; i < ms->count; i++) {
            ...
            getKeysResult result = GETKEYS_RESULT_INIT;
            numkeys = getKeysFromCommand(mcmd,margv,margc,&result);
            keyindex = result.keys;
            for (j = 0; j < numkeys; j++) {
                robj *thiskey = margv[keyindex[j].pos];
                int thisslot = keyHashSlot((char*)thiskey->ptr,
                                        sdslen(thiskey->ptr));
                ...
            }
            getKeysFreeResult(&result);
        }
        ...
    }
    ```

  - 优先确定第一个 key 对应的槽位和节点，后续基于此进行槽位的一致性校验
  - 若槽无节点负责，返回 `CLUSTER_REDIR_DOWN_UNBOUND` 错误
  - 若槽属于当前节点，且处于迁出（`migrating_slots_to`）状态，即从别的节点迁出至该节点，标记 `migrating_slot`
  - 若槽处于导入（`importing_slots_from`）状态，即从别的节点导入至该节点，标记 `importing_slot`

    ```c
    clusterNode *getNodeByQuery(client *c, struct redisCommand *cmd, robj **argv, int argc, int *hashslot, int *error_code) {
        ...
        for (i = 0; i < ms->count; i++) {
            ...
            for (j = 0; j < numkeys; j++) {
                ...
                if (firstkey == NULL) {
                    firstkey = thiskey;
                    slot = thisslot;
                    n = server.cluster->slots[slot];

                    if (n == NULL) {
                        getKeysFreeResult(&result);
                        if (error_code)
                            *error_code = CLUSTER_REDIR_DOWN_UNBOUND;
                        return NULL;
                    }

                    if (n == myself &&
                        server.cluster->migrating_slots_to[slot] != NULL)
                    {
                        migrating_slot = 1;
                    } else if (server.cluster->importing_slots_from[slot] != NULL) {
                        importing_slot = 1;
                    }
                }
                ...
            }
            ...
        }
        ...
    }
    ```

  - 校验所有键属于同一槽，否则返回 `CLUSTER_REDIR_CROSS_SLOT` 错误
  - 设置多键标记位 `multiple_keys`

    ```c
    clusterNode *getNodeByQuery(client *c, struct redisCommand *cmd, robj **argv, int argc, int *hashslot, int *error_code) {
        ...
        for (i = 0; i < ms->count; i++) {
            ...
            for (j = 0; j < numkeys; j++) {
                ...
                if (firstkey == NULL) {
                    ...
                } else {
                    /* If it is not the first key/channel, make sure it is exactly
                    * the same key/channel as the first we saw. */
                    if (!equalStringObjects(firstkey,thiskey)) {
                        if (slot != thisslot) {
                            /* Error: multiple keys from different slots. */
                            getKeysFreeResult(&result);
                            if (error_code)
                                *error_code = CLUSTER_REDIR_CROSS_SLOT;
                            return NULL;
                        } else {
                            /* Flag this request as one with multiple different
                            * keys/channels. */
                            multiple_keys = 1;
                        }
                    }
                }
                ...
            }
            ...
        }
        ...
    }
    ```

  - 若多键且槽不稳定（迁移或导入中），统计缺失的键数量（`missing_keys`）。

    ```c
    clusterNode *getNodeByQuery(client *c, struct redisCommand *cmd, robj **argv, int argc, int *hashslot, int *error_code) {
        ...
        for (i = 0; i < ms->count; i++) {
            ...
            for (j = 0; j < numkeys; j++) {
                ...
                int flags = LOOKUP_NOTOUCH | LOOKUP_NOSTATS | LOOKUP_NONOTIFY;
                if ((migrating_slot || importing_slot) && !is_pubsubshard &&
                    lookupKeyReadWithFlags(&server.db[0], thiskey, flags) == NULL)
                {
                    missing_keys++;
                }
            }
            ...
        }
        ...
    }
    ```

  - 获取节点异常时，返回当前节点

    ```c
    clusterNode *getNodeByQuery(client *c, struct redisCommand *cmd, robj **argv, int argc, int *hashslot, int *error_code) {
        ...
        /* No key at all in command? then we can serve the request
        * without redirections or errors in all the cases. */
        if (n == NULL) return myself;
        ...
    }
    ```

- **集群宕机处理**
  - 若集群状态非 `CLUSTER_OK`，根据配置决定是否允许读取
  - 针对发布订阅命令，检查 `cluster_allow_pubsubshard_when_down` 配置
  - 校验是否允许读命令 `cluster_allow_reads_when_down`
  - 校验是否为写命令，宕机期间不允许写命令，但是可通过上述配置允许读命令

    ```c
    clusterNode *getNodeByQuery(client *c, struct redisCommand *cmd, robj **argv, int argc, int *hashslot, int *error_code) {
        ...
        /* Cluster is globally down but we got keys? We only serve the request
        * if it is a read command and when allow_reads_when_down is enabled. */
        if (server.cluster->state != CLUSTER_OK) {
            if (pubsubshard_included) {
                if (!server.cluster_allow_pubsubshard_when_down) {
                    if (error_code) *error_code = CLUSTER_REDIR_DOWN_STATE;
                    return NULL;
                }
            } else if (!server.cluster_allow_reads_when_down) {
                if (error_code) *error_code = CLUSTER_REDIR_DOWN_STATE;
                return NULL;
            } else if (cmd_flags & CMD_WRITE) {
                if (error_code) *error_code = CLUSTER_REDIR_DOWN_RO_STATE;
                return NULL;
            } else {

            }
        }
        ...
    }
    ```

- **槽迁移 / 导入处理**
  - 修改 `hashslot` 引用
  - 若槽在迁移中且命令是 `MIGRATE`，直接由本地节点处理
  - 针对迁移状态（`migrating_slot`）且缺失键（`missing_keys > 0`）
    - 返回 `CLUSTER_REDIR_ASK` 错误，即 `ask` 重定向，并返回目标节点
  - 针对导入状态（`importing_slot`）且是 `ASKING` 命令，无法继续重定向逻辑
    - 如果是且不缺键，返回本地节点
    - 若多键且缺键，返回 `CLUSTER_REDIR_UNSTABLE` 错误。

    ```c
    clusterNode *getNodeByQuery(client *c, struct redisCommand *cmd, robj **argv, int argc, int *hashslot, int *error_code) {
        ...
        if (hashslot) *hashslot = slot;

        if ((migrating_slot || importing_slot) && cmd->proc == migrateCommand)
            return myself;

        if (migrating_slot && missing_keys) {
            if (error_code) *error_code = CLUSTER_REDIR_ASK;
            return server.cluster->migrating_slots_to[slot];
        }

        if (importing_slot &&
            (c->flags & CLIENT_ASKING || cmd->flags & CMD_ASKING))
        {
            if (multiple_keys && missing_keys) {
                if (error_code) *error_code = CLUSTER_REDIR_UNSTABLE;
                return NULL;
            } else {
                return myself;
            }
        }
        ...
    }
    ```

- **只读请求优化**
  - 若当前节点是从节点，主节点负责该槽，且请求是只读的，直接由本地节点处理，避免重定向

    ```c
    clusterNode *getNodeByQuery(client *c, struct redisCommand *cmd, robj **argv, int argc, int *hashslot, int *error_code) {
        ...
        int is_write_command = (c->cmd->flags & CMD_WRITE) ||
                            (c->cmd->proc == execCommand && (c->mstate.cmd_flags & CMD_WRITE));
        if (((c->flags & CLIENT_READONLY) || is_pubsubshard) &&
            !is_write_command &&
            nodeIsSlave(myself) &&
            myself->slaveof == n)
        {
            return myself;
        }
        ...
    }
    ```

- **最终决策**
  - 若目标节点非本地（`n != myself`），返回 `CLUSTER_REDIR_MOVED` 错误，即 `moved` 重定向
  - 返回最终节点

    ```c
    clusterNode *getNodeByQuery(client *c, struct redisCommand *cmd, robj **argv, int argc, int *hashslot, int *error_code) {
        ...
        /* Base case: just return the right node. However if this node is not
        * myself, set error_code to MOVED since we need to issue a redirection. */
        if (n != myself && error_code) *error_code = CLUSTER_REDIR_MOVED;
        return n;
        ...
    }
    ```

## 定时任务

服务器的定时任务 [`serverCron()`](https://github.com/redis/redis/blob/7.0.0/src/server.c#L1157) 中，会每 100ms 调用一次 Cluster 的定时任务 [`clusterCron()`](https://github.com/redis/redis/blob/7.0.0/src/cluster.c#L3975)。

```c
int serverCron(struct aeEventLoop *eventLoop, long long id, void *clientData) {
    ...
    run_with_period(100) {
        if (server.cluster_enabled) clusterCron();
    }
    ...
}
```

[`clusterCron()`](https://github.com/redis/redis/blob/7.0.0/src/cluster.c#L3975) 函数负责维护集群状态、处理节点间通信、故障检测与恢复等关键操作，其核心逻辑如下所示：

- **初始化**
  - 调用 [`clusterUpdateMyselfHostname()`](https://github.com/redis/redis/blob/7.0.0/src/cluster.c#L601) 函数更新主机名
  - 设置握手超时时间 `handshake_timeout`，并限制最少为 1000 ms

    ```c
    void clusterCron(void) {
        ...
        clusterUpdateMyselfHostname();

        /* The handshake timeout is the time after which a handshake node that was
        * not turned into a normal node is removed from the nodes. Usually it is
        * just the NODE_TIMEOUT value, but when NODE_TIMEOUT is too small we use
        * the value of 1 second. */
        handshake_timeout = server.cluster_node_timeout;
        if (handshake_timeout < 1000) handshake_timeout = 1000;
        ...
    }
    ```

- **维护节点连接**
  - 调用 [`clusterNodeCronResizeBuffers()](https://github.com/redis/redis/blob/7.0.0/src/cluster.c#L3935) 函数调整连接的缓冲区，仅可能减小
  - 调用 [`clusterNodeCronFreeLinkOnBufferLimitReached()`](https://github.com/redis/redis/blob/7.0.0/src/cluster.c#L3955) 函数释放缓冲区过大的连接
  - 调用 [`clusterNodeCronUpdateClusterLinksMemUsage()`](https://github.com/redis/redis/blob/7.0.0/src/cluster.c#L3969) 函数统计集群连接内存占用情况
  - 调用 [`clusterNodeCronHandleReconnect()`](https://github.com/redis/redis/blob/7.0.0/src/cluster.c#L3887) 函数，针对失联的场景，重新建立连接

    ```c
    void clusterCron(void) {
        ...
        while((de = dictNext(di)) != NULL) {
            clusterNode *node = dictGetVal(de);
            /* The sequence goes:
            * 1. We try to shrink link buffers if possible.
            * 2. We free the links whose buffers are still oversized after possible shrinking.
            * 3. We update the latest memory usage of cluster links.
            * 4. We immediately attempt reconnecting after freeing links.
            */
            clusterNodeCronResizeBuffers(node);
            clusterNodeCronFreeLinkOnBufferLimitReached(node);
            clusterNodeCronUpdateClusterLinksMemUsage(node);
            if(clusterNodeCronHandleReconnect(node, handshake_timeout, now)) continue;
        }
        ...
    }
    ```

- **随机心跳**
  - 每十次定时任务中，随机选取 5 个节点，向 `pong_received` 最小，即最早收到 `pong` 消息的节点发送 `ping` 消息

    ```c
    void clusterCron(void) {
        ...
        if (!(iteration % 10)) {
            int j;
            for (j = 0; j < 5; j++) {
                de = dictGetRandomKey(server.cluster->nodes);
                clusterNode *this = dictGetVal(de);

                if (this->link == NULL || this->ping_sent != 0) continue;
                if (this->flags & (CLUSTER_NODE_MYSELF|CLUSTER_NODE_HANDSHAKE))
                    continue;
                if (min_pong_node == NULL || min_pong > this->pong_received) {
                    min_pong_node = this;
                    min_pong = this->pong_received;
                }
            }
            if (min_pong_node) {
                serverLog(LL_DEBUG,"Pinging node %.40s", min_pong_node->name);
                clusterSendPing(min_pong_node->link, CLUSTERMSG_TYPE_PING);
            }
        }
        ...
    }
    ```

- **统计主从状态**
  - 统计孤儿主节点数量（`orphaned_masters`）
    - 从节点数量为 0，槽位不为 0，且标志位 `CLUSTER_NODE_MIGRATE_TO` 不为空
  - 统计所有节点中，从节点的最大数量（`max_slaves`）
  - 如果当前节点为从节点，记录其对应的主节点的从节点数量（`this_slaves`）

    ```c
    void clusterCron(void) {
        ...
        while((de = dictNext(di)) != NULL) {
            ...
            if (nodeIsSlave(myself) && nodeIsMaster(node) && !nodeFailed(node)) {
                int okslaves = clusterCountNonFailingSlaves(node);

                if (okslaves == 0 && node->numslots > 0 &&
                    node->flags & CLUSTER_NODE_MIGRATE_TO)
                {
                    orphaned_masters++;
                }
                if (okslaves > max_slaves) max_slaves = okslaves;
                if (nodeIsSlave(myself) && myself->slaveof == node)
                    this_slaves = okslaves;
            }
            ...
        }
        ...
    }
    ```

- **更新连接**
  - 针对长时间无响应的节点，主动释放连接，在下次任务中会进行重连，判断条件为：
    - 处于连接状态，且连接时间超过超时时间
    - 已经发送 `ping` 命令，但未收到响应
    - 距离发送 `ping` 命令和上次收到数据，超过超时时间的一半

    ```c
    void clusterCron(void) {
        ...
        while((de = dictNext(di)) != NULL) {
            ...
            mstime_t ping_delay = now - node->ping_sent;
            mstime_t data_delay = now - node->data_received;
            if (node->link &&
                now - node->link->ctime >
                server.cluster_node_timeout &&
                node->ping_sent &&
                ping_delay > server.cluster_node_timeout/2 &&
                data_delay > server.cluster_node_timeout/2)
            {
                /* Disconnect the link, it will be reconnected automatically. */
                freeClusterLink(node->link);
            }
            ...
        }
        ...
    }
    ```

- **固定条件心跳**
  - 针对上次距离上次发送 `ping` 命令超过超时时间的节点，发送 `ping` 命令
  - 如果当前节点为主节点，且目标节点为正在执行故障转移的从节点，发送 `ping` 命令
    - 在故障转移期间，保障高频通信，确保状态及时更新

    ```c
    void clusterCron(void) {
        ...
        while((de = dictNext(di)) != NULL) {
            ...
            if (node->link &&
                node->ping_sent == 0 &&
                (now - node->pong_received) > server.cluster_node_timeout/2)
            {
                clusterSendPing(node->link, CLUSTERMSG_TYPE_PING);
                continue;
            }

            if (server.cluster->mf_end &&
                nodeIsMaster(myself) &&
                server.cluster->mf_slave == node &&
                node->link)
            {
                clusterSendPing(node->link, CLUSTERMSG_TYPE_PING);
                continue;
            }
            ...
        }
        ...
    }
    ```

- **主观下线**
  - 当 `ping` 命令响应和数据响应均超时，且未标记故障，则添加主观下线标记位 `CLUSTER_NODE_PFAIL`
  - 设置更新标记位 `update_state`，后续将更新集群配置，并广播

    ```c
    void clusterCron(void) {
        ...
        while((de = dictNext(di)) != NULL) {
            ...
            mstime_t node_delay = (ping_delay < data_delay) ? ping_delay :
                                                            data_delay;

            if (node_delay > server.cluster_node_timeout) {
                /* Timeout reached. Set the node as possibly failing if it is
                * not already in this state. */
                if (!(node->flags & (CLUSTER_NODE_PFAIL|CLUSTER_NODE_FAIL))) {
                    serverLog(LL_DEBUG,"*** NODE %.40s possibly failing",
                        node->name);
                    node->flags |= CLUSTER_NODE_PFAIL;
                    update_state = 1;
                }
            }
        }
        ...
    }
    ```

- **更新主从关系**
  - 调用 [`replicationSetMaster()`](https://github.com/redis/redis/blob/7.0.0/src/replication.c#L2908) 函数，重新绑定主从关系，其触发条件为：
    - 当前节点为从节点
    - 未绑定主节点，且集群中存在有效的主节点地址
  - 调用 [`clusterHandleSlaveMigration()`](https://github.com/redis/redis/blob/7.0.0/src/cluster.c#L3716) 函数执行从节点迁移，提高孤儿节点的可用性，其触发条件为：
    - 存在孤儿主节点（`orphaned_masters`）
    - 当前节点为从节点，且其主节点的从节点数量最多并超过 1 个
    - 设置允许自动迁移标记位 `cluster_allow_replica_migration`

    ```c
    void clusterCron(void) {
        ...
        /* If we are a slave node but the replication is still turned off,
        * enable it if we know the address of our master and it appears to
        * be up. */
        if (nodeIsSlave(myself) &&
            server.masterhost == NULL &&
            myself->slaveof &&
            nodeHasAddr(myself->slaveof))
        {
            replicationSetMaster(myself->slaveof->ip, myself->slaveof->port);
        }
        ...
        if (nodeIsSlave(myself)) {
            ...
            /* If there are orphaned slaves, and we are a slave among the masters
            * with the max number of non-failing slaves, consider migrating to
            * the orphaned masters. Note that it does not make sense to try
            * a migration if there is no master with at least *two* working
            * slaves. */
            if (orphaned_masters && max_slaves >= 2 && this_slaves == max_slaves && server.cluster_allow_replica_migration)
                clusterHandleSlaveMigration(max_slaves);
        }

        ...
    }
    ```

- **更新故障转移状态**
  - 调用 [`manualFailoverCheckTimeout`](https://github.com/redis/redis/blob/7.0.0/src/cluster.c#L3848) 函数，如果手动故障转移执行超时，强制终止
  - 如果当前节点为从节点，调用 [`clusterHandleManualFailover()`](https://github.com/redis/redis/blob/7.0.0/src/cluster.c#L3857) 函数，推进手动故障转移流程
  - 如果当前节点为从节点，且未设置禁止自动故障转移标记位 `CLUSTER_MODULE_FLAG_NO_FAILOVER`，调用 [`clusterHandleSlaveFailover()`](https://github.com/redis/redis/blob/7.0.0/src/cluster.c#L3518) 函数执行自动故障转移

    ```c
    void clusterCron(void) {
        ...
        /* Abort a manual failover if the timeout is reached. */
        manualFailoverCheckTimeout();

        if (nodeIsSlave(myself)) {
            clusterHandleManualFailover();
            if (!(server.cluster_module_flags & CLUSTER_MODULE_FLAG_NO_FAILOVER))
                clusterHandleSlaveFailover();
            ...
        }
        ...
    }
    ```

- **更新配置**
  - 若集群状态发生变更（`update_state`），或处于异常状态（`CLUSTER_FAIL`），则调用 [`clusterUpdateState()`](https://github.com/redis/redis/blob/7.0.0/src/cluster.c#L4372) 函数，更新当前集群的状态

    ```c
    void clusterCron(void) {
        ...
        if (update_state || server.cluster->state == CLUSTER_FAIL)
            clusterUpdateState();
    }
    ```

## 消息通知

Redis Cluster 采用去中心化架构，集群中每个节点均独立维护完整的元数据信息（如哈希槽分配、主从拓扑、节点健康状态及数据迁移进度等）。节点间基于 Gossip 协议实现分布式协同：通过周期性随机选取部分节点交换增量状态信息（如 PING/PONG 消息），以低带宽开销逐步扩散局部变更，最终保障全局元数据的一致性。

消息类型 [`CLUSTERMSG_TYPE`](https://github.com/redis/redis/blob/7.0.0/src/cluster.h#L89) 如下所示：

```c
/* Message types.
 *
 * Note that the PING, PONG and MEET messages are actually the same exact
 * kind of packet. PONG is the reply to ping, in the exact format as a PING,
 * while MEET is a special PING that forces the receiver to add the sender
 * as a node (if it is not already in the list). */
#define CLUSTERMSG_TYPE_PING 0          /* Ping */
#define CLUSTERMSG_TYPE_PONG 1          /* Pong (reply to Ping) */
#define CLUSTERMSG_TYPE_MEET 2          /* Meet "let's join" message */
#define CLUSTERMSG_TYPE_FAIL 3          /* Mark node xxx as failing */
#define CLUSTERMSG_TYPE_PUBLISH 4       /* Pub/Sub Publish propagation */
#define CLUSTERMSG_TYPE_FAILOVER_AUTH_REQUEST 5 /* May I failover? */
#define CLUSTERMSG_TYPE_FAILOVER_AUTH_ACK 6     /* Yes, you have my vote */
#define CLUSTERMSG_TYPE_UPDATE 7        /* Another node slots configuration */
#define CLUSTERMSG_TYPE_MFSTART 8       /* Pause clients for manual failover */
#define CLUSTERMSG_TYPE_MODULE 9        /* Module cluster API message. */
#define CLUSTERMSG_TYPE_PUBLISHSHARD 10 /* Pub/Sub Publish shard propagation */
#define CLUSTERMSG_TYPE_COUNT 11        /* Total number of message types. */
```

### 消息发送

集群的消息通信机制中，消息发送流程通过以下步骤实现：

- 消息封装与调度：所有集群消息统一由 [`clusterSendMessage()`](https://github.com/redis/redis/blob/7.0.0/src/cluster.c#L2740) 函数负责创建消息对象（`msg`）并触发发送流程；
- 异步回调注册：通过调用 [`connSetWriteHandlerWithBarrier()`](https://github.com/redis/redis/blob/7.0.0/src/connection.h#L191) 函数，将网络连接的写回调设置为 [`clusterWriteHandler()`](https://github.com/redis/redis/blob/7.0.0/src/cluster.c#L2599)，该操作会确保在处理新消息前完成其他待处理的连接操作；
- 缓冲区写入：消息内容会被追加到连接的发送缓冲区（`link->sndbuf`）中暂存；
- 网络层发送：当 I/O 多路复用模块检测到连接可写时，最终由注册的 [`clusterWriteHandler()`](https://github.com/redis/redis/blob/7.0.0/src/cluster.c#L2599) 函数将缓冲区数据通过套接字发送至目标节点。

    ```c
    void clusterSendMessage(clusterLink *link, unsigned char *msg, size_t msglen) {
        if (sdslen(link->sndbuf) == 0 && msglen != 0)
            connSetWriteHandlerWithBarrier(link->conn, clusterWriteHandler, 1);

        link->sndbuf = sdscatlen(link->sndbuf, msg, msglen);

        /* Populate sent messages stats. */
        clusterMsg *hdr = (clusterMsg*) msg;
        uint16_t type = ntohs(hdr->type);
        if (type < CLUSTERMSG_TYPE_COUNT)
            server.cluster->stats_bus_messages_sent[type]++;
    }

    void clusterWriteHandler(connection *conn) {
        clusterLink *link = connGetPrivateData(conn);
        ssize_t nwritten;

        nwritten = connWrite(conn, link->sndbuf, sdslen(link->sndbuf));
        if (nwritten <= 0) {
            serverLog(LL_DEBUG,"I/O error writing to node link: %s",
                (nwritten == -1) ? connGetLastError(conn) : "short write");
            handleLinkIOError(link);
            return;
        }
        sdsrange(link->sndbuf,nwritten,-1);
        if (sdslen(link->sndbuf) == 0)
            connSetWriteHandler(link->conn, NULL);
    }
    ```

集群中的广播操作由 [`clusterBroadcastMessage()`](https://github.com/redis/redis/blob/7.0.0/src/cluster.c#L2759) 函数进行处理，函数内部会遍历所有正常连接的节点，复用 [`clusterSendMessage()`](https://github.com/redis/redis/blob/7.0.0/src/cluster.c#L2740) 函数执行消息发送逻辑。

```c
void clusterBroadcastMessage(void *buf, size_t len) {
    dictIterator *di;
    dictEntry *de;

    di = dictGetSafeIterator(server.cluster->nodes);
    while((de = dictNext(di)) != NULL) {
        clusterNode *node = dictGetVal(de);

        if (!node->link) continue;
        if (node->flags & (CLUSTER_NODE_MYSELF|CLUSTER_NODE_HANDSHAKE))
            continue;
        clusterSendMessage(node->link,buf,len);
    }
    dictReleaseIterator(di);
}
```

### 消息处理

在集群初始化，即[`clusterInit()`](https://github.com/redis/redis/blob/7.0.0/src/cluster.c#L606) 函数中，会注册 socket 处理函数 [`clusterAcceptHandler()`](https://github.com/redis/redis/blob/7.0.0/src/cluster.c#L846)，用于处理节点间连接请求。

```c
void clusterInit(void) {
    ...
    if (createSocketAcceptHandler(&server.cfd, clusterAcceptHandler) != C_OK) {
        serverPanic("Unrecoverable error creating Redis Cluster socket accept handler.");
    }
    ...
}
```

当连接到达时，最终会调用 [`clusterConnAcceptHandler()`](https://github.com/redis/redis/blob/7.0.0/src/cluster.c#L822) 函数来创建连接，并将 [`clusterReadHandler()`](https://github.com/redis/redis/blob/7.0.0/src/cluster.c#L2663) 函数设置为连接的写回调函数。

```c
void clusterAcceptHandler(aeEventLoop *el, int fd, void *privdata, int mask) {
    ...
    while(max--) {
        ...
        /* Accept the connection now.  connAccept() may call our handler directly
         * or schedule it for later depending on connection implementation.
         */
        if (connAccept(conn, clusterConnAcceptHandler) == C_ERR) {
            ...
        }
    }
}

static void clusterConnAcceptHandler(connection *conn) {
    ...
    link = createClusterLink(NULL);
    link->conn = conn;
    connSetPrivateData(conn, link);

    /* Register read handler */
    connSetReadHandler(conn, clusterReadHandler);
}
```

[`clusterReadHandler()`](https://github.com/redis/redis/blob/7.0.0/src/cluster.c#L2663) 函数内部会对接收到的数据做合法性处理，然后由 [`clusterProcessPacket()`](https://github.com/redis/redis/blob/7.0.0/src/cluster.c#L2065) 函数区分消息类型，实现相对应的逻辑。

```c
void clusterReadHandler(connection *conn) {
    ...
    while(1) { /* Read as long as there is data to read. */
        ...
        /* Total length obtained? Process this packet. */
        if (rcvbuflen >= 8 && rcvbuflen == ntohl(hdr->totlen)) {
            if (clusterProcessPacket(link)) {
                ...
            } else {
                return; /* Link no longer valid. */
            }
        }
    }
}

int clusterProcessPacket(clusterLink *link) {
    ...
    /* Initial processing of PING and MEET requests replying with a PONG. */
    if (type == CLUSTERMSG_TYPE_PING || type == CLUSTERMSG_TYPE_MEET) {
        ...
    }

    /* PING, PONG, MEET: process config information. */
    if (type == CLUSTERMSG_TYPE_PING || type == CLUSTERMSG_TYPE_PONG ||
        type == CLUSTERMSG_TYPE_MEET)
    {
        ...
    } else if (type == CLUSTERMSG_TYPE_FAIL) {
        ...
    } else if (type == CLUSTERMSG_TYPE_PUBLISH || type == CLUSTERMSG_TYPE_PUBLISHSHARD) {
        ...
    } else if (type == CLUSTERMSG_TYPE_FAILOVER_AUTH_REQUEST) {
        ...
    } else if (type == CLUSTERMSG_TYPE_FAILOVER_AUTH_ACK) {
        ...
    } else if (type == CLUSTERMSG_TYPE_MFSTART) {
        ...
    } else if (type == CLUSTERMSG_TYPE_UPDATE) {
        ...
    } else if (type == CLUSTERMSG_TYPE_MODULE) {
        ...
    } else {
        serverLog(LL_WARNING,"Received unknown packet type: %d", type);
    }
    return 1;
}
```

### PING & PONG & MEET

[`clusterSendPing()`](https://github.com/redis/redis/blob/7.0.0/src/cluster.c#L2880) 函数用于发送 `PING`、 `PONG` 和 `MEET` 消息，在维持心跳以外，其核心是通过 Gossip 协议，传播当前节点中所维护的，集群中其他节点的状态，从而确保集群状态的一致性。其中各消息的触发时机如下所示：

- `PING`
  - 建立连接时，向目标节点发送
  - 定时任务中，随机选择部分节点发送
  - 故障转移流程中，主动触发状态同步，并增加定时任务中的触发频率

- `MEET`
  - 节点间首次建立连接时，替代 `PING` 消息，让目标节点添加当前节点至其配置中，

- `PONG`
  - 节点收到 `PING` 或 `PONG` 消息时进行响应
  - 自身状态发送重大变化时，例如槽位迁移或故障转移，主动向其他节点广播

Gossip 结构体为 [`clusterMsgDataGossip`](https://github.com/redis/redis/blob/7.0.0/src/cluster.h#L225)，主要包含节点名称、心跳信息、IP/Port 信息和节点当前状态：

```c
typedef struct {
    char nodename[CLUSTER_NAMELEN];
    uint32_t ping_sent;
    uint32_t pong_received;
    char ip[NET_IP_STR_LEN];  /* IP address last time it was seen */
    uint16_t port;              /* base port last time it was seen */
    uint16_t cport;             /* cluster port last time it was seen */
    uint16_t flags;             /* node->flags copy */
    uint16_t pport;             /* plaintext-port, when base port is TLS */
    uint16_t notused1;
} clusterMsgDataGossip;
```

函数核心逻辑如下所示：

- **Gossip 数量控制**
  - 计算传播携带节点的上限 `freshnodes`，总结点减去自身节点与目标节点
  - 计算携带节点的数量 `wanted`，直接取节点总数的 10%
  - 更新节点数量的极值，最小值限制为 3，最大值限制为 $N-2$
  - 额外获取处于主观下线状态的节点数量 `pfail_wanted`，强制传播 `PFAIL` 状态的节点，加速 `PFAIL -> FAIL` 的状态转变，保障故障转移流程高效执行

    ```c
    void clusterSendPing(clusterLink *link, int type) {
        ...
        int freshnodes = dictSize(server.cluster->nodes)-2;

        wanted = floor(dictSize(server.cluster->nodes)/10);
        if (wanted < 3) wanted = 3;
        if (wanted > freshnodes) wanted = freshnodes;

        int pfail_wanted = server.cluster->stats_pfail_nodes;
        ...
    }
    ```

- **构造消息对象**
  - 构建缓冲区长度 `estlen`，包括消息头长度、Gossip 节点长度和 `PING` 消息的扩展数据
    - `clusterMsgData` 是一个 union 类型的联合体，在 `PING/PONG` 的场景下，数据由 Gossip 实际包含的节点数量动态决定，不需要使用固定空间
  - 若发送消息类型为 `PING`，更新 `ping_sent` 时间戳
    - `PONG` 与 `MEET` 消息会复用该函数，所以额外判断消息类型再赋值
  - 调用 [`clusterBuildMessageHdr()`](https://github.com/redis/redis/blob/7.0.0/src/cluster.c#L2777) 函数构建消息对象 `hdr`

    ```c
    void clusterSendPing(clusterLink *link, int type) {
        ...
        estlen = sizeof(clusterMsg) - sizeof(union clusterMsgData);
        estlen += (sizeof(clusterMsgDataGossip)*(wanted + pfail_wanted));
        estlen += sizeof(clusterMsgPingExt) + getHostnamePingExtSize();

        if (estlen < (int)sizeof(clusterMsg)) estlen = sizeof(clusterMsg);
        buf = zcalloc(estlen);
        hdr = (clusterMsg*) buf;

        if (!link->inbound && type == CLUSTERMSG_TYPE_PING)
            link->node->ping_sent = mstime();
        clusterBuildMessageHdr(hdr,type);
        ...
    }
    ```

- **Gossip 填充**
  - 循环调用 [`clusterSetGossipEntry()`](https://github.com/redis/redis/blob/7.0.0/src/cluster.c#L2864) 函数执行填充操作，并限制执行上限为 `wanted*3`
  - 每次填充时，通过 `dictGetRandomKey()` 函数随机获取一个节点，并过滤以下节点
    - 自身节点，该信息已包含在消息头中
    - 处于 `PFAIL` 状态的节点，后续会统一添加
    - 处于 `HANDSHAKE` 或 `NOADDR` 状态的节点，不能正常执行 `PING` 命令
    - 已断开连接且无槽位的节点
    - 已经添加过的节点
  - 每次执行完成后，更新相关计数器信息，即 `freshnodes` 与 `gossipcount`

    ```c
    void clusterSendPing(clusterLink *link, int type) {
        ...
        int maxiterations = wanted*3;
        while(freshnodes > 0 && gossipcount < wanted && maxiterations--) {
            dictEntry *de = dictGetRandomKey(server.cluster->nodes);
            clusterNode *this = dictGetVal(de);

            /* Don't include this node: the whole packet header is about us
            * already, so we just gossip about other nodes. */
            if (this == myself) continue;

            /* PFAIL nodes will be added later. */
            if (this->flags & CLUSTER_NODE_PFAIL) continue;

            if (this->flags & (CLUSTER_NODE_HANDSHAKE|CLUSTER_NODE_NOADDR) ||
                (this->link == NULL && this->numslots == 0))
            {
                freshnodes--; /* Technically not correct, but saves CPU. */
                continue;
            }

            /* Do not add a node we already have. */
            if (clusterNodeIsInGossipSection(hdr,gossipcount,this)) continue;

            clusterSetGossipEntry(hdr,gossipcount,this);
            freshnodes--;
            gossipcount++;
        }
        ...
    }
    ```

  - 调用 [`clusterSetGossipEntry()`](https://github.com/redis/redis/blob/7.0.0/src/cluster.c#L2864) 函数，添加所有处于 `PFAIL` 状态且不处于 `HANDSHAKE` 和 `NOADDR` 状态的节点
  - 更新相关计数器信息

    ```c
    void clusterSendPing(clusterLink *link, int type) {
        ...
        if (pfail_wanted) {
            dictIterator *di;
            dictEntry *de;

            di = dictGetSafeIterator(server.cluster->nodes);
            while((de = dictNext(di)) != NULL && pfail_wanted > 0) {
                clusterNode *node = dictGetVal(de);
                if (node->flags & CLUSTER_NODE_HANDSHAKE) continue;
                if (node->flags & CLUSTER_NODE_NOADDR) continue;
                if (!(node->flags & CLUSTER_NODE_PFAIL)) continue;
                clusterSetGossipEntry(hdr,gossipcount,node);
                freshnodes--;
                gossipcount++;
                /* We take the count of the slots we allocated, since the
                * PFAIL stats may not match perfectly with the current number
                * of PFAIL nodes. */
                pfail_wanted--;
            }
            dictReleaseIterator(di);
        }
        ...
    }
    ```

- **填充扩展数据**
  - 调用 [`getInitialPingExt()`](https://github.com/redis/redis/blob/7.0.0/src/cluster.c#L1967) 函数初始化拓展信息的结构体
  - 调用 [`writeHostnamePingExt()`](https://github.com/redis/redis/blob/7.0.0/src/cluster.c#L1992) 函数写入主机名拓展信息

    ```c
    void clusterSendPing(clusterLink *link, int type) {
        ...
        int totlen = 0;
        int extensions = 0;
        clusterMsgPingExt *cursor = getInitialPingExt(hdr, gossipcount);
        if (sdslen(myself->hostname) != 0) {
            hdr->mflags[0] |= CLUSTERMSG_FLAG0_EXT_DATA;
            totlen += writeHostnamePingExt(&cursor);
            extensions++;
        }
        ...
    }
    ```

- **消息发送**
  - 根据实际情况，更新数据长度，并填充相关字段
  - 调用 [`clusterSendMessage()`](https://github.com/redis/redis/blob/7.0.0/src/cluster.c#L2740) 函数执行发送操作

    ```c
    void clusterSendPing(clusterLink *link, int type) {
        ...
        /* Compute the actual total length and send! */
        totlen += sizeof(clusterMsg)-sizeof(union clusterMsgData);
        totlen += (sizeof(clusterMsgDataGossip)*gossipcount);
        hdr->count = htons(gossipcount);
        hdr->extensions = htons(extensions);
        hdr->totlen = htonl(totlen);
        clusterSendMessage(link,buf,totlen);
        zfree(buf);
    }
    ```

[`clusterProcessPacket()`](https://github.com/redis/redis/blob/7.0.0/src/cluster.c#L2065) 函数中，针对 `PING & PONG & MEET` 类型的消息，处理逻辑如下所示：

- **更新 IP 配置**
  - 若为 `MEET` 消息，或当前节点 IP 未初始化，则更新 IP 地址
  - 调用 `connSockName()` 函数获取本地 IP 地址
  - 调用 [`clusterDoBeforeSleep()`](https://github.com/redis/redis/blob/7.0.0/src/cluster.c#L4221) 函数来触发配置持久化

    ```c
    int clusterProcessPacket(clusterLink *link) {
        ...
        if (type == CLUSTERMSG_TYPE_PING || type == CLUSTERMSG_TYPE_MEET) {
            if ((type == CLUSTERMSG_TYPE_MEET || myself->ip[0] == '\0') &&
                server.cluster_announce_ip == NULL)
            {
                char ip[NET_IP_STR_LEN];

                if (connSockName(link->conn,ip,sizeof(ip),NULL) != -1 &&
                    strcmp(ip,myself->ip))
                {
                    memcpy(myself->ip,ip,NET_IP_STR_LEN);
                    serverLog(LL_WARNING,"IP address for this node updated to %s",
                        myself->ip);
                    clusterDoBeforeSleep(CLUSTER_TODO_SAVE_CONFIG);
                }
            }
            ...
        }
        ...
    }
    ```

- **处理 MEET 消息**
  - 针对 `MEET` 消息，创建新节点，然后将其添加至集群中，并将配置持久化
  - 调用 [`clusterProcessGossipSection()`](https://github.com/redis/redis/blob/7.0.0/src/cluster.c#L1627) 函数处理 Gossip 节点信息

    ```c
    int clusterProcessPacket(clusterLink *link) {
        ...
        if (type == CLUSTERMSG_TYPE_PING || type == CLUSTERMSG_TYPE_MEET) {
            ...
            if (!sender && type == CLUSTERMSG_TYPE_MEET) {
                clusterNode *node;

                node = createClusterNode(NULL,CLUSTER_NODE_HANDSHAKE);
                nodeIp2String(node->ip,link,hdr->myip);
                node->port = ntohs(hdr->port);
                node->pport = ntohs(hdr->pport);
                node->cport = ntohs(hdr->cport);
                clusterAddNode(node);
                clusterDoBeforeSleep(CLUSTER_TODO_SAVE_CONFIG);
            }

            if (!sender && type == CLUSTERMSG_TYPE_MEET)
                clusterProcessGossipSection(hdr,link);
            ...
        }
        ...
    }
    ```

- **响应 PONG 命令**
  - 针对 `PING` 与 `MEET` 消息，最终强制调用 [`clusterSendPing()`](https://github.com/redis/redis/blob/7.0.0/src/cluster.c#L2880) 函数发送 `PONG` 消息

    ```c
    int clusterProcessPacket(clusterLink *link) {
        ...
        if (type == CLUSTERMSG_TYPE_PING || type == CLUSTERMSG_TYPE_MEET) {
            ...
            clusterSendPing(link,CLUSTERMSG_TYPE_PONG);
        }
        ...
    }
    ```

- **更新节点基本信息**
  - 针对出站连接，节点处于握手阶段，且为已知节点（`sender` 存在）：
    - 调用 [`nodeUpdateAddressIfNeeded()`](https://github.com/redis/redis/blob/7.0.0/src/cluster.c#L1764) 函数更新节点 `sender`，并将配置持久化
    - 移除建连时创建的临时节点 `link->node`
    - 在下次发起定时任务中，使用最新配置，重建连接，避免连接存在其他请求导致并发问题
  - 如果是未知节点：
    - 调用 [`clusterRenameNode()`](https://github.com/redis/redis/blob/7.0.0/src/cluster.c#L1235) 函数更新临时节点 `link->node` 相关信息
    - 移除 `HANDSHAKE` 标记位，更新主从标记位
    - 将配置持久化

    ```c
    int clusterProcessPacket(clusterLink *link) {
        ...
        if (type == CLUSTERMSG_TYPE_PING || type == CLUSTERMSG_TYPE_PONG ||
            type == CLUSTERMSG_TYPE_MEET)
        {
            ...
            if (!link->inbound) {
                if (nodeInHandshake(link->node)) {
                    if (sender) {
                        if (nodeUpdateAddressIfNeeded(sender,link,hdr))
                        {
                            clusterDoBeforeSleep(CLUSTER_TODO_SAVE_CONFIG|
                                                CLUSTER_TODO_UPDATE_STATE);
                        }
                        clusterDelNode(link->node);
                        return 0;
                    }

                    clusterRenameNode(link->node, hdr->sender);
                    serverLog(LL_DEBUG,"Handshake with node %.40s completed.",
                        link->node->name);
                    link->node->flags &= ~CLUSTER_NODE_HANDSHAKE;
                    link->node->flags |= flags&(CLUSTER_NODE_MASTER|CLUSTER_NODE_SLAVE);
                    clusterDoBeforeSleep(CLUSTER_TODO_SAVE_CONFIG);
                }
                ...
            }
            ...
        }
        ...
    }
    ```

  - 针对出站连接，节点不处于握手阶段，且节点信息不匹配：
    - 为节点添加 `NOADDR` 标记位，清空 ip/port 信息
    - 释放连接，并将配置持久化

    ```c
    int clusterProcessPacket(clusterLink *link) {
        ...
        if (type == CLUSTERMSG_TYPE_PING || type == CLUSTERMSG_TYPE_PONG ||
            type == CLUSTERMSG_TYPE_MEET)
        {
            ...
            if (!link->inbound) {
                if (nodeInHandshake(link->node)) {
                    ...
                } else if (memcmp(link->node->name,hdr->sender,
                            CLUSTER_NAMELEN) != 0)
                {
                    /* If the reply has a non matching node ID we
                    * disconnect this node and set it as not having an associated
                    * address. */
                    serverLog(LL_DEBUG,"PONG contains mismatching sender ID. About node %.40s added %d ms ago, having flags %d",
                        link->node->name,
                        (int)(now-(link->node->ctime)),
                        link->node->flags);
                    link->node->flags |= CLUSTER_NODE_NOADDR;
                    link->node->ip[0] = '\0';
                    link->node->port = 0;
                    link->node->pport = 0;
                    link->node->cport = 0;
                    freeClusterLink(link);
                    clusterDoBeforeSleep(CLUSTER_TODO_SAVE_CONFIG);
                    return 0;
                }
            }
            ...
        }
        ...
    }
    ```

  - 目标节点为已知节点时，重置 `NOFAILOVER` 标记位
  - 目标为已知节点，当前为 `PING` 消息，且非握手节阶段时，调用 [`nodeUpdateAddressIfNeeded()`](https://github.com/redis/redis/blob/7.0.0/src/cluster.c#L1764) 函数更新节点 `sender`，并将配置持久化
    - `PING` 消息由对方发起，地址信息是最准确的

    ```c
    int clusterProcessPacket(clusterLink *link) {
        ...
        if (type == CLUSTERMSG_TYPE_PING || type == CLUSTERMSG_TYPE_PONG ||
            type == CLUSTERMSG_TYPE_MEET)
        {
            ...
            if (sender) {
                int nofailover = flags & CLUSTER_NODE_NOFAILOVER;
                sender->flags &= ~CLUSTER_NODE_NOFAILOVER;
                sender->flags |= nofailover;
            }

            /* Update the node address if it changed. */
            if (sender && type == CLUSTERMSG_TYPE_PING &&
                !nodeInHandshake(sender) &&
                nodeUpdateAddressIfNeeded(sender,link,hdr))
            {
                clusterDoBeforeSleep(CLUSTER_TODO_SAVE_CONFIG|
                                    CLUSTER_TODO_UPDATE_STATE);
            }

            ...
        }
        ...
    }
    ```

  - 针对出站连接，处理 `PONG` 消息
    - 将 `pong_received` 设置为当前时间
    - 移除 `ping_sent` 标记位，表示已收到响应
    - 如果节点被标记超时，即主观下线，则移除 `PFAIL` 标记位，并将配置持久化
    - 如果节点被标记客观下线，调用 [`clearNodeFailureIfNeeded()`](https://github.com/redis/redis/blob/7.0.0/src/cluster.c#L1511) 函数来清除其下线标记位

    ```c
    int clusterProcessPacket(clusterLink *link) {
        ...
        if (type == CLUSTERMSG_TYPE_PING || type == CLUSTERMSG_TYPE_PONG ||
            type == CLUSTERMSG_TYPE_MEET)
        {
            ...
            if (!link->inbound && type == CLUSTERMSG_TYPE_PONG) {
                link->node->pong_received = now;
                link->node->ping_sent = 0;

                /* The PFAIL condition can be reversed without external
                * help if it is momentary (that is, if it does not
                * turn into a FAIL state).
                *
                * The FAIL condition is also reversible under specific
                * conditions detected by clearNodeFailureIfNeeded(). */
                if (nodeTimedOut(link->node)) {
                    link->node->flags &= ~CLUSTER_NODE_PFAIL;
                    clusterDoBeforeSleep(CLUSTER_TODO_SAVE_CONFIG|
                                        CLUSTER_TODO_UPDATE_STATE);
                } else if (nodeFailed(link->node)) {
                    clearNodeFailureIfNeeded(link->node);
                }
            }
            ...
        }
        ...
    }
    ```

- **更新节点主从关系**
  - 如果发送节点的主节点为空（`CLUSTER_NODE_NULL_NAME`），说明其是主节点
    - 调用 [`clusterSetNodeAsMaster()`](https://github.com/redis/redis/blob/7.0.0/src/cluster.c#L1804) 函数，将 `sender` 设置为主节点
    - 如果当前配置中，`sender` 是从节点，则移除其现有的主从关系，并将其标记为主节点
    - 将配置持久化
  - 如果发送节点存在主节点，说明其是从节点
    - 如果当前记录节点 `sender` 是主节点，移除其槽位信息与主节点标记位，将其标记为从节点，并将配置持久化
    - 如果主节点信息冲突，则修改主从关系，并将配置持久化

    ```c
    int clusterProcessPacket(clusterLink *link) {
        ...
        if (type == CLUSTERMSG_TYPE_PING || type == CLUSTERMSG_TYPE_PONG ||
            type == CLUSTERMSG_TYPE_MEET)
        {
            ...
            if (sender) {
                if (!memcmp(hdr->slaveof,CLUSTER_NODE_NULL_NAME,
                    sizeof(hdr->slaveof)))
                {
                    /* Node is a master. */
                    clusterSetNodeAsMaster(sender);
                } else {
                    /* Node is a slave. */
                    clusterNode *master = clusterLookupNode(hdr->slaveof, CLUSTER_NAMELEN);

                    if (nodeIsMaster(sender)) {
                        /* Master turned into a slave! Reconfigure the node. */
                        clusterDelNodeSlots(sender);
                        sender->flags &= ~(CLUSTER_NODE_MASTER|
                                        CLUSTER_NODE_MIGRATE_TO);
                        sender->flags |= CLUSTER_NODE_SLAVE;

                        /* Update config and state. */
                        clusterDoBeforeSleep(CLUSTER_TODO_SAVE_CONFIG|
                                            CLUSTER_TODO_UPDATE_STATE);
                    }

                    /* Master node changed for this slave? */
                    if (master && sender->slaveof != master) {
                        if (sender->slaveof)
                            clusterNodeRemoveSlave(sender->slaveof,sender);
                        clusterNodeAddSlave(master,sender);
                        sender->slaveof = master;

                        /* Update config. */
                        clusterDoBeforeSleep(CLUSTER_TODO_SAVE_CONFIG);
                    }
                }
            }
            ...
        }
        ...
    }

    void clusterSetNodeAsMaster(clusterNode *n) {
        if (nodeIsMaster(n)) return;

        if (n->slaveof) {
            clusterNodeRemoveSlave(n->slaveof,n);
            if (n != myself) n->flags |= CLUSTER_NODE_MIGRATE_TO;
        }
        n->flags &= ~CLUSTER_NODE_SLAVE;
        n->flags |= CLUSTER_NODE_MASTER;
        n->slaveof = NULL;

        /* Update config and state. */
        clusterDoBeforeSleep(CLUSTER_TODO_SAVE_CONFIG|
                            CLUSTER_TODO_UPDATE_STATE);
    }
    ```

- **更新节点槽位关系**
  - 判断 `sender` 或 `sender->slaveof` 的槽位信息是否发生变化
  - 如果 `sender` 是主节点，且槽位信息发送变化，则调用 [`clusterUpdateSlotsConfigWith()](https://github.com/redis/redis/blob/7.0.0/src/cluster.c#L1831) 函数更新其配置

    ```c
    int clusterProcessPacket(clusterLink *link) {
        ...
        if (type == CLUSTERMSG_TYPE_PING || type == CLUSTERMSG_TYPE_PONG ||
            type == CLUSTERMSG_TYPE_MEET)
        {
            ...
            clusterNode *sender_master = NULL; /* Sender or its master if slave. */
            int dirty_slots = 0; /* Sender claimed slots don't match my view? */

            if (sender) {
                sender_master = nodeIsMaster(sender) ? sender : sender->slaveof;
                if (sender_master) {
                    dirty_slots = memcmp(sender_master->slots,
                            hdr->myslots,sizeof(hdr->myslots)) != 0;
                }
            }

            if (sender && nodeIsMaster(sender) && dirty_slots)
                clusterUpdateSlotsConfigWith(sender,senderConfigEpoch,hdr->myslots);
            ...
        }
        ...
    }
    ```

  - 如果槽位发生变化，循环判断发送方的槽位配置是否过期
    - 比较 `server.cluster->slots[j]->configEpoch` 和 `senderConfigEpoch` 大小
    - 如果发送者的配置过期，则调用 [`clusterSendUpdate()`](https://github.com/redis/redis/blob/7.0.0/src/cluster.c#L3127) 函数发送 `UPDATE` 消息

    ```c
    int clusterProcessPacket(clusterLink *link) {
        ...
        if (type == CLUSTERMSG_TYPE_PING || type == CLUSTERMSG_TYPE_PONG ||
            type == CLUSTERMSG_TYPE_MEET)
        {
            ...
            if (sender && dirty_slots) {
                int j;

                for (j = 0; j < CLUSTER_SLOTS; j++) {
                    if (bitmapTestBit(hdr->myslots,j)) {
                        if (server.cluster->slots[j] == sender ||
                            server.cluster->slots[j] == NULL) continue;
                        if (server.cluster->slots[j]->configEpoch >
                            senderConfigEpoch)
                        {
                            serverLog(LL_VERBOSE,
                                "Node %.40s has old slots configuration, sending "
                                "an UPDATE message about %.40s",
                                    sender->name, server.cluster->slots[j]->name);
                            clusterSendUpdate(sender->link,
                                server.cluster->slots[j]);

                            /* TODO: instead of exiting the loop send every other
                            * UPDATE packet for other nodes that are the new owner
                            * of sender's slots. */
                            break;
                        }
                    }
                }
            }
            ...
        }
        ...
    }
    ```

- **处理配置冲突**
  - 如果当前节点与发送方都是主节点，且配置版本一致，说明出现冲突
  - 调用 [`clusterHandleConfigEpochCollision()`](https://github.com/redis/redis/blob/7.0.0/src/cluster.c#L1363) 函数进行处理
  - 函数内部根据节点名称的字典序，确保仅由节点 ID 较大的一方来处理
  - 解决冲突时，直接将集群的版本自增，然后将当前节点的配置更新为集群的最新版本，并将配置持久化

    ```c
    int clusterProcessPacket(clusterLink *link) {
        ...
        if (type == CLUSTERMSG_TYPE_PING || type == CLUSTERMSG_TYPE_PONG ||
            type == CLUSTERMSG_TYPE_MEET)
        {
            ...
            if (sender &&
                nodeIsMaster(myself) && nodeIsMaster(sender) &&
                senderConfigEpoch == myself->configEpoch)
            {
                clusterHandleConfigEpochCollision(sender);
            }
            ...
        }
        ...
    }

    void clusterHandleConfigEpochCollision(clusterNode *sender) {
        /* Prerequisites: nodes have the same configEpoch and are both masters. */
        if (sender->configEpoch != myself->configEpoch ||
            !nodeIsMaster(sender) || !nodeIsMaster(myself)) return;
        /* Don't act if the colliding node has a smaller Node ID. */
        if (memcmp(sender->name,myself->name,CLUSTER_NAMELEN) <= 0) return;
        /* Get the next ID available at the best of this node knowledge. */
        server.cluster->currentEpoch++;
        myself->configEpoch = server.cluster->currentEpoch;
        clusterSaveConfigOrDie(1);
        serverLog(LL_VERBOSE,
            "WARNING: configEpoch collision with node %.40s."
            " configEpoch set to %llu",
            sender->name,
            (unsigned long long) myself->configEpoch);
    }
    ```

- **更新 Gossip 信息**
  - 调用 [`clusterProcessGossipSection()`](https://github.com/redis/redis/blob/7.0.0/src/cluster.c#L1627) 函数处理 Gossip 节点信息
  - 对于已知节点，会更新故障标记、`PONG` 时间戳与节点地址信息
  - 对于未知节点，且节点信息正常，会创建新节点并加入集群

    ```c
    int clusterProcessPacket(clusterLink *link) {
        ...
        if (type == CLUSTERMSG_TYPE_PING || type == CLUSTERMSG_TYPE_PONG ||
            type == CLUSTERMSG_TYPE_MEET)
        {
            ...
            if (sender) {
                clusterProcessGossipSection(hdr,link);
                clusterProcessPingExtensions(hdr,link);
            }
        }
        ...
    }
    ```

## 故障转移

### 主观下线（PFAIL）

Cluster 的定时任务 [`clusterCron()`](https://github.com/redis/redis/blob/7.0.0/src/cluster.c#L3975) 中，针对响应超时的节点，则添加主观下线标记位 `CLUSTER_NODE_PFAIL`：

```c
void clusterCron(void) {
    ...
    while((de = dictNext(di)) != NULL) {
        ...
        mstime_t node_delay = (ping_delay < data_delay) ? ping_delay :
                                                        data_delay;

        if (node_delay > server.cluster_node_timeout) {
            /* Timeout reached. Set the node as possibly failing if it is
            * not already in this state. */
            if (!(node->flags & (CLUSTER_NODE_PFAIL|CLUSTER_NODE_FAIL))) {
                serverLog(LL_DEBUG,"*** NODE %.40s possibly failing",
                    node->name);
                node->flags |= CLUSTER_NODE_PFAIL;
                update_state = 1;
            }
        }
    }
    ...
}
```

### 客观下线（FAIL）

在处理 Gossip 信息时，即 [`clusterProcessGossipSection()`](https://github.com/redis/redis/blob/7.0.0/src/cluster.c#L1627) 函数中，会专门对节点的故障状态做处理：

- 如果节点处于 `FAIL` 或 `PFAIL` 状态
  - 调用 [`clusterNodeAddFailureReport()`](https://github.com/redis/redis/blob/7.0.0/src/cluster.c#L996) 添加故障报告，如果重复添加报告，会更新报告有效期
  - 调用 [`markNodeAsFailingIfNeeded()`](https://github.com/redis/redis/blob/7.0.0/src/cluster.c#L1479) 函数，根据当前故障报告数量，判断是否要将节点标记为 `FAIL` 状态
- 如果节点处于正常状态
  - 调用 [`clusterNodeDelFailureReport()`](https://github.com/redis/redis/blob/7.0.0/src/cluster.c#L1053) 函数，移除故障报告

```c
void clusterProcessGossipSection(clusterMsg *hdr, clusterLink *link) {
    ...
    while(count--) {
        ...
        /* Update our state accordingly to the gossip sections */
        node = clusterLookupNode(g->nodename, CLUSTER_NAMELEN);
        if (node) {
            /* We already know this node.
               Handle failure reports, only when the sender is a master. */
            if (sender && nodeIsMaster(sender) && node != myself) {
                if (flags & (CLUSTER_NODE_FAIL|CLUSTER_NODE_PFAIL)) {
                    if (clusterNodeAddFailureReport(node,sender)) {
                        serverLog(LL_VERBOSE,
                            "Node %.40s reported node %.40s as not reachable.",
                            sender->name, node->name);
                    }
                    markNodeAsFailingIfNeeded(node);
                } else {
                    if (clusterNodeDelFailureReport(node,sender)) {
                        serverLog(LL_VERBOSE,
                            "Node %.40s reported node %.40s is back online.",
                            sender->name, node->name);
                    }
                }
            }
            ...
        }
        ...
    }
}
```

在 [`markNodeAsFailingIfNeeded()`](https://github.com/redis/redis/blob/7.0.0/src/cluster.c#L1479) 函数中，会调用 [`clusterNodeFailureReportsCount()`](https://github.com/redis/redis/blob/7.0.0/src/cluster.c#L1076) 计算当前有效的故障报告的数量，函数内部会先调用 [`clusterNodeCleanupFailureReports()`](https://github.com/redis/redis/blob/7.0.0/src/cluster.c#L1026) 函数清理过期报告，再进行统计。如果故障报告数量大于集群节点数量的一半，即满足 `needed_quorum`，会将节点标记为 `FAIL` 状态，然后调用 [`clusterSendFail()`](https://github.com/redis/redis/blob/7.0.0/src/cluster.c#L3115) 函数广播 `FAIL` 消息。

```c
void markNodeAsFailingIfNeeded(clusterNode *node) {
    int failures;
    int needed_quorum = (server.cluster->size / 2) + 1;

    if (!nodeTimedOut(node)) return; /* We can reach it. */
    if (nodeFailed(node)) return; /* Already FAILing. */

    failures = clusterNodeFailureReportsCount(node);
    /* Also count myself as a voter if I'm a master. */
    if (nodeIsMaster(myself)) failures++;
    if (failures < needed_quorum) return; /* No weak agreement from masters. */

    serverLog(LL_NOTICE,
        "Marking node %.40s as failing (quorum reached).", node->name);

    /* Mark the node as failing. */
    node->flags &= ~CLUSTER_NODE_PFAIL;
    node->flags |= CLUSTER_NODE_FAIL;
    node->fail_time = mstime();

    clusterSendFail(node->name);
    clusterDoBeforeSleep(CLUSTER_TODO_UPDATE_STATE|CLUSTER_TODO_SAVE_CONFIG);
}

int clusterNodeFailureReportsCount(clusterNode *node) {
    clusterNodeCleanupFailureReports(node);
    return listLength(node->fail_reports);
}

void clusterSendFail(char *nodename) {
    clusterMsg buf[1];
    clusterMsg *hdr = (clusterMsg*) buf;

    clusterBuildMessageHdr(hdr,CLUSTERMSG_TYPE_FAIL);
    memcpy(hdr->data.fail.about.nodename,nodename,CLUSTER_NAMELEN);
    clusterBroadcastMessage(buf,ntohl(hdr->totlen));
}
```

### 故障恢复

Cluster 的定时任务 [`clusterCron()`](https://github.com/redis/redis/blob/7.0.0/src/cluster.c#L3975) 中，如果当前节点是从节点，且未设置禁止自动故障转移标记位 `CLUSTER_MODULE_FLAG_NO_FAILOVER`，会调用 [`clusterHandleSlaveFailover()`](https://github.com/redis/redis/blob/7.0.0/src/cluster.c#L3518) 函数，处理自动故障转移逻辑。

```c
void clusterCron(void) {
    ...
    if (nodeIsSlave(myself)) {
        clusterHandleManualFailover();
        if (!(server.cluster_module_flags & CLUSTER_MODULE_FLAG_NO_FAILOVER))
            clusterHandleSlaveFailover();
        ...
    }
    ...
}
```

在 [`clusterHandleSlaveFailover()`](https://github.com/redis/redis/blob/7.0.0/src/cluster.c#L3518) 函数的核心逻辑，如下所示：

- **参数初始化**
  - 初始化必要参数

    ```c
    void clusterHandleSlaveFailover(void) {
        mstime_t data_age;
        mstime_t auth_age = mstime() - server.cluster->failover_auth_time;
        int needed_quorum = (server.cluster->size / 2) + 1;
        int manual_failover = server.cluster->mf_end != 0 &&
                            server.cluster->mf_can_start;
        mstime_t auth_timeout, auth_retry_time;

        server.cluster->todo_before_sleep &= ~CLUSTER_TODO_HANDLE_FAILOVER;

        auth_timeout = server.cluster_node_timeout*2;
        if (auth_timeout < 2000) auth_timeout = 2000;
        auth_retry_time = auth_timeout*2;
        ...
    }
    ```

- **前置条件检查**
  - 当前节点必须为从节点
  - 当前节点必须有主节点，且处于故障状态，或正进行手动故障转移
  - 当前未启用禁止自动故障转移配置 `cluster-slave-no-failover`，或正进行手动故障转移
  - 对应主节点必须有负责的槽位

    ```c
    void clusterHandleSlaveFailover(void) {
        ...
        if (nodeIsMaster(myself) ||
            myself->slaveof == NULL ||
            (!nodeFailed(myself->slaveof) && !manual_failover) ||
            (server.cluster_slave_no_failover && !manual_failover) ||
            myself->slaveof->numslots == 0)
        {
            /* There are no reasons to failover, so we set the reason why we
            * are returning without failing over to NONE. */
            server.cluster->cant_failover_reason = CLUSTER_CANT_FAILOVER_NONE;
            return;
        }
        ...
    }
    ```

- **检查从节点状态**
  - 计算与主节点的断连时长，并减去网络波动超时时间 `cluster_node_timeout`
    - 若处于连接状态，计算最后交互的时间
    - 否则取断开时间
  - 断连时长超过一定期限，认为数据过旧，除非手动故障转移

    ```c
    void clusterHandleSlaveFailover(void) {
        ...
        if (server.repl_state == REPL_STATE_CONNECTED) {
            data_age = (mstime_t)(server.unixtime - server.master->lastinteraction)
                    * 1000;
        } else {
            data_age = (mstime_t)(server.unixtime - server.repl_down_since) * 1000;
        }

        if (data_age > server.cluster_node_timeout)
            data_age -= server.cluster_node_timeout;

        if (server.cluster_slave_validity_factor &&
            data_age >
            (((mstime_t)server.repl_ping_slave_period * 1000) +
            (server.cluster_node_timeout * server.cluster_slave_validity_factor)))
        {
            if (!manual_failover) {
                clusterLogCantFailover(CLUSTER_CANT_FAILOVER_DATA_AGE);
                return;
            }
        }
        ...
    }
    ```

- **更新延迟时间**
  - 若之前的选举已经超时，且到达重试时间，则重置选举相关属性，并额外添加一定的延迟时间
  - 在初始化或重置选举时间时，均调用 [`clusterGetSlaveRank()`](https://github.com/redis/redis/blob/7.0.0/src/cluster.c#L3393) 函数获取从节点偏移量的排名，并根据排名，添加相对应的延迟时间
    - 保障偏移量大的节点，优先发起选举
  - 针对手动故障转移，立即触发选举，不添加延迟时间

    ```c
    void clusterHandleSlaveFailover(void) {
        ...
        if (auth_age > auth_retry_time) {
            server.cluster->failover_auth_time = mstime() +
                500 + /* Fixed delay of 500 milliseconds, let FAIL msg propagate. */
                random() % 500; /* Random delay between 0 and 500 milliseconds. */
            server.cluster->failover_auth_count = 0;
            server.cluster->failover_auth_sent = 0;
            server.cluster->failover_auth_rank = clusterGetSlaveRank();

            server.cluster->failover_auth_time +=
                server.cluster->failover_auth_rank * 1000;
            if (server.cluster->mf_end) {
                server.cluster->failover_auth_time = mstime();
                server.cluster->failover_auth_rank = 0;
                clusterDoBeforeSleep(CLUSTER_TODO_HANDLE_FAILOVER);
            }
            serverLog(LL_WARNING,
                "Start of election delayed for %lld milliseconds "
                "(rank #%d, offset %lld).",
                server.cluster->failover_auth_time - mstime(),
                server.cluster->failover_auth_rank,
                replicationGetSlaveOffset());
            clusterBroadcastPong(CLUSTER_BROADCAST_LOCAL_SLAVES);
            return;
        }

        if (server.cluster->failover_auth_sent == 0 &&
            server.cluster->mf_end == 0)
        {
            int newrank = clusterGetSlaveRank();
            if (newrank > server.cluster->failover_auth_rank) {
                long long added_delay =
                    (newrank - server.cluster->failover_auth_rank) * 1000;
                server.cluster->failover_auth_time += added_delay;
                server.cluster->failover_auth_rank = newrank;
                serverLog(LL_WARNING,
                    "Replica rank updated to #%d, added %lld milliseconds of delay.",
                    newrank, added_delay);
            }
        }
        ...
    }
    ```

- **处理时间窗口**
  - 若未到达预定时间 `failover_auth_time`，直接结束
  - 若选举超时，直接结束

    ```c
    void clusterHandleSlaveFailover(void) {
        ...
        if (mstime() < server.cluster->failover_auth_time) {
            clusterLogCantFailover(CLUSTER_CANT_FAILOVER_WAITING_DELAY);
            return;
        }

        if (auth_age > auth_timeout) {
            clusterLogCantFailover(CLUSTER_CANT_FAILOVER_EXPIRED);
            return;
        }
        ...
    }
    ```

- **发起选举请求**
  - 若还未发起投票请求，则调用 [`clusterRequestFailoverAuth()`](https://github.com/redis/redis/blob/7.0.0/src/cluster.c#L3236) 函数广播 `FAILOVER_AUTH_REQUEST` 消息，发起选举投票

    ```c
    void clusterHandleSlaveFailover(void) {
        ...
        if (server.cluster->failover_auth_sent == 0) {
            server.cluster->currentEpoch++;
            server.cluster->failover_auth_epoch = server.cluster->currentEpoch;
            serverLog(LL_WARNING,"Starting a failover election for epoch %llu.",
                (unsigned long long) server.cluster->currentEpoch);
            clusterRequestFailoverAuth();
            server.cluster->failover_auth_sent = 1;
            clusterDoBeforeSleep(CLUSTER_TODO_SAVE_CONFIG|
                                CLUSTER_TODO_UPDATE_STATE|
                                CLUSTER_TODO_FSYNC_CONFIG);
            return; /* Wait for replies. */
        }
        ...
    }
    ```

- **处理选举结果**
  - 如果票数超过集群数量的一半，即满足 `needed_quorum`，则将其更新为主节点
  - 修改当前节点的版本，使其保持最新
  - 调用 [`clusterFailoverReplaceYourMaster()`](https://github.com/redis/redis/blob/7.0.0/src/cluster.c#L3480) 函数晋升为主节点

    ```c
    void clusterHandleSlaveFailover(void) {
        ...
        if (server.cluster->failover_auth_count >= needed_quorum) {
            /* We have the quorum, we can finally failover the master. */

            serverLog(LL_WARNING,
                "Failover election won: I'm the new master.");

            /* Update my configEpoch to the epoch of the election. */
            if (myself->configEpoch < server.cluster->failover_auth_epoch) {
                myself->configEpoch = server.cluster->failover_auth_epoch;
                serverLog(LL_WARNING,
                    "configEpoch set to %llu after successful failover",
                    (unsigned long long) myself->configEpoch);
            }

            /* Take responsibility for the cluster slots. */
            clusterFailoverReplaceYourMaster();
        } else {
            clusterLogCantFailover(CLUSTER_CANT_FAILOVER_WAITING_VOTES);
        }
    }
    ```

- **节点晋升**
  - 修改主从标记位
  - 迁移哈希槽
  - 更新状态和配置
  - 广播 `PONG` 消息，通知状态变化
  - 重置故障转移状态

    ```c
    void clusterFailoverReplaceYourMaster(void) {
        int j;
        clusterNode *oldmaster = myself->slaveof;

        if (nodeIsMaster(myself) || oldmaster == NULL) return;

        /* 1) Turn this node into a master. */
        clusterSetNodeAsMaster(myself);
        replicationUnsetMaster();

        /* 2) Claim all the slots assigned to our master. */
        for (j = 0; j < CLUSTER_SLOTS; j++) {
            if (clusterNodeGetSlotBit(oldmaster,j)) {
                clusterDelSlot(j);
                clusterAddSlot(myself,j);
            }
        }

        /* 3) Update state and save config. */
        clusterUpdateState();
        clusterSaveConfigOrDie(1);

        /* 4) Pong all the other nodes so that they can update the state
        *    accordingly and detect that we switched to master role. */
        clusterBroadcastPong(CLUSTER_BROADCAST_ALL);

        /* 5) If there was a manual failover in progress, clear the state. */
        resetManualFailover();
    }
    ```

## Q & A

1. 为什么槽位数量为 16384（2^14）

    - 16384 是在历史中，经过实践验证，平衡性能与成本的选择，后续版本兼容该历史配置
    - 与更大的数值相比，如 65535，会需要更大的数据结构
      - 槽位位图的数据量会从 16384 bit 变为 65535 bit，即从 2 KB 变为 8 KB，消息通信的网络压力会更大
      - 同理，在存储槽位映射表时，所需要的内存也会变为原来的 4 倍，内存压力也会更大
    - 与更小的数值相比，如 1024，更容易导致分配不均，且单个槽位内的数据量偏大
      - 增加单节点的内存、CPU、网络等压力，容易受热点问题影响
      - 主从同步数据传输压力更大
      - 持久化时所需时间更长
      - 不利于槽位的迁移，进而影响服务可用性。而槽位数量较多时，相同的数据量会尽可能分配至多个槽位，可以并行迁移

<br>

2. 每次发送 `ping` 消息时，为什么选择携带 10% 节点的信息

   - 任意两个节点，在最差的情况下，也会每隔 `node_timeout/2` 的时间发送一次 `ping` 消息，也就是在 `node_timeout` 的时间窗口内，总共有 4 次数据传递（2 次自己发出，2 次对方发出），而在（`node_timeout*2`），总共会有 8 次数据传递
   - 当集群中节点数量为 N 时，在 `node_timeout*2` 的时间窗口中，某个节点信息被传出的概率为 $10\%*8*N$，即每个节点会收到 $80\%N$ 的节点数据，能够覆盖集群大部分节点
   - [`clusterSendPing()`](https://github.com/redis/redis/blob/7.0.0/src/cluster.c#L2880) 函数中的注释信息如下所示：

    ```c
    void clusterSendPing(clusterLink *link, int type) {
        ...
        /* How many gossip sections we want to add? 1/10 of the number of nodes
        * and anyway at least 3. Why 1/10?
        *
        * If we have N masters, with N/10 entries, and we consider that in
        * node_timeout we exchange with each other node at least 4 packets
        * (we ping in the worst case in node_timeout/2 time, and we also
        * receive two pings from the host), we have a total of 8 packets
        * in the node_timeout*2 failure reports validity time. So we have
        * that, for a single PFAIL node, we can expect to receive the following
        * number of failure reports (in the specified window of time):
        *
        * PROB * GOSSIP_ENTRIES_PER_PACKET * TOTAL_PACKETS:
        *
        * PROB = probability of being featured in a single gossip entry,
        *        which is 1 / NUM_OF_NODES.
        * ENTRIES = 10.
        * TOTAL_PACKETS = 2 * 4 * NUM_OF_MASTERS.
        *
        * If we assume we have just masters (so num of nodes and num of masters
        * is the same), with 1/10 we always get over the majority, and specifically
        * 80% of the number of nodes, to account for many masters failing at the
        * same time.
        *
        * Since we have non-voting slaves that lower the probability of an entry
        * to feature our node, we set the number of entries per packet as
        * 10% of the total nodes we have. */
        wanted = floor(dictSize(server.cluster->nodes)/10);
        ...
    }
    ```

## Ref

- [Scale with Redis Cluster](https://redis.io/docs/latest/operate/oss_and_stack/management/scaling/)
- [redis源码解析 pdf redis cluster 源码](https://blog.51cto.com/u_16099274/6468543)
- <https://github.com/SkyRainCho/redisDoc/blob/master/redis/cluster.md>
- [Redis集群（终篇）——故障自动检测与自动恢复（附优质Redis资源汇总）](https://zhuanlan.zhihu.com/p/106110578)
