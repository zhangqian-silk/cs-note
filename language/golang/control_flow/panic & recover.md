# Panic & Recover

`panic` 是 golang 中常见的异常之一，再调用 `panic` 后，会从当前位置结束函数调用，然后递归执行所有调用方中的 `defer` 语句，并最终造成程序崩溃。

`recover` 是专门针对于 `panic` 使用的函数，可以捕获 `panic` 异常，并防止程序崩溃。因为 `panic` 发生后，后续仅会执行 `defer` 语句，所以 `recover` 也只能在 `defer` 的作用域内使用。

## 特性

### 跨 goroutine

在想要对 `panic` 进行保护时，需要在 `defer` 作用域内使用 `recover` 函数。

但是 `defer` 关键字会与 goroutine 相关联，无法跨 goroutine 执行，相对应的，`recover` 函数也无法跨 goroutine 实现保护。

所以在新建 goroutine 时，应该需要额外关注，是否需要在入口函数处，重新通过 `defer` 关键字来使用 `recover` 函数。

## 底层实现

### 数据结构

`panic` 流程会通过 [_panic](https://github.com/golang/go/blob/go1.22.0/src/runtime/runtime2.go#L1047) 结构体进行控制。

```go
type _panic struct {
    argp unsafe.Pointer // pointer to arguments of deferred call run during panic; cannot move - known to liblink
    arg  any            // argument to panic
    link *_panic        // link to earlier panic

    // startPC and startSP track where _panic.start was called.
    startPC uintptr
    startSP unsafe.Pointer

    // The current stack frame that we're running deferred calls for.
    sp unsafe.Pointer
    lr uintptr
    fp unsafe.Pointer

    // retpc stores the PC where the panic should jump back to, if the
    // function last returned by _panic.next() recovers the panic.
    retpc uintptr

    // Extra state for handling open-coded defers.
    deferBitsPtr *uint8
    slotsPtr     unsafe.Pointer

    recovered   bool // whether this panic has been recovered
    goexit      bool
    deferreturn bool
}
```

### 节点替换

在编译的节点替换环节，[walkStmt()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/walk/stmt.go#L15) 函数、[walkExpr()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/walk/expr.go#L29) 函数和 [walkExpr1()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/walk/expr.go#L83) 函数，会对 `panic` 语句字与 `recover` 语句进行处理，转为真正的处理函数。

```go
func walkStmt(n ir.Node) ir.Node {
    ...
    switch n.Op() {
    case ...,
        ir.OPANIC,
        ir.ORECOVERFP:
        ...
        n = walkExpr(n, &init)
        ...
    ...
    }
}

func walkExpr(n ir.Node, init *ir.Nodes) ir.Node {
    ...
    n = walkExpr1(n, init)
    ...
}
```

<br>

在运行时：

- `panic` 语句会转化为 [gopanic()](https://github.com/golang/go/blob/go1.22.0/src/runtime/panic.go#L720) 函数的调用

    ```go
    func walkExpr1(n ir.Node, init *ir.Nodes) ir.Node {
        switch n.Op() {
        case ir.OPANIC:
            n := n.(*ir.UnaryExpr)
            return mkcall("gopanic", nil, init, n.X)
        ...
        }
    }
    ```

- `revocer` 语句会通过 [walkRecoverFP()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/walk/builtin.go#L680C6-L680C19) 函数 ，转化为 [gorecover()](https://github.com/golang/go/blob/go1.22.0/src/runtime/panic.go#L984) 的函数调用。

    ```go
    func walkExpr1(n ir.Node, init *ir.Nodes) ir.Node {
        switch n.Op() {
        case ir.ORECOVERFP:
            return walkRecoverFP(n.(*ir.CallExpr), init)
        ...
        }
    }

    func walkRecoverFP(nn *ir.CallExpr, init *ir.Nodes) ir.Node {
        return mkcall("gorecover", nn.Type(), init, walkExpr(nn.Args[0], init))
    }
    ```

### Panic

#### [gopanic()](https://github.com/golang/go/blob/go1.22.0/src/runtime/panic.go#L720)

核心处理逻辑如下所示：

- 判断传入参数并进行兜底赋值
  - 默认逻辑为 `PanicNilError`

    ```go
    func gopanic(e any) {
        if e == nil {
            if debug.panicnil.Load() != 1 {
                e = new(PanicNilError)
            } else {
                panicnil.IncNonDefault()
            }
        }
        ...
    }
    ```

<br>

- 判断当前 goroutine 的一些状态，这部分特殊场景会直接结束程序

  - `panic on system stack`：在系统调用时触发 `panic`

    ```go
    func gopanic(e any) {
        ...
        gp := getg()
        if gp.m.curg != gp {
            print("panic: ")
            printany(e)
            print("\n")
            throw("panic on system stack")
        }
        ...
    }
    ```

  - `panic during malloc`：在内存分配时触发 `panic`

    ```go
    func gopanic(e any) {
        ...
        if gp.m.mallocing != 0 {
            print("panic: ")
            printany(e)
            print("\n")
            throw("panic during malloc")
        }
        ...
    }
    ```

  - `panic during preemptoff`：在禁止抢占时时触发 `panic`

    ```go
    func gopanic(e any) {
        ...
        if gp.m.preemptoff != "" {
            print("panic: ")
            printany(e)
            print("\n")
            print("preempt off reason: ")
            print(gp.m.preemptoff)
            print("\n")
            throw("panic during preemptoff")
        }
        ...
    }
    ```

  - `panic holding locks`：在加锁时触发 `panic`

    ```go
    func gopanic(e any) {
        ...
        if gp.m.locks != 0 {
            print("panic: ")
            printany(e)
            print("\n")
            throw("panic holding locks")
        }
        ...
    }
    ```

<br>

- 正常链路下，函数主体逻辑

  - 创建新的 [_panic](https://github.com/golang/go/blob/go1.22.0/src/runtime/runtime2.go#L1047) 结构体，并将计数器 `runningPanicDefers` 加一
  - 通过 [start()](https://github.com/golang/go/blob/go1.22.0/src/runtime/panic.go#L786) 完成初始化逻辑
  - 通过 [nextDefer()](https://github.com/golang/go/blob/go1.22.0/src/runtime/panic.go#L831) 遍历所有 `defer` 语句并执行
  - 通过 [fatalpanic()](https://github.com/golang/go/blob/go1.22.0/src/runtime/panic.go#L1217) 函数退出程序

    ```go
    func gopanic(e any) {
        ...
        var p _panic
        p.arg = e

        runningPanicDefers.Add(1)

        p.start(getcallerpc(), unsafe.Pointer(getcallersp()))
        for {
            fn, ok := p.nextDefer()
            if !ok {
                break
            }
            fn()
        }

        preprintpanics(&p)

        fatalpanic(&p)   // should not return
        *(*int)(nil) = 0 // not reached
    }
    ```

#### [nextDefer()](https://github.com/golang/go/blob/go1.22.0/src/runtime/panic.go#L831)

核心处理逻辑如下所示：

- 预处理

  - 判断触发的 `panic` 与 goroutine 中绑定的 `panic` 实例是否一致
  - 判断 `recovered` 属性，如果在执行 `defer` 语句时曾触发过 `recover()` 函数，则改为调用 [recovery()](https://github.com/golang/go/blob/go1.22.0/src/runtime/panic.go#L1063) 函数，执行恢复逻辑

    ```go
    func (p *_panic) nextDefer() (func(), bool) {
        gp := getg()

        if !p.deferreturn {
            if gp._panic != p {
                throw("bad panic stack")
            }

            if p.recovered {
                mcall(recovery) // does not return
                throw("recovery failed")
            }
        }

        // The assembler adjusts p.argp in wrapper functions that shouldn't
        // be visible to recover(), so we need to restore it each iteration.
        p.argp = add(p.startSP, sys.MinFrameSize)
        ...
    }
    ```

  - 由编译器插入的 [deferreturn()](https://github.com/golang/go/blob/go1.22.0/src/runtime/panic.go#L592) 函数中会设置 `deferreturn` 属性，此时 `defer` 语句并非由 `panic` 进行触发，无需进行相关判断

    ```go
    func deferreturn() {
        var p _panic
        p.deferreturn = true

        p.start(getcallerpc(), unsafe.Pointer(getcallersp()))
        for {
            fn, ok := p.nextDefer()
            if !ok {
                break
            }
            fn()
        }
    }
    ```

<br>

- 获取 `defer` 语句所应执行的函数

  - 通过开放编码实现的 `defer` 语句，会将原本需要在 `defer` 函数中执行的逻辑，展开在所属函数内，避免了创建 `_defer` 对象、注册链表等操作
  - 在执行 `panic` 逻辑时，则需要根据相关字段，即 `deferBitsPtr` 和 `slotsPtr`，定位到需要执行的函数

    ```go
    func (p *_panic) nextDefer() (func(), bool) {
        ...
        for {
            for p.deferBitsPtr != nil {
                bits := *p.deferBitsPtr

                if bits == 0 {
                    p.deferBitsPtr = nil
                    break
                }

                // Find index of top bit set.
                i := 7 - uintptr(sys.LeadingZeros8(bits))

                // Clear bit and store it back.
                bits &^= 1 << i
                *p.deferBitsPtr = bits

                return *(*func())(add(p.slotsPtr, i*goarch.PtrSize)), true
            }
            ...
        }
    }
    ```

  - 通过传统方式实现 `defer` 语句时，直接返回 `_defer` 结构体上存储的 `fn` 实例

    ```go
    func (p *_panic) nextDefer() (func(), bool) {
        for {
            ...
        Recheck:
            if d := gp._defer; d != nil && d.sp == uintptr(p.sp) {
                if d.rangefunc {
                    gp._defer = deferconvert(d)
                    goto Recheck
                }

                fn := d.fn
                d.fn = nil

                p.retpc = d.pc

                // Unlink and free.
                gp._defer = d.link
                freedefer(d)

                return fn, true
            }
            ...
        }
    }
    ```

  - 在未找到 `defer` 语句时，通过 [nextFrame()](https://github.com/golang/go/blob/go1.22.0/src/runtime/panic.go#L905) 函数去寻找下一个包含 `defer` 语句的调用者
    - 若成功找到，则重复上述判断逻辑
    - 若未找到，则结束调用

    ```go
    func (p *_panic) nextDefer() (func(), bool) {
        for {
            ...
            if !p.nextFrame() {
                return nil, false
            }
        }
    }
    ```

#### [gorecover()](https://github.com/golang/go/blob/go1.22.0/src/runtime/panic.go#L984)

函数整体逻辑比较简单，仅是将 goroutine 上当前绑定的 `_panic` 结构体中的 `recovered` 字段设置为 `true`，标记该 `panic` 已经被处理了，并将触发 `panic()` 函数时传入的参数，返回给调用者。

```go
func gorecover(argp uintptr) any {
    gp := getg()
    p := gp._panic
    if p != nil && !p.goexit && !p.recovered && argp == uintptr(p.argp) {
        p.recovered = true
        return p.arg
    }
    return nil
}
```

## 参考

- <https://draveness.me/golang/docs/part2-foundation/ch05-keyword/golang-panic-recover/>
