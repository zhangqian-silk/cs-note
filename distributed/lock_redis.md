# Lock (Redis)

## 设计要点

**原子性操作**

- **获取锁**: 使用 `SETNX` 命令尝试获取锁
- **释放锁**: 使用 Lua 脚本实现
  - 先 `GET` key 的值，判断是否与尝试解锁者持有的 `value` 相同
  - 相同时，执行 `DEL` 操作来释放锁
- **续期锁**: 使用 Lua 脚本实现，
  - 先 `GET` key 的值，判断是否与尝试续期者持有的 `value` 相同
  - 相同时，执行 `EXPIRE` 操作来延长锁的过期时间

**锁的自动续期 (Watchdog / 看门狗机制)**

- 当成功获取锁后，会启动一个 goroutine 定期为锁续期，并初始化一个全新的 `chan` 用来监听锁的释放
  - 旧有的 `chan` 可能已经调用过 `close()`，不能复用
- 续期操作会校验 `value`，确保是自己持有的锁才续期
- 续期通常会在过期时间的某个比例点（如 0.5 处）尝试进行
- 续期结束的场景：
  - 锁释放：通过 `chan` 或其他方式，提醒续期 goroutine 结束运行
  - 续期失败：网络问题、Redis 实例问题等，重试多次后仍然失败，或是锁已被其他客户端获取等原因，续期 goroutine 会停止运行
- **核心作用**: 防止业务逻辑执行时间过长，导致锁在业务完成前就过期，从而被其他客户端抢占，引发并发问题

**可重入性**

- 通过重入计数器 `reentrantCnt` 和锁来保障，加锁成功时，计数加一，释放时，计数减一，当计数归零时，真正触发释放锁的逻辑
- **本地可重入**: 直接判断当前 `reentrantCnt` 的值，如果大于一，说明当前锁已被持有，则直接认为可重入，计数加一
- **分布式可重入 (基于 `uniqueID`)**: 在 `Lock()` 方法中，如果加锁失败，额外判断锁的 `value`，如果与当前锁实例一致，同样认为可重入
  - 场景一：本地并发获取锁失败，进入重试阶段，此时原本的锁释放，本地某一协程加锁成功，其他协程通过 `value` 判断实现可重入
  - 场景二：分布式场景下，通过 TrackID、ReqID、BusinessID 等来标识锁

**锁的唯一标识 (`value`)**

- 每个锁实例在尝试获取锁时，会额外设置锁的 `value` 并存储在 Redis 中
- `value` 默认为 UUID，也支持调用方注入，如 TraceID、RequestID、LogID、BusinessID 等
- **作用**
  - **防止误操作**：对于如下两种情况，需要避免对 A1 的续期或释放操作，影响 A2
    - A1 Lock -> A1 Unlock -> A2 Lock -> A1 Renew
    - A1 Lock -> A1 Expired -> A2 Lock -> A1 Unlock
  - **支持可重入**: 用作可重入的判断依据

## Impl

```go
package kv

import (
    "sync"
    "sync/atomic"
    "time"

    "code.byted.org/ad/sdk_phecda/app/util"
    "code.byted.org/kv/redis-v6"
    "github.com/google/uuid"
)

const (
    unlockLua = `
if redis.call("get", KEYS[1]) == ARGV[1] then
    return redis.call("del", KEYS[1])
else
    return 0
end
`
    renewLua = `
if redis.call("get", KEYS[1]) == ARGV[1] then
    return redis.call("expire", KEYS[1], ARGV[2])
else
    return 0
