# Select

Go 中的 `select` 可以用于 channel 的多路复用，例如在超时场景下，我们一方面需要等待操作的结果，另一方面需要限制操作的时间，超时直接结束，此时可以利用 `select` 同时监听多个 channel 的状态变化。

```go
select {
case res := <-ch:
    ...
case <-time.After(time.Second * 1):
    ...
}
```

<br>

`select` 本身的执行会阻塞当前 goroutine，直至某一个 `case` 满足条件并执行成功。

但是 `select` 支持 `default` 语句，如果此时所有 `case` 均不满足条件，无法完成写入或是读取操作，则会直接执行 `default` 的逻辑，从而避免阻塞。利用这一点，也可以实现非阻塞的写入或读取。

例如 `ch` 中缓冲区已满，如果此时要求程序的执行是非阻塞的，可以通过 `select` 和 `default` 关键字来实现。

```go
select {
case ch <- value:
    ...
default:
    ...
}
```

## 特性 & 常见用法

### 多路复用

`select` 可用来同时监听多条 channel 的状态，并执行读写操作。

如果在首次加载时，即有 `case` 语句可以执行，则会随机选择一条就行执行。如果所有 `case` 语句均处于阻塞状态，则 `select` 会阻塞当前 goroutine，直至有一条 `case` 语句率先满足执行条件，并执行。

同时也可以借助于循环语句，用于长时间不断监听所有 channel 的状态。

```go
loop:
    for {
        if xxx {
            break loop
        }
        select {
        case data1 := <-ch1:
            ...
        case data2 := <-ch2:
            ...
        case data3 := <-ch3:
            ...
        case data4 := <-ch4:
            ...
        }
    }
```

### 随机执行

