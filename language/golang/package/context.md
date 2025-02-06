# Context

Golang 中对于协程的创建特别简单，只需要通过 `go` 关键字即可实现，但是创建简单一定会提高维护的复杂度。

一是协程间数据共享比较复杂，针对于一读多写、一写多读、多读多写等场景，要么在接口中一路传递下去，要么分门别类，设计多个 channel 共同合作来进行控制。例如 traceID 等链路追踪相关字段，需要全链路共享，所以所有协程间都需要有办法进行读取。

二是协程间的生命周期管理，父协程在创建多个子协程时，一般会在后续链路中同步等待所有子协程的执行结果，或是在结束前手动释放所有子协程，不然会造成协程泄漏问题，所以也需要有较为简洁的方案来管理子协程。

针对于次，golang 单独提供了 [`context`](https://github.com/golang/go/blob/go1.22.0/src/context/context.go) 包，并定义了 [`Context`][Context_link] 接口，主要服务于协程间的消息通信：

- 传递取消信号（主动取消、被动超时）
- 传递数据（KV 的存储结构）

在 [`context`](https://github.com/golang/go/blob/go1.22.0/src/context/context.go) 包内部，还定义了一些服务于不同目的的 [`Context`][Context_link] 类，并对外提供了初始化方法，可以根据实际场景进行选择。

## Interface

[`Context`][Context_link] 接口如下所示：

- `Deadline()`：返回是否会自动取消，以及自动取消的时间
- `Done()`：返回一个只读的 `channel`，可以通过该 `channel` 进行监听是否结束
- `Err()`：返回被取消的原因，主动取消或是超时取消
- `Value()`：以键值对的方式读取数据

```go
type Context interface {
    Deadline() (deadline time.Time, ok bool)
    Done() <-chan struct{}
    Err() error
    Value(key any) any
}
```

## emptyCtx

[`emptyCtx`](https://github.com/golang/go/blob/go1.22.0/src/context/context.go#L177) 是默认的 [`Context`][Context_link] 类，不具备任何额外能力，常用于初始化的场景，后续可用于创建其他 [`Context`][Context_link] 类，也是唯一不需要父 `context` 即可进行创建的 [`Context`][Context_link] 类。

```go
// An emptyCtx is never canceled, has no values, and has no deadline.
// It is the common base of backgroundCtx and todoCtx.
type emptyCtx struct{}

func (emptyCtx) Deadline() (deadline time.Time, ok bool) {
    return
}

func (emptyCtx) Done() <-chan struct{} {
    return nil
}

func (emptyCtx) Err() error {
    return nil
}

func (emptyCtx) Value(key any) any {
    return nil
}
```

在日常使用时，可直接通过 [`Background()`](https://github.com/golang/go/blob/go1.22.0/src/context/context.go#L211) 函数创建：

```go
func Background() Context {
    return backgroundCtx{}
}

type backgroundCtx struct{ emptyCtx }
```

## cancelCtx

[`cancelCtx`](https://github.com/golang/go/blob/go1.22.0/src/context/context.go#L421) 是一个可以主动取消的 `context`，调用其取消函数可以同时取消当前的 `context` 以及所有的子 `context`。结构体内部包含了一些辅助变量，以及创建时的父 `context`。

```go
type cancelCtx struct {
    Context

    mu       sync.Mutex
    done     atomic.Value
    children map[canceler]struct{}
    err      error
    cause    error
}
```

通过 [`WithCancel()`](https://github.com/golang/go/blob/go1.22.0/src/context/context.go#L235) 函数可以创建一个 [`cancelCtx`](https://github.com/golang/go/blob/go1.22.0/src/context/context.go#L421) 类，同时得到其对应的 [`CancelFunc`](https://github.com/golang/go/blob/go1.22.0/src/context/context.go#L227)，用于主动取消，函数内部会调用 [`cancel()`](https://github.com/golang/go/blob/go1.22.0/src/context/context.go#L536) 方法执行真正的取消逻辑，并将 `context` 的结束原因设置为 [`Canceled`](https://github.com/golang/go/blob/go1.22.0/src/context/context.go#L163)。

```go
type CancelFunc func()

var Canceled = errors.New("context canceled")

func WithCancel(parent Context) (ctx Context, cancel CancelFunc) {
    c := withCancel(parent)
    return c, func() { c.cancel(true, Canceled, nil) }
}
```

同时，还可以通过 [`WithCancelCause()`](https://github.com/golang/go/blob/go1.22.0/src/context/context.go#L263) 函数进行创建，该函数会返回 [`CancelCauseFunc()`](https://github.com/golang/go/blob/go1.22.0/src/context/context.go#L250) 类型的取消函数，可以手动指定取消原因。

```go
type CancelCauseFunc func(cause error)

func WithCancelCause(parent Context) (ctx Context, cancel CancelCauseFunc) {
    c := withCancel(parent)
    return c, func(cause error) { c.cancel(true, Canceled, cause) }
}
```

创建时，会通过 [`propagateCancel()`](https://github.com/golang/go/blob/go1.22.0/src/context/context.go#L462) 方法构造 `context` 间的父子关系，当父 `context` 取消时，所有的子 `context` 也都会被取消

```go
func withCancel(parent Context) *cancelCtx {
    if parent == nil {
        panic("cannot create context from nil parent")
    }
    c := &cancelCtx{}
    c.propagateCancel(parent, c)
    return c
}
```

在 [`cancel()`](https://github.com/golang/go/blob/go1.22.0/src/context/context.go#L536) 方法中，首次执行时会赋值 `err` 和 `cause` 参数信息，并将 `done` 设置为一个已经关闭的 `channel`，用于实现 [`Context`][Context_link] 接口相关信息，然后遍历调用所有子 `context` 的 [`cancel()`](https://github.com/golang/go/blob/go1.22.0/src/context/context.go#L536) 方法。方法内部通过锁解决并发问题，并做了幂等处理。

```go
// closedchan is a reusable closed channel.
var closedchan = make(chan struct{})

func init() {
    close(closedchan)
}

func (c *cancelCtx) cancel(removeFromParent bool, err, cause error) {
    if err == nil {
        panic("context: internal error: missing cancel error")
    }
    if cause == nil {
        cause = err
    }
    c.mu.Lock()
    if c.err != nil {
        c.mu.Unlock()
        return // already canceled
    }
    c.err = err
    c.cause = cause
    d, _ := c.done.Load().(chan struct{})
    if d == nil {
        c.done.Store(closedchan)
    } else {
        close(d)
    }
    for child := range c.children {
        // NOTE: acquiring the child's lock while holding parent's lock.
        child.cancel(false, err, cause)
    }
    c.children = nil
    c.mu.Unlock()

    if removeFromParent {
        removeChild(c.Context, c)
    }
}
```

在 [`propagateCancel()`](https://github.com/golang/go/blob/go1.22.0/src/context/context.go#L462) 方法中，会统一处理 `context` 间的父子关系。

- 赋值父 `context`，并判断其是否可以取消，若不可以，直接结束

    ```go
    func (c *cancelCtx) propagateCancel(parent Context, child canceler) {
        c.Context = parent

        done := parent.Done()
        if done == nil {
            return // parent is never canceled
        }
        ...
    }
    ```

- 判断父 `context` 是否已经取消，若已经取消，同步取消子 `context`

    ```go
    func (c *cancelCtx) propagateCancel(parent Context, child canceler) {
        ...
        select {
        case <-done:
            // parent is already canceled
            child.cancel(false, parent.Err(), Cause(parent))
            return
        default:
        }
        ...
    }
    ```

- 当父 `context` 是 [`cancelCtx`](https://github.com/golang/go/blob/go1.22.0/src/context/context.go#L421) 类型时，如果已经被取消，则直接取消子 `context`，如果还未被取消，则将子 `context` 添加至其 `children` 集合中，建立父子关系

```go
func (c *cancelCtx) propagateCancel(parent Context, child canceler) {
    ...
    if p, ok := parentCancelCtx(parent); ok {
        // parent is a *cancelCtx, or derives from one.
        p.mu.Lock()
        if p.err != nil {
            // parent has already been canceled
            child.cancel(false, p.err, p.cause)
        } else {
            if p.children == nil {
                p.children = make(map[canceler]struct{})
            }
            p.children[child] = struct{}{}
        }
        p.mu.Unlock()
        return
    }
    ...
}
```

- 当父 `context` 实现了 `afterFuncer` 接口时，通过 [`stopCtx`](https://github.com/golang/go/blob/go1.22.0/src/context/context.go#L355) 类型来间接管理继承关系

```go
func (c *cancelCtx) propagateCancel(parent Context, child canceler) {
    ...
    if a, ok := parent.(afterFuncer); ok {
        // parent implements an AfterFunc method.
        c.mu.Lock()
        stop := a.AfterFunc(func() {
            child.cancel(false, parent.Err(), Cause(parent))
        })
        c.Context = stopCtx{
            Context: parent,
            stop:    stop,
        }
        c.mu.Unlock()
        return
    }
    ...
}
```

- 在方法最后，运行一个单独的 goroutine，用于监听 `parent.Done()` 和 `child.Done()` 两个 channel，并在父 `context` 先关闭时，主动取消子 `context`

```go
func (c *cancelCtx) propagateCancel(parent Context, child canceler) {
    ...
    goroutines.Add(1)
    go func() {
        select {
        case <-parent.Done():
            child.cancel(false, parent.Err(), Cause(parent))
        case <-child.Done():
        }
    }()
}
```

## timerCtx

[`timerCtx`](https://github.com/golang/go/blob/go1.22.0/src/context/context.go#L648) 继承了 [`cancelCtx`](https://github.com/golang/go/blob/go1.22.0/src/context/context.go#L421)，支持使用者手动取消，同时额外维护了定时器 `timer` 和截止时间 `deadline`，支持定时取消的能力。

```go
type timerCtx struct {
    cancelCtx
    timer *time.Timer // Under cancelCtx.mu.

    deadline time.Time
}
```

通过 [`WithDeadline()`](https://github.com/golang/go/blob/go1.22.0/src/context/context.go#L611) 和 [`WithDeadlineCause()`](https://github.com/golang/go/blob/go1.22.0/src/context/context.go#L618) 函数，可以创建一个带有截止时间的 [`timerCtx`](https://github.com/golang/go/blob/go1.22.0/src/context/context.go#L648)。

- 判断父 `context` 的截止时间，如果早于当前截止时间，则当前截止时间永远不会被触发，直接返回一个普通的 [`cancelCtx`](https://github.com/golang/go/blob/go1.22.0/src/context/context.go#L421)
- 创建一个 [`timerCtx`](https://github.com/golang/go/blob/go1.22.0/src/context/context.go#L648)，并同样通过 [`propagateCancel()`](https://github.com/golang/go/blob/go1.22.0/src/context/context.go#L462) 方法，处理 `context` 间的父子关系
- 计算是否已经到达截止时间，如果是的话，则直接调用 [`cancel()`](https://github.com/golang/go/blob/go1.22.0/src/context/context.go#L536) 方法
- 若未到截止时间，且当前 `context` 未被取消，则创建一个定时任务，在到达截止时间时，调用 [`cancel()`](https://github.com/golang/go/blob/go1.22.0/src/context/context.go#L536) 方法

```go
var DeadlineExceeded error = deadlineExceededError{}

func WithDeadline(parent Context, d time.Time) (Context, CancelFunc) {
    return WithDeadlineCause(parent, d, nil)
}

func WithDeadlineCause(parent Context, d time.Time, cause error) (Context, CancelFunc) {
    if parent == nil {
        panic("cannot create context from nil parent")
    }
    if cur, ok := parent.Deadline(); ok && cur.Before(d) {
        // The current deadline is already sooner than the new one.
        return WithCancel(parent)
    }
    c := &timerCtx{
        deadline: d,
    }
    c.cancelCtx.propagateCancel(parent, c)
    dur := time.Until(d)
    if dur <= 0 {
        c.cancel(true, DeadlineExceeded, cause) // deadline has already passed
        return c, func() { c.cancel(false, Canceled, nil) }
    }
    c.mu.Lock()
    defer c.mu.Unlock()
    if c.err == nil {
        c.timer = time.AfterFunc(dur, func() {
            c.cancel(true, DeadlineExceeded, cause)
        })
    }
    return c, func() { c.cancel(true, Canceled, nil) }
}
```

通过 [`WithTimeout()`](https://github.com/golang/go/blob/go1.22.0/src/context/context.go#L689) 和 [`WithTimeoutCause()`](https://github.com/golang/go/blob/go1.22.0/src/context/context.go#L696) 函数，可以创建一个带有超时时间的 [`timerCtx`](https://github.com/golang/go/blob/go1.22.0/src/context/context.go#L648)。函数内部会将超时时间转化为实际的截止时间，并复用 [`WithDeadline()`](https://github.com/golang/go/blob/go1.22.0/src/context/context.go#L611) 和 [`WithDeadlineCause()`](https://github.com/golang/go/blob/go1.22.0/src/context/context.go#L618) 函数来实现。

```go
func WithTimeout(parent Context, timeout time.Duration) (Context, CancelFunc) {
    return WithDeadline(parent, time.Now().Add(timeout))
}

func WithTimeoutCause(parent Context, timeout time.Duration, cause error) (Context, CancelFunc) {
    return WithDeadlineCause(parent, time.Now().Add(timeout), cause)
}
```

## valueCtx

[`valueCtx`](https://github.com/golang/go/blob/go1.22.0/src/context/context.go#L728) 继承了 [`Context`][Context_link] 接口，并额外维护了一个键值对，用于在协程间传递数据。

```go
type valueCtx struct {
    Context
    key, val any
}
```

通过 [`WithValue()`](https://github.com/golang/go/blob/go1.22.0/src/context/context.go#L713) 函数可以创建一个带有键值对的 [`valueCtx`](https://github.com/golang/go/blob/go1.22.0/src/context/context.go#L728)。需要注意 `key` 不能为空，且必须是可比较的类型。

```go
func WithValue(parent Context, key, val any) Context {
    if parent == nil {
        panic("cannot create context from nil parent")
    }
    if key == nil {
        panic("nil key")
    }
    if !reflectlite.TypeOf(key).Comparable() {
        panic("key is not comparable")
    }
    return &valueCtx{parent, key, val}
}
```

[`valueCtx`](https://github.com/golang/go/blob/go1.22.0/src/context/context.go#L728) 的 [`Value()`](https://github.com/golang/go/blob/go1.22.0/src/context/context.go#L752) 方法会先判断当前 `context` 的键值对是否为目标键，如果是则直接返回，否则通过 [`value()`](https://github.com/golang/go/blob/go1.22.0/src/context/context.go#L759) 函数递归判断父 `context`。

```go
func (c *valueCtx) Value(key any) any {
    if c.key == key {
        return c.val
    }
    return value(c.Context, key)
}
```

[`value()`](https://github.com/golang/go/blob/go1.22.0/src/context/context.go#L759) 函数是一个递归函数，用于在 `context` 的父子链中，区分 `context` 的具体类型，并查找指定的键值对。

```go
func value(c Context, key any) any {
    for {
        switch ctx := c.(type) {
        case *valueCtx:
            if key == ctx.key {
                return ctx.val
            }
            c = ctx.Context
        case *cancelCtx:
            if key == &cancelCtxKey {
                return c
            }
            c = ctx.Context
        case withoutCancelCtx:
            if key == &cancelCtxKey {
                // This implements Cause(ctx) == nil
                // when ctx is created using WithoutCancel.
                return nil
            }
            c = ctx.c
        case *timerCtx:
            if key == &cancelCtxKey {
                return &ctx.cancelCtx
            }
            c = ctx.Context
        case backgroundCtx, todoCtx:
            return nil
        default:
            return c.Value(key)
        }
    }
}
```

一般场景下，[`valueCtx`](https://github.com/golang/go/blob/go1.22.0/src/context/context.go#L728) 仅用来传递请求相关的数据，例如请求 ID、Track ID、用户身份、服务环境标识等不可变信息，不适合替代函数参数来使用。

## Ref

- <https://github.com/golang/go/blob/go1.22.0/src/context/context.go>
- <https://draveness.me/golang/docs/part3-runtime/ch06-concurrency/golang-context/>

[Context_link]: https://github.com/golang/go/blob/go1.22.0/src/context/context.go#L68
