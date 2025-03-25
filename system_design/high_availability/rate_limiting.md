# Rate Limiting

限流的主要目标是通过限制请求速率或并发量，保护系统免受过载影响，确保核心服务稳定运行，主要场景如下：

- **负载过高**：如服务器性能一般，机器数量较少，请求耗时长
- **突发流量**：如秒杀活动、热点事件带来的瞬时高并发
- **恶意攻击**：如 DDoS 攻击、爬虫高频请求
- **下游限额**：如下游服务有明确的 QPS 调用要求

## 固定窗口计数器（Fixed Window）

将时间划分为多个时间窗口，统计单位时间（如1秒）内的请求数，超限则拒绝，到达下一个时间窗口时重置计数器。

**优点**

- 实现简单

**缺点**

- 限流不够平滑，请求有可能集中在窗口前一半，导致整体有一半的时间完全不可用
- 无法保障速率，窗口切换时可能双倍流量（如第0.9秒和第1.1秒各100次）

**单机实现**

```python
def allow_request():
    now = current_time()
    if now - window_start > window_sec:  # 窗口过期则重置
        counter = 0
        window_start = now
    if counter >= threshold:
        return False
    counter += 1
    return True
```

**分布式实现**

```python
# Redis key: rate_limit:{service}
def allow_request(key):
    current = redis.incr(key)
    if current == 1:
        redis.expire(key, window_sec)
    return current <= threshold
```

## 滑动窗口计数器（Sliding Window）

将时间分割为多个小窗口（如 1 分钟分为 60 个 1 秒窗口），动态统计最近 N 个窗口的总请求。

**优点**

- 解决固定窗口的临界问题，精度更高

**缺点**

- 内存占用和计算量随子窗口数增加而上升。  

**单机实现**

```python
# slot_size = 1 秒、slot_count = 60
# 表示时间轴按 1 秒分槽，共 60 个槽，覆盖 60 秒的窗口
def allow_request():
    now = current_time()
    slot_idx = (now // slot_size) % slot_count
    if slot_idx != current_slot:  # 新子窗口，重置旧数据
        slots[slot_idx] = 0
        current_slot = slot_idx
    if sum(slots) >= threshold:
        return False
    slots[slot_idx] += 1
    return True
```

**分布式实现**

```python
# Redis key: sliding_window:{service}
def allow_request(key):
    now = current_time()
    redis.ZREMRANGEBYSCORE(key, 0, now - window_sec)
    redis.ZADD(key, {now: now})
    redis.EXPIRE(key, window_sec)
    count = redis.ZCARD(key)
    return count <= threshold:
```

## 漏桶算法（Leaky Bucket）

请求以任意速率进入桶，以固定速率流出，桶满则拒绝。

**优点**

- 严格平滑流量，可以控制限流速率

**缺点**

- 无法应对突发流量，只能以固定速率处理
- 如果处理速度始终小于请求发送速度，在桶满后，大部分新请求会被丢弃，服务可用性下降

**单机实现**

```python
def allow_request():
    now = current_time()
    # 计算漏出水量：时间差 * 流出速率
    leaked = (now - last_leak_time) * leak_rate
    water = max(0, water - leaked)
    last_leak_time = now
    if water >= threshold:
        return False
    water += 1
    return True
```

**分布式实现**

```python
# Redis key: leaky_bucket:{service}
def allow_request(key):
    script = """
    local key = KEYS[1]
    local leak_rate = tonumber(ARGV[1])
    local threshold = tonumber(ARGV[2])
    local now = tonumber(ARGV[3])
    local current_level, last_time = redis.call('HMGET', key, 'current_level', 'last_time' )
    current_level = tonumber(current_level) or 0
    last_time = tonumber(last_time) or now
    local leaked = (now - last_time) * leak_rate
    current_level = math.max(current_level - leaked, 0)
    if current_level + 1 <= threshold then
        redis.call('HMSET', key, 'last_time', now, 'current_level', current_level + 1)
        redis.call('EXPIRE', key, math.ceil(threshold / leak_rate))
        return 1
    else
        return 0
    end
    """

    return redis.EVAL(script, 1, key, leak_rate, threshold, current_time())
```

## 令牌桶算法（Token Bucket）

系统以特定速率生成令牌并放入令牌桶，直至桶满，请求需获取令牌才能被处理。

**优点**

- 允许突发流量（最多取完桶内令牌），兼顾灵活性与保护性
- 可以根据实际负载，动态调整令牌生成速率

**缺点**

- 流出速度不受管控，需要额外考虑令牌产生速率与桶的容量
- 需要维护令牌状态，实现略复杂

**单机实现**

```python
def allow_request():
    now = current_time()
    # 计算新增令牌数：时间差 * 生成速率
    new_tokens = (now - last_refill_time) * refill_rate
    tokens = min(threshold, tokens + new_tokens)
    last_refill_time = now
    if tokens < 1:
        return False
    tokens -= 1
    return True
```

**分布式实现**

```python
# Redis key: token_bucket:{service}
def allow_request(key):
    script = """
    local key = KEYS[1]
    local refill_rate = tonumber(ARGV[1])
    local threshold = tonumber(ARGV[2])
    local now = tonumber(ARGV[3])
    local tokens, last_refill = redis.call('HMGET', key, 'tokens', 'last_refill')
    tokens = tonumber(tokens) or threshold
    last_refill = tonumber(last_refill) or now
    local time_passed = now - last_refill
    if time_passed > 0 then
        local new_tokens = time_passed * refill_rate
        tokens = math.min(tokens + new_tokens, threshold)
        last_refill = now
    end
    if tokens >= 1 then
        tokens = tokens - 1
        redis.call('HMSET', key, 'tokens', tokens, 'last_refill', last_refill)
        redis.call('EXPIRE', key, math.ceil(threshold / refill_rate) * 2)
        return 1
    else
        return 0
    end
    """

    return redis.EVAL(script, 1, key, refill_rate, threshold, current_time())
```

## Ref

- <https://javaguide.cn/high-availability/limit-request.html>
- <https://github.com/2637309949/go-interview/blob/master/docs%2FNetwork%2F%E9%99%90%E6%B5%81%E7%AD%96%E7%95%A5.md>