`select` 中的多条 `case` 语句如果同时满足条件，最终会随机执行某一条。其原因是在底层的 [selectgo()](https://github.com/golang/go/blob/go1.22.0/src/runtime/select.go#L121) 函数中，循环判断各 `case` 语句时，引入了随机数，能够避免饥饿问题。

例如在循环中执行 `select` 语句时，如果第一条 `case` 语句永远可以执行，则后续所有的 `case` 语句则都无法执行。

相比与 `switch` 语句，`switch` 语句更类似于对 `if-elif` 这种条件判断的简化，所以可以顺序执行，且开发者也应该明确知道这个特性。`select` 语句是用来对 channel 管道实现多路复用，本身就有并发的特性存在，故需要引入随机性来避免上述问题。

### 非阻塞执行

直接使用 channel 进行发送或接收时，均是阻塞操作，但是可通过缓冲区进行缓解。而对于 `select` 的场景下，可通过 `default` 语句实现非阻塞操作，在无法执行发送或接收逻辑时，执行 `default` 语句。

```go
select {
case ch <- data:
    ...
default:
    ...
}
```

### 超时判断

在接收耗时操作的结果时，可同步利用 [time.After()](https://github.com/golang/go/blob/go1.22.0/src/time/sleep.go#L156) 开启一个定时器，再指定的时间后，从 [time.After()](https://github.com/golang/go/blob/go1.22.0/src/time/sleep.go#L156) 函数返回的 channel 中接收数据，执行对应的 `case` 语句，并结束 `select` 语句。

```go
select {
case data := <-ch:
    ...
case <-time.After(time.Second * 3):
    ...
}
```

<br>

其中 [time.After()](https://github.com/golang/go/blob/go1.22.0/src/time/sleep.go#L156) 函数会返回由 [NewTimer()](https://github.com/golang/go/blob/go1.22.0/src/time/sleep.go#L86) 函数构建的结构体 [Timer](https://github.com/golang/go/blob/go1.22.0/src/time/sleep.go#L50) 中的 channel 实例，并由 `time` 包保障在到达指定时间后，向该 channel 中发送一个 `Time` 类型的数据。

```go
// After waits for the duration to elapse and then sends the current time
// on the returned channel.
// It is equivalent to NewTimer(d).C.
// The underlying Timer is not recovered by the garbage collector
// until the timer fires. If efficiency is a concern, use NewTimer
// instead and call Timer.Stop if the timer is no longer needed.
func After(d Duration) <-chan Time {
    return NewTimer(d).C
}

// NewTimer creates a new Timer that will send
// the current time on its channel after at least duration d.
func NewTimer(d Duration) *Timer {
    c := make(chan Time, 1)
    t := &Timer{
        C: c,
        r: runtimeTimer{
            when: when(d),
            f:    sendTime,
            arg:  c,
        },
    }
    startTimer(&t.r)
    return t
}
```

## 底层实现

### 数据结构

对应于 `select` 中的 `case` 语句，每一个都是 `scase` 结构体，包含该条语句所引用的 channel 的结构体 `hchan`，以及发送或接收数据时所用到的元素 `elem`：

```go
// Select case descriptor.
type scase struct {
    c    *hchan         // chan
    elem unsafe.Pointer // data element
}
```

### 节点替换

在节点替换阶段，[walkStmt()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/walk/stmt.go#L15) 函数、[walkSelect](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/walk/select.go#L15) 函数和 [walkSelectCases()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/walk/select.go#L33) 函数会对 `select` 中每个 `case` 分支进行代码逻辑优化和处理。

```go
func walkStmt(n ir.Node) ir.Node {
switch n.Op() {
    case ir.OSELECT:
        n := n.(*ir.SelectStmt)
        walkSelect(n)
        return n
    ...
    }
}

func walkSelect(sel *ir.SelectStmt) {
    ...
    init = append(init, walkSelectCases(sel.Cases)...)
    ...
}

func walkSelectCases(cases []*ir.CommClause) []ir.Node {
    ...
}
```

<br>

[walkSelectCases()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/walk/select.go#L33) 函数内部，根据 `case` 数量区分了 4 种场景，分别进行优化处理：

```go
func walkSelectCases(cases []*ir.CommClause) []ir.Node {
    ncas := len(cases)
    ...
    if ncas == 0 {
        ...
        return
    }
    if ncas == 1 {
        ...
        return
    }
    ...
    if ncas == 2 && dflt != nil {
        ...
        return
    }
    ...
    return
}
```

### Zero-case Select

#### 语句转换

当 `case` 数量为 0 时，会优化为调用 [block()](https://github.com/golang/go/blob/go1.22.0/src/runtime/select.go#L102) 函数，永久阻塞当前 goroutine：

```go
func walkSelectCases(cases []*ir.CommClause) []ir.Node {
    ...
    // optimization: zero-case select
    if ncas == 0 {
        return []ir.Node{mkcallstmt("block")}
    }
    ...
}

func block() {
    gopark(nil, nil, waitReasonSelectNoCases, traceBlockForever, 1) // forever
}
```

#### 代码示例

- 原代码：

    ```go
    select { }
    ```

- 转化后的示例代码：

    ```go
    block()
    ```

### Single Op

#### 语句转换

当 `case` 数量为 1 时，会优化为直接执行相关的通信操作和对应的函数体，并在结束时插入一条 `break` 语句，用于跳出 `select` 结构。

```go
func walkSelectCases(cases []*ir.CommClause) []ir.Node {
    ...
    // optimization: one-case select: single op.
    if ncas == 1 {
        ...
        l := cas.Init()
        if cas.Comm != nil { // not default:
            n := cas.Comm
            l = append(l, ir.TakeInit(n)...)
            ...
            l = append(l, n)
        }

        l = append(l, cas.Body...)
        l = append(l, ir.NewBranchStmt(base.Pos, ir.OBREAK, nil))
        return l
    }
    ...
}
```

<br>

此外，还针对接收数据的语句做了额外处理，如果接收语句的两个返回元素均为空标识符，则直接将 n 同样设置为空标识符，否则设置为接收两个值的赋值操作：

```go
func walkSelectCases(cases []*ir.CommClause) []ir.Node {
    ...
    // optimization: one-case select: single op.
    if ncas == 1 {
        ...
        if cas.Comm != nil { // not default:
            ...
            switch n.Op() {
            case ir.OSELRECV2:
                r := n.(*ir.AssignListStmt)
                if ir.IsBlank(r.Lhs[0]) && ir.IsBlank(r.Lhs[1]) {
                    n = r.Rhs[0]
                    break
                }
                r.SetOp(ir.OAS2RECV)
            ...
            }
            ...
        }
        ...
    }
    ...
}
```

#### 代码示例

- 原代码：

    ```go
    select { 
    case ch <- value:
        body
    }
    ```

- 转化后的示例代码：

    ```go
    ch <- value
    body
    break
    ```

### Single Non-blocking Op

#### 语句转换

对于包含两条 `case` 语句，且其中一条是 `default` 语句的情况，会将其转化为 `if` 语句，并调用 channel 中发送数据和接收数据的特定函数，实现非阻塞调用，并在最后添加 `break` 语句，用于跳出 `select` 结构。

```go
func walkSelectCases(cases []*ir.CommClause) []ir.Node {
    ...
    // optimization: two-case select but one is default: single non-blocking op.
    if ncas == 2 && dflt != nil {
        ...
        n := cas.Comm
        ir.SetPos(n)
        r := ir.NewIfStmt(base.Pos, nil, nil, nil)
        ...
        return []ir.Node{r, ir.NewBranchStmt(base.Pos, ir.OBREAK, nil)}
    }
    ...
}
```

<br>

对于新构建的 `if` 语句来说：

- `if` 语句的 `Init` 块，对应了 `case` 语句的 `Init` 块
- `if` 语句的 `Body` 块，对应了 `case` 语句中的 `Body` 块
- `if` 语句的 `Else` 块，对应了 `default` 语句中的 `Body` 块

    ```go
    func walkSelectCases(cases []*ir.CommClause) []ir.Node {
        ...
        if ncas == 2 && dflt != nil {
            ...
            r.SetInit(cas.Init())
            ...
            r.Body = cas.Body
            r.Else = append(dflt.Init(), dflt.Body...)
            ...
        }
        ...
    }
    ```

- `if` 语句的 `Cond` 块，根据具体的操作节点分别进行构造

  - 对应 `OSEND` 语句，`Cond` 块会直接转化为 [selectnbsend()](https://github.com/golang/go/blob/go1.22.0/src/runtime/chan.go#L693) 函数的调用，函数内部会非阻塞地调用 [chansend()](https://github.com/golang/go/blob/go1.22.0/src/runtime/chan.go#L160) 函数

    ```go
    func walkSelectCases(cases []*ir.CommClause) []ir.Node {
        ...
        if ncas == 2 && dflt != nil {
            ...
            var cond ir.Node
            switch n.Op() {
            case ir.OSEND:
                n := n.(*ir.SendStmt)
                ch := n.Chan
                cond = mkcall1(chanfn("selectnbsend", 2, ch.Type()), types.Types[types.TBOOL], r.PtrInit(), ch, n.Value)
            ...
            }

            r.Cond = typecheck.Expr(cond)
            ...
        }
        ...
    }

    func selectnbsend(c *hchan, elem unsafe.Pointer) (selected bool) {
        return chansend(c, elem, false, getcallerpc())
    }
    ```

  - 对应 `OSELRECV2` 语句，`Cond` 块会被设置为一个临时变量
  - 之后会创建一个 [selectnbrecv()](https://github.com/golang/go/blob/go1.22.0/src/runtime/chan.go#L713) 函数的调用语句和赋值语句，并将其添加至 `Init` 块中
  - [selectnbrecv()](https://github.com/golang/go/blob/go1.22.0/src/runtime/chan.go#L713) 函数的第一个返回值会赋值给 `Cond` 块对应的临时变量

    ```go
    func walkSelectCases(cases []*ir.CommClause) []ir.Node {
        ...
        if ncas == 2 && dflt != nil {
            ...
            var cond ir.Node
            switch n.Op() {
            case ir.OSELRECV2:
                ...
                cond = typecheck.TempAt(base.Pos, ir.CurFunc, types.Types[types.TBOOL])
                fn := chanfn("selectnbrecv", 2, ch.Type())
                call := mkcall1(fn, fn.Type().ResultsTuple(), r.PtrInit(), elem, ch)
                as := ir.NewAssignListStmt(r.Pos(), ir.OAS2, []ir.Node{cond, n.Lhs[1]}, []ir.Node{call})
                r.PtrInit().Append(typecheck.Stmt(as))
            ...
            }

            r.Cond = typecheck.Expr(cond)
            ...
        }
        ...
    }

    func selectnbrecv(elem unsafe.Pointer, c *hchan) (selected, received bool) {
        return chanrecv(c, elem, false)
    }
    ```

#### 代码示例

- 原代码：

    ```go
    select { 
    case ch <- value:
        body
    default:
        body2
    }

    select { 
    case value, ok <- ch:
        body
    default:
        body2
    }
    ```

- 转化后的示例代码：

    ```go
    if selectnbsend(ch, value) {
        body
    } else {
        body2
    }

    if selected, ok = selectnbrecv(&value, c); selected {
        body
    } else {
        body2
    }
    ```

### Multi Op

#### 语句转换

- 初始化

  - 更新计数器 `ncas`，排除 `default` 的场景，并初始化发送语句计数器 `nsends` 和发送语句计数器 `nrecvs`
  - 初始化 Multi Op 场景下的 `init` 节点切片，并最终返回该切片
  - 创建长度为 `ncas` 的切片 `casorder`，用于存储 `case` 语句
  - 创建长度为 `ncas` 的数组 `selv`，用于存储 `case` 语句在运行时的结构体，并将其的初始化逻辑添加至 `init` 节点中
  - 创建一个长度为 `ncas` 二倍的数组，用于后续 [selectgo()](https://github.com/golang/go/blob/go1.22.0/src/runtime/select.go#L121C6-L121C14) 函数中排序使用（用来存储轮询顺序和锁定顺序）

    ```go
    func walkSelectCases(cases []*ir.CommClause) []ir.Node {
        ...
        if dflt != nil {
            ncas--
        }
        casorder := make([]*ir.CommClause, ncas)
        nsends, nrecvs := 0, 0

        var init []ir.Node

        // generate sel-struct
        base.Pos = sellineno
        selv := typecheck.TempAt(base.Pos, ir.CurFunc, types.NewArray(scasetype(), int64(ncas)))
        init = append(init, typecheck.Stmt(ir.NewAssignStmt(base.Pos, selv, nil)))

        // No initialization for order; runtime.selectgo is responsible for that.
        order := typecheck.TempAt(base.Pos, ir.CurFunc, types.NewArray(types.Types[types.TUINT16], 2*int64(ncas)))
        ...
        return init
    }
    ```

<br>

- 注册 `case` 节点

  - 将 `case` 语句的初始化添加在 `init` 节点中
  - 过滤 `default` 语句

    ```go
    func walkSelectCases(cases []*ir.CommClause) []ir.Node {
        ...
        // register cases
        for _, cas := range cases {
            ir.SetPos(cas)

            init = append(init, ir.TakeInit(cas)...)

            n := cas.Comm
            if n == nil { // default:
                continue
            }
            ...
        }
        ...
    }
    ```

  - 根据 `OSEND` 和 `OSELRECV2` 节点，设置对应的索引 `i`，channel `c`，发送或接收的元素 `elem`，计数器 `sends` 或 `nrecvs`
  - 其中对应于索引，发送语句正排，接收语句倒排

    ```go
    func walkSelectCases(cases []*ir.CommClause) []ir.Node {
        ...
        // register cases
        for _, cas := range cases {
            ...
            var i int
            var c, elem ir.Node
            switch n.Op() {
            default:
                base.Fatalf("select %v", n.Op())
            case ir.OSEND:
                n := n.(*ir.SendStmt)
                i = nsends
                nsends++
                c = n.Chan
                elem = n.Value
            case ir.OSELRECV2:
                n := n.(*ir.AssignListStmt)
                nrecvs++
                i = ncas - nrecvs
                recv := n.Rhs[0].(*ir.UnaryExpr)
                c = recv.X
                elem = n.Lhs[0]
            }
            ...
        }
        ...
    }
    ```

  - 将 `case` 语句更新至 `casorder` 数组中索引所在的位置
  - 将 channel `c` 和数据元素 `elem` 更新至 `selv` 数组中索引所在位置的 `case` 结构体中

    ```go
    func walkSelectCases(cases []*ir.CommClause) []ir.Node {
        ...
        // register cases
        for _, cas := range cases {
            ...
            casorder[i] = cas

            setField := func(f string, val ir.Node) {
                r := ir.NewAssignStmt(base.Pos, ir.NewSelectorExpr(base.Pos, ir.ODOT, ir.NewIndexExpr(base.Pos, selv, ir.NewInt(base.Pos, int64(i))), typecheck.Lookup(f)), val)
                init = append(init, typecheck.Stmt(r))
            }

            c = typecheck.ConvNop(c, types.Types[types.TUNSAFEPTR])
            setField("c", c)
            if !ir.IsBlank(elem) {
                elem = typecheck.ConvNop(elem, types.Types[types.TUNSAFEPTR])
                setField("elem", elem)
            }
            ...
        }
        ...
    }
    ```

  - 校验计数器是否正确

    ```go
    func walkSelectCases(cases []*ir.CommClause) []ir.Node {
        ...
        if nsends+nrecvs != ncas {
            base.Fatalf("walkSelectCases: miscount: %v + %v != %v", nsends, nrecvs, ncas)
        }
        ...
    }
    ```

<br>

- 执行 [selectgo()](https://github.com/golang/go/blob/go1.22.0/src/runtime/select.go#L121) 函数，用于确认最终选中的 `case` 语句

  - 创建一条新的赋值语句，其中左值为临时变量 `chosen` 和 `recvOK`，右值为 [selectgo()](https://github.com/golang/go/blob/go1.22.0/src/runtime/select.go#L121) 函数调用
    - `chosen` 用于接收最终被选中的 `case` 语句的索引
    - `recvOK` 用于表示接收操作是否成功
    - `fnInit` 用于存储调用函数前的初始化代码，编译器会做一些优化和 debug 功能
  - 将 `fnInit` 相关语句以及赋值语句 `r` 添加至 `init` 列表中

    ```go
    func walkSelectCases(cases []*ir.CommClause) []ir.Node {
        ...
        // run the select
        base.Pos = sellineno
        chosen := typecheck.TempAt(base.Pos, ir.CurFunc, types.Types[types.TINT])
        recvOK := typecheck.TempAt(base.Pos, ir.CurFunc, types.Types[types.TBOOL])
        r := ir.NewAssignListStmt(base.Pos, ir.OAS2, nil, nil)
        r.Lhs = []ir.Node{chosen, recvOK}
        fn := typecheck.LookupRuntime("selectgo")
        var fnInit ir.Nodes
        r.Rhs = []ir.Node{mkcall1(fn, fn.Type().ResultsTuple(), &fnInit, bytePtrToIndex(selv, 0), bytePtrToIndex(order, 0), pc0, ir.NewInt(base.Pos, int64(nsends)), ir.NewInt(base.Pos, int64(nrecvs)), ir.NewBool(base.Pos, dflt == nil))}
        init = append(init, fnInit...)
        init = append(init, typecheck.Stmt(r))
        ...
    }
    ```

  - [selectgo()](https://github.com/golang/go/blob/go1.22.0/src/runtime/select.go#L121) 函数内部会确认各 `case` 处理的优先级，以及通过循环，等待处理完成

    ```go
    // selectgo returns the index of the chosen scase, which matches the
    // ordinal position of its respective select{recv,send,default} call.
    // Also, if the chosen scase was a receive operation, it reports whether
    // a value was received.
    func selectgo(cas0 *scase, order0 *uint16, pc0 *uintptr, nsends, nrecvs int, block bool) (int, bool) {
        ...
    }
    ```

<br>

- 定义分发函数 `dispatch`

  - 定义节点数组 `list`，用于存储 `case` 语句最终执行的代码块
  - 如果 `case` 语句是 `OSELRECV2` 操作节点，则将其转为赋值语句，如果第二个返回值变量不为空，则将 `recvOK` 的值赋值给 `n.Lhs[1]`
  - 将 `case` 语句中的 `body` 代码块都添加至 list 中
  - 额外向 list 中添加一条 `break` 语句

    ```go
    func walkSelectCases(cases []*ir.CommClause) []ir.Node {
        ...
        // dispatch cases
        dispatch := func(cond ir.Node, cas *ir.CommClause) {
            var list ir.Nodes

            if n := cas.Comm; n != nil && n.Op() == ir.OSELRECV2 {
                n := n.(*ir.AssignListStmt)
                if !ir.IsBlank(n.Lhs[1]) {
                    x := ir.NewAssignStmt(base.Pos, n.Lhs[1], recvOK)
                    list.Append(typecheck.Stmt(x))
                }
            }

            list.Append(cas.Body.Take()...)
            list.Append(ir.NewBranchStmt(base.Pos, ir.OBREAK, nil))
            ...
        }
        ...
    }
    ```

  - 构建最终执行的节点 `r`
    - 如果 `cond` 代码块存在，则创建一个条件语句，满足条件时执行 `list` 代码块中逻辑
    - 如果 `cond` 代码块不存在，则直接创建一个代码块语句，并执行 `list` 代码块中逻辑
  - 将 `r` 添加至 `init` 列表中

    ```go
    func walkSelectCases(cases []*ir.CommClause) []ir.Node {
        ...
        // dispatch cases
        dispatch := func(cond ir.Node, cas *ir.CommClause) {
            ...
            var r ir.Node
            if cond != nil {
                cond = typecheck.Expr(cond)
                cond = typecheck.DefaultLit(cond, nil)
                r = ir.NewIfStmt(base.Pos, cond, list, nil)
            } else {
                r = ir.NewBlockStmt(base.Pos, list)
            }

            init = append(init, r)
        }
        ...
    }
    ```

<br>

- 通过分化函数，转化所有 `case` 语句与 default 语句

  - 如果存在 default 语句，则进行转化，其中 `cond` 对应的逻辑为 `chosen < 0`

    ```go
    func walkSelectCases(cases []*ir.CommClause) []ir.Node {
        ...
        if dflt != nil {
            ir.SetPos(dflt)
            dispatch(ir.NewBinaryExpr(base.Pos, ir.OLT, chosen, ir.NewInt(base.Pos, 0)), dflt)
        }
        ...
    }
    ```

  - 遍历转化 `case` 语句，其中 `cond` 对应的逻辑为 `chosen == i`
  - 如果 i 为最后一个索引，即 `len(casorder)-1`，则不指定 `cond` 代码块

    ```go
    func walkSelectCases(cases []*ir.CommClause) []ir.Node {
        ...
        for i, cas := range casorder {
            ir.SetPos(cas)
            if i == len(casorder)-1 {
                dispatch(nil, cas)
                break
            }
            dispatch(ir.NewBinaryExpr(base.Pos, ir.OEQ, chosen, ir.NewInt(base.Pos, int64(i))), cas)
        }
        ...
    }
    ```

<br>

- 返回转化结果

  - 上述所有逻辑处理完成后，返回最终转化的节点列表

    ```go
    func walkSelectCases(cases []*ir.CommClause) []ir.Node {
        ...
        return init
    }
    ```

#### 代码示例

- 原代码：

    ```go
    select { 
    case ch <- value1:
        body1
    case value2, ok <- ch2:
        body2
    case value3, _ <- ch3:
        body3
    default:
        body4
    }
    ```

- 转化后的示例代码：

    ```go
    selv := [3]scase{}
    order := [6]uint16
    for i, cas := range cases {
        c := scase{}
        c.kind = ...
        c.elem = ...
        c.c = ...
    }
    chosen, revcOK := selectgo(selv, order, 3)
    if chosen < 0 {
        body4
        break
    }
    if chosen == 0 {
        ...
        body1
        break
    }
    if chosen == 1 {
        ...
        body3
        break
    }
    if chosen == 2 {
        ...
        ok = revcOK
        body2
        break
    }
    ```

对于转化后的代码：

- `case` 语句在 `selv` 切片中对应的索引值，发送语句正排，接收语句倒排
- 发送和接收操作，全部在 `selectgo()` 函数中进行处理

### [selectgo()](https://github.com/golang/go/blob/go1.22.0/src/runtime/select.go#L121)

- 初始化

  - 限制 `case` 的最大数量为 `1<<16`，即 65535
  - 声明一些重要变量
    - `ncases`：case 总数
    - `scases`：case 的切片
    - `pollorder`：channel 的轮询顺序
    - `lockorder`：channel 的锁定顺序

    ```go
    func selectgo(cas0 *scase, order0 *uint16, pc0 *uintptr, nsends, nrecvs int, block bool) (int, bool) {
        // NOTE: In order to maintain a lean stack size, the number of scases
        // is capped at 65536.
        cas1 := (*[1 << 16]scase)(unsafe.Pointer(cas0))
        order1 := (*[1 << 17]uint16)(unsafe.Pointer(order0))

        ncases := nsends + nrecvs
        scases := cas1[:ncases:ncases]
        pollorder := order1[:ncases:ncases]
        lockorder := order1[ncases:][:ncases:ncases]
        // NOTE: pollorder/lockorder's underlying array was not zero-initialized by compiler.
    
        ...
    }
    ```

  - 随机交换 `pollorder` 中的元素，生成随机的轮询顺序
    - 同时优化下 `case` 的数量，即 `norder`，排除不存在 `channel` 的 case
    - 交换时，通过 [cheaprandn()](https://github.com/golang/go/blob/go1.22.0/src/runtime/rand.go#L222) 函数随机生成一个范围为 $[0, norder+1)$ 的整数

    ```go
    func selectgo(cas0 *scase, order0 *uint16, pc0 *uintptr, nsends, nrecvs int, block bool) (int, bool) {
        ...
        // generate permuted order
        norder := 0
        for i := range scases {
            cas := &scases[i]

            // Omit cases without channels from the poll and lock orders.
            if cas.c == nil {
                cas.elem = nil // allow GC
                continue
            }

            j := cheaprandn(uint32(norder + 1))
            pollorder[norder] = pollorder[j]
            pollorder[j] = uint16(i)
            norder++
        }
        pollorder = pollorder[:norder]
        lockorder = lockorder[:norder]
        ...
    }
    ```

  - 将 `lockorder` 构建为一个最大堆
    - 各元素间通过 `c.sortkey()`，即 channel 对应的地址进行比较
    - 在外层循环中，每次循环结束，`lockorder` 中的前 `i` 个元素会被调整为一个极大堆
    - 在内层循环中，每次循环会判断当前元素 `j` 的父节点，即 `(j-1)/2`，与 `i` 所对应的元素的大小，若 `j` 小于 `i`，则交换元素位置，并继续向上寻找 `j` 的父元素做比较，直至找到大于 `i` 的元素或找到堆顶元素
      - 内层循环中，每次比较理论上应该交换 `j` 节点与其父节点的值，将 `i` 节点的值一路交换上去
      - 但是 `j` 的父节点再下次循环中，仍然可能和下一个父节点中的值做交换，所以每次循环中仅将 `j` 的父节点的值赋给 `j` 节点，在循环结束后，再给 `j` 节点赋值正确的值，即 `i` 节点的值

    ```go
    func selectgo(cas0 *scase, order0 *uint16, pc0 *uintptr, nsends, nrecvs int, block bool) (int, bool) {
        ...
        // sort the cases by Hchan address to get the locking order.
        // simple heap sort, to guarantee n log n time and constant stack footprint.
        for i := range lockorder {
            j := i
            // Start with the pollorder to permute cases on the same channel.
            c := scases[pollorder[i]].c
            for j > 0 && scases[lockorder[(j-1)/2]].c.sortkey() < c.sortkey() {
                k := (j - 1) / 2
                lockorder[j] = lockorder[k]
                j = k
            }
            lockorder[j] = pollorder[i]
        }
        ...
    }
    ```

  - 实现堆排序，将 `lockorder` 中的元素按照 channel 的地址升序进行排列
    > 每次循环将堆顶与堆在数组中的末尾元素交换，使得数组末端有序
    > 再将非有序的部分重新调整结构，使其满足最大堆的形式
    > 重复以上流程直至最终数组有序
    - 每次先将 `i` 的值，更新为索引为 `0` 的值，即当前堆中的最大值
    - 通过循环，将切片中 $[1, i]$ 部分的元素，在 $[0, i-1]$ 范围内重新排序为最大堆
      - 先将 `k` 更新为 `j` 节点的左子树，如果此时 `k` 的右侧元素已经为排序后的部分，则结束循环
      - 如果此时 `k+1` 即右子树的值更大且不属于有序的部分，则通过 `k++` 将 `k` 更新为 `j` 的右子树
      - 如果此时 `k` 的值大于 `i` 的值，则交换 `j` 与 `k` 的值，将 `j` 更新为子节点 `k`，并继续循环
      - 出于同样的原因，在交换 `j` 与 `k` 的值时，仅仅将 `k` 的值赋给 `j`，在最终循环结束后，将 `i` 的值赋值给 `j`，完成数据交换

    ```go
    func selectgo(cas0 *scase, order0 *uint16, pc0 *uintptr, nsends, nrecvs int, block bool) (int, bool) {
        ...
        for i := len(lockorder) - 1; i >= 0; i-- {
            o := lockorder[i]
            c := scases[o].c
            lockorder[i] = lockorder[0]
            j := 0
            for {
                k := j*2 + 1
                if k >= i {
                    break
                }
                if k+1 < i && scases[lockorder[k]].c.sortkey() < scases[lockorder[k+1]].c.sortkey() {
                    k++
                }
                if c.sortkey() < scases[lockorder[k]].c.sortkey() {
                    lockorder[j] = lockorder[k]
                    j = k
                    continue
                }
                break
            }
            lockorder[j] = o
        }
        ...
    }
    ```

  - 通过 [sellock()](https://github.com/golang/go/blob/go1.22.0/src/runtime/select.go#L33) 函数，按照上述确定的 `lockorder` 中的顺序，对 `scases` 中的 `case` 语句进行加锁处理
    - [sellock()](https://github.com/golang/go/blob/go1.22.0/src/runtime/select.go#L33) 按照 channel 的地址有序排列后，在加锁时可以跳过相同的实例，避免重复加锁导致死锁
    - 相同的 channel 实例，在第一次访问时执行加锁逻辑，避免中途被释放，后续访问异常

    ```go
    func selectgo(cas0 *scase, order0 *uint16, pc0 *uintptr, nsends, nrecvs int, block bool) (int, bool) {
        ...
        // lock all the channels involved in the select
        sellock(scases, lockorder)
        ...
    }

    func sellock(scases []scase, lockorder []uint16) {
        var c *hchan
        for _, o := range lockorder {
            c0 := scases[o].c
            if c0 != c {
                c = c0
                lock(&c.lock)
            }
        }
    }
    ```

<br>

- 按照 `pollorder` 的顺序进行遍历，查找正在等待的 channel，即可以执行发送或接收语句的 channel

  - 判断接收语句能否执行
    - 发送和接收语句在数组中分别为正排和倒排，所以 `casi >= nsends` 说明为接收语句
    - 如果 channel `c` 的发送者等待队列 `sendq` 不为空，执行 `recv` 逻辑，唤醒休眠的 goroutine 并获取数据
    - 如果 channel `c` 中的数据量大于 0，执行 `bufrecv` 逻辑，从缓冲区中获取数据
    - 如果 channel `c` 已经被关闭，执行 `rclose` 逻辑，从关闭的 channel 中读取数据

    ```go
    func selectgo(cas0 *scase, order0 *uint16, pc0 *uintptr, nsends, nrecvs int, block bool) (int, bool) {
        ...
        for _, casei := range pollorder {
            casi = int(casei)
            cas = &scases[casi]
            c = cas.c

            if casi >= nsends {
                sg = c.sendq.dequeue()
                if sg != nil {
                    goto recv
                }
                if c.qcount > 0 {
                    goto bufrecv
                }
                if c.closed != 0 {
                    goto rclose
                }
            } else {
                ...
            }
        }
        ...
    }
    ```

  - 判断发送语句能否执行
    - `casi >= nsends` 不满足时，即 `casi < nsends`，说明为接收语句
    - 如果 channel `c` 已经被关闭，执行 `sclose` 逻辑，向关闭的 channel 中发送数据
    - 如果 channel `c` 的接收者等待队列 `recvq` 不为空，执行 `send` 逻辑，唤醒休眠的 goroutine 并发送数据
    - 如果 channel `c` 中的缓冲区还有剩余，执行 `bufsend` 逻辑，向缓冲区中写入数据

    ```go
    func selectgo(cas0 *scase, order0 *uint16, pc0 *uintptr, nsends, nrecvs int, block bool) (int, bool) {
        ...
        for _, casei := range pollorder {
            casi = int(casei)
            cas = &scases[casi]
            c = cas.c

            if casi >= nsends {
                ...
            } else {
                if c.closed != 0 {
                    goto sclose
                }
                sg = c.recvq.dequeue()
                if sg != nil {
                    goto send
                }
                if c.qcount < c.dataqsiz {
                    goto bufsend
                }
            }
        }
        ...
    }
    ```

  - 如果是非阻塞调用，则选中 `default` 语句，即 `casi = -1`，解锁所有 channel 并执行 `retc` 逻辑，结束函数调用
    - [selunlock()](https://github.com/golang/go/blob/go1.22.0/src/runtime/select.go#L44) 函数用于解锁所有 `case` 语句中的 channel 实例，按照 `lockorder` 的逆序进行解锁，与加锁时顺序相反
    - 相同的 channel 实例，在最后一次访问时解锁，避免提前释放导致后续访问异常

    ```go
    func selectgo(cas0 *scase, order0 *uint16, pc0 *uintptr, nsends, nrecvs int, block bool) (int, bool) {
        ...
        if !block {
            selunlock(scases, lockorder)
            casi = -1
            goto retc
        }
        ...
    }

    func selunlock(scases []scase, lockorder []uint16) {
        for i := len(lockorder) - 1; i >= 0; i-- {
            c := scases[lockorder[i]].c
            if i > 0 && c == scases[lockorder[i-1]].c {
                continue // will unlock it on the next iteration
            }
            unlock(&c.lock)
        }
    }
    ```

<br>

- 若不存在可立即执行的语句，则将 `case` 语句中的 channel 绑定在当前的 goroutine 上，并添加至对应的等待队列，等待唤醒

    ```go
    func selectgo(cas0 *scase, order0 *uint16, pc0 *uintptr, nsends, nrecvs int, block bool) (int, bool) {
        ...
        // pass 2 - enqueue on all chans
        gp = getg()
        nextp = &gp.waiting
        for _, casei := range lockorder {
            ... // 一些赋值语句，构建 channel 等待队列的 sudog 结构体
            if casi < nsends {
                c.sendq.enqueue(sg)
            } else {
                c.recvq.enqueue(sg)
            }
        }
        ...
    }
    ```

  - 通过 `gopark()` 函数，挂起当前 goroutine

    ```go
    func selectgo(cas0 *scase, order0 *uint16, pc0 *uintptr, nsends, nrecvs int, block bool) (int, bool) {
        ...
        // wait for someone to wake us up
        gp.param = nil
        ...
        sellock(scases, lockorder)
        ...
    }
    ```

<br>

- 唤醒后，查找对应的 `sudog` 实例，并确定可执行的 `case` 语句
  - 参数初始化
    - `sg`：goroutine 被唤醒后所需要处理的 `sudog` 实例
    - `casi`：最终返回的被选中的 `case` 的索引
    - `cas`：选中的 `case` 语句
    - `caseSuccess`：case 语句执行结果

   ```go
    func selectgo(cas0 *scase, order0 *uint16, pc0 *uintptr, nsends, nrecvs int, block bool) (int, bool) {
        ...
        sg = (*sudog)(gp.param)
        gp.param = nil

        // pass 3 - dequeue from unsuccessful chans
        casi = -1
        cas = nil
        caseSuccess = false
        ...
    }
    ```

  - 按照 `lockorder` 顺序，遍历并找到被唤醒的 `sudog` 实例对应的 `case` 语句

    ```go
    func selectgo(cas0 *scase, order0 *uint16, pc0 *uintptr, nsends, nrecvs int, block bool) (int, bool) {
        ...
        for _, casei := range lockorder {
            k = &scases[casei]
            if sg == sglist {
                // sg has already been dequeued by the G that woke us up.
                casi = int(casei)
                cas = k
                caseSuccess = sglist.success
            } else {
                ...
            }
            ...
        }
        ...
    }
    ```

  - 对于未选中的语句，将其从 channel 的等待者队列中移除

    ```go
    func selectgo(cas0 *scase, order0 *uint16, pc0 *uintptr, nsends, nrecvs int, block bool) (int, bool) {
        ...
        for _, casei := range lockorder {
            k = &scases[casei]
            if sg == sglist {
                ...
            } else {
                c = k.c
                if int(casei) < nsends {
                    c.sendq.dequeueSudoG(sglist)
                } else {
                    c.recvq.dequeueSudoG(sglist)
                }
            }
            ...
        }
        ...
    }
    ```

  - 最终判断状态，并结束函数调用
    - 如果是 send 语句，且执行失败，则执行 `sclose` 逻辑，如果是 recvice 语句，则更新结果为成功
    - 默认逻辑下，解锁所有 channel 实例，并执行 `retc` 逻辑，结束函数调用

    ```go
    func selectgo(cas0 *scase, order0 *uint16, pc0 *uintptr, nsends, nrecvs int, block bool) (int, bool) {
        ...
        if casi < nsends {
            if !caseSuccess {
                goto sclose
            }
        } else {
            recvOK = caseSuccess
        }


        selunlock(scases, lockorder)
        goto retc
        ...
    }
    ```

<br>

- `goto` 语句对应的代码块逻辑，其中读取、发送等逻辑，与 channel 本身的代码逻辑类似

  - `bufrecv`：从缓冲区中读取数据

    ```go
    func selectgo(cas0 *scase, order0 *uint16, pc0 *uintptr, nsends, nrecvs int, block bool) (int, bool) {
        ...
    bufrecv:
        // can receive from buffer
        recvOK = true
        qp = chanbuf(c, c.recvx)
        if cas.elem != nil {
            typedmemmove(c.elemtype, cas.elem, qp)
        }
        typedmemclr(c.elemtype, qp)
        c.recvx++
        if c.recvx == c.dataqsiz {
            c.recvx = 0
        }
        c.qcount--
        selunlock(scases, lockorder)
        goto retc
        ...
    }
    ```

  - `bufsend`：向缓冲区中发送数据

    ```go
    func selectgo(cas0 *scase, order0 *uint16, pc0 *uintptr, nsends, nrecvs int, block bool) (int, bool) {
        ...
    bufsend:
        // can send to buffer
        typedmemmove(c.elemtype, chanbuf(c, c.sendx), cas.elem)
        c.sendx++
        if c.sendx == c.dataqsiz {
            c.sendx = 0
        }
        c.qcount++
        selunlock(scases, lockorder)
        goto retc
        ...
    }
    ```

  - `recv`：唤醒休眠的发送者，并接收数据

    ```go
    func selectgo(cas0 *scase, order0 *uint16, pc0 *uintptr, nsends, nrecvs int, block bool) (int, bool) {
        ...
    recv:
        // can receive from sleeping sender (sg)
        recv(c, sg, cas.elem, func() { selunlock(scases, lockorder) }, 2)
        recvOK = true
        goto retc
        ...
    }
    ```

  - `rclose`：从关闭的 channel 中读取数据

    ```go
    func selectgo(cas0 *scase, order0 *uint16, pc0 *uintptr, nsends, nrecvs int, block bool) (int, bool) {
        ...
    rclose:
        // read at end of closed channel
        selunlock(scases, lockorder)
        recvOK = false
        if cas.elem != nil {
            typedmemclr(c.elemtype, cas.elem)
        }
        goto retc
        ...
    }
    ```

  - `send`：唤醒休眠的接收者，并发送数据

    ```go
    func selectgo(cas0 *scase, order0 *uint16, pc0 *uintptr, nsends, nrecvs int, block bool) (int, bool) {
        ...
    send:
        // can send to a sleeping receiver (sg)
        send(c, sg, cas.elem, func() { selunlock(scases, lockorder) }, 2)
        goto retc
        ...
    }
    ```

  - `retc`：结束函数调用，并返回对应的值

    ```go
    func selectgo(cas0 *scase, order0 *uint16, pc0 *uintptr, nsends, nrecvs int, block bool) (int, bool) {
        ...
    retc:
        return casi, recvOK
        ...
    }
    ```

  - `sclose`：向关闭的 channel 中发送数据，解锁所有 channel 后触发 panic

    ```go
    func selectgo(cas0 *scase, order0 *uint16, pc0 *uintptr, nsends, nrecvs int, block bool) (int, bool) {
        ...
    sclose:
        // send on closed channel
        selunlock(scases, lockorder)
        panic(plainError("send on closed channel"))
        ...
    }
    ```

## 参考

- <https://draveness.me/golang/docs/part2-foundation/ch05-keyword/golang-select/>
- <https://www.topgoer.com/%E6%B5%81%E7%A8%8B%E6%8E%A7%E5%88%B6/%E6%9D%A1%E4%BB%B6%E8%AF%AD%E5%8F%A5select.html>
