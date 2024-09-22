# Expiration Algorithm

对于过期时间来说，其本身为可选项，所以没有必要在原本的键值对的基础上，额外设置字段用于存储过期时间，容易造成内存浪费，而是采用一个新的哈希表单独进行存储，其中 key 为键值对的键，而 value 为对应的过期时间。

在读取数据时，Redis 会先从过期时间对应的哈希表中获取 key 的过期时间，如果获取不到或是还未过期，再从数据哈希表中进行读取。

在删除策略上，有以下三种常见策略：

- 定时删除
  - 主动删除，设置 key 的过期时间时，同步创建一个定时任务来执行删除逻辑
  - 可以保障过期的 key 可以及时删除，但是会有较大的 cpu 开销
<br>

- 惰性删除
  - 被动删除，当访问特定 key 值时，如果过期，则将其删除
  - 性能开销小，但是容易造成内存浪费
<br>

- 定期删除
  - 主动删除，每隔一段时间，随即从哈希表中取出一定数量的 key 进行检查，删除其中过期的键值对、
  - 间隔时间和每次检查数量不好确认，时间短、数量多则 cpu 开销大，时间长、数量少则内存浪费严重

在 Redis 内部，采用了惰性删除结合定期删除的策略。

## 惰性删除

惰性删除通过 [expireIfNeeded()](https://github.com/redis/redis/blob/7.0.0/src/db.c#L1662) 函数进行处理，主体逻辑如下所示：

- 通过 [keyIsExpired()](https://github.com/redis/redis/blob/7.0.0/src/db.c#L1600) 函数判断 key 是否过期，如果过期则通过 [deleteExpiredKeyAndPropagate()](https://github.com/redis/redis/blob/7.0.0/src/db.c#L1547) 函数进行删除

    ```c
    int expireIfNeeded(redisDb *db, robj *key, int force_delete_expired) {
        if (!keyIsExpired(db,key)) return 0;
        ...
        /* Delete the key */
        deleteExpiredKeyAndPropagate(db,key);
        return 1;
    }
    ```

## 定期删除

定期删除在实际执行时，需要通过循环判断 key 是否过期，在执行时，分为了快速和慢速两种类型，快速模式每次会执行较短时间，但执行的频率较高，慢速模式每次执行的时间较长，但执行的频率较慢。

主体逻辑通过 [activeExpireCycle()](https://github.com/redis/redis/blob/7.0.0/src/expire.c#L113) 函数进行实现，如下所示：

- 初始化参数配置
  - `effort`：预设的采样比例，用于控制其他参数
  - `config_keys_per_loop`：每次循环判断的 key 值数量
  - `config_cycle_fast_duration`：快循环的最大的执行时间
  - `config_cycle_slow_time_perc`：慢循环最大的 cpu 占用率
  - `config_cycle_acceptable_stale`：可接受的过期比例，如果低于该比例，会结束循环

    ```c
    #define ACTIVE_EXPIRE_CYCLE_KEYS_PER_LOOP 20 /* Keys for each DB loop. */
    #define ACTIVE_EXPIRE_CYCLE_FAST_DURATION 1000 /* Microseconds. */
    #define ACTIVE_EXPIRE_CYCLE_SLOW_TIME_PERC 25 /* Max % of CPU to use. */
    #define ACTIVE_EXPIRE_CYCLE_ACCEPTABLE_STALE 10 /* % of stale keys after which
                                                    we do extra efforts. */
    void activeExpireCycle(int type) {
        unsigned long
        effort = server.active_expire_effort-1, /* Rescale from 0 to 9. */
        config_keys_per_loop = ACTIVE_EXPIRE_CYCLE_KEYS_PER_LOOP +
                            ACTIVE_EXPIRE_CYCLE_KEYS_PER_LOOP/4*effort,
        config_cycle_fast_duration = ACTIVE_EXPIRE_CYCLE_FAST_DURATION +
                                    ACTIVE_EXPIRE_CYCLE_FAST_DURATION/4*effort,
        config_cycle_slow_time_perc = ACTIVE_EXPIRE_CYCLE_SLOW_TIME_PERC +
                                    2*effort,
        config_cycle_acceptable_stale = ACTIVE_EXPIRE_CYCLE_ACCEPTABLE_STALE-
                                        effort;
        ...
    }
    ```

<br>

- 判断快速模式执行条件
  - 如果上次执行没有因为超时结束，且过期 key 值比例小于阈值，则不执行
  - 如果和上次开始执行快速模式的间隔小于两次执行时间阈值，则不执行

    ```c
    void activeExpireCycle(int type) {
        ...
        if (type == ACTIVE_EXPIRE_CYCLE_FAST) {
            /* Don't start a fast cycle if the previous cycle did not exit
            * for time limit, unless the percentage of estimated stale keys is
            * too high. Also never repeat a fast cycle for the same period
            * as the fast cycle total duration itself. */
            if (!timelimit_exit &&
                server.stat_expired_stale_perc < config_cycle_acceptable_stale)
                return;

            if (start < last_fast_cycle + (long long)config_cycle_fast_duration*2)
                return;

            last_fast_cycle = start;
        }
        ...
    }
    ```

<br>

- 计算超时时间
  - 对于慢速模式，通过 cpu 最大占用率和函数调用频率计算超时时间
  - 对于快速模式，直接读取配置的超时时间

    ```c
    void activeExpireCycle(int type) {
        ...
        /* We can use at max 'config_cycle_slow_time_perc' percentage of CPU
        * time per iteration. Since this function gets called with a frequency of
        * server.hz times per second, the following is the max amount of
        * microseconds we can spend in this function. */
        timelimit = config_cycle_slow_time_perc*1000000/server.hz/100;
        timelimit_exit = 0;
        if (timelimit <= 0) timelimit = 1;

        if (type == ACTIVE_EXPIRE_CYCLE_FAST)
            timelimit = config_cycle_fast_duration; /* in microseconds. */
        ...
    }
    ```

<br>

- 通过内外循环判断各数据库中 key 值过期情况
  - 每次内循环会判断一定数量的 key 值的过期情况
  - 如果内循环结束后，没有超时且已过期的 key 值的比例过高，则继续进行内循环

    <br>

  - 如果存储过期时间的哈希表为空，则结束外循环
  - 每 16 轮判断一次时间，如果超时，则结束外循环
  - 如果已过期的 key 值小于可结束的阈值，则结束外循环

    ```c
    void activeExpireCycle(int type) {
        ...
        for (j = 0; j < dbs_per_call && timelimit_exit == 0; j++) {
            ...
            do {
                ...
                /* If there is nothing to expire try next DB ASAP. */
                if ((num = dictSize(db->expires)) == 0) {
                    db->avg_ttl = 0;
                    break;
                }
                ...
                /* We can't block forever here even if there are many keys to
                * expire. So after a given amount of milliseconds return to the
                * caller waiting for the other active expire cycle. */
                if ((iteration & 0xf) == 0) { /* check once every 16 iterations. */
                    elapsed = ustime()-start;
                    if (elapsed > timelimit) {
                        timelimit_exit = 1;
                        server.stat_expired_time_cap_reached_count++;
                        break;
                    }
                }
                /* We don't repeat the cycle for the current database if there are
                * an acceptable amount of stale keys (logically expired but yet
                * not reclaimed). */
            } while (sampled == 0 ||
                    (expired*100/sampled) > config_cycle_acceptable_stale);
        }
        ...
    }
    ```

    <br>

  - 具体对于 key 值的处理，通过 [activeExpireCycleTryExpire()](https://github.com/redis/redis/blob/7.0.0/src/expire.c#L54) 函数进行实现
  - 如果循环判断的 key 值次数（`sampled`）超过设定的最大值（`num`），则结束内循环
  - 如果循环判断的哈希桶数（`checked_buckets`）超过设定的最大值（`max_buckets`），则结束内循环

    ```c
    void activeExpireCycle(int type) {
        ...
        for (j = 0; j < dbs_per_call && timelimit_exit == 0; j++) {
            ...
            do {
                ...
                if (num > config_keys_per_loop)
                    num = config_keys_per_loop;
                
                long max_buckets = num*20;
                long checked_buckets = 0;

                while (sampled < num && checked_buckets < max_buckets) {
                    for (int table = 0; table < 2; table++) {
                        ...
                        unsigned long idx = db->expires_cursor;
                        idx &= DICTHT_SIZE_MASK(db->expires->ht_size_exp[table]);
                        dictEntry *de = db->expires->ht_table[table][idx];
                        ...
                        checked_buckets++;
                        while(de) {
                            ...
                            if (activeExpireCycleTryExpire(db,e,now)) expired++;
                            ...
                            sampled++;
                        }
                    }
                    db->expires_cursor++;
                }
                ...
            } while (...);
        }
        ...
    }
    ```

    <br>

  - [activeExpireCycleTryExpire()](https://github.com/redis/redis/blob/7.0.0/src/expire.c#L54) 函数内部逻辑较为简单，如果 key 值过期，同样使用 [deleteExpiredKeyAndPropagate()](https://github.com/redis/redis/blob/7.0.0/src/db.c#L1547) 函数进行删除

      ```c
      int activeExpireCycleTryExpire(redisDb *db, dictEntry *de, long long now) {
          long long t = dictGetSignedIntegerVal(de);
          if (now > t) {
              sds key = dictGetKey(de);
              robj *keyobj = createStringObject(key,sdslen(key));
              deleteExpiredKeyAndPropagate(db,keyobj);
              decrRefCount(keyobj);
              return 1;
          } else {
              return 0;
          }
      }
      ```
