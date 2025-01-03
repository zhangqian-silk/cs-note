# Sync

## Mutex

> [wiki/互斥锁](https://zh.wikipedia.org/wiki/%E4%BA%92%E6%96%A5%E9%94%81)

互斥锁（英语：Mutual exclusion，缩写 Mutex）是一种用于多线程编程中，防止两条线程同时对同一公共资源（比如全局变量）进行读写的机制。

### 数据结构

Golang 中互斥锁的数据结构为 [sync.Mutex](https://github.com/golang/go/blob/go1.22.0/src/sync/mutex.go#L34)，其中 `state` 用来表示互斥锁当前的状态，`sema` 是用于控制锁的信号量。

```go
type Mutex struct {
    state int32
    sema  uint32
}
```

针对于 `state` 字段，其中低三位用于表示状态，其他位用于表示当前正在等待的 goroutine 的数量：

- `mutexLocked`：锁定标志位，当前互斥锁已经被锁定
- `mutexWoken`：唤醒标志位，当前已选中需要被唤醒的 goroutine
- `mutexStarving`：饥饿模式标志位，当前互斥锁进入饥饿状态
- `m.state >> mutexWaiterShift`：当前正在等待的 goroutine 的数量

```go
mutexLocked = 1 << iota // mutex is locked
mutexWoken
mutexStarving
mutexWaiterShift = iota
```

### 公平性

> [wiki/Starvation_(computer_science)](https://en.wikipedia.org/wiki/Starvation_(computer_science))

饥饿是指在并发环境下，某一个进程永远无法获取其运行所需资源的现象，在调度算法或互斥锁算法的场景下，都有可能出现。

在 Golang 中，互斥锁存在两种模式，正常模式(normal)和饥饿模式(starvation)，在普通模式下，互斥锁是一种非公平锁，goroutine 在申请锁时，会直接去竞争锁，如果获取到，就会直接占有锁，执行后续逻辑，获取不到才会进入等待队列，队列中会按照先进先出的顺序来获取锁。

从锁的分配的角度来说，如果等待队列非空，且同时有一个新的 goroutine 在获取锁，那么在竞争时，后者会极大概率胜出，因为他本身就持有 CPU 资源，可以减少唤醒 goroutine 的开销。

但是相对的，如果竞争锁的 goroutine 特别多，每次锁均分配给了新参与竞价的 goroutine，那么队列中末尾的 goroutine，很有可能永远获取不到锁，也就是出现饥饿现象。

在饥饿模式下，互斥锁是一种公平锁，锁的所有权会直接移交给队列中的首位 goroutine，确保队列中的所有 goroutine 均有机会分配资源来执行，所有想要获取锁的 goroutine 直接进入等待队列的尾部。

总的来说，正常模式，即非公平锁，会减少整体的性能开销，但是会出现饥饿现象，饥饿模式，即公平锁，能够确保队列中的所有 goroutine 均会获取锁，避免尾部的非预期内的延迟，亦即饥饿现象，但是会有较多的资源浪费在唤醒等待中的 goroutine。

在目前的设计上，如果 goroutine 的等待时间超过了饥饿模式的时间阈值，即 1 ms/1e6 ns，会将互斥锁切换至饥饿模式。相对应的，如果 goroutine 获取到了锁，等待时间小于 1ms，或是位于等待队列尾部，会将互斥锁切换至正常模式。

```go
starvationThresholdNs = 1e6
```

### 加锁

对互斥锁进行加锁，需要调用 [Lock()](https://github.com/golang/go/blob/go1.22.0/src/sync/mutex.go#L81) 方法进行处理，方法的主干比较简单，通过 CAS 操作进行加锁处理，如果操作成功，即 `m.state` 当前等于 0，则说明当前锁是空闲的，且加锁成功，直接结束即可。

```go
func (m *Mutex) Lock() {
    // Fast path: grab unlocked mutex.
    if atomic.CompareAndSwapInt32(&m.state, 0, mutexLocked) {
        return
    }
    // Slow path (outlined so that the fast path can be inlined)
    m.lockSlow()
}
```

如果加锁操作失败，则调用 [lockSlow()](https://github.com/golang/go/blob/go1.22.0/src/sync/mutex.go#L117) 方法，通过自旋或休眠等方式等待锁的释放。

- 初始化当前 goroutine 相关的标志位

  - `waitStartTime`：用于后续计算等待时间，并判断是否要切换锁的状态
  - `starving`：饥饿状态的标志位
  - `awoke`：唤醒状态的标志位
  - `iter`：自旋次数，用于决定是否能够进行子旋等待锁的释放
  - `old`：锁的历史的状态位

    ```go
    func (m *Mutex) lockSlow() {
        var waitStartTime int64
        starving := false
        awoke := false
        iter := 0
        old := m.state
    }
    ```

- 处理自旋逻辑

  - 当 goroutine 处于自旋状态时，会一直持有 CPU 资源，可以避免因为 goroutine 的调度而产生的性能损失，但是自旋本身也会浪费资源，所以只适合短时间的阻塞行为，需要严格控制进入自旋状态的条件
  - 当前锁必须处于正常模式，且处于加锁状态，否则自旋没有意义，前者仅会分配给等待队列中的 goroutine，后者可直接尝试去获取锁
  - 需要 [runtime_canSpin()](https://github.com/golang/go/blob/go1.22.0/src/runtime/proc.go#L7045) 返回 `true`，表示当前系统状态允许进入自旋
  - 进入自旋状态后，如果当前等待队列不为空，且不存在其他正在被唤醒的 goroutine，则将当前 goroutine 设置为唤醒状态，并同步修改锁的状态位，避免 [Unlock()](https://github.com/golang/go/blob/go1.22.0/src/sync/mutex.go#L212) 方法中再去唤醒其他 goroutine
  - 最后通过 [runtime_doSpin](https://github.com/golang/go/blob/go1.22.0/src/runtime/proc.go#L7062) 执行自旋逻辑，并更新相关状态位，进入下一次循环判断

    ```go
    func (m *Mutex) lockSlow() {
        ...
        for {
            // Don't spin in starvation mode, ownership is handed off to waiters
            // so we won't be able to acquire the mutex anyway.
            if old&(mutexLocked|mutexStarving) == mutexLocked && runtime_canSpin(iter) {
                // Active spinning makes sense.
                // Try to set mutexWoken flag to inform Unlock
                // to not wake other blocked goroutines.
                if !awoke && old&mutexWoken == 0 && old>>mutexWaiterShift != 0 &&
                    atomic.CompareAndSwapInt32(&m.state, old, old|mutexWoken) {
                    awoke = true
                }
                runtime_doSpin()
                iter++
                old = m.state
                continue
            }
            ...
        }
    }
    ```

  - 在 [runtime_canSpin()](https://github.com/golang/go/blob/go1.22.0/src/runtime/proc.go#L7045) 函数中，主要进行如下判断：

    - 当前自旋次数，需要小于 4，即不能长时间处于自旋状态
    - 当前环境需要是多核环境
    - 当前存在空闲的 process
    - 当前 goroutine 中绑定的 process 的待运行队列不为空

    ```go
    const (
        active_spin = 4
    )

    //go:linkname sync_runtime_canSpin sync.runtime_canSpin
    //go:nosplit
    func sync_runtime_canSpin(i int) bool {
        // sync.Mutex is cooperative, so we are conservative with spinning.
        // Spin only few times and only if running on a multicore machine and
        // GOMAXPROCS>1 and there is at least one other running P and local runq is empty.
        // As opposed to runtime mutex we don't do passive spinning here,
        // because there can be work on global runq or on other Ps.
        if i >= active_spin || ncpu <= 1 || gomaxprocs <= sched.npidle.Load()+sched.nmspinning.Load()+1 {
            return false
        }
        if p := getg().m.p.ptr(); !runqempty(p) {
            return false
        }
        return true
    }
    ```

  - 在 [runtime_doSpin](https://github.com/golang/go/blob/go1.22.0/src/runtime/proc.go#L7062) 函数中，CPU 会执行 30 次空操作指令，保持 CPU 占用

    ```go
    const (
        active_spin_cnt = 30
    )
    //go:linkname sync_runtime_doSpin sync.runtime_doSpin
    //go:nosplit
    func sync_runtime_doSpin() {
        procyield(active_spin_cnt)
    }
    ```

- 计算锁的状态

  - 创建一个新的标志位状态，用于存储期望状态，后续统一在 CAS 操作中进行更改
  - 如果当前锁不是饥饿模式(`mutexStarving`)，则修改 `mutexLocked` 标志位，进行加锁处理，避免分配给其他 goroutine
    - 通过与操作进行修改，如果原本该标志位已经为 1，说明锁被其他 goroutine 锁定，则此次操作不会产生额外影响
  - 如果当前锁处于锁定状态(`mutexLocked`)或饥饿模式(`mutexStarving`)，则当前 goroutine 需要加入等待队列中
    - 前者说明 goroutine 此时抢占失败
    - 后者为公平锁模式，需要严格按照等待队列分配锁资源
  - 如果当前 goroutine 处于饥饿状态，且锁处于锁定状态(`mutexLocked`)，则将锁标记为饥饿模式(`mutexStarving`)
    - 通过与操作进行修改，如果锁本身已经处于饥饿模式，则此次操作不会产生额外影响
  - 如果当前 goroutine 处于被唤醒的状态，则修改 `mutexWoken` 状态位为 0
    - 如果后续成功获取到锁，则无需再次修改锁的状态位
    - 如果后续仍然没有获取到锁，则该 goroutine 会处于挂起的状态，也需要将 `mutexWoken` 状态位复位为 0，保障在 [Unlock()](https://github.com/golang/go/blob/go1.22.0/src/sync/mutex.go#L212) 时能够正常唤醒
    - 获取不到锁的原因主要在于锁在正常模式下，是非公平锁，存在竞争

    ```go
    func (m *Mutex) lockSlow() {
        ...
        for {
            ...
            new := old
            // Don't try to acquire starving mutex, new arriving goroutines must queue.
            if old&mutexStarving == 0 {
                new |= mutexLocked
            }
            if old&(mutexLocked|mutexStarving) != 0 {
                new += 1 << mutexWaiterShift
            }
            // The current goroutine switches mutex to starvation mode.
            // But if the mutex is currently unlocked, don't do the switch.
            // Unlock expects that starving mutex has waiters, which will not
            // be true in this case.
            if starving && old&mutexLocked != 0 {
                new |= mutexStarving
            }
            if awoke {
                // The goroutine has been woken from sleep,
                // so we need to reset the flag in either case.
                if new&mutexWoken == 0 {
                    throw("sync: inconsistent mutex state")
                }
                new &^= mutexWoken
            }
            ...
        }
    }
    ```

- 更新锁的状态

  - 更新成功时，如果锁在更新前是未锁定的状态，且没有处于饥饿模式，则说明抢占成功且加锁成功，直接结束
  - 如果更新成功，但是抢占失败：
    - 更新队列 lifo 标志，如果 goroutine 之前已经处于等待时刻，说明此时已被唤醒过，则提高在队列中的优先级，采用后进先出策略
    - 更新等待时间，用于饥饿模式、队列 lifo 标记等逻辑的判断
    - 通过 [runtime_SemacquireMutex](https://github.com/golang/go/blob/go1.22.0/src/runtime/sema.go#L76) 函数等待信号量的释放
  - 如果更新失败，则说明在此期间锁的状态发生了变化，则更新下 `old` 字段为最新值，并重新进行循环

    ```go
    func (m *Mutex) lockSlow() {
        ...
        for {
            ...
            if atomic.CompareAndSwapInt32(&m.state, old, new) {
                if old&(mutexLocked|mutexStarving) == 0 {
                    break // locked the mutex with CAS
                }
                // If we were already waiting before, queue at the front of the queue.
                queueLifo := waitStartTime != 0
                if waitStartTime == 0 {
                    waitStartTime = runtime_nanotime()
                }
                runtime_SemacquireMutex(&m.sema, queueLifo, 1)
                ...
            } else {
                old = m.state
            }
        }
    }
    ```

- 唤醒逻辑

  - 在 goroutine 成功获取到信号量资源后，会继续执行唤醒后的逻辑
  - 通过唤醒后的等待时间，更新饥饿状态的标志位
  - 更新 `old` 字段为最新值
  - 如果当前锁为饥饿模式，则直接获取到锁
    - 饥饿模式下，锁为公平锁，不存在竞争的情况，被唤醒后一定可以获取到锁
    - 更新锁的状态，将 `mutexLocked` 标志位更新为 1，并将等待数据减 1
    - 如果当前 goroutine 不处于饥饿状态或当前 goroutine 为等待队列中最后一个元素，则将锁退出饥饿模式
    - 获取成功后，直接退出循环
  - 如果当前锁不是饥饿模式，则更新唤醒标志位、重置自旋计数器，并重新开始循环

    ```go
    func (m *Mutex) lockSlow() {
        ...
        for {
            ...
            if atomic.CompareAndSwapInt32(&m.state, old, new) {
                ...
                runtime_SemacquireMutex(&m.sema, queueLifo, 1)
                starving = starving || runtime_nanotime()-waitStartTime > starvationThresholdNs
                old = m.state
                if old&mutexStarving != 0 {
                    // If this goroutine was woken and mutex is in starvation mode,
                    // ownership was handed off to us but mutex is in somewhat
                    // inconsistent state: mutexLocked is not set and we are still
                    // accounted as waiter. Fix that.
                    if old&(mutexLocked|mutexWoken) != 0 || old>>mutexWaiterShift == 0 {
                        throw("sync: inconsistent mutex state")
                    }
                    delta := int32(mutexLocked - 1<<mutexWaiterShift)
                    if !starving || old>>mutexWaiterShift == 1 {
                        // Exit starvation mode.
                        // Critical to do it here and consider wait time.
                        // Starvation mode is so inefficient, that two goroutines
                        // can go lock-step infinitely once they switch mutex
                        // to starvation mode.
                        delta -= mutexStarving
                    }
                    atomic.AddInt32(&m.state, delta)
                    break
                }
                awoke = true
                iter = 0
            }
            ...
        }
    }
    ```

### 解锁

对互斥锁进行解锁，需要调用 [Unlock()](https://github.com/golang/go/blob/go1.22.0/src/sync/mutex.go#L212) 方法进行处理，方法内部先尝试通过 `atomic` 库修改 `mutexLocked` 标志位，如果操作后的结果为 0，则说明快速路径下解锁成功。

```go
func (m *Mutex) Unlock() {
    // Fast path: drop lock bit.
    new := atomic.AddInt32(&m.state, -mutexLocked)
    if new != 0 {
        // Outlined slow path to allow inlining the fast path.
        // To hide unlockSlow during tracing we skip one extra frame when tracing GoUnblock.
        m.unlockSlow(new)
    }
}
```

如果操作后的结果不为 0，说明当前还存在其他等待解锁的 goroutine，此时调用 [unlockSlow()](https://github.com/golang/go/blob/go1.22.0/src/sync/mutex.go#L227) 方法进行处理。

- 校验 `mutexLocked` 标志位，不允许重复解锁
  - 用户操作异常的场景，直接报错

    ```go
    func (m *Mutex) unlockSlow(new int32) {
        if (new+mutexLocked)&mutexLocked == 0 {
            fatal("sync: unlock of unlocked mutex")
        }
        ...
    }
    ```

- 正常模式下，通过循环去唤醒等待解锁的 goroutine
  - 在等待队列为空，锁已经被抢占、存在被唤醒者、处于饥饿模式这些任一情况，则直接结束，不需要主动唤醒其他 goroutine
  - 如果当前还存在等待中的 goroutine，则将等待队列数减一，标记为唤醒状态，然后通过 CAS 操作更新锁的状态位，更新成功时，通过 [runtime_Semrelease()](https://github.com/golang/go/blob/go1.22.0/src/runtime/sema.go#L71) 函数释放信号量，唤醒其他等待中的 goroutine

    ```go
    func (m *Mutex) unlockSlow(new int32) {
        ...
        if new&mutexStarving == 0 {
            old := new
            for {
                // If there are no waiters or a goroutine has already
                // been woken or grabbed the lock, no need to wake anyone.
                // In starvation mode ownership is directly handed off from unlocking
                // goroutine to the next waiter. We are not part of this chain,
                // since we did not observe mutexStarving when we unlocked the mutex above.
                // So get off the way.
                if old>>mutexWaiterShift == 0 || old&(mutexLocked|mutexWoken|mutexStarving) != 0 {
                    return
                }
                // Grab the right to wake someone.
                new = (old - 1<<mutexWaiterShift) | mutexWoken
                if atomic.CompareAndSwapInt32(&m.state, old, new) {
                    runtime_Semrelease(&m.sema, false, 1)
                    return
                }
                old = m.state
            }
        } else {
            ...
        }
    }
    ```

- 饥饿模式下，直接通过 [runtime_Semrelease()](https://github.com/golang/go/blob/go1.22.0/src/runtime/sema.go#L71) 函数释放信号量，唤醒其他等待中的 goroutine，并强制唤醒队首元素
  - 饥饿模式下，锁为公平锁状态，不需要处理竞争的问题

    ```go
    func (m *Mutex) unlockSlow(new int32) {
        ...
        if new&mutexStarving == 0 {
            ...
        } else {
            // Starving mode: handoff mutex ownership to the next waiter, and yield
            // our time slice so that the next waiter can start to run immediately.
            // Note: mutexLocked is not set, the waiter will set it after wakeup.
            // But mutex is still considered locked if mutexStarving is set,
            // so new coming goroutines won't acquire it.
            runtime_Semrelease(&m.sema, true, 1)
        }
    }
    ```

## RWMutex

> [wiki/读写锁](https://zh.wikipedia.org/wiki/%E8%AF%BB%E5%86%99%E9%94%81)

读写锁是并发控制的其中一种机制，读操作是可重入的，写操作是互斥的，即多个读操作可以并发执行，多个写操作或者读写操作之间，无法并发执行。

### 数据结构

读写锁的数据结构为 [RWMutex](https://github.com/golang/go/blob/go1.22.0/src/sync/rwmutex.go#L35):

- `w`：底层互斥锁，用于实现写操作的互斥逻辑
- `writeSem` & `readerSem`：读、写操作的信号量，用于读、写之间的互斥、等待
- `readerCount`：读操作数量，被写操作阻塞时为负数
- `readerWait`：写操作阻塞时，正在执行的读操作的数量

```go
type RWMutex struct {
    w           Mutex        // held if there are pending writers
    writerSem   uint32       // semaphore for writers to wait for completing readers
    readerSem   uint32       // semaphore for readers to wait for completing writers
    readerCount atomic.Int32 // number of pending readers
    readerWait  atomic.Int32 // number of departing readers
}
```

### 写锁

在获取写锁时，需要调用 [Lock()](https://github.com/golang/go/blob/go1.22.0/src/sync/rwmutex.go#L140) 方法进行处理，先阻塞后续的读写操作，然后等待当前的所有读操作执行完成后，完成写锁的获取。

- 调用 `rw.w.Lock()` 方法，获取底层互斥锁，用于解决多个写操作之间的竞争问题
- 修改 `readerCount` 的值，将其变为负值，阻塞后续的读操作
- 获取当前正在活跃的读操作的数量，即变量 `r`
- 如果有活跃的读操作，则进入休眠状态，等待读操作执行完成，即等待 `rw.writerSem` 信号量的释放

    ```go
    func (rw *RWMutex) Lock() {
        // First, resolve competition with other writers.
        rw.w.Lock()
        // Announce to readers there is a pending writer.
        r := rw.readerCount.Add(-rwmutexMaxReaders) + rwmutexMaxReaders
        // Wait for active readers.
        if r != 0 && rw.readerWait.Add(r) != 0 {
            runtime_SemacquireRWMutex(&rw.writerSem, false, 0)
        }
    }
    ```

<br>

在释放时，需要调用 [Unlock()](https://github.com/golang/go/blob/go1.22.0/src/sync/rwmutex.go#L197) 方法进行处理，优先释放读锁，再释放写锁，避免连续的写操作导致读操作无法执行。

- 修改 `readerCount` 的值，将其变为正值，允许读操作进行
- 获取被阻塞的的读操作的数量，即变量 `r`
- 通过循环，释放 `rw.readerSem` 信号量，唤醒所有被阻塞的读操作的 goroutine
- 调用 `rw.w.Unlock()` 释放底层互斥锁

    ```go
    func (rw *RWMutex) Unlock() {
        // Announce to readers there is no active writer.
        r := rw.readerCount.Add(rwmutexMaxReaders)
        if r >= rwmutexMaxReaders {
            fatal("sync: Unlock of unlocked RWMutex")
        }
        // Unblock blocked readers, if any.
        for i := 0; i < int(r); i++ {
            runtime_Semrelease(&rw.readerSem, false, 0)
        }
        // Allow other writers to proceed.
        rw.w.Unlock()
    }
    ```

### 读锁

在获取读锁时，需要调用 [RLock()](https://github.com/golang/go/blob/go1.22.0/src/sync/rwmutex.go#L63) 方法进行处理:

- 将 `readerCount` 的值加一
- 如果结果为负数，说明被写操作阻塞，进入休眠状态，等待信号量 `rw.readerSem` 的释放。

    ```go
    func (rw *RWMutex) RLock() {
        if rw.readerCount.Add(1) < 0 {
            // A writer is pending, wait for it.
            runtime_SemacquireRWMutexR(&rw.readerSem, false, 0)
        }
    }
    ```

在释放锁时，需要调用 [RUnlock()](https://github.com/golang/go/blob/go1.22.0/src/sync/rwmutex.go#L110) 方法和 [rUnlockSlow()](https://github.com/golang/go/blob/go1.22.0/src/sync/rwmutex.go#L125) 方法进行进行处理：

- 将 `readerCount` 的值减一
- 如果结果为非负数，直接解锁成功
- 如果结果为负数，说明有正在执行的写操作，则调用 [rUnlockSlow()](https://github.com/golang/go/blob/go1.22.0/src/sync/rwmutex.go#L125) 方法，减少 `readerWait` 的数量，如果最终结果为一，说明所有阻塞写操作的读操作均已结束，则释放 `rw.writerSem` 信号量，唤醒写操作的 goroutine

```go
func (rw *RWMutex) RUnlock() {
    if r := rw.readerCount.Add(-1); r < 0 {
        // Outlined slow-path to allow the fast-path to be inlined
        rw.rUnlockSlow(r)
    }
}

func (rw *RWMutex) rUnlockSlow(r int32) {
    if r+1 == 0 || r+1 == -rwmutexMaxReaders {
        fatal("sync: RUnlock of unlocked RWMutex")
    }
    // A writer is pending.
    if rw.readerWait.Add(-1) == 0 {
        // The last reader unblocks the writer.
        runtime_Semrelease(&rw.writerSem, false, 1)
    }
}
```

## WaitGroup

> [wiki/同步屏障](https://zh.wikipedia.org/wiki/%E5%90%8C%E6%AD%A5%E5%B1%8F%E9%9A%9C)

同步屏障是并行计算中的一种同步方式，在 golang 中，
[WaitGroup][WaitGroupLink] 可以用于等待一组 goroutine 执行结束。

### 数据结构

[WaitGroup][WaitGroupLink] 的数据结构如下所示：

- `noCopy`：该属性可以保障 [WaitGroup][WaitGroupLink] 的实例不会触发拷贝操作
  - 对于 [WaitGroup][WaitGroupLink] 而言，对于其的操作需要必须是要配套进行的，否则会有死锁等严重风险，锁等结构体类似

- `state`：数据存储，高 32 位存储计数，后 32 位存储当前等待的协程的数量

- `sema`：信号量，用于并发控制

```go
type WaitGroup struct {
    noCopy noCopy

    state atomic.Uint64 // high 32 bits are counter, low 32 bits are waiter count.
    sema  uint32
}
```

### 核心方法

[WaitGroup][WaitGroupLink] 对外提供了 3 个方法用于实现同步屏障：

- 对于需要在同步屏障前执行的任务，在执行前调用 [Add()][AddLink] 方法将其计数加一，在执行完成后，调用 [Done()][DoneLink] 方法，将其计数减一

- 对于需要在同步屏障后执行的任务，在执行前调用 [Wait()][WaitLink] 方法，等待计数归零

需要注意的是，在真正调用 [Wait()][WaitLink] 方法前，一般要确保所有需要在同步屏障前执行的任务，均已调用 [Add()][AddLink] 方法，不然会有时机同步问题。

- [Add()][AddLink]：
  - 在方法调用时，会传入需要添加的任务总量，并存储在 `state` 的高 32 位中
  - 方法内部会做一些合法性校验，限制任务总量不能为负数和限制，限制特殊情况下，[Add()][AddLink] 方法与 [Wait()][WaitLink] 方法的并发调用

    ```go
    func (wg *WaitGroup) Add(delta int) {
        state := wg.state.Add(uint64(delta) << 32)
        v := int32(state >> 32)
        w := uint32(state)

        if v < 0 {
            panic("sync: negative WaitGroup counter")
        }
        if w != 0 && delta > 0 && v == int32(delta) {
            panic("sync: WaitGroup misuse: Add called concurrently with Wait")
        }
        if v > 0 || w == 0 {
            return
        }
        ...
    }
    ```

- [Done()][DoneLink]：
  - 方法底层通过 [Add()][AddLink] 方法实现，并传入值为 -1
  - 当 `v == 0 && w > 0` 时，会按照当前等待者的数量，依次调用 `runtime_Semrelease()` 函数，唤醒所有等待者

    ```go
    func (wg *WaitGroup) Done() {
        wg.Add(-1)
    }

    func (wg *WaitGroup) Add(delta int) {
        ...
        if v > 0 || w == 0 {
            return
        }
        
        if wg.state.Load() != state {
            panic("sync: WaitGroup misuse: Add called concurrently with Wait")
        }
        // Reset waiters count to 0.
        wg.state.Store(0)
        for ; w != 0; w-- {
            runtime_Semrelease(&wg.sema, false, 0)
        }
    }
    ```

- [Wait()][WaitLink]：
  - 在方法调用时，会给 `state` 的低 32 位计数加一，
  - 并同步调用 `runtime_Semacquire()` 函数，休眠当前协程，等待唤醒

    ```go
    func (wg *WaitGroup) Wait() {
        for {
            state := wg.state.Load()
            v := int32(state >> 32)
            w := uint32(state)
            if v == 0 {
                // Counter is 0, no need to wait.
                return
            }
            // Increment waiters count.
            if wg.state.CompareAndSwap(state, state+1) {
                runtime_Semacquire(&wg.sema)
                if wg.state.Load() != 0 {
                    panic("sync: WaitGroup is reused before previous Wait has returned")
                }
                return
            }
        }
    }
    ```

## Once

[Once][OnceLink] 可以用来确保某段代码只会执行一次，其内部结构较为简单，通过一个状态位 `done` 存储执行状态，以及通过锁 `m` 避免并发问题。

```go
type Once struct {
    done atomic.Uint32
    m    Mutex
}
```

在使用上，每个需要限制的场景，都需要创建对应的 [Once][OnceLink] 结构体的实例，然后通过 [Do()](https://github.com/golang/go/blob/go1.22.0/src/sync/once.go#L48) 方法，来执行对应的函数：

```go
func (o *Once) Do(f func()) {
    if o.done.Load() == 0 {
        o.doSlow(f)
    }
}

func (o *Once) doSlow(f func()) {
    o.m.Lock()
    defer o.m.Unlock()
    if o.done.Load() == 0 {
        defer o.done.Store(1)
        f()
    }
}
```

## 引用

- <https://draveness.me/golang/docs/part3-runtime/ch06-concurrency/golang-sync-primitives/>
- <https://juejin.cn/post/7091903444193116168>

[WaitGroupLink]: https://github.com/golang/go/blob/go1.22.0/src/sync/waitgroup.go
[AddLink]: https://github.com/golang/go/blob/go1.22.0/src/sync/waitgroup.go#L43
[DoneLink]: https://github.com/golang/go/blob/go1.22.0/src/sync/waitgroup.go#L86
[WaitLink]: https://github.com/golang/go/blob/go1.22.0/src/sync/waitgroup.go#L91
[OnceLink]: https://github.com/golang/go/blob/go1.22.0/src/sync/once.go#L18
