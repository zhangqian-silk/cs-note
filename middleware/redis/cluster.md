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

## 数据分片

Cluster 使用了一致性哈希作为分片方式，将所有数据划分为 16384 个哈希槽（Hash Slot），每个键通过 CRC16 算法计算出一个哈希值，再对 16384 取模，确定其所属的槽位，进而确定所在节点。

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



错误码类型
CLUSTER_REDIR_CROSS_SLOT：多键跨槽。
CLUSTER_REDIR_UNSTABLE：槽迁移中且缺键。
CLUSTER_REDIR_DOWN_*：集群宕机相关错误。
CLUSTER_REDIR_ASK/MOVED：临时 / 永久重定向。
该函数通过精细的状态判断，确保 Redis 集群在分布式环境下正确路由请求，同时处理异常状态，保障数据一致性和可用性。

```c
clusterNode *getNodeByQuery(client *c, struct redisCommand *cmd, robj **argv, int argc, int *hashslot, int *error_code) {
    ...

    ...
}
```

```c
clusterNode *getNodeByQuery(client *c, struct redisCommand *cmd, robj **argv, int argc, int *hashslot, int *error_code) {
    clusterNode *n = NULL;
    robj *firstkey = NULL;
    int multiple_keys = 0;
    multiState *ms, _ms;
    multiCmd mc;
    int i, slot = 0, migrating_slot = 0, importing_slot = 0, missing_keys = 0;

    /* Allow any key to be set if a module disabled cluster redirections. */
    if (server.cluster_module_flags & CLUSTER_MODULE_FLAG_NO_REDIRECTION)
        return myself;

    /* Set error code optimistically for the base case. */
    if (error_code) *error_code = CLUSTER_REDIR_NONE;

    /* Modules can turn off Redis Cluster redirection: this is useful
     * when writing a module that implements a completely different
     * distributed system. */

    /* We handle all the cases as if they were EXEC commands, so we have
     * a common code path for everything */
    if (cmd->proc == execCommand) {
        /* If CLIENT_MULTI flag is not set EXEC is just going to return an
         * error. */
        if (!(c->flags & CLIENT_MULTI)) return myself;
        ms = &c->mstate;
    } else {
        /* In order to have a single codepath create a fake Multi State
         * structure if the client is not in MULTI/EXEC state, this way
         * we have a single codepath below. */
        ms = &_ms;
        _ms.commands = &mc;
        _ms.count = 1;
        mc.argv = argv;
        mc.argc = argc;
        mc.cmd = cmd;
    }

    int is_pubsubshard = cmd->proc == ssubscribeCommand ||
            cmd->proc == sunsubscribeCommand ||
            cmd->proc == spublishCommand;

    /* Check that all the keys are in the same hash slot, and obtain this
     * slot and the node associated. */
    for (i = 0; i < ms->count; i++) {
        struct redisCommand *mcmd;
        robj **margv;
        int margc, numkeys, j;
        keyReference *keyindex;

        mcmd = ms->commands[i].cmd;
        margc = ms->commands[i].argc;
        margv = ms->commands[i].argv;

        getKeysResult result = GETKEYS_RESULT_INIT;
        numkeys = getKeysFromCommand(mcmd,margv,margc,&result);
        keyindex = result.keys;

        for (j = 0; j < numkeys; j++) {
            robj *thiskey = margv[keyindex[j].pos];
            int thisslot = keyHashSlot((char*)thiskey->ptr,
                                       sdslen(thiskey->ptr));

            if (firstkey == NULL) {
                /* This is the first key we see. Check what is the slot
                 * and node. */
                firstkey = thiskey;
                slot = thisslot;
                n = server.cluster->slots[slot];

                /* Error: If a slot is not served, we are in "cluster down"
                 * state. However the state is yet to be updated, so this was
                 * not trapped earlier in processCommand(). Report the same
                 * error to the client. */
                if (n == NULL) {
                    getKeysFreeResult(&result);
                    if (error_code)
                        *error_code = CLUSTER_REDIR_DOWN_UNBOUND;
                    return NULL;
                }

                /* If we are migrating or importing this slot, we need to check
                 * if we have all the keys in the request (the only way we
                 * can safely serve the request, otherwise we return a TRYAGAIN
                 * error). To do so we set the importing/migrating state and
                 * increment a counter for every missing key. */
                if (n == myself &&
                    server.cluster->migrating_slots_to[slot] != NULL)
                {
                    migrating_slot = 1;
                } else if (server.cluster->importing_slots_from[slot] != NULL) {
                    importing_slot = 1;
                }
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

            /* Migrating / Importing slot? Count keys we don't have.
             * If it is pubsubshard command, it isn't required to check
             * the channel being present or not in the node during the
             * slot migration, the channel will be served from the source
             * node until the migration completes with CLUSTER SETSLOT <slot>
             * NODE <node-id>. */
            int flags = LOOKUP_NOTOUCH | LOOKUP_NOSTATS | LOOKUP_NONOTIFY;
            if ((migrating_slot || importing_slot) && !is_pubsubshard &&
                lookupKeyReadWithFlags(&server.db[0], thiskey, flags) == NULL)
            {
                missing_keys++;
            }
        }
        getKeysFreeResult(&result);
    }

    /* No key at all in command? then we can serve the request
     * without redirections or errors in all the cases. */
    if (n == NULL) return myself;

    /* Cluster is globally down but we got keys? We only serve the request
     * if it is a read command and when allow_reads_when_down is enabled. */
    if (server.cluster->state != CLUSTER_OK) {
        if (is_pubsubshard) {
            if (!server.cluster_allow_pubsubshard_when_down) {
                if (error_code) *error_code = CLUSTER_REDIR_DOWN_STATE;
                return NULL;
            }
        } else if (!server.cluster_allow_reads_when_down) {
            /* The cluster is configured to block commands when the
             * cluster is down. */
            if (error_code) *error_code = CLUSTER_REDIR_DOWN_STATE;
            return NULL;
        } else if (cmd->flags & CMD_WRITE) {
            /* The cluster is configured to allow read only commands */
            if (error_code) *error_code = CLUSTER_REDIR_DOWN_RO_STATE;
            return NULL;
        } else {
            /* Fall through and allow the command to be executed:
             * this happens when server.cluster_allow_reads_when_down is
             * true and the command is not a write command */
        }
    }

    /* Return the hashslot by reference. */
    if (hashslot) *hashslot = slot;

    /* MIGRATE always works in the context of the local node if the slot
     * is open (migrating or importing state). We need to be able to freely
     * move keys among instances in this case. */
    if ((migrating_slot || importing_slot) && cmd->proc == migrateCommand)
        return myself;

    /* If we don't have all the keys and we are migrating the slot, send
     * an ASK redirection. */
    if (migrating_slot && missing_keys) {
        if (error_code) *error_code = CLUSTER_REDIR_ASK;
        return server.cluster->migrating_slots_to[slot];
    }

    /* If we are receiving the slot, and the client correctly flagged the
     * request as "ASKING", we can serve the request. However if the request
     * involves multiple keys and we don't have them all, the only option is
     * to send a TRYAGAIN error. */
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

    /* Handle the read-only client case reading from a slave: if this
     * node is a slave and the request is about a hash slot our master
     * is serving, we can reply without redirection. */
    int is_write_command = (c->cmd->flags & CMD_WRITE) ||
                           (c->cmd->proc == execCommand && (c->mstate.cmd_flags & CMD_WRITE));
    if (((c->flags & CLIENT_READONLY) || is_pubsubshard) &&
        !is_write_command &&
        nodeIsSlave(myself) &&
        myself->slaveof == n)
    {
        return myself;
    }

    /* Base case: just return the right node. However if this node is not
     * myself, set error_code to MOVED since we need to issue a redirection. */
    if (n != myself && error_code) *error_code = CLUSTER_REDIR_MOVED;
    return n;
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

[`clusterCron()`](https://github.com/redis/redis/blob/7.0.0/src/cluster.c#L3975) 函数负责维护集群状态、处理节点间通信、故障检测与恢复等关键操作。

关键设计要点
双循环结构：首次循环处理链接资源，二次循环处理状态检测，避免资源操作影响状态判断。
随机 PING 策略：平衡网络开销与故障检测灵敏度，避免集中探测。
渐进式故障标记：先标记 PFAIL，再通过 gossip 协议协商确认 FAIL 状态，防止误判。
资源管理：动态调整缓冲区、释放闲置链接，优化内存使用。
从节点自治：从节点主动参与故障转移决策，提升集群可用性。
通过以上流程，clusterCron 确保了 Redis 集群的高可用性、数据一致性及资源高效利用。



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

## 数据迁移

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

## Ref

- [Scale with Redis Cluster](https://redis.io/docs/latest/operate/oss_and_stack/management/scaling/)
- [redis源码解析 pdf redis cluster 源码](https://blog.51cto.com/u_16099274/6468543)
