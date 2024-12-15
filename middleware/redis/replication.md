# Replication

## 主从架构

为了提高服务的可用性，避免单机故障导致服务不可用，Redis 也提供了集群部署的能力，其中最基础的模式为主从模式。

在 Redis 中，主从架构一般为一主多从，且读写分离。所有的写操作仅在主服务器进行，并由主服务器同步至从服务器，主要包含如下三种场景：

- 全量复制：当主从服务器连接时，如果数据差异过大，比如首次进行数据同步，主服务器会将当前所有数据创建一个快照，然后发送给从服务器
- 命令传播：当主服务器中数据发生变更时，会将命令同步发送给从服务器
- 增量复制：当主从服务器连接时，如果数据差异过小，比如偶发的网络中断，主服务器会根据从服务器给的索引，将新增数据发送给从服务器

此外，采用读写分离的方案，也能显著减少在高并发的场景下，保障数据一致性所需的成本。

在集群中从服务器数量过多时，主服务器向所有从服务器发送指令也会变成性能问题，此时可采用主从从的架构方案，数据从主服务器同步至少部分从服务器，再由这部分从服务器继续同步给其他从服务器。

## 首次同步

当设置服务器的主从关系时，服务器间需要进行一些数据交互来建立主从关系，此时从服务器的状态，即 [repl_state](https://github.com/redis/redis/blob/7.0.0/src/server.h#L392)，可能存在如下几种情况：

> capa 是 capabilities 的缩写，capa 请求目的是告诉主服务器支持的主从复制能力
>
> ```c
> /* Slave capabilities. */
> #define SLAVE_CAPA_NONE 0
> #define SLAVE_CAPA_EOF (1<<0)    /* Can parse the RDB EOF streaming format. */
> #define SLAVE_CAPA_PSYNC2 (1<<1) /* Supports PSYNC2 protocol. */
> ```
>
> psync 命令用于部分数据同步，区别于历史的 sync 同步命令，加了 Partial 作为命令前缀，会根据具体情况来判断是全量同步还是部分同步

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

### 初始化

- REPL_STATE_NONE：服务初始化
  - 在服务器初始化时，即 [initServerConfig()](https://github.com/redis/redis/blob/7.0.0/src/server.c#L1830)函数，会统一执行状态位的初始化逻辑

    ```c
    void initServerConfig(void) {
        ...
        server.repl_state = REPL_STATE_NONE;
        ...
    }
    ```

  - 之后，从服务器开始响应 `replicaof` 命令，在 [replicaofCommand()](https://github.com/redis/redis/blob/7.0.0/src/replication.c#L3040) 函数中，调用 [replicationSetMaster()](https://github.com/redis/redis/blob/7.0.0/src/replication.c#L2908) 函数来设置主服务器相关数据，更新从服务器状态，并触发建连操作，并更新状态为 `REPL_STATE_CONNECT`

    ```c
    void replicaofCommand(client *c) {
        ...
        replicationSetMaster(c->argv[1]->ptr, port);
        ...
    }

    void replicationSetMaster(char *ip, int port) {
        ... // 重置主从连接相关数据
        server.repl_state = REPL_STATE_CONNECT;
        serverLog(LL_NOTICE,"Connecting to MASTER %s:%d",
            server.masterhost, server.masterport);
        connectWithMaster();
    }
    ```

### 建连

- REPL_STATE_CONNECT：从服务器开始连接主服务器
  - 在函数 [connectWithMaster()](https://github.com/redis/redis/blob/7.0.0/src/replication.c#L2832) 中，根据 `tls_replication` 标记位，决定发起 TLS 连接或者是普通的 Socket 连接，连接成功后，修改状态为 `REPL_STATE_CONNECTING`
  - 同时将函数 [syncWithMaster()](https://github.com/redis/redis/blob/7.0.0/src/replication.c#L2530) 设置为回调函数，后续从服务器的状态变更，即 `server.repl_state`，均在该函数中处理

    ```c
    int connectWithMaster(void) {
        server.repl_transfer_s = server.tls_replication ? connCreateTLS() : connCreateSocket();
        if (connConnect(server.repl_transfer_s, server.masterhost, server.masterport,
                    server.bind_source_addr, syncWithMaster) == C_ERR) {
            serverLog(LL_WARNING,"Unable to connect to MASTER: %s",
                    connGetLastError(server.repl_transfer_s));
            connClose(server.repl_transfer_s);
            server.repl_transfer_s = NULL;
            return C_ERR;
        }


        server.repl_transfer_lastio = server.unixtime;
        server.repl_state = REPL_STATE_CONNECTING;
        serverLog(LL_NOTICE,"MASTER <-> REPLICA sync started");
        return C_OK;
    }
    ```

  - 函数 [connConnect()](https://github.com/redis/redis/blob/7.0.0/src/connection.h#L121) 会调用传入的 `connection` 实例所对应的建连方法，例如 [connCreateTLS()](https://github.com/redis/redis/blob/7.0.0/src/tls.c#L467) 函数，最终会调用 [connTLSConnect()](https://github.com/redis/redis/blob/7.0.0/src/tls.c#L771)

    ```c
    static inline int connConnect(connection *conn, const char *addr, int port, const char *src_addr,
            ConnectionCallbackFunc connect_handler) {
        return conn->type->connect(conn, addr, port, src_addr, connect_handler);
    }

    static connection *connCreateTLS(void) {
        return createTLSConnection(1);
    }

    static connection *createTLSConnection(int client_side) {
        ...
        conn->c.type = &CT_TLS;
        ...
        return (connection *) conn;
    }

    static ConnectionType CT_TLS = {
        ...
        .connect = connTLSConnect,
        ...
    };

    static int connTLSConnect(connection *conn_, const char *addr, int port, const char *src_addr, ConnectionCallbackFunc connect_handler) {
        ...
    }
    ```

<br>

- REPL_STATE_CONNECTING：建连成功，发送 `ping` 命令请求，等待主服务器回应
  - 建连函数会调用之前所设置的回调函数 [syncWithMaster()](https://github.com/redis/redis/blob/7.0.0/src/replication.c#L2530)
  - 在建连成功后，会更新网络连接的读写回调，在此函数中统一进行处理
  - 修改状态为 `REPL_STATE_RECEIVE_PING_REPLY`

    ```c
    void syncWithMaster(connection *conn) {
        ...
        /* Send a PING to check the master is able to reply without errors. */
        if (server.repl_state == REPL_STATE_CONNECTING) {
            serverLog(LL_NOTICE,"Non blocking connect for SYNC fired the event.");
            /* Delete the writable event so that the readable event remains
            * registered and we can wait for the PONG reply. */
            connSetReadHandler(conn, syncWithMaster);
            connSetWriteHandler(conn, NULL);
            server.repl_state = REPL_STATE_RECEIVE_PING_REPLY;
            /* Send the PING, don't check for errors at all, we have the timeout
            * that will take care about this. */
            err = sendCommand(conn,"PING",NULL);
            if (err) goto write_error;
            return;
        }
        ...
    }
    ```

  - 主服务器在 [pingCommand()](https://github.com/redis/redis/blob/7.0.0/src/server.c#L4237) 函数中，处理 `ping` 命令

    ```c
    void pingCommand(client *c) {
        ...
    }
    ```

<br>

- REPL_STATE_RECEIVE_PING_REPLY：等待收到 `ping` 命令响应
  - 从服务器校验 `pong` 命令返回数据，无误后执行后续流程
  - 修改状态为 `REPL_STATE_SEND_HANDSHAKE`，继续执行后续握手逻辑

    ```c
    void syncWithMaster(connection *conn) {
        ...
        /* Receive the PONG command. */
        if (server.repl_state == REPL_STATE_RECEIVE_PING_REPLY) {
            err = receiveSynchronousResponse(conn);

            /* We accept only two replies as valid, a positive +PONG reply
            * (we just check for "+") or an authentication error.
            * Note that older versions of Redis replied with "operation not
            * permitted" instead of using a proper error code, so we test
            * both. */
            if (err[0] != '+' &&
                strncmp(err,"-NOAUTH",7) != 0 &&
                strncmp(err,"-NOPERM",7) != 0 &&
                strncmp(err,"-ERR operation not permitted",28) != 0)
            {
                serverLog(LL_WARNING,"Error reply to PING from master: '%s'",err);
                sdsfree(err);
                goto error;
            } else {
                serverLog(LL_NOTICE,
                    "Master replied to PING, replication can continue...");
            }
            sdsfree(err);
            err = NULL;
            server.repl_state = REPL_STATE_SEND_HANDSHAKE;
        }
        ...
    }
    ```

### 握手

- REPL_STATE_SEND_HANDSHAKE：开始执行握手阶段
  - 如果服务器需要进行认证，则发送 `auth` 请求，传递认证信息（user && password）
  - 同步发送 `REPLCONF` 命令，即 `port` 请求、`ip` 请求和 `capa` 请求
    - `capa` 中的 `eof` 代表全量复制，`psync2` 代表部分复制
  - 修改状态为 `REPL_STATE_RECEIVE_AUTH_REPLY`，即优先处理 `auth` 请求

    ```c
    void syncWithMaster(connection *conn) {
        ...
        if (server.repl_state == REPL_STATE_SEND_HANDSHAKE) {
            /* AUTH with the master if required. */
            if (server.masterauth) {
                ...
                err = sendCommandArgv(conn, argc, args, lens);
                if (err) goto write_error;
            }

            {
                ...
                err = sendCommand(conn,"REPLCONF",
                        "listening-port",portstr, NULL);
                sdsfree(portstr);
                if (err) goto write_error;
            }

            if (server.slave_announce_ip) {
                err = sendCommand(conn,"REPLCONF",
                        "ip-address",server.slave_announce_ip, NULL);
                if (err) goto write_error;
            }

            err = sendCommand(conn,"REPLCONF",
                    "capa","eof","capa","psync2",NULL);
            if (err) goto write_error;

            server.repl_state = REPL_STATE_RECEIVE_AUTH_REPLY;
            return;
        }
        ...
    }
    ```

  - 相对应的，主服务器分别在 [authCommand()](https://github.com/redis/redis/blob/7.0.0/src/acl.c#L2956) 和 [replconfCommand()](https://github.com/redis/redis/blob/7.0.0/src/replication.c#L1138) 中进行处理

    ```c
    void authCommand(client *c) {
        ...
    }

    void replconfCommand(client *c) {
        ...
    }
    ```

<br>

- REPL_STATE_RECEIVE_AUTH_REPLY：处理 `auth` 请求响应
  - 确认不需要进行认证或响应无误后，修改状态为 `REPL_STATE_RECEIVE_PORT_REPLY`

    ```c
    void syncWithMaster(connection *conn) {
        ...
        if (server.repl_state == REPL_STATE_RECEIVE_AUTH_REPLY && !server.masterauth)
            server.repl_state = REPL_STATE_RECEIVE_PORT_REPLY;

        /* Receive AUTH reply. */
        if (server.repl_state == REPL_STATE_RECEIVE_AUTH_REPLY) {
            err = receiveSynchronousResponse(conn);
            if (err[0] == '-') {
                serverLog(LL_WARNING,"Unable to AUTH to MASTER: %s",err);
                sdsfree(err);
                goto error;
            }
            sdsfree(err);
            err = NULL;
            server.repl_state = REPL_STATE_RECEIVE_PORT_REPLY;
            return;
        }
        ...
    }
    ```

<br>

- REPL_STATE_RECEIVE_PORT_REPLY：处理 `port` 请求响应
  - 确认返回数据，并修改状态为 `REPL_STATE_RECEIVE_IP_REPLY`

    ```c
    void syncWithMaster(connection *conn) {
        ...
        /* Receive REPLCONF listening-port reply. */
        if (server.repl_state == REPL_STATE_RECEIVE_PORT_REPLY) {
            err = receiveSynchronousResponse(conn);
            /* Ignore the error if any, not all the Redis versions support
            * REPLCONF listening-port. */
            if (err[0] == '-') {
                serverLog(LL_NOTICE,"(Non critical) Master does not understand "
                                    "REPLCONF listening-port: %s", err);
            }
            sdsfree(err);
            server.repl_state = REPL_STATE_RECEIVE_IP_REPLY;
            return;
        }
        ...
    }
    ```

<br>

- REPL_STATE_RECEIVE_IP_REPLY：处理 `ip` 请求响应
  - 确认返回数据，并修改状态为 `REPL_STATE_RECEIVE_CAPA_REPLY`

    ```c
    void syncWithMaster(connection *conn) {
        ...
        if (server.repl_state == REPL_STATE_RECEIVE_IP_REPLY && !server.slave_announce_ip)
            server.repl_state = REPL_STATE_RECEIVE_CAPA_REPLY;

        /* Receive REPLCONF ip-address reply. */
        if (server.repl_state == REPL_STATE_RECEIVE_IP_REPLY) {
            err = receiveSynchronousResponse(conn);
            /* Ignore the error if any, not all the Redis versions support
            * REPLCONF ip-address. */
            if (err[0] == '-') {
                serverLog(LL_NOTICE,"(Non critical) Master does not understand "
                                    "REPLCONF ip-address: %s", err);
            }
            sdsfree(err);
            server.repl_state = REPL_STATE_RECEIVE_CAPA_REPLY;
            return;
        }
        ...
    }
    ```

<br>

- REPL_STATE_RECEIVE_CAPA_REPLY：处理 `capa` 请求
  - 确认返回数据，并修改状态为 `REPL_STATE_SEND_PSYNC`，开始执行数据同步操作

    ```c
    void syncWithMaster(connection *conn) {
        ...
        /* Receive CAPA reply. */
        if (server.repl_state == REPL_STATE_RECEIVE_CAPA_REPLY) {
            err = receiveSynchronousResponse(conn);
            /* Ignore the error if any, not all the Redis versions support
            * REPLCONF capa. */
            if (err[0] == '-') {
                serverLog(LL_NOTICE,"(Non critical) Master does not understand "
                                    "REPLCONF capa: %s", err);
            }
            sdsfree(err);
            err = NULL;
            server.repl_state = REPL_STATE_SEND_PSYNC;
        }
        ...
    }
    ```

### 确认同步方式

- REPL_STATE_SEND_PSYNC：开始执行主从复制，进行数据同步
  - 从服务器调用 [slaveTryPartialResynchronization()](https://github.com/redis/redis/blob/7.0.0/src/replication.c#L2366) 函数，发送同步命令

    ```c
    void syncWithMaster(connection *conn) {
        ...
        if (server.repl_state == REPL_STATE_SEND_PSYNC) {
            if (slaveTryPartialResynchronization(conn,0) == PSYNC_WRITE_ERROR) {
                err = sdsnew("Write error sending the PSYNC command.");
                abortFailover("Write error to failover target");
                goto write_error;
            }
            server.repl_state = REPL_STATE_RECEIVE_PSYNC_REPLY;
            return;
        }
        ...
    }
    ```

  - 从服务器写操作：发送 `psync` 命令，并根据历史缓存情况，构造命令的参数信息

    ```c
    int slaveTryPartialResynchronization(connection *conn, int read_reply) {
        ...
        /* Writing half */
        if (!read_reply) {
            server.master_initial_offset = -1;

            if (server.cached_master) {
                psync_replid = server.cached_master->replid;
                snprintf(psync_offset,sizeof(psync_offset),"%lld", server.cached_master->reploff+1);
                serverLog(LL_NOTICE,"Trying a partial resynchronization (request %s:%s).", psync_replid, psync_offset);
            } else {
                serverLog(LL_NOTICE,"Partial resynchronization not possible (no cached master)");
                psync_replid = "?";
                memcpy(psync_offset,"-1",3);
            }

            if (server.failover_state == FAILOVER_IN_PROGRESS) {
                reply = sendCommand(conn,"PSYNC",psync_replid,psync_offset,"FAILOVER",NULL);
            } else {
                reply = sendCommand(conn,"PSYNC",psync_replid,psync_offset,NULL);
            }
            ...
            return PSYNC_WAIT_REPLY;
        }
        ...
    }
    ```

<br>

- [syncCommand()](https://github.com/redis/redis/blob/7.0.0/src/replication.c#L910)：主服务器处理 `psync` 命令，根据入参情况，决定是全量复制还是增量复制
  - 解析命令参数，确认是 `psync` 命令后，通过 [masterTryPartialResynchronization()](https://github.com/redis/redis/blob/7.0.0/src/replication.c#L715) 函数，判断能否执行增量复制，若可以，则提前结束
  - 若不能执行增量复制，或是传入的命令为老版本的 `sync` 命令，则执行全量复制

    ```c
    /* SYNC and PSYNC command implementation. */
    void syncCommand(client *c) {
        ...
        if (!strcasecmp(c->argv[0]->ptr,"psync")) {
            ...
            if (masterTryPartialResynchronization(c, psync_offset) == C_OK) {
                server.stat_sync_partial_ok++;
                return; /* No full resync needed, return. */
            } else {
                char *master_replid = c->argv[1]->ptr;

                /* Increment stats for failed PSYNCs, but only if the
                * replid is not "?", as this is used by slaves to force a full
                * resync on purpose when they are not able to partially
                * resync. */
                if (master_replid[0] != '?') server.stat_sync_partial_err++;
            }
        } else {
            /* If a slave uses SYNC, we are dealing with an old implementation
            * of the replication protocol (like redis-cli --slave). Flag the client
            * so that we don't expect to receive REPLCONF ACK feedbacks. */
            c->flags |= CLIENT_PRE_PSYNC;
        }
        ...
    }
    ```

  - 在 [masterTryPartialResynchronization()](https://github.com/redis/redis/blob/7.0.0/src/replication.c#L715) 函数中，在如下这些情况下，不会执行增量复制，转而执行全量复制：
    - 传入的 `master_replid` 与 `server.replid`、`server.replid2` 不一致
    - 传入的 `master_replid` 与 `server.replid` 不一致，与 `server.replid2` 一致，但是数据偏移量大于 `server.second_replid_offset`，即主从从的架构下，从节点与当前节点同属于一个主节点，但是从节点数据比当前节点更新（偏移量更大）
    - 不存在积压缓冲区，即 `server.repl_backlog`，或是数据偏移量小于或大于当前积压缓冲区范围

    ```c
    int masterTryPartialResynchronization(client *c, long long psync_offset) {
        char *master_replid = c->argv[1]->ptr;
        ...
        if (strcasecmp(master_replid, server.replid) &&
            (strcasecmp(master_replid, server.replid2) ||
            psync_offset > server.second_replid_offset))
        {
            ...
            goto need_full_resync;
        }

        /* We still have the data our slave is asking for? */
        if (!server.repl_backlog ||
            psync_offset < server.repl_backlog->offset ||
            psync_offset > (server.repl_backlog->offset + server.repl_backlog->histlen))
        {
            ...
            goto need_full_resync;
        }
        ...
    need_full_resync:
        /* We need a full resync for some reason... Note that we can't
        * reply to PSYNC right now if a full SYNC is needed. The reply
        * must include the master offset at the time the RDB file we transfer
        * is generated, so we need to delay the reply to that moment. */
        return C_ERR;
    }
    ```

  - 在确认需要全量同步后，修改一些参数配置，例如修改状态为 `SLAVE_STATE_WAIT_BGSAVE_START`，初始化积压缓冲区 `server.repl_backlog` 等

    ```c
    /* SYNC and PSYNC command implementation. */
    void syncCommand(client *c) {
        ...
        /* Setup the slave as one waiting for BGSAVE to start. The following code
        * paths will change the state if we handle the slave differently. */
        c->replstate = SLAVE_STATE_WAIT_BGSAVE_START;
        ...
        /* Create the replication backlog if needed. */
        if (listLength(server.slaves) == 1 && server.repl_backlog == NULL) {
            /* When we create the backlog from scratch, we always use a new
            * replication ID and clear the ID2, since there is no valid
            * past history. */
            changeReplicationId();
            clearReplicationId2();
            createReplicationBacklog();
            ...
        }
        ...
    }
    ```

  - 场景一：当前正在执行 `bgsave`，且目标是写入磁盘，即正在执行自身的持久化策略，判断能否复用这次命令的执行结果，发送给当前从服务器
    - 遍历从服务器列表，找到一个状态为 `SLAVE_STATE_WAIT_BGSAVE_END` 的从服务器副本，即等待 `bgsave` 命令执行完成
    - 确保从服务器副本开启了 `CLIENT_REPL_RDBONLY`，或当前从服务器未开启
    - 确保从服务器副本与当前服务器副本支持的主从复制能力一致，即 `slave_capa` 一致
    - 确保请求内容一致，即 `slave_req` 一致（特殊情况下，需要执行主从复制，但是不需要复制数据、函数、lua 脚本等内容）

    ```c
    #define RDB_CHILD_TYPE_DISK 1     /* RDB is written to disk. */
    #define CLIENT_REPL_RDBONLY (1ULL<<42) /* This client is a replica that only wants
                                              RDB without replication buffer. */

    /* Slave requirements */
    #define SLAVE_REQ_NONE 0
    #define SLAVE_REQ_RDB_EXCLUDE_DATA (1 << 0)      /* Exclude data from RDB */
    #define SLAVE_REQ_RDB_EXCLUDE_FUNCTIONS (1 << 1) /* Exclude functions from RDB */
  
    /* SYNC and PSYNC command implementation. */
    void syncCommand(client *c) {
        ...
        /* CASE 1: BGSAVE is in progress, with disk target. */
        if (server.child_type == CHILD_TYPE_RDB &&
            server.rdb_child_type == RDB_CHILD_TYPE_DISK)
        {
            ...
            while((ln = listNext(&li))) {
                slave = ln->value;
                /* If the client needs a buffer of commands, we can't use
                * a replica without replication buffer. */
                if (slave->replstate == SLAVE_STATE_WAIT_BGSAVE_END &&
                    (!(slave->flags & CLIENT_REPL_RDBONLY) ||
                    (c->flags & CLIENT_REPL_RDBONLY)))
                    break;
            }
            if (ln && ((c->slave_capa & slave->slave_capa) == slave->slave_capa) &&
                c->slave_req == slave->slave_req) {
                ...
            }
            ...
        }
        ...
    }
    ```

  - 上述判断通过后，代表可以复用
    - 如果当前从服务器需要缓冲区数据，即 `CLIENT_REPL_RDBONLY` 标记未设置，则通过 [copyReplicaOutputBuffer()](https://github.com/redis/redis/blob/7.0.0/src/networking.c#L1161) 函数将副本从服务器缓冲区复制到当前从服务器
    - 通过 [replicationSetupSlaveForFullResync()](https://github.com/redis/redis/blob/7.0.0/src/replication.c#L686) 函数，构造全量复制的请求响应（即 `+FULLRESYNC` 关键字）

    ```c
    /* SYNC and PSYNC command implementation. */
    void syncCommand(client *c) {
        ...
        /* CASE 1: BGSAVE is in progress, with disk target. */
        if (server.child_type == CHILD_TYPE_RDB &&
            server.rdb_child_type == RDB_CHILD_TYPE_DISK)
        {
            ...
            if (!(c->flags & CLIENT_REPL_RDBONLY))
                copyReplicaOutputBuffer(c,slave);
            replicationSetupSlaveForFullResync(c,slave->psync_initial_offset);
            ...
        }
        ...
    }

    int replicationSetupSlaveForFullResync(client *slave, long long offset) {
        ...
        if (!(slave->flags & CLIENT_PRE_PSYNC)) {
            buflen = snprintf(buf,sizeof(buf),"+FULLRESYNC %s %lld\r\n",
                            server.replid,offset);
            if (connWrite(slave->conn,buf,buflen) != buflen) {
                freeClientAsync(slave);
                return C_ERR;
            }
        }
        return C_OK;
    }
    ```

  - 场景二：当前正在执行 `bgsave`，且目标是网络请求，即将 RDB 文件同步给从服务器
    - 直接结束当次请求，避免子进程过多，等待下一次 `bgsave` 操作进行数据同步

    ```c
    #define RDB_CHILD_TYPE_SOCKET 2   /* RDB is written to slave socket. */

    /* SYNC and PSYNC command implementation. */
    void syncCommand(client *c) {
        ...
        /* CASE 2: BGSAVE is in progress, with socket target. */
        else if (server.child_type == CHILD_TYPE_RDB &&
                server.rdb_child_type == RDB_CHILD_TYPE_SOCKET)
        {
            /* There is an RDB child process but it is writing directly to
            * children sockets. We need to wait for the next BGSAVE
            * in order to synchronize. */
            serverLog(LL_NOTICE,"Current BGSAVE has socket target. Waiting for next BGSAVE for SYNC");
        }
        ...
    }
    ```

  - 场景三：没有正在执行的 `bgsave` 操作
    - 如果当前主服务器支持无磁盘同步，从服务器支持网络同步，且开启了延迟配置，则延迟至定时函数 [replicationCron()](https://github.com/redis/redis/blob/7.0.0/src/replication.c#L3514) 中执行
    - 如果当前存在其他活跃子进程，则同样延迟执行
    - 其他情况下，通过 [startBgsaveForReplication()](https://github.com/redis/redis/blob/7.0.0/src/replication.c#L831) 函数触发一次 `bgsave` 操作，并最终同步给从服务器

    ```c
    #define SLAVE_CAPA_EOF (1<<0)    /* Can parse the RDB EOF streaming format. */

    struct redisServer {
        int repl_diskless_sync; /* Master send RDB to slaves sockets directly. */
        int repl_diskless_sync_delay; /* Delay to start a diskless repl BGSAVE. */
    }

    /* SYNC and PSYNC command implementation. */
    void syncCommand(client *c) {
        ...
        /* CASE 3: There is no BGSAVE is in progress. */
        else {
            if (server.repl_diskless_sync && (c->slave_capa & SLAVE_CAPA_EOF) &&
                server.repl_diskless_sync_delay)
            {
                /* Diskless replication RDB child is created inside
                * replicationCron() since we want to delay its start a
                * few seconds to wait for more slaves to arrive. */
                serverLog(LL_NOTICE,"Delay next BGSAVE for diskless SYNC");
            } else {
                /* We don't have a BGSAVE in progress, let's start one. Diskless
                * or disk-based mode is determined by replica's capacity. */
                if (!hasActiveChildProcess()) {
                    startBgsaveForReplication(c->slave_capa, c->slave_req);
                } else {
                    serverLog(LL_NOTICE,
                        "No BGSAVE in progress, but another BG operation is active. "
                        "BGSAVE for replication delayed");
                }
            }
        }
        return;
    }
    ```

  - 在 [startBgsaveForReplication()](https://github.com/redis/redis/blob/7.0.0/src/replication.c#L831) 函数中，启动了 `bgsave` 操作，用于生成 RDB 文件并同步给从服务器，同时还构造全量复制的请求响应（即 `+FULLRESYNC` 关键字）
    - 根据当前同步方式是否为网络同步，调用不同的方法执行 `bgsave` 操作，即 [rdbSaveToSlavesSockets()](https://github.com/redis/redis/blob/7.0.0/src/rdb.c#L3361) 和 [rdbSaveBackground()](https://github.com/redis/redis/blob/7.0.0/src/rdb.c#L1464)
    - 针对于 [rdbSaveBackground()](https://github.com/redis/redis/blob/7.0.0/src/rdb.c#L1464) 函数，与持久化方案执行逻辑相同。之后会在定时任务 [serverCron()](https://github.com/redis/redis/blob/7.0.0/src/server.c#L1157) 中，将结果发送给从服务器
    - 此外，这里还针对于基于磁盘的同步方案，通过 [replicationSetupSlaveForFullResync()](https://github.com/redis/redis/blob/7.0.0/src/replication.c#L686) 函数发送全量复制的请求响应

    ```c
    int startBgsaveForReplication(int mincapa, int req) {
        ...
        socket_target = (server.repl_diskless_sync || req & SLAVE_REQ_RDB_MASK) && (mincapa & SLAVE_CAPA_EOF);
        ...
        if (socket_target)
            retval = rdbSaveToSlavesSockets(req,rsiptr);
        else
            retval = rdbSaveBackground(req,server.rdb_filename,rsiptr);
        ...
        /* If the target is socket, rdbSaveToSlavesSockets() already setup
        * the slaves for a full resync. Otherwise for disk target do it now.*/
        if (!socket_target) {
            listRewind(server.slaves,&li);
            while((ln = listNext(&li))) {
                client *slave = ln->value;

                if (slave->replstate == SLAVE_STATE_WAIT_BGSAVE_START) {
                    /* Check slave has the exact requirements */
                    if (slave->slave_req != req)
                        continue;
                    replicationSetupSlaveForFullResync(slave, getPsyncInitialOffset());
                }
            }
        }
        ...
    }
    ```

  - 针对于 [rdbSaveToSlavesSockets()](https://github.com/redis/redis/blob/7.0.0/src/rdb.c#L3361) 函数，首先会针对于所有 `SLAVE_STATE_WAIT_BGSAVE_START` 状态下的从服务器，调用 [replicationSetupSlaveForFullResync()](https://github.com/redis/redis/blob/7.0.0/src/replication.c#L686) 函数发送全量复制的请求响应，然后通过子进程直接创建 RDB 文件，并刷新至管道中，最终在父进程中通过 [rdbPipeReadHandler()](https://github.com/redis/redis/blob/7.0.0/src/replication.c#L1444) 函数将数据发送给从服务器

    ```c
    int rdbSaveToSlavesSockets(int req, rdbSaveInfo *rsi) {
        ...
        while((ln = listNext(&li))) {
            client *slave = ln->value;
            if (slave->replstate == SLAVE_STATE_WAIT_BGSAVE_START) {
                /* Check slave has the exact requirements */
                if (slave->slave_req != req)
                    continue;
                server.rdb_pipe_conns[server.rdb_pipe_numconns++] = slave->conn;
                replicationSetupSlaveForFullResync(slave,getPsyncInitialOffset());
            }
        }
        /* Create the child process. */
        if ((childpid = redisFork(CHILD_TYPE_RDB)) == 0) {
            /* Child */
            ...
            retval = rdbSaveRioWithEOFMark(req,&rdb,NULL,rsi);
            if (retval == C_OK && rioFlush(&rdb) == 0)
                retval = C_ERR;
            ...
        } else {
            /* Parent */
            ...
            if (aeCreateFileEvent(server.el, server.rdb_pipe_read, AE_READABLE, rdbPipeReadHandler,NULL) == AE_ERR) {
                serverPanic("Unrecoverable error creating server.rdb_pipe_read file event.");
            }
            ...
        }
        return C_OK; /* Unreached. */
    }
    ```

<br>

- 解析 `psync` 响应数据
  - [syncWithMaster()](https://github.com/redis/redis/blob/7.0.0/src/replication.c#L2530) 函数继续调用 [slaveTryPartialResynchronization()](https://github.com/redis/redis/blob/7.0.0/src/replication.c#L2366) 函数，`read_reply` 参数为 1，代表执行读逻辑
  - [slaveTryPartialResynchronization()](https://github.com/redis/redis/blob/7.0.0/src/replication.c#L2366) 函数内部，获取主服务器的响应，并返回不同的枚举值

    ```c
    void syncWithMaster(connection *conn) {
        ...
        psync_result = slaveTryPartialResynchronization(conn,1);
        ...
    }

    int slaveTryPartialResynchronization(connection *conn, int read_reply) {
        ...
        /* Reading half */
        reply = receiveSynchronousResponse(conn);
        ...
    }
    ```

  - `PSYNC_WAIT_REPLY`：处理空消息，继续等待主服务器响应
  - `PSYNC_FULLRESYNC`：解析 `+FULLRESYNC` 关键字并更新相关状态位，执行全量复制逻辑
  - `PSYNC_CONTINUE`：解析 `+CONTINUE` 关键字并更新相关状态位，执行增量复制逻辑
  - `PSYNC_TRY_LATER`：解析 `-NOMASTERLINK` 或 `-LOADING` 关键字，稍后重试
  - `PSYNC_NOT_SUPPORTED`：数据解析失败，兜底返回

    ```c
    int slaveTryPartialResynchronization(connection *conn, int read_reply) {
        ...
        if (sdslen(reply) == 0) {
            /* The master may send empty newlines after it receives PSYNC
            * and before to reply, just to keep the connection alive. */
            sdsfree(reply);
            return PSYNC_WAIT_REPLY;
        }

        if (!strncmp(reply,"+FULLRESYNC",11)) {
            ...
            return PSYNC_FULLRESYNC;
        }

        if (!strncmp(reply,"+CONTINUE",9)) {
            ...
            return PSYNC_CONTINUE;
        }

        if (!strncmp(reply,"-NOMASTERLINK",13) ||
            !strncmp(reply,"-LOADING",8))
        {
            serverLog(LL_NOTICE,
                "Master is currently unable to PSYNC "
                "but should be in the future: %s", reply);
            sdsfree(reply);
            return PSYNC_TRY_LATER;
        }
        ...
        return PSYNC_NOT_SUPPORTED;
    }
    ```

<br>

- `REPL_STATE_TRANSFER`：处理 `psync` 结果，并准备接收数据
  - `PSYNC_WAIT_REPLY`：直接结束，等待下次响应

    ```c
    void syncWithMaster(connection *conn) {
        ...
        if (psync_result == PSYNC_WAIT_REPLY) return; /* Try again later... */
        ...
    }
    ```

  - `FAILOVER_IN_PROGRESS`：故障恢复时，校验结果是否能够正常处理
    - 若可以执行数据同步，则清楚故障标记位
    - 若不行，则直接结束

    ```c
    void syncWithMaster(connection *conn) {
        ...
        /* Check the status of the planned failover. We expect PSYNC_CONTINUE,
        * but there is nothing technically wrong with a full resync which
        * could happen in edge cases. */
        if (server.failover_state == FAILOVER_IN_PROGRESS) {
            if (psync_result == PSYNC_CONTINUE || psync_result == PSYNC_FULLRESYNC) {
                clearFailoverState();
            } else {
                abortFailover("Failover target rejected psync request");
                return;
            }
        }
        ...
    }
    ```

  - `PSYNC_TRY_LATER`：按照异常处理，稍后重试

    ```c
    void syncWithMaster(connection *conn) {
        ...
        /* If the master is in an transient error, we should try to PSYNC
        * from scratch later, so go to the error path. This happens when
        * the server is loading the dataset or is not connected with its
        * master and so forth. */
        if (psync_result == PSYNC_TRY_LATER) goto error;
        ...
    }
    ```

  - `PSYNC_CONTINUE`：执行增量复制逻辑

    ```c
    void syncWithMaster(connection *conn) {
        ...
        if (psync_result == PSYNC_CONTINUE) {
            serverLog(LL_NOTICE, "MASTER <-> REPLICA sync: Master accepted a Partial Resynchronization.");
            if (server.supervised_mode == SUPERVISED_SYSTEMD) {
                redisCommunicateSystemd("STATUS=MASTER <-> REPLICA sync: Partial Resynchronization accepted. Ready to accept connections in read-write mode.\n");
            }
            return;
        }
        ...
    }
    ```

  - `PSYNC_NOT_SUPPORTED`：返回数据非法，重试执行 `sync` 命令，即尝试执行全量复制
    - 若结果仍然异常，则按照异常处理

    ```c
    void syncWithMaster(connection *conn) {
        ...
        /* Fall back to SYNC if needed. Otherwise psync_result == PSYNC_FULLRESYNC
        * and the server.master_replid and master_initial_offset are
        * already populated. */
        if (psync_result == PSYNC_NOT_SUPPORTED) {
            serverLog(LL_NOTICE,"Retrying with SYNC...");
            if (connSyncWrite(conn,"SYNC\r\n",6,server.repl_syncio_timeout*1000) == -1) {
                serverLog(LL_WARNING,"I/O error writing to MASTER: %s",
                    strerror(errno));
                goto error;
            }
        }
        ...
    }
    ```

  - 其他情况下，即结果为 `PSYNC_FULLRESYNC`，或是 `sync` 命令重试成功，执行全量复制
    - 确认数据接收方式，如果需要磁盘缓存，则创建临时文件
    - 更新读方法为 [readSyncBulkPayload()](https://github.com/redis/redis/blob/7.0.0/src/replication.c#L1801) 函数，用于接收 RDB 文件
    - 修改状态为 `REPL_STATE_TRANSFER`

    ```c
    void syncWithMaster(connection *conn) {
        ...
        /* Prepare a suitable temp file for bulk transfer */
        if (!useDisklessLoad()) {
            while(maxtries--) {
                snprintf(tmpfile,256,
                    "temp-%d.%ld.rdb",(int)server.unixtime,(long int)getpid());
                dfd = open(tmpfile,O_CREAT|O_WRONLY|O_EXCL,0644);
                if (dfd != -1) break;
                sleep(1);
            }
            if (dfd == -1) {
                serverLog(LL_WARNING,"Opening the temp file needed for MASTER <-> REPLICA synchronization: %s",strerror(errno));
                goto error;
            }
            server.repl_transfer_tmpfile = zstrdup(tmpfile);
            server.repl_transfer_fd = dfd;
        }

        /* Setup the non blocking download of the bulk file. */
        if (connSetReadHandler(conn, readSyncBulkPayload)
                == C_ERR)
        {
            char conninfo[CONN_INFO_LEN];
            serverLog(LL_WARNING,
                "Can't create readable event for SYNC: %s (%s)",
                strerror(errno), connGetInfo(conn, conninfo, sizeof(conninfo)));
            goto error;
        }

        server.repl_state = REPL_STATE_TRANSFER;
        ...
    }
    ```

### 数据传输

- `RDB_CHILD_TYPE_SOCKET`：通过 socket 传输时，会在创建 RDB 文件时直接将数据发送给从服务器
  - [rdbSaveToSlavesSockets()](https://github.com/redis/redis/blob/7.0.0/src/rdb.c#L3361) 函数，首先会针对于所有 `SLAVE_STATE_WAIT_BGSAVE_START` 状态下的从服务器，调用 [replicationSetupSlaveForFullResync()](https://github.com/redis/redis/blob/7.0.0/src/replication.c#L686) 函数发送全量复制的请求响应，并将符合条件的从服务器连接，额外进行存储
  - 之后函数会通过子进程直接创建 RDB 文件，并刷新至管道中，最终在父进程中通过 [rdbPipeReadHandler()](https://github.com/redis/redis/blob/7.0.0/src/replication.c#L1444) 函数将数据发送给从服务器

    ```c
    int rdbSaveToSlavesSockets(int req, rdbSaveInfo *rsi) {
        ...
        while((ln = listNext(&li))) {
            client *slave = ln->value;
            if (slave->replstate == SLAVE_STATE_WAIT_BGSAVE_START) {
                /* Check slave has the exact requirements */
                if (slave->slave_req != req)
                    continue;
                server.rdb_pipe_conns[server.rdb_pipe_numconns++] = slave->conn;
                replicationSetupSlaveForFullResync(slave,getPsyncInitialOffset());
            }
        }
        /* Create the child process. */
        if ((childpid = redisFork(CHILD_TYPE_RDB)) == 0) {
            /* Child */
            ...
            retval = rdbSaveRioWithEOFMark(req,&rdb,NULL,rsi);
            if (retval == C_OK && rioFlush(&rdb) == 0)
                retval = C_ERR;
            ...
        } else {
            /* Parent */
            ...
            if (aeCreateFileEvent(server.el, server.rdb_pipe_read, AE_READABLE, rdbPipeReadHandler,NULL) == AE_ERR) {
                serverPanic("Unrecoverable error creating server.rdb_pipe_read file event.");
            }
            ...
        }
        return C_OK; /* Unreached. */
    }
    ```

  - 在 [rdbSaveRioWithEOFMark()](https://github.com/redis/redis/blob/7.0.0/src/rdb.c#L1372) 函数内部，会给数据添加上额外标识，即以 `$EOF:` 关键字开头，紧跟一段长度为 `RDB_EOF_MARK_SIZE` 的随机字符，并同时以该字符结尾

    ```c
    #define RDB_EOF_MARK_SIZE 40
    int rdbSaveRioWithEOFMark(int req, rio *rdb, int *error, rdbSaveInfo *rsi) {
        ...
        getRandomHexChars(eofmark,RDB_EOF_MARK_SIZE);
        if (error) *error = 0;
        if (rioWrite(rdb,"$EOF:",5) == 0) goto werr;
        if (rioWrite(rdb,eofmark,RDB_EOF_MARK_SIZE) == 0) goto werr;
        if (rioWrite(rdb,"\r\n",2) == 0) goto werr;
        if (rdbSaveRio(req,rdb,error,RDBFLAGS_NONE,rsi) == C_ERR) goto werr;
        if (rioWrite(rdb,eofmark,RDB_EOF_MARK_SIZE) == 0) goto werr;
        ...
    }
    ```

  - [rdbPipeReadHandler()](https://github.com/redis/redis/blob/7.0.0/src/replication.c#L1444) 函数内部将数据从管道读取至缓冲区中后，遍历从服务器连接，执行发送逻辑

    ```c
    /* Called in diskless master, when there's data to read from the child's rdb pipe */
    void rdbPipeReadHandler(struct aeEventLoop *eventLoop, int fd, void *clientData, int mask) {
        ...
        while (1) {
            server.rdb_pipe_bufflen = read(fd, server.rdb_pipe_buff, PROTO_IOBUF_LEN);
            ...
            for (i=0; i < server.rdb_pipe_numconns; i++)
            {
                connection *conn = server.rdb_pipe_conns[i];
                ...
                if ((nwritten = connWrite(conn, server.rdb_pipe_buff, server.rdb_pipe_bufflen)) == -1) {
                    ...
                }
                ...
            }
            ...
        }
    }
    ```

<br>

- 主服务器定时任务
  - 在确认同步方式后，主服务器会同步触发一次 `bgsave` 的操作，并根据实际配置，决定是先保存为 RDB 文件，再传输文件，还是直接通过 socket 直接传输
  - 在服务器的定时任务 [serverCron()](https://github.com/redis/redis/blob/7.0.0/src/server.c#L1157) 中，如果有活跃的子进程，会统一在 [checkChildrenDone()](https://github.com/redis/redis/blob/7.0.0/src/server.c#L1052) 函数中判断其执行情况（`waitpid()` 函数返回值不为 0，说明存在结束的子进程），并针对 `bgsave` 任务，调用 [backgroundSaveDoneHandler()](https://github.com/redis/redis/blob/7.0.0/src/rdb.c#L3324) 函数进行处理
  - 最终，会调用 [updateSlavesWaitingBgsave()](https://github.com/redis/redis/blob/7.0.0/src/replication.c#L1544) 函数执行数据传输操作

    ```c
    int serverCron(struct aeEventLoop *eventLoop, long long id, void *clientData) {
        ...
        if (hasActiveChildProcess() || ldbPendingChildren())
        {
            run_with_period(1000) receiveChildInfo();
            checkChildrenDone();
        } 
        ...
    }

    void checkChildrenDone(void) {
        ...
        if ((pid = waitpid(-1, &statloc, WNOHANG)) != 0) {
            ...
            if (pid == server.child_pid) {
                if (server.child_type == CHILD_TYPE_RDB) {
                    backgroundSaveDoneHandler(exitcode, bysignal);
                } 
            }
            ...
        }
        ...
    }

    void backgroundSaveDoneHandler(int exitcode, int bysignal) {
        ...
        updateSlavesWaitingBgsave((!bysignal && exitcode == 0) ? C_OK : C_ERR, type);
    }
    ```

  - [updateSlavesWaitingBgsave()](https://github.com/redis/redis/blob/7.0.0/src/replication.c#L1544) 函数内部遍历所有状态为 `SLAVE_STATE_WAIT_BGSAVE_END` 的从服务器，并区分其通过 socket 传输数据还是传输 RDB 文件，执行不同的策略
    - 针对于 socket 传输，在 `bgsave` 任务的完成时，传输任务已经完成，此时将从服务器设置为在线状态
    - 针对于 RDB 传输，设置网络连接的写回调为 [sendBulkToSlave()](https://github.com/redis/redis/blob/7.0.0/src/replication.c#L1344)，执行数据传输逻辑

    ```c
    void updateSlavesWaitingBgsave(int bgsaveerr, int type) {
        ...
        while((ln = listNext(&li))) {
            client *slave = ln->value;

            if (slave->replstate == SLAVE_STATE_WAIT_BGSAVE_END) {
                if (type == RDB_CHILD_TYPE_SOCKET) {
                    serverLog(LL_NOTICE,
                        "Streamed RDB transfer with replica %s succeeded (socket). Waiting for REPLCONF ACK from slave to enable streaming",
                            replicationGetSlaveName(slave));
                    replicaPutOnline(slave);
                    slave->repl_start_cmd_stream_on_ack = 1;
                } else {
                    ...
                    if (connSetWriteHandler(slave->conn,sendBulkToSlave) == C_ERR) {
                        freeClientAsync(slave);
                        continue;
                    }
                }
            }
        }
    }
    ```

<br>

- `RDB_CHILD_TYPE_DISK`：传输 RDB 文件时，每次会传输特定大小的数据，直至最终发送完成
  - 正式发送 RDB 文件前，先将文件大小发送至从服务器
  - 然后每次从文件中读取 `PROTO_IOBUF_LEN` 大小的数据存储在缓冲区中并发送至从服务器
  - 之后会多次调用该函数，直至发送完毕，并同样将从服务器设置为上限状态

    ```c
    #define PROTO_IOBUF_LEN         (1024*16)  /* Generic I/O buffer size */
    void sendBulkToSlave(connection *conn) {
        ...
        /* Before sending the RDB file, we send the preamble as configured by the
        * replication process. Currently the preamble is just the bulk count of
        * the file in the form "$<length>\r\n". */
        if (slave->replpreamble) {
            ...
        }

        /* If the preamble was already transferred, send the RDB bulk data. */
        lseek(slave->repldbfd,slave->repldboff,SEEK_SET);
        buflen = read(slave->repldbfd,buf,PROTO_IOBUF_LEN);
        ...
        if ((nwritten = connWrite(conn,buf,buflen)) == -1) {
            ...
        }
        slave->repldboff += nwritten;
        atomicIncr(server.stat_net_output_bytes, nwritten);
        if (slave->repldboff == slave->repldbsize) {
            close(slave->repldbfd);
            slave->repldbfd = -1;
            connSetWriteHandler(slave->conn,NULL);
            replicaPutOnline(slave);
            replicaStartCommandStream(slave);
        }
    }
    ```

- 从服务器读取数据
  - 在与主服务确认同步方式后，从服务器会将 [readSyncBulkPayload()](https://github.com/redis/redis/blob/7.0.0/src/replication.c#L1801) 函数设置为连接的读回调函数，并处理所收到的用于同步的数据
  - 函数内部首先解析数据格式，针对于 socket 格式数据，会以 `EOF:` 关键字开头，并紧跟长度为 `CONFIG_RUN_ID_SIZE` 的标记位（长度与 `RDB_EOF_MARK_SIZE` 相同），针对于 RDB 文件格式的数据，会解析出其文件长度

    ```c
    #define CONFIG_RUN_ID_SIZE 40
    #define RDB_EOF_MARK_SIZE 40
    void readSyncBulkPayload(connection *conn) {
        ...
        if (server.repl_transfer_size == -1) {
            ...
            if (strncmp(buf+1,"EOF:",4) == 0 && strlen(buf+5) >= CONFIG_RUN_ID_SIZE) {
                usemark = 1;
                memcpy(eofmark,buf+5,CONFIG_RUN_ID_SIZE);
                memset(lastbytes,0,CONFIG_RUN_ID_SIZE);
                /* Set any repl_transfer_size to avoid entering this code path
                * at the next call. */
                server.repl_transfer_size = 0;
                serverLog(LL_NOTICE,
                    "MASTER <-> REPLICA sync: receiving streamed RDB from master with EOF %s",
                    use_diskless_load? "to parser":"to disk");
            } else {
                usemark = 0;
                server.repl_transfer_size = strtol(buf+1,NULL,10);
                serverLog(LL_NOTICE,
                    "MASTER <-> REPLICA sync: receiving %lld bytes from master %s",
                    (long long) server.repl_transfer_size,
                    use_diskless_load? "to parser":"to disk");
            }
            return;
        }
        ...
    }
    ```

  - 之后针对于需要磁盘的场景下（即通过 RDB 文件实现数据同步），会将读取到的数据写入之前从服务器创建的临时 RDB 文件 `server.repl_transfer_fd` 中
  - 在数据量级较大时，会主动执行一次刷盘操作，避免传输结束时出现较大延迟
  - 最后判断读取是否结束，存在标识位的情况，会判断结尾数据是否与标识位相同，不使用标识位时，会判断读取的数据是否达到预期的长度，若未结束则直接结束函数，等待下次数据传输

    ```c
    void readSyncBulkPayload(connection *conn) {
        ...
        if (!use_diskless_load) {
            /* Read the data from the socket, store it to a file and search
            * for the EOF. */
            ...
            /* Update the last I/O time for the replication transfer (used in
            * order to detect timeouts during replication), and write what we
            * got from the socket to the dump file on disk. */
            server.repl_transfer_lastio = server.unixtime;
            if ((nwritten = write(server.repl_transfer_fd,buf,nread)) != nread) {
                ...
            }
            server.repl_transfer_read += nread;
            ...
            /* Sync data on disk from time to time, otherwise at the end of the
            * transfer we may suffer a big delay as the memory buffers are copied
            * into the actual disk. */
            if (server.repl_transfer_read >=
                server.repl_transfer_last_fsync_off + REPL_MAX_WRITTEN_BEFORE_FSYNC)
            {
                off_t sync_size = server.repl_transfer_read -
                                server.repl_transfer_last_fsync_off;
                rdb_fsync_range(server.repl_transfer_fd,
                    server.repl_transfer_last_fsync_off, sync_size);
                server.repl_transfer_last_fsync_off += sync_size;
            }
            ...
            if (!eof_reached) return;
        }
        ...
    }
    ```

  - 针对于后续逻辑，有如下两种场景
    - 对于 socket 传输，阻塞后续网络数据传输，并读取当前所有传输数据
    - 对于 RDB 传输，此时已经获取到了完整的 RDB 文件

    ```c
    void readSyncBulkPayload(connection *conn) {
        ...
        /* We reach this point in one of the following cases:
        *
        * 1. The replica is using diskless replication, that is, it reads data
        *    directly from the socket to the Redis memory, without using
        *    a temporary RDB file on disk. In that case we just block and
        *    read everything from the socket.
        *
        * 2. Or when we are done reading from the socket to the RDB file, in
        *    such case we want just to read the RDB file in memory. */
        ...
    }
    ```

  - 取消其他子进程

    ```c
    void readSyncBulkPayload(connection *conn) {
        ...
        /* We need to stop any AOF rewriting child before flushing and parsing
        * the RDB, otherwise we'll create a copy-on-write disaster. */
        if (server.aof_state != AOF_OFF) stopAppendOnly();
        /* Also try to stop save RDB child before flushing and parsing the RDB:
        * 1. Ensure background save doesn't overwrite synced data after being loaded.
        * 2. Avoid copy-on-write disaster. */
        if (server.child_type == CHILD_TYPE_RDB) {
            if (!use_diskless_load) {
                serverLog(LL_NOTICE,
                    "Replica is about to load the RDB file received from the "
                    "master, but there is a pending RDB child running. "
                    "Killing process %ld and removing its temp file to avoid "
                    "any race",
                    (long) server.child_pid);
            }
            killRDBChild();
        }
        ...
    }
    ```

  - 针对于无盘缓存，阻塞网络连接，然后通过 [rdbLoadRioWithLoadingCtx()](https://github.com/redis/redis/blob/7.0.0/src/rdb.c#L2893) 函数，直接将 RDB 数据加载至内存中，完成数据同步

    ```c
    void readSyncBulkPayload(connection *conn) {
        ...
        if (use_diskless_load) {
            ...
            connBlock(conn);
            ...
            if (rdbLoadRioWithLoadingCtx(&rdb,RDBFLAGS_REPLICATION,&rsi,&loadingCtx) != C_OK) {
                ...
            }
            ...
        } 
        ...
    }
    ```

  - 针对于磁盘缓存，会通过 `fsync` 函数，强制将文件刷新至磁盘中，然后将文件重命名为正式的 RDB 文件，最后调用 [rdbLoad()](https://github.com/redis/redis/blob/7.0.0/src/rdb.c#L3246) 函数将 RDB 文件中的数据加载至内存中，完成数据同步

    ```c
    void readSyncBulkPayload(connection *conn) {
        ...
        if (use_diskless_load) {
            ...
        } else {
            /* Make sure the new file (also used for persistence) is fully synced
            * (not covered by earlier calls to rdb_fsync_range). */
            if (fsync(server.repl_transfer_fd) == -1) {
                ...
            }

            /* Rename rdb like renaming rewrite aof asynchronously. */
            int old_rdb_fd = open(server.rdb_filename,O_RDONLY|O_NONBLOCK);
            if (rename(server.repl_transfer_tmpfile,server.rdb_filename) == -1) {
                ...
            }
            ...
            if (rdbLoad(server.rdb_filename,&rsi,RDBFLAGS_REPLICATION) != C_OK) {
                ...
            }
            ...
        }
        ...
    }
    ```

  - 创建从服务器与主服务器连接的客户端，并更新状态位为 `REPL_STATE_CONNECTED`

    ```c
    void readSyncBulkPayload(connection *conn) {
        ...
        /* Final setup of the connected slave <- master link */
        replicationCreateMasterClient(server.repl_transfer_s,rsi.repl_stream_db);
        server.repl_state = REPL_STATE_CONNECTED;
        ...
    }
    ```

  - 复制请求 ID 以及数据偏移量，并创建缓冲区，用于之后可以执行部分复制

    ```c
    void readSyncBulkPayload(connection *conn) {
        ...
        /* After a full resynchronization we use the replication ID and
        * offset of the master. The secondary ID / offset are cleared since
        * we are starting a new history. */
        memcpy(server.replid,server.master->replid,sizeof(server.replid));
        server.master_repl_offset = server.master->reploff;
        clearReplicationId2();

        /* Let's create the replication backlog if needed. Slaves need to
        * accumulate the backlog regardless of the fact they have sub-slaves
        * or not, in order to behave correctly if they are promoted to
        * masters after a failover. */
        if (server.repl_backlog == NULL) createReplicationBacklog();
        ...
    }
    ```

  - 针对于通过 socket 传输的场景，发送 ack 响应
  - 针对于开启 AOF 的场景，主动触发一次 AOF 重写

    ```c
    void readSyncBulkPayload(connection *conn) {
        ...
        /* Send the initial ACK immediately to put this replica in online state. */
        if (usemark) replicationSendAck();

        /* Restart the AOF subsystem now that we finished the sync. This
        * will trigger an AOF rewrite, and when done will start appending
        * to the new file. */
        if (server.aof_enabled) restartAOFAfterSYNC();
        return;
    }
    ```

## Ref

- <https://redis.io/docs/latest/operate/oss_and_stack/management/replication/>
