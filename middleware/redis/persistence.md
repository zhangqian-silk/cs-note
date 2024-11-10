# Persistence

Redis 中数据持久化方案分为两种：

- RDB(Redis Database)：定期存储内存中的数据快照，恢复时直接将二进制数据加载进内存
- AOF(Append Only File)：存储 Redis 的写操作日志，恢复时按照指定顺序重新执行

最终的持久化策略，自然也可以分为四种：

- 不启用持久化方案
- 启用 RDB
- 启用 AOF
- 启用 RDB 与 AOF 的混合方案

用户需要根据业务特性，做对应的选择。

## RDB

RDB 方案会存储当前内存中的全量数据的快照，一般仅在子线程中执行，避免阻塞主线程。为了加快子线程的创建，对于共享内存采用了写时复制([copy-on-write](https://zh.wikipedia.org/wiki/%E5%AF%AB%E5%85%A5%E6%99%82%E8%A4%87%E8%A3%BD))的方案，所有读操作共同使用一份副本文件，当有写操作发生时，单独将这部分内存复制一份，用于修改。

RDB 的执行时机可以设置多个检查点，比如间隔 5 分钟，或是新增了 100 条写入操作

- 执行间隔长，一般会增加数据丢失时的数据量
- 执行间隔短，会频繁触发创建子线程的操作，增加 CPU 开销

RDB 方案的优缺点如下所示：

- 优点

  - 方便进行数据恢复，有利于故障重启、备份恢复、数据迁移等场景
  - 不同备份文件间可用来对比版本差异
  - 性能高，主线程仅负责创建子线程，耗时的 IO 操作由子线程执行

- 缺点

  - 写时复制会增加内存占用，极端情况会导致内存占用翻倍
  - 快照仅会记录某一个特定时间点的数据，在下次执行前会存在数据丢失风险

## AOF

AOF 方案会针对于写操作，在执行完命令之后，将该命令追加至缓冲区中，然后通过 `write()` 函数，将缓冲区的数据写入内核缓冲区，由内核控制将缓冲区数据写入硬盘中。

在将数据最终从内核缓冲区写入硬盘时，由三种策略可供选择，同样需要在性能与数据丢失间做抉择：

- Always 策略：每次执行写操作命令时，同步将命令写入硬盘中的日志文件
- Everysec 策略：每次执行完写操作命令时，仅将命令写入内核缓冲区，每秒定时将缓冲区中数据写入硬盘
- No 策略：交由系统控制写回硬盘的时机

### 实现逻辑

Redis 在正常执行了用户的操作命令后，会通过命令传播模块，将命令同步至从服务器，同时也会在这个时机将命令写入 AOF 缓存中。之后会在服务端的定时任务或其他时机，将 AOF 缓存写入至硬盘中。

- [propagateNow()](https://github.com/redis/redis/blob/7.0.0/src/server.c#L3029)：将命令传播至 AOF 和从服务器

  ```c
  static void propagateNow(int dbid, robj **argv, int argc, int target) {
      if (!shouldPropagate(target))
          return;

      /* This needs to be unreachable since the dataset should be fixed during 
      * client pause, otherwise data may be lost during a failover. */
      serverAssert(!(areClientsPaused() && !server.client_pause_in_transaction));

      if (server.aof_state != AOF_OFF && target & PROPAGATE_AOF)
          feedAppendOnlyFile(dbid,argv,argc);
      if (target & PROPAGATE_REPL)
          replicationFeedSlaves(server.slaves,dbid,argv,argc);
  }
  ```

- [feedAppendOnlyFile()](https://github.com/redis/redis/blob/7.0.0/src/aof.c#L1263)：将写操作添加至内存缓冲区

  - 初始化逻辑，创建 `buf` 字符串，校验底层数据库 ID

    ```c
    void feedAppendOnlyFile(int dictid, robj **argv, int argc) {
        sds buf = sdsempty();

        serverAssert(dictid >= 0 && dictid < server.dbnum);
        ...
    }
    ```

  - 添加时间戳注解，用于解决 AOF 当前记录时间戳与服务端 unix 时间戳不一致的问题

    ```c
    void feedAppendOnlyFile(int dictid, robj **argv, int argc) {
        ...
        /* Feed timestamp if needed */
        if (server.aof_timestamp_enabled) {
            sds ts = genAofTimestampAnnotationIfNeeded(0);
            if (ts != NULL) {
                buf = sdscatsds(buf, ts);
                sdsfree(ts);
            }
        }
        ...
    }
    ```

  - 判断存储 AOF 的数据库是否发生变化

    ```c
    void feedAppendOnlyFile(int dictid, robj **argv, int argc) {
        ...
        /* The DB this command was targeting is not the same as the last command
        * we appended. To issue a SELECT command is needed. */
        if (dictid != server.aof_selected_db) {
            char seldb[64];

            snprintf(seldb,sizeof(seldb),"%d",dictid);
            buf = sdscatprintf(buf,"*2\r\n$6\r\nSELECT\r\n$%lu\r\n%s\r\n",
                (unsigned long)strlen(seldb),seldb);
            server.aof_selected_db = dictid;
        }
        ...
    }
    ```

  - 将操作命令写入缓冲区

    ```c
    void feedAppendOnlyFile(int dictid, robj **argv, int argc) {
        ...
        /* All commands should be propagated the same way in AOF as in replication.
        * No need for AOF-specific translation. */
        buf = catAppendOnlyGenericCommand(buf,argc,argv);

        /* Append to the AOF buffer. This will be flushed on disk just before
        * of re-entering the event loop, so before the client will get a
        * positive reply about the operation performed. */
        if (server.aof_state == AOF_ON ||
            (server.aof_state == AOF_WAIT_REWRITE && server.child_type == CHILD_TYPE_AOF))
        {
            server.aof_buf = sdscatlen(server.aof_buf, buf, sdslen(buf));
        }

        sdsfree(buf);
    }
    ```

- Flush 命令的调用时机

  - [rewriteAppendOnlyFileBackground()](https://github.com/redis/redis/blob/7.0.0/src/aof.c#L2381)：AOF 的重写任务

    ```c
    int rewriteAppendOnlyFileBackground(void) {
        ...
        flushAppendOnlyFile(1);
        ...
    }
    ```

  - [serverCron()](https://github.com/redis/redis/blob/7.0.0/src/server.c#L1157)：服务端的定时任务

    ```c
    int serverCron(struct aeEventLoop *eventLoop, long long id, void *clientData) {
        ...
        /* AOF postponed flush: Try at every cron cycle if the slow fsync
        * completed. */
        if ((server.aof_state == AOF_ON || server.aof_state == AOF_WAIT_REWRITE) &&
            server.aof_flush_postponed_start)
        {
            flushAppendOnlyFile(0);
        }

        /* AOF write errors: in this case we have a buffer to flush as well and
        * clear the AOF error in case of success to make the DB writable again,
        * however to try every second is enough in case of 'hz' is set to
        * a higher frequency. */
        run_with_period(1000) {
            if ((server.aof_state == AOF_ON || server.aof_state == AOF_WAIT_REWRITE) &&
                server.aof_last_write_status == C_ERR) 
                {
                    flushAppendOnlyFile(0);
                }
        }

        ...
    }
    ```

  - [beforeSleep()](https://github.com/redis/redis/blob/7.0.0/src/server.c#L1509)：事件循环前触发

    ```c
    void beforeSleep(struct aeEventLoop *eventLoop) {
        ...
        if (ProcessingEventsWhileBlocked) {
            ...
            if (server.aof_state == AOF_ON || server.aof_state == AOF_WAIT_REWRITE)
                flushAppendOnlyFile(0);
            ...
            return;
        }
        ...
        if (server.aof_state == AOF_ON || server.aof_state == AOF_WAIT_REWRITE)
            flushAppendOnlyFile(0);
        ...
    }
    ```

  - [finishShutdown()](https://github.com/redis/redis/blob/7.0.0/src/server.c#L4051)：在 `shutdown` 命令执行时触发

    ```c
    int finishShutdown(void) {
        ...
        if (server.aof_state != AOF_OFF) {
            /* Append only file: flush buffers and fsync() the AOF at exit */
            serverLog(LL_NOTICE,"Calling fsync() on the AOF file.");
            flushAppendOnlyFile(1);
            if (redis_fsync(server.aof_fd) == -1) {
                serverLog(LL_WARNING,"Fail to fsync the AOF file: %s.",
                                    strerror(errno));
            }
        }
        ...
    }
    ```

- [flushAppendOnlyFile()](https://github.com/redis/redis/blob/7.0.0/src/aof.c#L1025)：通过 [aofWrite()](https://github.com/redis/redis/blob/7.0.0/src/aof.c#L987) 函数将内存缓冲区数据写入内核缓冲区，并最终通过 [redis_fsync()](https://github.com/redis/redis/blob/unstable/src/config.h#L110) 函数写入硬盘

  - 判空，当前内存缓冲区为空时，判断是否执行 fsync 逻辑，否则直接 return

    ```c
    void flushAppendOnlyFile(int force) {
        ...
        if (sdslen(server.aof_buf) == 0) {
            /* Check if we need to do fsync even the aof buffer is empty,
            * because previously in AOF_FSYNC_EVERYSEC mode, fsync is
            * called only when aof buffer is not empty, so if users
            * stop write commands before fsync called in one second,
            * the data in page cache cannot be flushed in time. */
            if (server.aof_fsync == AOF_FSYNC_EVERYSEC &&
                server.aof_fsync_offset != server.aof_current_size &&
                server.unixtime > server.aof_last_fsync &&
                !(sync_in_progress = aofFsyncInProgress())) {
                goto try_fsync;
            } else {
                return;
            }
        }
        ...
    }
    ```

  - `AOF_FSYNC_EVERYSEC` 策略时，判断当前是否正在执行 sync 逻辑
    - 如果正在执行，且等待时间未超过 2s，则直接 return

    ```c
    void flushAppendOnlyFile(int force) {
        ...
        if (server.aof_fsync == AOF_FSYNC_EVERYSEC)
            sync_in_progress = aofFsyncInProgress();

        if (server.aof_fsync == AOF_FSYNC_EVERYSEC && !force) {
            /* With this append fsync policy we do background fsyncing.
            * If the fsync is still in progress we can try to delay
            * the write for a couple of seconds. */
            if (sync_in_progress) {
                if (server.aof_flush_postponed_start == 0) {
                    /* No previous write postponing, remember that we are
                    * postponing the flush and return. */
                    server.aof_flush_postponed_start = server.unixtime;
                    return;
                } else if (server.unixtime - server.aof_flush_postponed_start < 2) {
                    /* We were already waiting for fsync to finish, but for less
                    * than two seconds this is still ok. Postpone again. */
                    return;
                }
                /* Otherwise fall through, and go write since we can't wait
                * over two seconds. */
                server.aof_delayed_fsync++;
                serverLog(LL_NOTICE,"Asynchronous AOF fsync is taking too long (disk is busy?). Writing the AOF buffer without waiting for fsync to complete, this may slow down Redis.");
            }
        }
        ...
    }
    ```

  - 调用 [aofWrite()](https://github.com/redis/redis/blob/7.0.0/src/aof.c#L987) 函数，并最终调用 `write()` 函数，将内存缓冲区数据写入内核缓冲区

    ```c
    void flushAppendOnlyFile(int force) {
        ...
        nwritten = aofWrite(server.aof_fd,server.aof_buf,sdslen(server.aof_buf));
        ...
    }

    ssize_t aofWrite(int fd, const char *buf, size_t len) {
        ssize_t nwritten = 0, totwritten = 0;

        while(len) {
            nwritten = write(fd, buf, len);

            if (nwritten < 0) {
                if (errno == EINTR) continue;
                return totwritten ? totwritten : -1;
            }

            len -= nwritten;
            buf += nwritten;
            totwritten += nwritten;
        }

        return totwritten;
    }
    ```

  - 判断写入情况，并处理异常逻辑
    - 如果时是 `AOF_FSYNC_ALWAYS` 策略，因为要保障较强的一致性，所以直接 exit
    - 如果是其他策略写入失败，则进行重试，并移除已经写入成功的数据
    - 如果最终写入成功，则移除上一次异常的标记位，表示成功恢复

    ```c
    void flushAppendOnlyFile(int force) {
        ...
        if (nwritten != (ssize_t)sdslen(server.aof_buf)) {
            ...
            /* Handle the AOF write error. */
            if (server.aof_fsync == AOF_FSYNC_ALWAYS) {
                serverLog(LL_WARNING,"Can't recover from AOF write error when the AOF fsync policy is 'always'. Exiting...");
                exit(1);
            } else {
                server.aof_last_write_status = C_ERR;

                /* Trim the sds buffer if there was a partial write, and there
                * was no way to undo it with ftruncate(2). */
                if (nwritten > 0) {
                    server.aof_current_size += nwritten;
                    server.aof_last_incr_size += nwritten;
                    sdsrange(server.aof_buf,nwritten,-1);
                }
                return; /* We'll try again on the next call... */
            }
        } else {
            /* Successful write(2). If AOF was in error state, restore the
            * OK state and log the event. */
            if (server.aof_last_write_status == C_ERR) {
                serverLog(LL_WARNING,
                    "AOF write error looks solved, Redis can write again.");
                server.aof_last_write_status = C_OK;
            }
        }
        ...
    }
    ```

  - 更新标记位，并清空内存缓存

    ```c
    void flushAppendOnlyFile(int force) {
        ...
        server.aof_current_size += nwritten;
        server.aof_last_incr_size += nwritten;

        /* Re-use AOF buffer when it is small enough. The maximum comes from the
        * arena size of 4k minus some overhead (but is otherwise arbitrary). */
        if ((sdslen(server.aof_buf)+sdsavail(server.aof_buf)) < 4000) {
            sdsclear(server.aof_buf);
        } else {
            sdsfree(server.aof_buf);
            server.aof_buf = sdsempty();
        }
        ...
    }
    ```

  - 尝试执行 fsync 逻辑

    - 如果开启了 `aof_no_fsync_on_rewrite` 设置，且当前有活跃子线程在执行 IO 操作，则直接返回，不执行 fsync 逻辑
      - AOF 重写耗时可能较长，该标记位可以避免因为 AOF 重写而导致 fsync 操作被阻塞太长时间
      - 相对应的，在此期间宕机，也会丢掉一些日志数据
    - 针对于 `AOF_FSYNC_ALWAYS` 策略，直接调用 [redis_fsync()](https://github.com/redis/redis/blob/unstable/src/config.h#L110) 函数，将数据写入硬盘
    - 针对于 `AOF_FSYNC_EVERYSEC` 策略，如果当前没有正在执行 sync 操作，则创建一个后台任务，执行 fsync 逻辑
    - 针对于 `AOF_FSYNC_NO` 策略，不做特殊处理，由内核决定 fsync 的执行时机

    ```c
    void flushAppendOnlyFile(int force) {
        ...
    try_fsync:
        /* Don't fsync if no-appendfsync-on-rewrite is set to yes and there are
        * children doing I/O in the background. */
        if (server.aof_no_fsync_on_rewrite && hasActiveChildProcess())
            return;

        /* Perform the fsync if needed. */
        if (server.aof_fsync == AOF_FSYNC_ALWAYS) {
            /* redis_fsync is defined as fdatasync() for Linux in order to avoid
            * flushing metadata. */
            latencyStartMonitor(latency);
            /* Let's try to get this data on the disk. To guarantee data safe when
            * the AOF fsync policy is 'always', we should exit if failed to fsync
            * AOF (see comment next to the exit(1) after write error above). */
            if (redis_fsync(server.aof_fd) == -1) {
                serverLog(LL_WARNING,"Can't persist AOF for fsync error when the "
                "AOF fsync policy is 'always': %s. Exiting...", strerror(errno));
                exit(1);
            }
            latencyEndMonitor(latency);
            latencyAddSampleIfNeeded("aof-fsync-always",latency);
            server.aof_fsync_offset = server.aof_current_size;
            server.aof_last_fsync = server.unixtime;
        } else if ((server.aof_fsync == AOF_FSYNC_EVERYSEC &&
                    server.unixtime > server.aof_last_fsync)) {
            if (!sync_in_progress) {
                aof_background_fsync(server.aof_fd);
                server.aof_fsync_offset = server.aof_current_size;
            }
            server.aof_last_fsync = server.unixtime;
        }
    }
    ```

### 日志重写

因为 AOF 日志会完整的记录用户所有操作，文件大小一定会越来越大。且对于数据本身而言，会有过期、更新、删除等变更，日志里肯定会有冗余数据。

为此，Redis 提供了 AOF 的重写机制，在重写时，会读取当前内存中的所有数据，生成对应的写命令，并将其存入新的 AOF 文件中。全部记录完成后，用新的 AOF 文件替换现有的 AOF 文件，且两份文件会保障最终一致。

### 优缺点

- 优点

  - 数据丢失最小，根据文件同步的策略，最多也仅会丢失秒级别的数据
  - 对于日志以 append 的方式写入，写入新操作出现异常时，不会影响历史数据
  - 可以通过重写等方案减少日志文件大小，且安全性高
  - 日志文件记录了所有操作步骤，格式简单，可以自定义修改
    - 例如误执行 `FLUSHALL` 操作，可以在重建时仅从操作日志中移除该条操作

- 缺点

  - AOF 文件占用通常要大于 RDB 文件
  - 在恢复数据时，要顺序执行日志中的所有命令，性能也会更差一些
  - 在执行命令时，因为要同时将命令写入日志文件中，所以存在阻塞风险
    - 对于用户当次的写操作命令，Redis 会先返回结果，异步执行 AOF 操作，不会阻塞当前命令，但是可能会阻塞下一次的命令
  - 在重写 AOF 日志文件时，同样存在阻塞风险

## Ref

- <https://redis.io/docs/latest/operate/oss_and_stack/management/persistence/>
- <https://xiaolincoding.com/redis/storage/aof.html>
- <https://xiaolincoding.com/redis/storage/rdb.html>
