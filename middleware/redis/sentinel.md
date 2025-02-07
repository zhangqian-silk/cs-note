# Sentinel

哨兵是提高 Redis 可用性的方案之一，主要包含如下能力：

- **监控**：监控主从节点是否正常工作
- **通知**：通过接口，通知监控到的异常情况
- **自动故障转移**：当主节点异常时，可以自动执行故障转移，从可用的从节点中选举出新的主节点，并完成切换工作
- **提供配置**：提供服务发现能力，可以实时的返回给客户端最新的主节点地址

## 初始化

哨兵是一个特殊模式的 Redis 进程，在启动程序时，即 [`main()`](https://github.com/redis/redis/blob/7.0.0/src/server.c#L6832) 函数中，会在 [`checkForSentinelMode()`](https://github.com/redis/redis/blob/7.0.0/src/server.c#L6556) 函数中通过关键字进行区分，并在后续流程中，额外初始化相关配置。

```c
int main(int argc, char **argv) {
    ...
    char *exec_name = strrchr(argv[0], '/');
    if (exec_name == NULL) exec_name = argv[0];
    server.sentinel_mode = checkForSentinelMode(argc,argv, exec_name);
    ...
}

/* Returns 1 if there is --sentinel among the arguments or if
 * executable name contains "redis-sentinel". */
int checkForSentinelMode(int argc, char **argv, char *exec_name) {
    if (strstr(exec_name,"redis-sentinel") != NULL) return 1;

    for (int j = 1; j < argc; j++)
        if (!strcmp(argv[j],"--sentinel")) return 1;
    return 0;
}
```

除此以外，还会调用 [`sentinelHandleConfiguration()`](https://github.com/redis/redis/blob/7.0.0/src/sentinel.c#L1810) 函数，遍历配置文件，并最终通过 [`sentinelHandleConfiguration()`](https://github.com/redis/redis/blob/7.0.0/src/sentinel.c#L1855) 函数加载相关配置，例如连接主节点。

```c
int main(int argc, char **argv) {
    ...
    if (argc >= 2) {
        ...
        if (server.sentinel_mode) loadSentinelConfigFromQueue();
        ...
    }
    ...
}

void loadSentinelConfigFromQueue(void) {
    ...
    for (j = 0; j < sizeof(sentinel_configs) / sizeof(sentinel_configs[0]); j++) {
        listRewind(sentinel_configs[j],&li);
        while((ln = listNext(&li))) {
            struct sentinelLoadQueueEntry *entry = ln->value;
            err = sentinelHandleConfiguration(entry->argv,entry->argc);
            ...
        }
    }
    ...
}

const char *sentinelHandleConfiguration(char **argv, int argc) {
    ...
    if (!strcasecmp(argv[0],"monitor") && argc == 5) {
        /* monitor <name> <host> <port> <quorum> */
        int quorum = atoi(argv[4]);

        if (quorum <= 0) return "Quorum must be 1 or greater.";
        if (createSentinelRedisInstance(argv[1],SRI_MASTER,argv[2],
                                        atoi(argv[3]),quorum,NULL) == NULL)
        {
            return sentinelCheckCreateInstanceErrors(SRI_MASTER);
        }
    }
    ...
}
```

## 定时任务

在定时任务 [`serverCron()`](https://github.com/redis/redis/blob/7.0.0/src/server.c#L1157) 中，哨兵相关的任务统一封装在函数 [`sentinelTimer()`](https://github.com/redis/redis/blob/7.0.0/src/sentinel.c#L5355) 中。

```c
int serverCron(struct aeEventLoop *eventLoop, long long id, void *clientData) {
    ...
    /* Run the Sentinel timer if we are in sentinel mode. */
    if (server.sentinel_mode) sentinelTimer();
    ...
}

void sentinelTimer(void) {
    sentinelCheckTiltCondition();
    sentinelHandleDictOfRedisInstances(sentinel.masters);
    sentinelRunPendingScripts();
    sentinelCollectTerminatedScripts();
    sentinelKillTimedoutScripts();

    /* We continuously change the frequency of the Redis "timer interrupt"
     * in order to desynchronize every Sentinel from every other.
     * This non-determinism avoids that Sentinels started at the same time
     * exactly continue to stay synchronized asking to be voted at the
     * same time again and again (resulting in nobody likely winning the
     * election because of split brain voting). */
    server.hz = CONFIG_DEFAULT_HZ + rand() % CONFIG_DEFAULT_HZ;
}
```

在 [`sentinelHandleDictOfRedisInstances()`](https://github.com/redis/redis/blob/7.0.0/src/sentinel.c#L5300) 函数中，会递归处理所有主节点，以及对应的从节点和哨兵节点

```c
void sentinelHandleDictOfRedisInstances(dict *instances) {
    di = dictGetIterator(instances);
    while((de = dictNext(di)) != NULL) {
        sentinelRedisInstance *ri = dictGetVal(de);

        sentinelHandleRedisInstance(ri);
        if (ri->flags & SRI_MASTER) {
            sentinelHandleDictOfRedisInstances(ri->slaves);
            sentinelHandleDictOfRedisInstances(ri->sentinels);
            if (ri->failover_state == SENTINEL_FAILOVER_STATE_UPDATE_CONFIG) {
                switch_to_promoted = ri;
            }
        }
    }
    if (switch_to_promoted)
        sentinelFailoverSwitchToPromotedSlave(switch_to_promoted);
    dictReleaseIterator(di);
}
```

在 [`sentinelHandleRedisInstance()`](https://github.com/redis/redis/blob/7.0.0/src/sentinel.c#L5264) 函数中，会根据节点类型不同，执行不同的逻辑：

- [`sentinelReconnectInstance()`](https://github.com/redis/redis/blob/7.0.0/src/sentinel.c#L2399)：重连接所有下线实例
- [`sentinelSendPeriodicCommands()`](https://github.com/redis/redis/blob/7.0.0/src/sentinel.c#L3113)：周期性的发送消息，用于监控实例状态
- [`sentinelCheckSubjectivelyDown()`](https://github.com/redis/redis/blob/7.0.0/src/sentinel.c#L4422)：判断主观下线
- [`sentinelCheckObjectivelyDown()`](https://github.com/redis/redis/blob/7.0.0/src/sentinel.c#L4496)：判断客观下线
- [`sentinelStartFailoverIfNeeded()`](https://github.com/redis/redis/blob/7.0.0/src/sentinel.c#L4857)：判断是否需要执行故障转移操作
- [`sentinelAskMasterStateToOtherSentinels()`](https://github.com/redis/redis/blob/7.0.0/src/sentinel.c#L4576)：向其他哨兵询问主节点状态，确保所有哨兵保持同步
- [`sentinelFailoverStateMachine()`](https://github.com/redis/redis/blob/7.0.0/src/sentinel.c#L5216)：故障转移状态机，推进故障转移流程

```c
/* Perform scheduled operations for the specified Redis instance. */
void sentinelHandleRedisInstance(sentinelRedisInstance *ri) {
    /* ========== MONITORING HALF ============ */
    /* Every kind of instance */
    sentinelReconnectInstance(ri);
    sentinelSendPeriodicCommands(ri);

    /* ============== ACTING HALF ============= */
    /* We don't proceed with the acting half if we are in TILT mode.
     * TILT happens when we find something odd with the time, like a
     * sudden change in the clock. */
    if (sentinel.tilt) {
        if (mstime()-sentinel.tilt_start_time < sentinel_tilt_period) return;
        sentinel.tilt = 0;
        sentinelEvent(LL_WARNING,"-tilt",NULL,"#tilt mode exited");
    }

    /* Every kind of instance */
    sentinelCheckSubjectivelyDown(ri);

    /* Masters and slaves */
    if (ri->flags & (SRI_MASTER|SRI_SLAVE)) {
        /* Nothing so far. */
    }

    /* Only masters */
    if (ri->flags & SRI_MASTER) {
        sentinelCheckObjectivelyDown(ri);
        if (sentinelStartFailoverIfNeeded(ri))
            sentinelAskMasterStateToOtherSentinels(ri,SENTINEL_ASK_FORCED);
        sentinelFailoverStateMachine(ri);
        sentinelAskMasterStateToOtherSentinels(ri,SENTINEL_NO_FLAGS);
    }
}
```

## 监控

在 [`sentinelSendPeriodicCommands()`](https://github.com/redis/redis/blob/7.0.0/src/sentinel.c#L3113) 函数中，哨兵会定期向节点发送 PING、INFO 和 HELLO 三种命令，用来实现监控能力。

### INFO

INFO 命令用于主动获取指定实例的状态信息，并通过 [`sentinelInfoReplyCallback()`](https://github.com/redis/redis/blob/7.0.0/src/sentinel.c#L2769) 函数处理返回数据。

```c
void sentinelSendPeriodicCommands(sentinelRedisInstance *ri) {
    ...
    /* Send INFO to masters and slaves, not sentinels. */
    if ((ri->flags & SRI_SENTINEL) == 0 &&
        (ri->info_refresh == 0 ||
        (now - ri->info_refresh) > info_period))
    {
        retval = redisAsyncCommand(ri->link->cc,
            sentinelInfoReplyCallback, ri, "%s",
            sentinelInstanceMapCommand(ri,"INFO"));
        if (retval == C_OK) ri->link->pending_commands++;
    }
    ...
}

void sentinelInfoReplyCallback(redisAsyncContext *c, void *reply, void *privdata) {
    ...
    link->pending_commands--;
    r = reply;

    if (r->type == REDIS_REPLY_STRING)
        sentinelRefreshInstanceInfo(ri,r->str);
}
```

在 [`sentinelRefreshInstanceInfo()`](https://github.com/redis/redis/blob/7.0.0/src/sentinel.c#L2512) 函数中，会将实例返回的信息，更新至实例中。

```c
void sentinelRefreshInstanceInfo(sentinelRedisInstance *ri, const char *info) {
    ...
    /* Process line by line. */
    lines = sdssplitlen(info,strlen(info),"\r\n",2,&numlines);
    for (j = 0; j < numlines; j++) {
        ...
        /* run_id:<40 hex chars>*/
        if (sdslen(l) >= 47 && !memcmp(l,"run_id:",7)) {
            ...
        }
        
        /* old versions: slave0:<ip>,<port>,<state>
         * new versions: slave0:ip=127.0.0.1,port=9999,... */
        if ((ri->flags & SRI_MASTER) &&
            sdslen(l) >= 7 &&
            !memcmp(l,"slave",5) && isdigit(l[5]))
        {
            ...
        }

         /* master_link_down_since_seconds:<seconds> */
        if (sdslen(l) >= 32 &&
            !memcmp(l,"master_link_down_since_seconds",30))
        {
            ri->master_link_down_time = strtoll(l+31,NULL,10)*1000;
        }

        /* role:<role> */
        if (sdslen(l) >= 11 && !memcmp(l,"role:master",11)) role = SRI_MASTER;
        else if (sdslen(l) >= 10 && !memcmp(l,"role:slave",10)) role = SRI_SLAVE;

        if (role == SRI_SLAVE) {
            ...
        }
    }
    ...
}
```

如果发现实例的角色发生了变化，则额外进行相关处理：

```c
void sentinelRefreshInstanceInfo(sentinelRedisInstance *ri, const char *info) {
    ...
    /* Remember when the role changed. */
    if (role != ri->role_reported) {
        ...
    }

    /* None of the following conditions are processed when in tilt mode, so
     * return asap. */
    if (sentinel.tilt) return;

    /* Handle master -> slave role switch. */
    if ((ri->flags & SRI_MASTER) && role == SRI_SLAVE) {
        /* Nothing to do, but masters claiming to be slaves are
         * considered to be unreachable by Sentinel, so eventually
         * a failover will be triggered. */
    }

    /* Handle slave -> master role switch. */
    if ((ri->flags & SRI_SLAVE) && role == SRI_MASTER) {
        ...
    }

    /* Handle slaves replicating to a different master address. */
    if ((ri->flags & SRI_SLAVE) &&
        role == SRI_SLAVE &&
        (ri->slave_master_port != ri->master->addr->port ||
         !sentinelAddrEqualsHostname(ri->master->addr, ri->slave_master_host)))
    {
        ...
    }

    /* Detect if the slave that is in the process of being reconfigured
     * changed state. */
    if ((ri->flags & SRI_SLAVE) && role == SRI_SLAVE &&
        (ri->flags & (SRI_RECONF_SENT|SRI_RECONF_INPROG)))
    {
        ...
    }
}
```

### PING

[`sentinelSendPing()`](https://github.com/redis/redis/blob/7.0.0/src/sentinel.c#L3093) 函数会向指定实例发送 PING 命令，用于心跳检测。

```c
void sentinelSendPeriodicCommands(sentinelRedisInstance *ri) {
    ...
    /* Send PING to all the three kinds of instances. */
    if ((now - ri->link->last_pong_time) > ping_period &&
               (now - ri->link->last_ping_time) > ping_period/2) {
        sentinelSendPing(ri);
    }
    ...
}

/* Send a PING to the specified instance and refresh the act_ping_time
 * if it is zero (that is, if we received a pong for the previous ping).
 *
 * On error zero is returned, and we can't consider the PING command
 * queued in the connection. */
int sentinelSendPing(sentinelRedisInstance *ri) {
    int retval = redisAsyncCommand(ri->link->cc,
        sentinelPingReplyCallback, ri, "%s",
        sentinelInstanceMapCommand(ri,"PING"));
    if (retval == C_OK) {
        ri->link->pending_commands++;
        ri->link->last_ping_time = mstime();
        /* We update the active ping time only if we received the pong for
         * the previous ping, otherwise we are technically waiting since the
         * first ping that did not receive a reply. */
        if (ri->link->act_ping_time == 0)
            ri->link->act_ping_time = ri->link->last_ping_time;
        return 1;
    } else {
        return 0;
    }
}
```

在 [`sentinelPingReplyCallback()`](https://github.com/redis/redis/blob/7.0.0/src/sentinel.c#L2792) 函数中，会用实例的响应进行处理，在实例状态正常的情况下，更新 `last_avail_time` 和 `last_pong_time` 对应的时间，后续会用于实例下线的判断。

```c
void sentinelPingReplyCallback(redisAsyncContext *c, void *reply, void *privdata) {
    ...
    if (r->type == REDIS_REPLY_STATUS ||
        r->type == REDIS_REPLY_ERROR) {
        /* Update the "instance available" field only if this is an
         * acceptable reply. */
        if (strncmp(r->str,"PONG",4) == 0 ||
            strncmp(r->str,"LOADING",7) == 0 ||
            strncmp(r->str,"MASTERDOWN",10) == 0)
        {
            link->last_avail_time = mstime();
            link->act_ping_time = 0; /* Flag the pong as received. */
            ...
        } 
        ...
    }
    link->last_pong_time = mstime();
}
```

### HELLO

[`sentinelSendHello()`](https://github.com/redis/redis/blob/7.0.0/src/sentinel.c#L3014) 函数会通过 Pub/Sub 的方式，广播 Hello 消息，通知当前哨兵的信息以及主节点信息

```c
void sentinelSendPeriodicCommands(sentinelRedisInstance *ri) {
    ...
    /* PUBLISH hello messages to all the three kinds of instances. */
    if ((now - ri->last_pub_time) > sentinel_publish_period) {
        sentinelSendHello(ri);
    }
}

int sentinelSendHello(sentinelRedisInstance *ri) {
    ...
    /* Format and send the Hello message. */
    snprintf(payload,sizeof(payload),
        "%s,%d,%s,%llu," /* Info about this sentinel. */
        "%s,%s,%d,%llu", /* Info about current master. */
        announce_ip, announce_port, sentinel.myid,
        (unsigned long long) sentinel.current_epoch,
        /* --- */
        master->name,announceSentinelAddr(master_addr),master_addr->port,
        (unsigned long long) master->config_epoch);
    retval = redisAsyncCommand(ri->link->cc,
        sentinelPublishReplyCallback, ri, "%s %s %s",
        sentinelInstanceMapCommand(ri,"PUBLISH"),
        SENTINEL_HELLO_CHANNEL,payload);
    ...
}
```

## 故障判断

在 [`sentinelHandleRedisInstance()`](https://github.com/redis/redis/blob/7.0.0/src/sentinel.c#L5264) 函数中，哨兵会使用监控时所得到的信息，对节点进行故障判断。如果节点没有在指定的时间内响应 PING 命令，则将其标记为主观下线。

为了避免因网络或负载而出现的偶然性问题，哨兵通常会以集群的方式进行部署。如果哨兵集群内，有哨兵判断某一节点主观下线，则会向其他哨兵发起投票，当满足票数要求时，会认为该节点客观下线，并执行故障转移等相关操作。

```c
/* Perform scheduled operations for the specified Redis instance. */
void sentinelHandleRedisInstance(sentinelRedisInstance *ri) {
    ...
    /* Every kind of instance */
    sentinelCheckSubjectivelyDown(ri);
    ...
    /* Only masters */
    if (ri->flags & SRI_MASTER) {
        sentinelCheckObjectivelyDown(ri);
        if (sentinelStartFailoverIfNeeded(ri))
            sentinelAskMasterStateToOtherSentinels(ri,SENTINEL_ASK_FORCED);
        sentinelFailoverStateMachine(ri);
        sentinelAskMasterStateToOtherSentinels(ri,SENTINEL_NO_FLAGS);
    }
}
```

### 主观下线

[`sentinelCheckSubjectivelyDown()`](https://github.com/redis/redis/blob/7.0.0/src/sentinel.c#L4422) 函数内部会根据节点的相关状态，判断是否已经下线。

- 初始化未响应时间 `elapsed`，记录该实例上次活跃时间
  - `act_ping_time`：首次发送 PING 请求但未响应的时间
  - `disconnected`：断联标识位
  - `last_avail_time`：最后一次收到 PING 响应的时间

    ```c
    void sentinelCheckSubjectivelyDown(sentinelRedisInstance *ri) {
        mstime_t elapsed = 0;

        if (ri->link->act_ping_time)
            elapsed = mstime() - ri->link->act_ping_time;
        else if (ri->link->disconnected)
            elapsed = mstime() - ri->link->last_avail_time;
        ...
    }
    ```

- 校验命令连接的状态，判断通过后会断开连接
  - 判断命令连接的时长是否超过最短时长 `sentinel_min_link_reconnect_period`
  - 判断 PING 命令请求的阻塞时长是否超过超时时长 `down_after_period` 的一半

    ```c
    void sentinelCheckSubjectivelyDown(sentinelRedisInstance *ri) {
        ...
        /* Check if we are in need for a reconnection of one of the
        * links, because we are detecting low activity.
        *
        * 1) Check if the command link seems connected, was connected not less
        *    than SENTINEL_MIN_LINK_RECONNECT_PERIOD, but still we have a
        *    pending ping for more than half the timeout. */
        if (ri->link->cc &&
            (mstime() - ri->link->cc_conn_time) >
            sentinel_min_link_reconnect_period &&
            ri->link->act_ping_time != 0 && /* There is a pending ping... */
            /* The pending ping is delayed, and we did not receive
            * error replies as well. */
            (mstime() - ri->link->act_ping_time) > (ri->down_after_period/2) &&
            (mstime() - ri->link->last_pong_time) > (ri->down_after_period/2))
        {
            instanceLinkCloseConnection(ri->link,ri->link->cc);
        }
        ...
    }
    ```

- 校验订阅连接的状态，校验通过后会断开连接
  - 判断订阅连接的时长是否超过最短时长 `sentinel_min_link_reconnect_period`
  - 判断订阅连接，即 HELLO 命令请求的阻塞时长是否超过超时时长 `sentinel_publish_period` 的三倍

    ```c
    void sentinelCheckSubjectivelyDown(sentinelRedisInstance *ri) {
        ...
        /* 2) Check if the pubsub link seems connected, was connected not less
        *    than SENTINEL_MIN_LINK_RECONNECT_PERIOD, but still we have no
        *    activity in the Pub/Sub channel for more than
        *    SENTINEL_PUBLISH_PERIOD * 3.
        */
        if (ri->link->pc &&
            (mstime() - ri->link->pc_conn_time) >
            sentinel_min_link_reconnect_period &&
            (mstime() - ri->link->pc_last_activity) > (sentinel_publish_period*3))
        {
            instanceLinkCloseConnection(ri->link,ri->link->pc);
        }
        ...
    }
    ```

- 更新主观下线状态位
  - 如果未响应时间 `elapsed` 超过了下线超时时间 `down_after_period`，则认为主观下线
  - 如果当前是主节点，但上报为从节点，且上报时间超过了下线超时时间 `down_after_period` 加上两倍 INFO 时间 `sentinel_info_period` 的和，则认为主观下线
  - 如果当前节点处于重启状态，且重启时间超过了重启超时时间 `master_reboot_down_after_period`，则认为主观下线
  - 在认定该节点主观下线后，会更新 `SRI_S_DOWN` 状态位及时间，否则移除主观下线状态位

    ```c
    void sentinelCheckSubjectivelyDown(sentinelRedisInstance *ri) {
        ...
        /* Update the SDOWN flag. We believe the instance is SDOWN if:
        *
        * 1) It is not replying.
        * 2) We believe it is a master, it reports to be a slave for enough time
        *    to meet the down_after_period, plus enough time to get two times
        *    INFO report from the instance. */
        if (elapsed > ri->down_after_period ||
            (ri->flags & SRI_MASTER &&
            ri->role_reported == SRI_SLAVE &&
            mstime() - ri->role_reported_time >
            (ri->down_after_period+sentinel_info_period*2)) ||
            (ri->flags & SRI_MASTER_REBOOT && 
            mstime()-ri->master_reboot_since_time > ri->master_reboot_down_after_period))
        {
            /* Is subjectively down */
            if ((ri->flags & SRI_S_DOWN) == 0) {
                sentinelEvent(LL_WARNING,"+sdown",ri,"%@");
                ri->s_down_since_time = mstime();
                ri->flags |= SRI_S_DOWN;
            }
        } else {
            /* Is subjectively up */
            if (ri->flags & SRI_S_DOWN) {
                sentinelEvent(LL_WARNING,"-sdown",ri,"%@");
                ri->flags &= ~(SRI_S_DOWN|SRI_SCRIPT_KILL_SENT);
            }
        }
    }
    ```

### 客观下线

[`sentinelCheckObjectivelyDown()`](https://github.com/redis/redis/blob/7.0.0/src/sentinel.c#L4496) 函数中，会根据其他哨兵的判断，来更新客观下线标识位。

- 初始化参数
  - `quorum`：投票数
  - `odown`：客观下线

```c
void sentinelCheckObjectivelyDown(sentinelRedisInstance *master) {
    ...
    dictIterator *di;
    dictEntry *de;
    unsigned int quorum = 0, odown = 0;
}
```

- 判断客观下线
  - 首先判断该主节点已经被认为主观下线
  - 遍历该主节点的所有哨兵，判断其投票结果，即 `SRI_MASTER_DOWN` 标识位
  - 如果票数 `quorum` 大于主节点要求，则认为其客观下线

```c
void sentinelCheckObjectivelyDown(sentinelRedisInstance *master) {
    ...
    if (master->flags & SRI_S_DOWN) {
        /* Is down for enough sentinels? */
        quorum = 1; /* the current sentinel. */
        /* Count all the other sentinels. */
        di = dictGetIterator(master->sentinels);
        while((de = dictNext(di)) != NULL) {
            sentinelRedisInstance *ri = dictGetVal(de);

            if (ri->flags & SRI_MASTER_DOWN) quorum++;
        }
        dictReleaseIterator(di);
        if (quorum >= master->quorum) odown = 1;
    }
    ...
}
```

- 更新客观下线标识位
  - 根据投票结果，修改该主节点的标识位及时间

```c
void sentinelCheckObjectivelyDown(sentinelRedisInstance *master) {
    ...
    /* Set the flag accordingly to the outcome. */
    if (odown) {
        if ((master->flags & SRI_O_DOWN) == 0) {
            sentinelEvent(LL_WARNING,"+odown",master,"%@ #quorum %d/%d",
                quorum, master->quorum);
            master->flags |= SRI_O_DOWN;
            master->o_down_since_time = mstime();
        }
    } else {
        if (master->flags & SRI_O_DOWN) {
            sentinelEvent(LL_WARNING,"-odown",master,"%@");
            master->flags &= ~SRI_O_DOWN;
        }
    }
}
```

### 投票

[`sentinelAskMasterStateToOtherSentinels()`](https://github.com/redis/redis/blob/7.0.0/src/sentinel.c#L4576) 函数会向其他哨兵发送请求，获取他们所存储的主节点的状态，并将其作为客观下线的投票结果。

- 初始化
  - 初始化局部变量
  - 遍历该主节点对应的所有哨兵节点

    ```c
    void sentinelAskMasterStateToOtherSentinels(sentinelRedisInstance *master, int flags) {
        dictIterator *di;
        dictEntry *de;

        di = dictGetIterator(master->sentinels);
        while((de = dictNext(di)) != NULL) {
            ...
        }
        dictReleaseIterator(di);
    }
    ```

- 判断未活跃时长
  - 计算距离该哨兵上次认为主节点故障的时间，即 `elapsed`
  - 如果时间过长，超过请求间隔 `sentinel_ask_period` 的五倍，则认为信息过旧，执行清除逻辑

    ```c
    void sentinelAskMasterStateToOtherSentinels(sentinelRedisInstance *master, int flags) {
        ...
        while((de = dictNext(di)) != NULL) {
            sentinelRedisInstance *ri = dictGetVal(de);
            mstime_t elapsed = mstime() - ri->last_master_down_reply_time;
            char port[32];
            int retval;

            /* If the master state from other sentinel is too old, we clear it. */
            if (elapsed > sentinel_ask_period*5) {
                ri->flags &= ~SRI_MASTER_DOWN;
                sdsfree(ri->leader);
                ri->leader = NULL;
            }
        }
        ...
    }
    ```

- 判断是否发送询问
  - 校验主节点是否处于主观下线状态
  - 校验与该哨兵节点是否断联
  - 校验 `SENTINEL_ASK_FORCED` 标识位，或是离上次故障时间是否满足阈值 `sentinel_ask_period`

    ```c
    void sentinelAskMasterStateToOtherSentinels(sentinelRedisInstance *master, int flags) {
        ...
        while((de = dictNext(di)) != NULL) {
            ...
            /* Only ask if master is down to other sentinels if:
            *
            * 1) We believe it is down, or there is a failover in progress.
            * 2) Sentinel is connected.
            * 3) We did not receive the info within SENTINEL_ASK_PERIOD ms. */
            if ((master->flags & SRI_S_DOWN) == 0) continue;
            if (ri->link->disconnected) continue;
            if (!(flags & SENTINEL_ASK_FORCED) &&
                mstime() - ri->last_master_down_reply_time < sentinel_ask_period)
                continue;
            ...
        }
        ...
    }
    ```

- 发送询问请求
  - 发送命为 `is-master-down-by-addr` 的命令，目标哨兵会在 [`sentinelCommand()`](https://github.com/redis/redis/blob/7.0.0/src/sentinel.c#L3753) 函数中进行判断该主节点是否处于主观下线的状态，并返回结果
  - 当前哨兵会通过 [`sentinelReceiveIsMasterDownReply()`](https://github.com/redis/redis/blob/7.0.0/src/sentinel.c#L4533) 函数处理返回数据

    ```c
    void sentinelAskMasterStateToOtherSentinels(sentinelRedisInstance *master, int flags) {
        ...
        while((de = dictNext(di)) != NULL) {
            ...
            /* Ask */
            ll2string(port,sizeof(port),master->addr->port);
            retval = redisAsyncCommand(ri->link->cc,
                        sentinelReceiveIsMasterDownReply, ri,
                        "%s is-master-down-by-addr %s %s %llu %s",
                        sentinelInstanceMapCommand(ri,"SENTINEL"),
                        announceSentinelAddr(master->addr), port,
                        sentinel.current_epoch,
                        (master->failover_state > SENTINEL_FAILOVER_STATE_NONE) ?
                        sentinel.myid : "*");
            if (retval == C_OK) ri->link->pending_commands++;
        }
        ...
    }

    void sentinelCommand(client *c) {
        ...
        if (!strcasecmp(c->argv[1]->ptr,"is-master-down-by-addr")) {
            ...
            /* It exists? Is actually a master? Is subjectively down? It's down.
             * Note: if we are in tilt mode we always reply with "0". */
            if (!sentinel.tilt && ri && (ri->flags & SRI_S_DOWN) &&
                                        (ri->flags & SRI_MASTER))
                isdown = 1;
            ...
            addReply(c, isdown ? shared.cone : shared.czero);
            ...
        }
        ...
    }
    ```

- 根据请求响应，更新标识位
  - 根据目标哨兵的返回值，更新 `SRI_MASTER_DOWN` 标识位，并作为之后客观下线的投票结果

```c
void sentinelReceiveIsMasterDownReply(redisAsyncContext *c, void *reply, void *privdata) {
    ...
    if (r->type == REDIS_REPLY_ARRAY && r->elements == 3 &&
        r->element[0]->type == REDIS_REPLY_INTEGER &&
        r->element[1]->type == REDIS_REPLY_STRING &&
        r->element[2]->type == REDIS_REPLY_INTEGER)
    {
        ri->last_master_down_reply_time = mstime();
        if (r->element[0]->integer == 1) {
            ri->flags |= SRI_MASTER_DOWN;
        } else {
            ri->flags &= ~SRI_MASTER_DOWN;
        }
        ...
    }
}
```

## 故障转移

[`sentinelStartFailoverIfNeeded()`](https://github.com/redis/redis/blob/7.0.0/src/sentinel.c#L4857) 函数中会通过客观下线标记位和时间频率，判断是否要执行故障转移操作。

```c
int sentinelStartFailoverIfNeeded(sentinelRedisInstance *master) {
    /* We can't failover if the master is not in O_DOWN state. */
    if (!(master->flags & SRI_O_DOWN)) return 0;

    /* Failover already in progress? */
    if (master->flags & SRI_FAILOVER_IN_PROGRESS) return 0;

    /* Last failover attempt started too little time ago? */
    if (mstime() - master->failover_start_time <
        master->failover_timeout*2)
    {
        ...
        return 0;
    }

    sentinelStartFailover(master);
    return 1;
}
```

[`sentinelStartFailover()`](https://github.com/redis/redis/blob/7.0.0/src/sentinel.c#L4833) 函数会将故障转移状态机设置为 `SENTINEL_FAILOVER_STATE_WAIT_START`，修改故障相关的其他属性，触发相关事件，开启故障转移流程。

```c
void sentinelStartFailover(sentinelRedisInstance *master) {
    serverAssert(master->flags & SRI_MASTER);

    master->failover_state = SENTINEL_FAILOVER_STATE_WAIT_START;
    master->flags |= SRI_FAILOVER_IN_PROGRESS;
    master->failover_epoch = ++sentinel.current_epoch;
    sentinelEvent(LL_WARNING,"+new-epoch",master,"%llu",
        (unsigned long long) sentinel.current_epoch);
    sentinelEvent(LL_WARNING,"+try-failover",master,"%@");
    master->failover_start_time = mstime()+rand()%SENTINEL_MAX_DESYNC;
    master->failover_state_change_time = mstime();
}
```

定时任务中的 [`sentinelFailoverStateMachine()`](https://github.com/redis/redis/blob/7.0.0/src/sentinel.c#L5216) 函数，会根据当前故障转移的状态，执行不同的逻辑，完成故障转移的全部流程。

```c
void sentinelFailoverStateMachine(sentinelRedisInstance *ri) {
    serverAssert(ri->flags & SRI_MASTER);

    if (!(ri->flags & SRI_FAILOVER_IN_PROGRESS)) return;

    switch(ri->failover_state) {
        case SENTINEL_FAILOVER_STATE_WAIT_START:
            sentinelFailoverWaitStart(ri);
            break;
        case SENTINEL_FAILOVER_STATE_SELECT_SLAVE:
            sentinelFailoverSelectSlave(ri);
            break;
        case SENTINEL_FAILOVER_STATE_SEND_SLAVEOF_NOONE:
            sentinelFailoverSendSlaveOfNoOne(ri);
            break;
        case SENTINEL_FAILOVER_STATE_WAIT_PROMOTION:
            sentinelFailoverWaitPromotion(ri);
            break;
        case SENTINEL_FAILOVER_STATE_RECONF_SLAVES:
            sentinelFailoverReconfNextSlave(ri);
            break;
    }
}
```

### SENTINEL_FAILOVER_STATE_WAIT_START

[`sentinelFailoverWaitStart()`](https://github.com/redis/redis/blob/7.0.0/src/sentinel.c#L4993) 函数主要包含以下逻辑：

- **Leader 选举**：通过分布式共识机制确定当前故障转移的 Leader，确保只有一个 Sentinel 主导故障转移
- **超时控制**：若非 Leader 且未强制触发，通过超时机制避免无意义的等待，保障系统及时回退
- **状态机推进**：作为 Leader 时，推进故障转移状态至 “选择从节点”，为后续晋升从节点为主节点做准备
- **容错与测试**：支持强制故障转移和模拟故障，增强系统健壮性和可测试性

函数的详细逻辑如下所示：

- **确定当前 Sentinel 是否成为 Leader**
  - **调用 [`sentinelGetLeader()`](https://github.com/redis/redis/blob/7.0.0/src/sentinel.c#L4690) 函数**：通过 ri->failover_epoch（当前故障转移的纪元）计算集群中的 Leader。该函数通过 Raft 算法或 Sentinel 的投票机制确定 Leader 的 ID。
  - **身份校验**：`isleader` 标志通过比对 leader 与当前 Sentinel 的 ID (`sentinel.myid`) 确认自身是否被选为 Leader。
  - **释放资源**：使用 `sdsfree()` 函数释放动态字符串内存，避免泄漏。

    ```c
    void sentinelFailoverWaitStart(sentinelRedisInstance *ri) {
        char *leader;
        int isleader;

        /* Check if we are the leader for the failover epoch. */
        leader = sentinelGetLeader(ri, ri->failover_epoch);
        isleader = leader && strcasecmp(leader,sentinel.myid) == 0;
        sdsfree(leader);
        ...
    }
    ```

- **非 Leader 且非强制故障转移的处理**
  - **条件判断**：如果当前 Sentinel 不是 Leader 且未设置 `SRI_FORCE_FAILOVER`（强制故障转移标志），则进入超时处理逻辑。
  - **计算选举超时时间**：`election_timeout` 取 `sentinel_election_timeout`（默认值）与 `ri->failover_timeout`（配置的超时时间）的较小值，确保故障转移及时终止。
  - **超时判定**：若当前时间 (`mstime()`) 与故障转移开始时间 (`ri->failover_start_time`) 的差值超过 `election_timeout`，则：
    - 触发事件 `-failover-abort-not-elected`，记录日志。
    - 调用 [`sentinelAbortFailover()`](https://github.com/redis/redis/blob/7.0.0/src/sentinel.c#L5245) 终止故障转移流程。
  - **直接返回**：终止当前函数执行，不继续后续流程。

    ```c
    void sentinelFailoverWaitStart(sentinelRedisInstance *ri) {
        ...
        /* If I'm not the leader, and it is not a forced failover via
        * SENTINEL FAILOVER, then I can't continue with the failover. */
        if (!isleader && !(ri->flags & SRI_FORCE_FAILOVER)) {
            mstime_t election_timeout = sentinel_election_timeout;

            /* The election timeout is the MIN between SENTINEL_ELECTION_TIMEOUT
            * and the configured failover timeout. */
            if (election_timeout > ri->failover_timeout)
                election_timeout = ri->failover_timeout;
            /* Abort the failover if I'm not the leader after some time. */
            if (mstime() - ri->failover_start_time > election_timeout) {
                sentinelEvent(LL_WARNING,"-failover-abort-not-elected",ri,"%@");
                sentinelAbortFailover(ri);
            }
            return;
        }
        ...
    }
    ```

- **Leader 或强制故障转移的后续操作**
  - **Leader 选举成功事件**：记录 `+elected-leader` 事件，表明当前 Sentinel 成为 Leader。
  - **模拟故障注入（测试用）**：若设置 `SENTINEL_SIMFAILURE_CRASH_AFTER_ELECTION` 标志（模拟故障场景），调用 [`sentinelSimFailureCrash()`](https://github.com/redis/redis/blob/7.0.0/src/sentinel.c#L4623) 主动崩溃，用于测试 Sentinel 高可用性。
  - **更新故障转移状态**：将 `ri->failover_state` 设置为 `SENTINEL_FAILOVER_STATE_SELECT_SLAVE`，表示进入 “选择从节点” 阶段。
  - **记录状态变更时间**：更新 `ri->failover_state_change_time` 为当前时间，用于后续状态超时控制。
  - **触发状态切换事件**：记录 `+failover-state-select-slave` 事件，通知进入从节点选择阶段。

    ```c
    void sentinelFailoverWaitStart(sentinelRedisInstance *ri) {
        ...
        sentinelEvent(LL_WARNING,"+elected-leader",ri,"%@");
        if (sentinel.simfailure_flags & SENTINEL_SIMFAILURE_CRASH_AFTER_ELECTION)
            sentinelSimFailureCrash();
        ri->failover_state = SENTINEL_FAILOVER_STATE_SELECT_SLAVE;
        ri->failover_state_change_time = mstime();
        sentinelEvent(LL_WARNING,"+failover-state-select-slave",ri,"%@");
    }
    ```

### SENTINEL_FAILOVER_STATE_SELECT_SLAVE

[`sentinelFailoverSelectSlave`](https://github.com/redis/redis/blob/7.0.0/src/sentinel.c#L5026) 函数负责从当前主库（`ri`）的从库列表中选出一个最合适的从库，将其标记为待提升状态，并推进故障转移流程到下一阶段，详细介绍如下所示：

- **选择候选从库**
  - 调用 [`sentinelSelectSlave()`](https://github.com/redis/redis/blob/7.0.0/src/sentinel.c#L4947) 从主库 `ri` 的从库列表中筛选符合条件的从库

    ```c
    void sentinelFailoverSelectSlave(sentinelRedisInstance *ri) {
        sentinelRedisInstance *slave = sentinelSelectSlave(ri);
        ...
    }
    ```

- **处理无可用从库的情况**
  - 若 [`sentinelSelectSlave()`](https://github.com/redis/redis/blob/7.0.0/src/sentinel.c#L4947) 返回 `NULL`，表示没有合适的从库。
  - 记录警告日志 `-failover-abort-no-good-slave`，表明因无可用从库导致故障转移中止。
  - 调用 [`sentinelAbortFailover()`](https://github.com/redis/redis/blob/7.0.0/src/sentinel.c#L5245) 终止故障转移流程，重置状态并清理相关标志。

    ```c
    void sentinelFailoverSelectSlave(sentinelRedisInstance *ri) {
        ...
        if (slave == NULL) {
            sentinelEvent(LL_WARNING,"-failover-abort-no-good-slave",ri,"%@");
            sentinelAbortFailover(ri);
        } else {
            ...
        }
    }
    ```

- **处理成功选中从库的情况**
  - 记录事件：输出 `+selected-slave` 日志，标记选中的从库。
  - 设置提升标志：为从库添加 `SRI_PROMOTED` 标志，防止其被重复选中。
  - 更新主库状态：
    - `ri->promoted_slave` 指向被选中的从库，记录待提升的目标。
    - 将故障转移状态推进到 `SENTINEL_FAILOVER_STATE_SEND_SLAVEOF_NOONE`，表示下一步将向该从库发送 `SLAVEOF NO ONE` 命令，使其停止复制并成为新主库。
    - 更新状态变更时间戳 `failover_state_change_time`，用于后续超时判断。
  - 记录状态变更：输出 `+failover-state-send-slaveof-noone` 日志，标志进入新阶段。

    ```c
    void sentinelFailoverSelectSlave(sentinelRedisInstance *ri) {
        ...
        if (slave == NULL) {
            ...
        } else {
            sentinelEvent(LL_WARNING,"+selected-slave",slave,"%@");
            slave->flags |= SRI_PROMOTED;
            ri->promoted_slave = slave;
            ri->failover_state = SENTINEL_FAILOVER_STATE_SEND_SLAVEOF_NOONE;
            ri->failover_state_change_time = mstime();
            sentinelEvent(LL_NOTICE,"+failover-state-send-slaveof-noone",
                slave, "%@");
        }
    }
    ```

### SENTINEL_FAILOVER_STATE_SEND_SLAVEOF_NOONE

sentinelFailoverSendSlaveOfNoOne(ri);

### SENTINEL_FAILOVER_STATE_WAIT_PROMOTION

sentinelFailoverWaitPromotion(ri);

### SENTINEL_FAILOVER_STATE_RECONF_SLAVES

sentinelFailoverReconfNextSlave(ri);

## Ref

- <https://redis.io/docs/latest/operate/oss_and_stack/management/sentinel/>
- <https://xiaolincoding.com/redis/cluster/sentinel.html>
- <https://juejin.cn/post/7274940764517548087>
- <https://juejin.cn/post/7281196961751203901>