end
`
)

var (
    cli *goredis.Client
)

type LockHelper struct {
    mu sync.mutex

    key   string
    value string

    reentrantCnt int
    renewStop    *SafeChan[struct{}]
}

// LockOption 锁配置，当 MaxRetry 和 MaxElapsedTime 均为 0 时，不进行重试
type LockOption struct {
    RetryInterval  time.Duration // 默认 100ms
    MaxElapsedTime time.Duration // 最大重试时间
    MaxRetry       int           // 最大重试次数
    LockExpire     time.Duration // 锁超时时间
}

var (
    unlockScript = redis.NewScript(unlockLua)
    renewScript  = redis.NewScript(renewLua)
)

func DefaultLockOption() LockOption {
    return LockOption{
        RetryInterval:  100 * time.Millisecond,
        MaxElapsedTime: 0,
        MaxRetry:       0,
        LockExpire:     10 * time.Second,
    }
}

func GenLockHelper(key string, uniqueID string) (*LockHelper, error) {
    if uniqueID == "" {
        // 通过注入 id 实现分布式场景下的可重入逻辑，如果不指定则随机生成
        uuidInstance, err := uuid.NewUUID()
        if err != nil {
            return nil, fmt.Errorf("generate uuid failed: %w", err)
        }
        uniqueID = uuidInstance.String()
    }

    return &LockHelper{
        key:       key,
        value:     uniqueID,
        renewStop: NewSafeChan[struct{}](),
    }, nil
}

func (l *LockHelper) Lock(opt LockOption) error {
    l.mu.Lock()
    if l.reentrantCnt > 0 {
        // 本地可重入
        l.reentrantCnt++
        l.mu.Unlock()
        return nil
    }
    l.mu.Unlock()

    expire := opt.LockExpire
    if expire <= 0 {
        expire = 100 * time.Millisecond
    }

    retryInterval := opt.RetryInterval
    if retryInterval <= 10*time.Millisecond {
        retryInterval = 10 * time.Millisecond
    }

    renewF := func() {
        l.Mu.Lock()
        defer l.Mu.Unlock()

        l.reentrantCnt++
        // 处理 chan 复用问题
        l.renewStop.Close()
        l.renewStop = util.NewSafeChan[struct{}]()
        go l.renew(expire, 0.4)
    }

    start := time.Now()
    retries := 0
    for {
        ok, err := cli.SetNX(l.key, l.value, expire).Result()
        if err != nil {
            return fmt.Errorf("redis setnx failed: %w", err)
        }
        if ok {
            renewF()
            return nil
        }

        // 分布式场景下可重入
        oldValue, err := cli.Get(l.key).Result()
        if err == nil && oldValue == l.value {
            renewF()
            return nil
        }

        // 重试控制
        if (opt.MaxRetry == 0 && opt.MaxElapsedTime == 0) ||
            (opt.MaxRetry > 0 && retries >= opt.MaxRetry) ||
            (opt.MaxElapsedTime > 0 && time.Since(start) > opt.MaxElapsedTime) {
            break
        }
        retries++
        time.Sleep(opt.RetryInterval)
    }
    return fmt.Errorf("lock timeout")
}

func (l *LockHelper) Unlock() error {
    l.mu.Lock()
    defer l.mu.Unlock()

    if l.reentrantCnt == 0 {
        return fmt.Errorf("unlock invalid, reentrantCnt is 0")
    }
    l.reentrantCnt--
    if l.reentrantCnt > 0 {
        return nil
    }

    res, err := unlockScript.Run(cli, []string{l.key}, l.value).Result()
    l.renewStop.Close()
    if err != nil {
        return fmt.Errorf("run unlock script failed: %w", err)
    }
    if val, ok := res.(int64); !ok || val == 0 {
        return fmt.Errorf("unlock failed")
    }

    return nil
}

func (l *LockHelper) renew(expire time.Duration, tryRatio float64) {
    if expire <= 0 {
        return
    }

    if tryRatio <= 0 || tryRatio > 1 {
        tryRatio = 0.4
    }
    t := time.NewTicker(time.Duration(float64(expire) * tryRatio))
    defer t.Stop()

    retries := 0
    for {
        select {
        case <-t.C:
            l.mu.Lock()
            if l.reentrantCnt == 0 {
                l.mu.Unlock()
                return
            }
            l.mu.Unlock()

            res, err := renewScript.Run(cli, []string{l.key}, l.value, expire).Result()
            if err != nil {
                retries++
                if retries >= 3 {
                    return
                }
                continue
            }

            retries = 0
            if val, ok := res.(int64); !ok || val == 0 {
                return
            }

        case <-l.renewStop.Output():
            return
        }
    }
}

type SafeChan[T any] struct {
    ch          chan T
    closedState int32
}

func NewSafeChan[T any]() *SafeChan[T] {
    return &SafeChan[T]{ch: make(chan T)}
}

func (sc *SafeChan[T]) Input(value T) (sent bool) {
    if sc.IsClosed() {
        return false
    }

    defer func() {
        if r := recover(); r != nil {
            sent = false
        }
    }()

    sc.ch <- value
    return true
}

func (sc *SafeChan[T]) Output() <-chan T {
    return sc.ch
}

func (sc *SafeChan[T]) Close() {
    if sc == nil || sc.ch == nil {
        return
    }
    if atomic.CompareAndSwapInt32(&sc.closedState, 0, 1) {
        close(sc.ch)
    }
}

func (sc *SafeChan[T]) IsClosed() bool {
    return atomic.LoadInt32(&sc.closedState) == 1
}
```

## Ref

- <https://javaguide.cn/distributed-system/distributed-lock-implementations.html>
- <https://redis.io/docs/latest/develop/use/patterns/distributed-locks/>
