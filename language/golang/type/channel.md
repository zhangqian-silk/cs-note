# Channel

在 Go 的并发模型中，相较于使用共享内存，更推荐使用管道 channel，来进行通信。

在设计上，channel 的读取与发送遵循了先进先出的规则，内部通过互斥锁来实现并发控制。

使用时，通过 `make()` 函数进行初始化，并通过 `chan` 关键字加元素类型，共同指明 channel 的类型。

```go
ch := make(chan int)
```

channel 主要通过发送、接收两种操作来实现通信行为，且两个操作均使用 `<-` 运算符实现，通过左值、右值进行区分。接收语句也可以不接受结果，仅实现接收的行为。

```go
ch <- 1
x := <-ch
<-ch
```

调用 channel 发送和接收操作时，都有可能阻塞当前 goroutine，如果当前 channel 中的数据还未被接收，则其他发送数据的 goroutine 均会被阻塞，此时可以在创建时额外声明缓冲区的大小，让发送数据的操作可以继续进行。

```go
ch := make(chan int, 10)
```

另外，对于发送次数、接收次数都不确定时的场景，接收时则需要始终进行等待，此时则需要手动关闭 channel，避免阻塞，且接收端也需要额外的参数，判断 channel 是否被关闭的状态，及时结束等待行为，同时也可以使用 `range` 循环，当 channel 关闭且没有值后，会自动跳出循环。

```go
close(ch)

for {
    x, ok := <-ch
    if !ok {
        break
    }
}

for x := range ch {}
```

大部分并发场景下，其实都是典型的生产者、消费者模型，即某些 goroutine 只生产数据，某些 goroutine 只消费数据，此时可以将 channel 进一步细化，用 `chan<-int` 表示只用来发送 `int` 的 channel，用 `<-chan int` 表示只用来接收 `int` 的 channel。

```go
func producer(ch chan<- int) { }
func consumer(ch <-chan int) { }
```

## 数据结构

在运行时，channel 使用结构体 [hchan](https://github.com/golang/go/blob/go1.22.0/src/runtime/chan.go#L33) 来表示：

```go
type hchan struct {
    qcount   uint           // total data in the queue
    dataqsiz uint           // size of the circular queue
    buf      unsafe.Pointer // points to an array of dataqsiz elements
    elemsize uint16
    closed   uint32
    elemtype *_type // element type
    sendx    uint   // send index
    recvx    uint   // receive index
    recvq    waitq  // list of recv waiters
    sendq    waitq  // list of send waiters

    lock mutex
}
```

- `qcount`、`dataqsiz`、`buf`、`sendx`、`recvx` 构成了一个循环队列，用于维护 channel 内部的缓冲区

- `elemsize` 和 `elemtype` 存储了 channel 所传递的元素的信息

- `sendq` 和 `recvq` 维护了目前被阻塞的 goroutine 列表，列表的数据结构为双向链表，[waitq](https://github.com/golang/go/blob/go1.22.0/src/runtime/chan.go#L54) 存储了链表的头尾节点，[sudog](https://github.com/golang/go/blob/master/src/runtime/runtime2.go#L356) 内部维护了 goroutine 相关信息，以及 `prev` 和 `next` 指针

    ```go
    type waitq struct {
        first *sudog
        last  *sudog
    }

    type sudog struct {
        g *g

        next *sudog
        prev *sudog
        ...
    }
    ```

- `lock` 为 channel 提供了并发控制

## 创建管道

### 类型检查

编译器会在类型检查阶段，通过 [typecheck1()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/typecheck/typecheck.go#L218)函数和 [tcMake()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/typecheck/func.go#L514) 函数，区分 `make` 函数真正创建的类型，将 `OMAKE` 节点，转化为 `OMAKECHAN` 节点：

```go
func typecheck1(n ir.Node, top int) ir.Node {
    ...
    switch n.Op() {
    case ir.OMAKE:
        n := n.(*ir.CallExpr)
        return tcMake(n)
    ...
    }
}
```

- [tcMake()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/typecheck/func.go#L514) 函数首先获取第一个参数，判断当前所要创建的元素类型，并区分 `slice`、`map` 和 channel 执行不同的逻辑。

    ```go
    func tcMake(n *ir.CallExpr) ir.Node {
        args := n.Args
        ...
        l := args[0]
        l = typecheck(l, ctxType)
        t := l.Type()
        ...
        switch t.Kind() {
        case types.TSLICE:
            ...
        case types.TMAP:
            ...
        case types.TCHAN:
            ...
        ...
        }
        ...
    }
    ```

- 其次更新 `args` 切片的索引，设置为 `1`，当 `i < len(args)` 时，说明在调用 `make()` 函数时，还额外指定了 `size` 参数，对应于 `types.TCHAN` 类型，就是指缓冲区的大小，当未额外指定时，默认缓冲区大小为 `0`

    ```go
    func tcMake(n *ir.CallExpr) ir.Node {
        ...
        i := 1
        var nn ir.Node
        switch t.Kind() {
        case types.TCHAN:
            l = nil
            if i < len(args) {
                l = args[i]
                i++
                l = Expr(l)
                l = DefaultLit(l, types.Types[types.TINT])
                ...
            } else {
                l = ir.NewInt(base.Pos, 0)
            }
            ...
        ...
        }
        ...
    }
    ```

- 最终构造出新的 `OMAKECHAN` 节点并返回，实现 `OMAKE` 节点的转换逻辑
  
    ```go
    func tcMake(n *ir.CallExpr) ir.Node {
        ...
        var nn ir.Node
        switch t.Kind() {
        case types.TCHAN:
            ...
            nn = ir.NewMakeExpr(n.Pos(), ir.OMAKECHAN, l, nil)
        ...
        }
        ...
        return nn
    }
    ```

### 节点替换

在节点替换阶段，[walkExpr1()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/walk/expr.go#L83) 函数和 [walkMakeChan()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/walk/builtin.go#L285) 函数会将 `OMAKECHAN` 节点，转化为真正的创建函数 [makechan64()](https://github.com/golang/go/blob/go1.22.0/src/runtime/chan.go#L64) 和 [makechan()](https://github.com/golang/go/blob/go1.22.0/src/runtime/chan.go#L64)。其中 [makechan64()](https://github.com/golang/go/blob/go1.22.0/src/runtime/chan.go#L64) 函数，但是最终也会调用至 [makechan()](https://github.com/golang/go/blob/go1.22.0/src/runtime/chan.go#L64) 函数来实现相关逻辑 。

```go
func walkExpr1(n ir.Node, init *ir.Nodes) ir.Node {
    switch n.Op() {
    case ir.OMAKECHAN:
        n := n.(*ir.MakeExpr)
        return walkMakeChan(n, init)
    ...
    }
}

func walkMakeChan(n *ir.MakeExpr, init *ir.Nodes) ir.Node {
    size := n.Len
    fnname := "makechan64"
    argtype := types.Types[types.TINT64]

    if size.Type().IsKind(types.TIDEAL) || size.Type().Size() <= types.Types[types.TUINT].Size() {
        fnname = "makechan"
        argtype = types.Types[types.TINT]
    }

    return mkcall1(chanfn(fnname, 1, n.Type()), n.Type(), init, reflectdata.MakeChanRType(base.Pos, n), typecheck.Conv(size, argtype))
}

func makechan64(t *chantype, size int64) *hchan {
    if int64(int(size)) != size {
        panic(plainError("makechan: size out of range"))
    }

    return makechan(t, int(size))
}
```

### [makechan()](https://github.com/golang/go/blob/go1.22.0/src/runtime/chan.go#L64)

- 在 [makechan()](https://github.com/golang/go/blob/go1.22.0/src/runtime/chan.go#L64) 函数内部，首先计算针对缓冲区所要分配的内存大小，并额外进行一些安全校验：

    ```go
    func makechan(t *chantype, size int) *hchan {
        elem := t.Elem

        // compiler checks this but be safe.
        if elem.Size_ >= 1<<16 {
            throw("makechan: invalid channel element type")
        }
        if hchanSize%maxAlign != 0 || elem.Align_ > maxAlign {
            throw("makechan: bad alignment")
        }

        mem, overflow := math.MulUintptr(elem.Size_, uintptr(size))
        if overflow || mem > maxAlloc-hchanSize || size < 0 {
            panic(plainError("makechan: size out of range"))
        }
        ...
    }
    ```

- 其次根据是否存在缓冲区、缓冲区是否存在指针两条逻辑，初始化 channel，当不涉及 GC 时，缓冲区与 channel 一起进行内存分配

    ```go
    func makechan(t *chantype, size int) *hchan {
        ...
        var c *hchan
        switch {
        case mem == 0:
            // Queue or element size is zero.
            c = (*hchan)(mallocgc(hchanSize, nil, true))
            // Race detector uses this location for synchronization.
            c.buf = c.raceaddr()
        case elem.PtrBytes == 0:
            // Elements do not contain pointers.
            // Allocate hchan and buf in one call.
            c = (*hchan)(mallocgc(hchanSize+mem, nil, true))
            c.buf = add(unsafe.Pointer(c), hchanSize)
        default:
            // Elements contain pointers.
            c = new(hchan)
            c.buf = mallocgc(mem, elem, true)
        }
        ...
    }
    ```

- 最后初始化 channel 中其他元素信息

    ```go
    func makechan(t *chantype, size int) *hchan {
        ...
        c.elemsize = uint16(elem.Size_)
        c.elemtype = elem
        c.dataqsiz = uint(size)
        lockInit(&c.lock, lockRankHchan)

        if debugChan {
            print("makechan: chan=", c, "; elemsize=", elem.Size_, "; dataqsiz=", size, "\n")
        }
        return c
    }
    ```

## 发送数据

在使用 `Chan <- Value` 语句发送数据时，主要分为如下三种情况：

- 当前 channel 有已被阻塞的、等待接收数据的 goroutine，则直接将数据发送给该 goroutine，并将其唤醒
- 当前不存在等待接收数据的 goroutine，则尝试将待发送的数据添加至缓冲区中
- 如果当前不存在等待接收数据的 goroutine 且缓冲区已满，或是没有缓冲区，则阻塞当前 goroutine，等待接收数据的函数将其唤醒

除此以外，还有一些特殊场景需要注意：

- 向未初始化的 channel 中发送数据时，会造成永久阻塞
- 向已经关闭的 channel 中发送数据时，会导致 `panic`

### 节点替换

`Chan <- Value` 语句对应了 `OSEND` 节点，在节点替换时，[walkExpr1()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/walk/expr.go#L83) 函数和 [walkSend()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/walk/expr.go#L859) 函数会将 `OSEND` 节点，转化为调用 [chansend1()](https://github.com/golang/go/blob/go1.22.0/src/runtime/chan.go#L144) 函数，并最终调用至 [chansend()](https://github.com/golang/go/blob/go1.22.0/src/runtime/chan.go#L160) 函数。

```go
func walkExpr1(n ir.Node, init *ir.Nodes) ir.Node {
    switch n.Op() {
    case ir.OSEND:
        n := n.(*ir.SendStmt)
        return walkSend(n, init)
    ...
    }
}

func walkSend(n *ir.SendStmt, init *ir.Nodes) ir.Node {
    n1 := n.Value
    n1 = typecheck.AssignConv(n1, n.Chan.Type().Elem(), "chan send")
    n1 = walkExpr(n1, init)
    n1 = typecheck.NodAddr(n1)
    return mkcall1(chanfn("chansend1", 2, n.Chan.Type()), nil, init, n.Chan, n1)
}

func chansend1(c *hchan, elem unsafe.Pointer) {
    chansend(c, elem, true, getcallerpc())
}

func chansend(c *hchan, ep unsafe.Pointer, block bool, callerpc uintptr) bool {
    ...
}
```

需要注意的时，从 `Chan <- Value` 触发的发送数据行为，`block` 参数为 `true`，说明需要阻塞当前 goroutine。

### [chansend()](https://github.com/golang/go/blob/go1.22.0/src/runtime/chan.go#L160)

- 预处理

  - 如果当前 channel 为空，且 `block` 为 `false`，则直接返回 `false`，说明调用失败
  - 如果当前 channel 为空，且 `block` 为 `true`，则通过 `gopark` 函数，阻塞当前 goroutine

    ```go
    func chansend(c *hchan, ep unsafe.Pointer, block bool, callerpc uintptr) bool {
        if c == nil {
            if !block {
                return false
            }
            gopark(nil, nil, waitReasonChanSendNilChan, traceBlockForever, 2)
            throw("unreachable")
        }
        ...
    }
    ```

  - 如果当前是非阻塞操作，channel 还未关闭，且缓冲区已满，则直接返回 `false`

    ```go
    func chansend(c *hchan, ep unsafe.Pointer, block bool, callerpc uintptr) bool {
        ...
        if !block && c.closed == 0 && full(c) {
            return false
        }
        ...
    }
    ```

  - 加锁，并检查 channel 是否已经关闭，如果是则解锁并触发 `panic`，即不能向已经关闭的 channel 中发送数据

    ```go
    func chansend(c *hchan, ep unsafe.Pointer, block bool, callerpc uintptr) bool {
        ...
        lock(&c.lock)

        if c.closed != 0 {
            unlock(&c.lock)
            panic(plainError("send on closed channel"))
        }
        ...
    }
    ```

- 直接向接收者发送数据

  - 如果当前的接收者队列中，能够找到接收者，则直接发送数据

    ```go
    func chansend(c *hchan, ep unsafe.Pointer, block bool, callerpc uintptr) bool {
        ...
        if sg := c.recvq.dequeue(); sg != nil {
            // Found a waiting receiver. We pass the value we want to send
            // directly to the receiver, bypassing the channel buffer (if any).
            send(c, sg, ep, func() { unlock(&c.lock) }, 3)
            return true
        }
        ...
    }
    ```

  - 在 [send()](https://github.com/golang/go/blob/go1.22.0/src/runtime/chan.go#L294) 函数中，如果接收者需要接收该元素，则将要发送的数据拷贝至 `Value <- Chan` 中 `Value` 对应的内存地址中
  - 获取接收者对应的 goroutine 并解锁当前的 channel
  - 最后标记接收成功，并通过 `goready` 函数，唤醒 goroutine

    ```go
    func send(c *hchan, sg *sudog, ep unsafe.Pointer, unlockf func(), skip int) {
        if sg.elem != nil {
            sendDirect(c.elemtype, sg, ep)
            sg.elem = nil
        }
        gp := sg.g
        unlockf()
        gp.param = unsafe.Pointer(sg)
        sg.success = true
        ...
        goready(gp, skip+1)
    }
    ```

- 将数据写入缓冲区

  - 如果当前还可以向缓冲区中添加数据，则将要发送的元素拷贝至缓冲区，并更新索引 `sendx` 和当前元素数量 `qcount`
  - 需要注意的缓冲区是环形队列，当 `sendx` 到达最大值时，需要归零
  - 最后解锁并返回 `true`，不会阻塞发送数据的 goroutine

    ```go
    func chansend(c *hchan, ep unsafe.Pointer, block bool, callerpc uintptr) bool {
        ...
        if c.qcount < c.dataqsiz {
            // Space is available in the channel buffer. Enqueue the element to send.
            qp := chanbuf(c, c.sendx)
            if raceenabled {
                racenotify(c, c.sendx, nil)
            }
            typedmemmove(c.elemtype, qp, ep)
            c.sendx++
            if c.sendx == c.dataqsiz {
                c.sendx = 0
            }
            c.qcount++
            unlock(&c.lock)
            return true
        }
        ...
    }
    ```

- 处理当前找不到接收者且缓冲区已满的情况

  - 如果是非阻塞场景，直接结束，返回 `false`

    ```go
    func chansend(c *hchan, ep unsafe.Pointer, block bool, callerpc uintptr) bool {
        ...
        if !block {
            unlock(&c.lock)
            return false
        }
        ...
    }
    ```

  - 阻塞场景时，获取当前 goroutine，并初始化发送者 `sudog` 相关信息

    ```go
    func chansend(c *hchan, ep unsafe.Pointer, block bool, callerpc uintptr) bool {
        ...
        gp := getg()
        mysg := acquireSudog()
        ...
        mysg.elem = ep
        mysg.waitlink = nil
        mysg.g = gp
        mysg.isSelect = false
        mysg.c = c
        ...
    }
    ```

  - 设置当前 goroutine 所等待的 `sudog`
  - 将当前的 `sudog` 添加至发送者的等待队列中
  - 调用 `gopark()` 函数，阻塞当前 goroutine
  - `KeepAlive()` 是一个特殊的函数，可以由编译器保障入参，在该行代码处之前，不会被 GC 回收，对应于当前函数，可以保障要发送的元素 `ep`，在 goroutine 挂起期间，不会被 GC 回收

    ```go
    func chansend(c *hchan, ep unsafe.Pointer, block bool, callerpc uintptr) bool {
        ...
        gp.waiting = mysg
        gp.param = nil
        c.sendq.enqueue(mysg)
        gp.parkingOnChan.Store(true)
        gopark(chanparkcommit, unsafe.Pointer(&c.lock), waitReasonChanSend, traceBlockChanSend, 2)
        // Ensure the value being sent is kept alive until the
        // receiver copies it out. The sudog has a pointer to the
        // stack object, but sudogs aren't considered as roots of the
        // stack tracer.
        KeepAlive(ep)
        ...
    }
    ```

- 执行唤醒后的数据处理操作

  - 修改 goroutine 被挂起时设置的标记位

    ```go
    func chansend(c *hchan, ep unsafe.Pointer, block bool, callerpc uintptr) bool {
        ...
        // someone woke us up.
        if mysg != gp.waiting {
            throw("G waiting list is corrupted")
        }
        gp.waiting = nil
        gp.activeStackChans = false
        ...
        gp.param = nil
        if mysg.releasetime > 0 {
            blockevent(mysg.releasetime-t0, 2)
        }
        mysg.c = nil
        releaseSudog(mysg)
        ...
    }
    ```

  - 判断当前发送状态，如果发送失败，则说明 channel 被关闭，则触发 `panic`，即不允许向关闭的 channel 中发送数据
  - 如果发送成功，返回 `true`

    ```go
    func chansend(c *hchan, ep unsafe.Pointer, block bool, callerpc uintptr) bool {
        ...
        closed := !mysg.success
        ...
        if closed {
            if c.closed == 0 {
                throw("chansend: spurious wakeup")
            }
            panic(plainError("send on closed channel"))
        }
        return true
    }
    ```

## 接收数据

使用 `<- Chan` 语句接收数据时，主要分为如下三种情况：

- 当前 channel 有已被阻塞的、等待发送数据的 goroutine，则直接从该 goroutine 中获取数据，并将其唤醒
- 当前不存在等待接收数据的 goroutine，则尝试从缓冲区中获取数据
- 如果当前不存在等待接收数据的 goroutine 且缓冲区为空，或是没有缓冲区，则阻塞当前 goroutine，等待发送数据的函数将其唤醒

除此以外，还有一些特殊场景需要注意：

- 从未初始化的 channel 中接收数据时，会造成永久阻塞
- 从已经关闭的 channel 中发送数据时，会获取到零值和 `false`，表示获取失败

### 节点替换

与发送数据类似，`<- Chan` 语句对应了 `OSEND` 节点，在节点替换时，[walkStmt()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/walk/stmt.go#L15) 函数和 [walkRecv()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/walk/walk.go#L54) 函数会将 `OSEND` 节点，转化为调用 [chanrecv1()](https://github.com/golang/go/blob/go1.22.0/src/runtime/chan.go#L441) 函数，并最终调用至 [chanrecv()](https://github.com/golang/go/blob/go1.22.0/src/runtime/chan.go#L457) 函数。

```go
func walkStmt(n ir.Node) ir.Node {
    switch n.Op() {
    case ir.ORECV:
        n := n.(*ir.UnaryExpr)
        return walkRecv(n)
    ...
    }
}

func walkRecv(n *ir.UnaryExpr) ir.Node {
    init := ir.TakeInit(n)

    n.X = walkExpr(n.X, &init)
    call := walkExpr(mkcall1(chanfn("chanrecv1", 2, n.X.Type()), nil, &init, n.X, typecheck.NodNil()), &init)
    return ir.InitExpr(init, call)
}

func chanrecv1(c *hchan, elem unsafe.Pointer) {
    chanrecv(c, elem, true)
}

func chanrecv(c *hchan, ep unsafe.Pointer, block bool) (selected, received bool) {
    ...
}
```

同样的，从 `<- Chan` 触发的接收数据的行为，`block` 参数为 `true`，说明需要阻塞当前 goroutine。

### [chanrecv()](https://github.com/golang/go/blob/go1.22.0/src/runtime/chan.go#L457)

- 预处理

  - 如果当前 channel 为空，且 `block` 为 `false`，则直接返回 `(false, false)`，说明调用失败
  - 如果当前 channel 为空，且 `block` 为 `true`，则通过 `gopark` 函数，阻塞当前 goroutine

    ```go
    func chanrecv(c *hchan, ep unsafe.Pointer, block bool) (selected, received bool) {
        if c == nil {
            if !block {
                return
            }
            gopark(nil, nil, waitReasonChanReceiveNilChan, traceBlockForever, 2)
            throw("unreachable")
        }
        ...
    }
    ```

  - 如果当前是非阻塞操作，channel 收到的数据为空且未关闭，则直接返回 `(false, false)`
  - 如果此时 channel 已经关闭，因为这里对 channel 处理时没有加锁，所以需要再次校验 channel 是否为空，如果仍为空，则返回 `(true, false)`，说明接收成功，但是没有收到数据

    ```go
    func chanrecv(c *hchan, ep unsafe.Pointer, block bool) (selected, received bool) {
        ...
        if !block && empty(c) {
            if atomic.Load(&c.closed) == 0 {
                return
            }
            if empty(c) {
                if raceenabled {
                    raceacquire(c.raceaddr())
                }
                if ep != nil {
                    typedmemclr(c.elemtype, ep)
                }
                return true, false
            }
        }
        ...
    }
    ```

- 直接接收数据

  - 加锁，并检查 channel 是否已经关闭
  - 如果已经关闭且缓冲区数据为空，则返回 `(true, false)`，表示接收成功，但是没有收到数据
  - 如果没有关闭，则尝试去找到一个正在等待的发送者，调用 [recv()](https://github.com/golang/go/blob/go1.22.0/src/runtime/chan.go#L615) 函数去接收数据，并返回 `(true, true)`，表示成功接收到数据

    ```go
    func chanrecv(c *hchan, ep unsafe.Pointer, block bool) (selected, received bool) {
        ...
        lock(&c.lock)

        if c.closed != 0 {
            if c.qcount == 0 {
                unlock(&c.lock)
                if ep != nil {
                    typedmemclr(c.elemtype, ep)
                }
                return true, false
            }
        } else {
            if sg := c.sendq.dequeue(); sg != nil {
                recv(c, sg, ep, func() { unlock(&c.lock) }, 3)
                return true, true
            }
        }
        ...
    }
    ```

- [recv()](https://github.com/golang/go/blob/go1.22.0/src/runtime/chan.go#L615) 函数处理逻辑

  - 如果不存在缓冲区，则直接将发送数据的 goroutine 中存储的数据拷贝至目标元素的内存地址中
  - 如果存在缓冲区，则将队列当前位置的数据拷贝至目标元素的内存地址中，并更新发送数据和接收数据对应的索引，即 `sendx` 和 `recvx`

    ```go
    func recv(c *hchan, sg *sudog, ep unsafe.Pointer, unlockf func(), skip int) {
        if c.dataqsiz == 0 {
            if ep != nil {
                // copy data from sender
                recvDirect(c.elemtype, sg, ep)
            }
        } else {
            qp := chanbuf(c, c.recvx)
            if ep != nil {
                typedmemmove(c.elemtype, ep, qp)
            }
            typedmemmove(c.elemtype, qp, sg.elem)
            c.recvx++
            if c.recvx == c.dataqsiz {
                c.recvx = 0
            }
            c.sendx = c.recvx // c.sendx = (c.sendx+1) % c.dataqsiz
        }
        ...
    }
    ```

  - 获取接收者对应的 goroutine 并解锁当前的 channel
  - 最后标记接收成功，并通过 `goready` 函数，唤醒 goroutine

    ```go
    func recv(c *hchan, sg *sudog, ep unsafe.Pointer, unlockf func(), skip int) {
        ...
        sg.elem = nil
        gp := sg.g
        unlockf()
        gp.param = unsafe.Pointer(sg)
        sg.success = true
        ...
        goready(gp, skip+1)
    }
    ```

- 从缓冲区中接收数据

  - 如果当前缓冲区中元素大于 0，则直接从缓冲区中获取数据，并拷贝至目标元素处
  - 更新接收数据对应的索引 `recvx` 和当前缓冲区中数据数量 `qcount`
  - 解锁 channel 并返回 `(true, true)`，表示成功接收数据

    ```go
    func chanrecv(c *hchan, ep unsafe.Pointer, block bool) (selected, received bool) {
        ...
        if c.qcount > 0 {
            qp := chanbuf(c, c.recvx)
            if ep != nil {
                typedmemmove(c.elemtype, ep, qp)
            }
            typedmemclr(c.elemtype, qp)
            c.recvx++
            if c.recvx == c.dataqsiz {
                c.recvx = 0
            }
            c.qcount--
            unlock(&c.lock)
            return true, true
        }
        ...
    }
    ```

- 处理当前找不到发送者且缓冲区没有数据的情况

  - 如果是非阻塞场景，直接结束，返回 `(false, false)`，表示接收失败

    ```go
    func chanrecv(c *hchan, ep unsafe.Pointer, block bool) (selected, received bool) {
        ...
        if !block {
            unlock(&c.lock)
            return false, false
        }
        ...
    }
    ```

  - 阻塞场景时，获取当前 goroutine，并初始化接收者 `sudog` 相关信息

    ```go
    func chanrecv(c *hchan, ep unsafe.Pointer, block bool) (selected, received bool) {
        ...
        gp := getg()
        mysg := acquireSudog()
        ...
        mysg.elem = ep
        mysg.waitlink = nil
        mysg.g = gp
        mysg.isSelect = false
        mysg.c = c
        ...
    }
    ```

  - 设置当前 goroutine 所等待的 `sudog`
  - 将当前的 `sudog` 添加至接收者的等待队列中
  - 调用 `gopark()` 函数，阻塞当前 goroutine

    ```go
    func chanrecv(c *hchan, ep unsafe.Pointer, block bool) (selected, received bool) {
        ...
        gp.waiting = mysg
        ...
        gp.param = nil
        c.recvq.enqueue(mysg)
        gp.parkingOnChan.Store(true)
        gopark(chanparkcommit, unsafe.Pointer(&c.lock), waitReasonChanReceive, traceBlockChanRecv, 2)
        ...
    }
    ```

- 执行唤醒后的数据处理操作

  - 修改 goroutine 被挂起时设置的标记位
  - 返回 `(true, success)`，标记成功接收数据

    ```go
    func chanrecv(c *hchan, ep unsafe.Pointer, block bool) (selected, received bool) {
        ...
        // someone woke us up.
        if mysg != gp.waiting {
            throw("G waiting list is corrupted")
        }
        gp.waiting = nil
        gp.activeStackChans = false
        ...
        success := mysg.success
        gp.param = nil
        mysg.c = nil
        releaseSudog(mysg)
        return true, success
    }
    ```

## 关闭 Channel

使用 `close()` 函数关闭 channel 时，会先将 channel 标记关闭，然后唤醒所有等待发送和接收的 goroutine，由他们继续处理后续逻辑

除此以外，还有一些特殊场景需要注意：

- 关闭未初始化的 channel 时，会触发 `panic`
- 重复关闭 channel 时，会触发 `panic`

### 节点替换

在节点替换时，[walkExpr1()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/walk/expr.go#L83) 函数和 [walkClose()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/walk/builtin.go#L154) 函数会将 `OCLOSE` 节点，转化为调用 [closechan()](https://github.com/golang/go/blob/go1.22.0/src/runtime/chan.go#L357) 函数。

```go
func walkExpr1(n ir.Node, init *ir.Nodes) ir.Node {
    switch n.Op() {
    case ir.OCLOSE:
        n := n.(*ir.UnaryExpr)
        return walkClose(n, init)
    ...
    }
}

func walkClose(n *ir.UnaryExpr, init *ir.Nodes) ir.Node {
    fn := typecheck.LookupRuntime("closechan", n.X.Type())
    return mkcall1(fn, nil, init, n.X)
}

func closechan(c *hchan) {
    ...
}
```

### [closechan()](https://github.com/golang/go/blob/go1.22.0/src/runtime/chan.go#L357)

- 预处理

  - 检查 channel 是否为空，如果是则触发 `panic`
  - 加锁，并检查 channel 是否已经被关闭，如果是则触发 `panic`

    ```go
    func closechan(c *hchan) {
        if c == nil {
            panic(plainError("close of nil channel"))
        }

        lock(&c.lock)
        if c.closed != 0 {
            unlock(&c.lock)
            panic(plainError("close of closed channel"))
        }
        ...
    }
    ```

  - 检查无误后，标记当前 channel 被关闭
  - 初始化存储所有等待中的 goroutine 的列表

    ```go
    func closechan(c *hchan) {
        ...
        c.closed = 1

        var glist gList
        ...
    }
    ```

- 获取所有等待中的发送者和接收者

  - 遍历 `recvq`，获取所有等待中的接收者，并释放用于接收数据的元素

    ```go
    func closechan(c *hchan) {
        ...
        // release all readers
        for {
            sg := c.recvq.dequeue()
            if sg == nil {
                break
            }
            if sg.elem != nil {
                typedmemclr(c.elemtype, sg.elem)
                sg.elem = nil
            }
            gp := sg.g
            gp.param = unsafe.Pointer(sg)
            sg.success = false
            glist.push(gp)
        }
        ...
    }
    ```

  - 遍历 `sendq`，获取所有等待中的发送者

    ```go
    func closechan(c *hchan) {
        ...
        // release all writers (they will panic)
        for {
            sg := c.sendq.dequeue()
            if sg == nil {
                break
            }
            sg.elem = nil
            gp := sg.g
            gp.param = unsafe.Pointer(sg)
            sg.success = false
            glist.push(gp)
        }
        ...
    }
    ```

- 执行处理逻辑

  - 解锁 channel
  - 唤醒所有的 goroutine，由 [chansend()](https://github.com/golang/go/blob/go1.22.0/src/runtime/chan.go#L160) 函数和 [chanrecv()](https://github.com/golang/go/blob/go1.22.0/src/runtime/chan.go#L457) 函数执行后续处理逻辑

    ```go
    func closechan(c *hchan) {
        ...
        unlock(&c.lock)

        // Ready all Gs now that we've dropped the channel lock.
        for !glist.empty() {
            gp := glist.pop()
            gp.schedlink = 0
            goready(gp, 3)
        }
    }
    ```

## 如何关闭 Channel

> 原文：[How to Gracefully Close Channels](https://go101.org/article/channel-closing.html)
> 译文：[如何优雅地关闭Go channel](https://www.jianshu.com/p/d24dfbb33781)

### 为什么要关闭 Channel

在使用 channel 进行通信时，如果数据传递次数是明确的，则很容易明确发送数据和接收数据两个函数的调用次数，不会出现阻塞，在正常的程序执行过程中，最终 channel 会被 GC 回收。

而当 channel 涉及到的发送次数和接收次数不明确时，接收者被迫需要进行循环处理，只有当接收到 channel 关闭的信号后，才可以跳出循环，继续程序的执行。

### Channel 的关闭原则

从上文的介绍中可以看到，在正常使用 channel 进行读、写时，还需要关注一下特殊场景：

- 使用未初始化的 channel

  - 向该 channel 中发送数据会导致永久阻塞
  - 从该 channel 中接收数据会导致永久阻塞
  - 关闭该 channel 会导致 `panic`

- 使用已关闭的 channel

  - 向该 channel 中发送数据会导致 `panic`
  - 从该 channel 中接收数据是合法的，且会有额外的状态位标记 channel 已经关闭
  - 关闭该 channel 会导致 `panic`

对于未初始化的场景较好避免，而且可以通过判空的方式兜底处理，但是对于已关闭的 channel，消息的发送者并没有合适的方案去主动判断是否已经关闭，则需要一定的代码设计来保障程序的安全性。

对此有一个 channel 的关闭原则，不要从接收者处关闭 channel，且存在多个发送者时，不要关闭 channel，换句话说，只有当 channel 有且仅有一个发送者时，才可以由这个发送者关闭 channel。

### 为什么不提供判断 channel 状态的函数

在并发场景下，channel 相较于共享内存，其优势是使用者不需要考虑加锁进行处理，但是在 channel 底层，仍然需要互斥锁来保障并发安全。针对于先使用函数判断 channel 是否关闭，再发送消息的场景，因为两步操作是非原子化的，没有办法保障发送消息时，channel 仍然未被关闭。

### 遵循关闭原则的方案

对于关闭原则，在理解上也比较符合直觉，当有且仅有一个发送者时，所有的数据均有该发送者产生，自然也具有关闭 channel 的主动权。

而当有多个发送者时，单个发送者不再产生数据时，不应到影响其他发送者，故所有发送者均不具备关闭 channel 的主动权。此时需要额外引入一个负责传递关闭消息的的 stop channel，所有的发送者和接收者均监听该 channel 的状态，由具体的业务场景决定，应该在什么场景下关闭 stop channel。

故需要针对于发送者和接收者不同的比例模型，采取不同的方案：

- 1 : 1 && 1 : N：此时的比例模型比较符合关闭原则，直接由发送者进行关闭即可
- M : N && N : 1：此时不满足比例模型，故所有的发送者不具备主动关闭的条件，需要根据具体的业务场景，由发送者或者接收者关闭一个专用的 stop channel，所有的发送者和接收者监听 stop channel 的状态来被动停止数据交互

### 使用 `defer - recover` 的方案

对于关闭的 channel，最危险的清空就是触发 `panic`，所以可以针对发送和关闭两个操作进行封装，使用 `recover` 进行避免：

```go
func SafeSend(ch chan T, value T) (closed bool) {
    defer func() {
        if recover() != nil {
            // the return result can be altered 
            // in a defer function call
            closed = true
        }
    }()
    
    ch <- value // panic if ch is closed
    return false // <=> closed = false; return
}

func SafeClose(ch chan T) (justClosed bool) {
    defer func() {
        if recover() != nil {
            justClosed = false
        }
    }()
    
    // assume ch != nil here.
    close(ch) // panic if ch is closed
    return true
}
```

### 保障原子操作

之所以不能提供判断 channel 是否关闭的方案，主要是因为判断函数和实际的操作函数，并非是原子操作，所以判断函数的结果不能完全作为操作的依据。此时可引入 cas 等方案，保障操作的原子性。

```go
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

- <https://draveness.me/golang/docs/part3-runtime/ch06-concurrency/golang-channel/>
- <https://juejin.cn/post/7033671944587182087>
- <https://go101.org/article/channel-closing.html>
- <https://www.jianshu.com/p/d24dfbb33781>
