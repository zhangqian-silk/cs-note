# For-Range

针对于容器类型，即数组、切片、哈希表，可以通过 `for-range` 来遍历集合中所有元素，来替换传统的 for 循环，在使用上也更为简洁。

- 切片：

    ```go
    slice := []int{1, 2, 3, 4, 5}
    for index, value := range slice {
        fmt.Printf("索引: %d, 值: %d\n", index, value)
    }
    ```

- 哈希表：

    ```go
    myMap := map[string]int{
        "a": 10,
        "b": 20,
        "c": 30,
    }
    for key, value := range myMap {
        fmt.Printf("键: %s, 值: %d\n", key, value)
    }
    ```

## For 循环

对于最经典的 `for` 循环来说，在编译器中会构建为一个 [ForStmt](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/ir/stmt.go#L218) 结构体，其中 `init`、`Cond`、`Post`、`Body` 代表了 `for` 循环必备的四块代码块。

```go
type miniStmt struct {
    miniNode
    init Nodes
}

// A ForStmt is a non-range for loop: for Init; Cond; Post { Body }
type ForStmt struct {
    miniStmt
    Label        *types.Sym
    Cond         Node
    Post         Node
    Body         Nodes
    DistinctVars bool
}
```

<br>

在生成 SSA 代码时，即 [stmt()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/ssagen/ssa.go#L1431) 方法中，会真正构建 `for` 循环的执行逻辑：

- 针对 `OFOR` 节点，创建 `Cond`、`Body`、`Incr`、`End` 四个代码块
  - 四个代码块分别对应了 `for` 循环的特定逻辑，即 `for Ninit; Cond; Incr { Body }`

    ```go
    func (s *state) stmt(n ir.Node) {
        ...
        switch n.Op() {
        case ir.OFOR:
            // OFOR: for Ninit; Left; Right { Nbody }
            // cond (Left); body (Nbody); incr (Right)
            n := n.(*ir.ForStmt)
            base.Assert(!n.DistinctVars) // Should all be rewritten before escape analysis
            bCond := s.f.NewBlock(ssa.BlockPlain)
            bBody := s.f.NewBlock(ssa.BlockPlain)
            bIncr := s.f.NewBlock(ssa.BlockPlain)
            bEnd := s.f.NewBlock(ssa.BlockPlain)

            // ensure empty for loops have correct position; issue #30167
            bBody.Pos = n.Pos()
            ...
        ...
        }
    }
    ```

<br>

- 构建 `Cond` 代码块，并从当前的结束代码块跳转至 `Cond` 代码块
  - 如果 `Cond` 代码块存在，则根据其结果为 `true` 还是 `false`，分别跳转至 `Body` 和 `End` 代码块
  - 如果 `Cond` 代码块为空，说明循环的条件表达式始终为真，则跳转至 `Body` 代码块

    ```go
    func (s *state) stmt(n ir.Node) {
        ...
        switch n.Op() {
        case ir.OFOR:
            ...
            // first, jump to condition test
            b := s.endBlock()
            b.AddEdgeTo(bCond)

            // generate code to test condition
            s.startBlock(bCond)
            if n.Cond != nil {
                s.condBranch(n.Cond, bBody, bEnd, 1)
            } else {
                b := s.endBlock()
                b.Kind = ssa.BlockPlain
                b.AddEdgeTo(bBody)
            }
            ...
        ...
        }
    }
    ```

<br>

- 设置 `continue` 和 `break` 的目标块，并处理标签相关逻辑
  - 当触发 `continue` 语句时，跳转至 `Incr` 代码块，开始下一次循环
  - 当触发 `break` 语句时，跳转至 `End` 代码块，结束当前循环

    ```go
    func (s *state) stmt(n ir.Node) {
        ...
        switch n.Op() {
        case ir.OFOR:
            ...
            // set up for continue/break in body
            prevContinue := s.continueTo
            prevBreak := s.breakTo
            s.continueTo = bIncr
            s.breakTo = bEnd
            var lab *ssaLabel
            if sym := n.Label; sym != nil {
                // labeled for loop
                lab = s.label(sym)
                lab.continueTarget = bIncr
                lab.breakTarget = bEnd
            }
            ...
        ...
        }
    }
    ```

<br>

- 构建 `Body` 代码块，处理循环体内部逻辑
  - 上述设置的 `continue` 和 `break` 的目标代码块，将针对 `Body` 代码块中的逻辑生效

    ```go
    func (s *state) stmt(n ir.Node) {
        ...
        switch n.Op() {
        case ir.OFOR:
            ...
            // generate body
            s.startBlock(bBody)
            s.stmtList(n.Body)
            ...
        ...
        }
    }
    ```

<br>

- 恢复 `continue` 和 `break` 的目标代码块
  - `Body` 代码块相关的逻辑已经构建结束，需要恢复原本的设置，例如循环嵌套的场景

    ```go
    func (s *state) stmt(n ir.Node) {
        ...
        switch n.Op() {
        case ir.OFOR:
            ...
            // tear down continue/break
            s.continueTo = prevContinue
            s.breakTo = prevBreak
            if lab != nil {
                lab.continueTarget = nil
                lab.breakTarget = nil
            }
            ...
        ...
        }
    }
    ```

<br>

- 构建 `Incr` 代码块
  - 设置 `Body` 代码块（如果存在）跳转至 `Incr` 代码块
  - 设置 `Incr` 代码块（如果存在）跳转至 `Cond` 代码块

    ```go
    func (s *state) stmt(n ir.Node) {
        ...
        switch n.Op() {
        case ir.OFOR:
            ...
            // done with body, goto incr
            if b := s.endBlock(); b != nil {
                b.AddEdgeTo(bIncr)
            }

            // generate incr
            s.startBlock(bIncr)
            if n.Post != nil {
                s.stmt(n.Post)
            }
            if b := s.endBlock(); b != nil {
                b.AddEdgeTo(bCond)
                // It can happen that bIncr ends in a block containing only VARKILL,
                // and that muddles the debugging experience.
                if b.Pos == src.NoXPos {
                    b.Pos = bCond.Pos
                }
            }
            ...
        ...
        }
    }
    ```

<br>

- 构建 `End` 代码块，需要注意的是 `End` 代码块仅能通过以下两种方式跳转
  - 通过 `Cond` 代码块在 `false` 的分支下跳转
  - 通过 `Body` 代码块在 `break` 语句下跳转

    ```go
    func (s *state) stmt(n ir.Node) {
        ...
        switch n.Op() {
        case ir.OFOR:
            ...
            s.startBlock(bEnd)
        ...
        }
    }
    ```

## Range 循环

Range 循环会同时用到 `for` 和 `range` 两个关键字，在很多场景下，可以提供比 `for` 循环更好的可读性，编译器在编译期，会通过 [walkRange()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/walk/range.go#L40) 函数，将其转为 `for` 循环，并根据 `range` 对象的类型，采取不同的处理方案来构造最终的 `for` 循环所需的各种语句。

```go
func walkRange(nrange *ir.RangeStmt) ir.Node {
    ...
    nfor := ir.NewForStmt(nrange.Pos(), nil, nil, nil, nil, nrange.DistinctVars)
    ...
    var n ir.Node = nfor

    n = walkStmt(n)

    base.Pos = lno
    return n
}
```

<br>

对于 `ForStmt` 所需的 `Init`、`Cond`、`Post`、`Body`、`Label` 代码块，会在这里从 `RangeStmt` 语句中进行转化处理：

```go
func walkRange(nrange *ir.RangeStmt) ir.Node {
    ...
    nfor.SetInit(nrange.Init())
    nfor.Label = nrange.Label
    ...
    typecheck.Stmts(init)

    nfor.PtrInit().Append(init...)

    typecheck.Stmts(nfor.Cond.Init())

    nfor.Cond = typecheck.Expr(nfor.Cond)
    nfor.Cond = typecheck.DefaultLit(nfor.Cond, nil)
    nfor.Post = typecheck.Stmt(nfor.Post)
    typecheck.Stmts(body)
    nfor.Body.Append(body...)
    nfor.Body.Append(nrange.Body...)
    ...
}
```

<br>

其中根据 `RangeStme` 中遍历的元素的类型不同，会对 `Init`、`Cond` 和 `Post` 做不同的处理，还会根据 `v1` 和 `v2` 的取值不同，构造不同的 `Body` 语句：

```go
func walkRange(nrange *ir.RangeStmt) ir.Node {
    ...
    v1, v2 := nrange.Key, nrange.Value
    ...
    var body []ir.Node
    var init []ir.Node
    switch k := t.Kind(); {
    default:
        base.Fatalf("walkRange")
    case types.IsInt[k]:
        ...
    case k == types.TARRAY, k == types.TSLICE, k == types.TPTR: // TPTR is pointer-to-array
        ...
    case k == types.TMAP:
        ...
    case k == types.TCHAN:
        ...
    case k == types.TSTRING:
        ...
    }
    ...
}
```

### 整数

#### 代码示例

通过对整数遍历，可以轻易构建一个指定次数的循环：

```go
a := 5
for v1 := range a {
    block(v1)
}
```

<br>

在经过编译器处理后，会转化为如下伪代码：

```go
a := 5
var v1 a.type
for hv1, hn := 0, a; hv1 < hn; hv1 += 1 {
    v1 = hv1
    block(v1)
}
```

#### 代码分析

- 初始化临时变量

  - 初始化迭代变量 `hv1`，类型为 `t` 本身，即每次迭代时的索引 `index`，并将 `hv1 = 0` 添加在 `ForStmt` 语句的 `Init` 代码块中
  - 初始化迭代长度 `hn`，即 `a` 本身，并将 `hn = a` 添加在 `ForStmt` 语句的 `Init` 代码块中

    ```go
    func walkRange(nrange *ir.RangeStmt) ir.Node {
        ...
        switch k := t.Kind(); {
        case types.IsInt[k]:
            hv1 := typecheck.TempAt(base.Pos, ir.CurFunc, t)
            hn := typecheck.TempAt(base.Pos, ir.CurFunc, t)

            init = append(init, ir.NewAssignStmt(base.Pos, hv1, nil))
            init = append(init, ir.NewAssignStmt(base.Pos, hn, a))
            ...
        ...
        }
        ...
    }
    ```

<br>

- 更新 `for` 循环的 `Cond` 和 `Post` 代码块

  - 设置 `Cond` 代码块为二元表达式 `hv1 < hn`
  - 设置 `Post` 代码块为赋值表达式 `hv1 = hv1 + 1`

    ```go
    func walkRange(nrange *ir.RangeStmt) ir.Node {
        ...
        switch k := t.Kind(); {
        case types.IsInt[k]:
            ...
            nfor.Cond = ir.NewBinaryExpr(base.Pos, ir.OLT, hv1, hn)
            nfor.Post = ir.NewAssignStmt(base.Pos, hv1, ir.NewBinaryExpr(base.Pos, ir.OADD, hv1, ir.NewInt(base.Pos, 1)))
            ...
        ...
        }
        ...
    }
    ```

<br>

- 更新 `for` 循环的 `Body` 代码块

  - 当 `v1` 非空时，`Body` 中会使用到索引字段，故需要在 `Body` 代码块中新增赋值语句
    - 新增 `v1 = hv1`

    ```go
    func walkRange(nrange *ir.RangeStmt) ir.Node {
        ...
        switch k := t.Kind(); {
        case types.IsInt[k]:
            ...
            if v1 != nil {
                body = []ir.Node{rangeAssign(nrange, hv1)}
            }
        ...
        }
        ...
    }
    ```

### 数组 & 切片

#### 代码示例

可以对数组或切片实现遍历操作，其中 `v1` 代表每次遍历时的索引，`v2` 代表该索引对应的数据：

```go
a := []int{1, 2, 3}
for v1, v2 := range a {
    block(v1, v2)
}
```

在经过编译器处理后，会转化为如下伪代码：

```go
a := []int{1, 2, 3}
var v1 int
var v2 a.elem.type
ha := a
for hv1, hn := 0, len(ha); hv1 < hn; hv1 += 1 {
    v1, v2 = hv1, ha[hv1]
    block(v1, v2)
}
```

#### 代码分析

- 校验遍历操作，判断是否为清空操作

  - 如果是，则直接返回新的语句，而非 `for` 语句

    ```go
    func walkRange(nrange *ir.RangeStmt) ir.Node {
        ...
        switch k := t.Kind(); {
        case k == types.TARRAY, k == types.TSLICE, k == types.TPTR: // TPTR is pointer-to-array
            if nn := arrayRangeClear(nrange, v1, v2, a); nn != nil {
                base.Pos = lno
                return nn
            }
            ...
        ...
        }
        ...
    }
    ```

  - 判断时，会通过 [arrayRangeClear()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/walk/range.go#L484) 函数判断循环操作是否为 `for i := range a { a[i] = zero }` 的一个实例，如果是，则通过 [arrayClear()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/walk/range.go#L484) 函数构造新的语句：

    ```go
    // Lower n into runtime·memclr if possible, for
    // fast zeroing of slices and arrays (issue 5373).
    // Look for instances of
    //
    //  for i := range a {
    //      a[i] = zero
    //  }
    //
    // in which the evaluation of a is side-effect-free.
    //
    // Parameters are as in walkRange: "for v1, v2 = range a".
    func arrayRangeClear(loop *ir.RangeStmt, v1, v2, a ir.Node) ir.Node {
        ...
        return arrayClear(stmt.Pos(), a, loop)
    }
    ```

  - [arrayClear()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/walk/range.go#L484) 函数内部，会将上述循环优化为 `memclr{NoHeap,Has}Pointers(hp, hn)` 语句，直接进行清空对应内存空间中的所有数据：

    ```go
    // arrayClear constructs a call to runtime.memclr for fast zeroing of slices and arrays.
    func arrayClear(wbPos src.XPos, a ir.Node, nrange *ir.RangeStmt) ir.Node {
        ...
        // Convert to
        //  if len(a) != 0 {
        //      hp = &a[0]
        //      hn = len(a)*sizeof(elem(a))
        //      memclr{NoHeap,Has}Pointers(hp, hn)
        //      i = len(a) - 1
        //  }
        ...
        var fn ir.Node
        if a.Type().Elem().HasPointers() {
            // memclrHasPointers(hp, hn)
            ir.CurFunc.SetWBPos(wbPos)
            fn = mkcallstmt("memclrHasPointers", hp, hn)
        } else {
            // memclrNoHeapPointers(hp, hn)
            fn = mkcallstmt("memclrNoHeapPointers", hp, hn)
        }

        n.Body.Append(fn)
        ...
        return walkStmt(n)
    }
    ```

<br>

- 初始化临时变量

  - 初始化迭代对象 `ha`，即数组/切片 `a` 本身，避免后续 `a` 发生了更改
  - 初始化迭代变量 `hv1`，类型为 `Int`，即每次迭代时的索引 `index`，并将 `hv1 = 0` 添加在 `ForStmt` 语句的 `Init` 代码块中
  - 初始化迭代长度 `hn`，类型为 `Int`，即 `len(ha)`，并将 `hn = len(ha)` 添加在 `ForStmt` 语句的 `Init` 代码块中

    ```go
    func walkRange(nrange *ir.RangeStmt) ir.Node {
        ...
        switch k := t.Kind(); {
        case k == types.TARRAY, k == types.TSLICE, k == types.TPTR: // TPTR is pointer-to-array
            ...
            // order.stmt arranged for a copy of the array/slice variable if needed.
            ha := a

            hv1 := typecheck.TempAt(base.Pos, ir.CurFunc, types.Types[types.TINT])
            hn := typecheck.TempAt(base.Pos, ir.CurFunc, types.Types[types.TINT])

            init = append(init, ir.NewAssignStmt(base.Pos, hv1, nil))
            init = append(init, ir.NewAssignStmt(base.Pos, hn, ir.NewUnaryExpr(base.Pos, ir.OLEN, ha)))
            ...
        ...
        }
        ...
    }
    ```

<br>

- 更新 `for` 循环的 `Cond` 和 `Post` 代码块

  - 设置 `Cond` 代码块为二元表达式 `hv1 < hn`
  - 设置 `Post` 代码块为赋值表达式 `hv1 = hv1 + 1`

    ```go
    func walkRange(nrange *ir.RangeStmt) ir.Node {
        ...
        switch k := t.Kind(); {
        case k == types.TARRAY, k == types.TSLICE, k == types.TPTR: // TPTR is pointer-to-array
            ...
            nfor.Cond = ir.NewBinaryExpr(base.Pos, ir.OLT, hv1, hn)
            nfor.Post = ir.NewAssignStmt(base.Pos, hv1, ir.NewBinaryExpr(base.Pos, ir.OADD, hv1, ir.NewInt(base.Pos, 1)))
            ...
        ...
        }
        ...
    }
    ```

<br>

- 更新 `for` 循环的 `Body` 代码块

  - 对应 `for range ha { body }` 格式的循环，不需要关心循环时的索引和数据，不需要额外修改 `Body` 代码块

    ```go
    func walkRange(nrange *ir.RangeStmt) ir.Node {
        ...
        switch k := t.Kind(); {
        case k == types.TARRAY, k == types.TSLICE, k == types.TPTR: // TPTR is pointer-to-array
            ...
            // for range ha { body }
            if v1 == nil {
                break
            }
            ...
        ...
        }
        ...
    }
    ```

  - 对应 `for v1 := range ha { body }` 格式的循环，`Body` 中会使用到索引字段，故需要在 `Body` 代码块中新增赋值语句
    - 新增 `v1 = hv1`

    ```go
    func walkRange(nrange *ir.RangeStmt) ir.Node {
        ...
        switch k := t.Kind(); {
        case k == types.TARRAY, k == types.TSLICE, k == types.TPTR: // TPTR is pointer-to-array
            ...
            // for v1 := range ha { body }
            if v2 == nil {
                body = []ir.Node{rangeAssign(nrange, hv1)}
                break
            }
            ...
        ...
        }
        ...
    }
    ```

  - 对应 `for v1, v2 := range ha { body }` 格式的循环，`Body` 中会同时使用到索引和数据字段，故需要在 `Body` 代码块中新增赋值语句
    - 新增 `v1, v2 = hv1, ha[hv1]`

    ```go
    func walkRange(nrange *ir.RangeStmt) ir.Node {
        ...
        switch k := t.Kind(); {
        case k == types.TARRAY, k == types.TSLICE, k == types.TPTR: // TPTR is pointer-to-array
            ...
            // for v1, v2 := range ha { body }
            if cheapComputableIndex(elem.Size()) {
                // v1, v2 = hv1, ha[hv1]
                tmp := ir.NewIndexExpr(base.Pos, ha, hv1)
                tmp.SetBounded(true)
                body = []ir.Node{rangeAssign2(nrange, hv1, tmp)}
                break
            }
            ...
        ...
        }
        ...
    }
    ```

### 哈希表

#### 代码示例

可以对哈希表实现遍历操作，其中 `v1` 代表每次遍历时的 key 值，`v2` 代表该 key 对应的数据：

```go
a := map[string]int{"1st": 1, "2nd": 2, "3rd": 3}
for v1, v2 := range a {
    block(v1, v2)
}
```

在经过编译器处理后，会转化为如下伪代码：

```go
a := map[string]int{"1st": 1, "2nd": 2, "3rd": 3}
var v1 a.key.type
var v2 a.elem.type

ha := a
var hit hiter
for mapiterinit(ha.type, ha, hit); hit.key != nil; mapiternext(&hit) {
    v1, v2 = *hit.key, *hit.elem
    block(v1, v2)
}
```

#### 代码分析

- 校验遍历操作，判断是否为清空操作

  - 如果是，则直接返回新的语句，而非 `for` 语句

    ```go
    func walkRange(nrange *ir.RangeStmt) ir.Node {
        ...
        if isMapClear(nrange) {
            return mapRangeClear(nrange)
        }
        ...
    }
    ```

  - 判断时，会通过 [isMapClear()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/walk/range.go#L416) 函数判断循环操作是否为 `for k := range m { delete(m, k) }` 的一个实例，如果是，则通过 [mapRangeClear()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/walk/range.go#L455) 函数构造新的语句：

    ```go
    // isMapClear checks if n is of the form:
    //
    //  for k := range m {
    //      delete(m, k)
    //  }
    //
    // where == for keys of map m is reflexive.
    func isMapClear(n *ir.RangeStmt) bool {
        ...
        return true
    }
    ```

  - [mapRangeClear()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/walk/range.go#L455) 函数内部，会将上述循环优化为调用 [mapclear()](https://github.com/golang/go/blob/go1.22.0/src/runtime/map.go#L989) 函数的语句，重置哈希表中的所有成员变量，恢复为刚创建时的情况：

    ```go
    // mapRangeClear constructs a call to runtime.mapclear for the map range idiom.
    func mapRangeClear(nrange *ir.RangeStmt) ir.Node {
        ...
        return mapClear(m, reflectdata.RangeMapRType(base.Pos, nrange))
    }

    // mapClear constructs a call to runtime.mapclear for the map m.
    func mapClear(m, rtyp ir.Node) ir.Node {
        t := m.Type()

        // instantiate mapclear(typ *type, hmap map[any]any)
        fn := typecheck.LookupRuntime("mapclear", t.Key(), t.Elem())
        n := mkcallstmt1(fn, rtyp, m)
        return walkStmt(typecheck.Stmt(n))
    }
    ```

<br>

- 初始化临时变量

  - 初始化迭代对象 `ha`，是对于原哈希表的引用
  - 初始化迭代器 `hit`，其实际类型为 [hiter](https://github.com/golang/go/blob/go1.22.0/src/runtime/map.go#L166)，是哈希表内部的迭代器类型
  - 初始化迭代器类型 `th`
  - 从迭代器中获取 `key` 和 `elem` 对应的字段，[Sym](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/types/sym.go#L28) 结构体可以用于代表一个对象的名称

    ```go
    func walkRange(nrange *ir.RangeStmt) ir.Node {
        ...
        switch k := t.Kind(); {
        case k == types.TMAP
            // order.stmt allocated the iterator for us.
            // we only use a once, so no copy needed.
            ha := a

            hit := nrange.Prealloc
            th := hit.Type()
            // depends on layout of iterator struct.
            // See cmd/compile/internal/reflectdata/reflect.go:MapIterType
            keysym := th.Field(0).Sym
            elemsym := th.Field(1).Sym // ditto
            ...
        ...
        }
        ...
    }

    // A hash iteration structure.
    type hiter struct {
        key         unsafe.Pointer // Must be in first position.  Write nil to indicate iteration end (see cmd/compile/internal/walk/range.go).
        elem        unsafe.Pointer // Must be in second position (see cmd/compile/internal/walk/range.go).
        t           *maptype
        h           *hmap
        ...
        startBucket uintptr        // bucket iteration started at
        offset      uint8          // intra-bucket offset to start from during iteration (should be big enough to hold bucketCnt-1)
        ...
    }
    ```

<br>

- 更新 `for` 循环的 `Init` 代码块

  - 创建 [mapiterinit()](https://github.com/golang/go/blob/go1.22.0/src/runtime/map.go#L816C43-L816C48) 函数的调用语句，并将其添加至 `Init` 代码块中
  - [mapiterinit()](https://github.com/golang/go/blob/go1.22.0/src/runtime/map.go#L816C43-L816C48) 语句内部会初始化迭代器中的字段，例如哈希表的类型，哈希表本身，等等
  - 在初始化时，会通过随机数来确认本次迭代开始的位置
  - 初始化的最后，会通过 [mapiternext()](https://github.com/golang/go/blob/go1.22.0/src/runtime/map.go#L862) 函数更新迭代器第一次迭代的元素

    ```go
    func walkRange(nrange *ir.RangeStmt) ir.Node {
        ...
        switch k := t.Kind(); {
        case k == types.TMAP
            ...
            fn := typecheck.LookupRuntime("mapiterinit", t.Key(), t.Elem(), th)
            init = append(init, mkcallstmt1(fn, reflectdata.RangeMapRType(base.Pos, nrange), ha, typecheck.NodAddr(hit)))
            ...
        ...
        }
        ...
    }

    // mapiterinit initializes the hiter struct used for ranging over maps.
    // The hiter struct pointed to by 'it' is allocated on the stack
    func mapiterinit(t *maptype, h *hmap, it *hiter) {
        ...
        it.t = t
        ...
        it.h = h
        ...
        // decide where to start
        r := uintptr(rand())
        it.startBucket = r & bucketMask(h.B)
        it.offset = uint8(r >> h.B & (bucketCnt - 1))
        ...
        mapiternext(it)
    }
    ```

<br>

- 更新 `for` 循环的 `Cond` 和 `Post` 代码块

  - 设置 `Cond` 代码块为二元表达式 `hit.key != nil`，即还存在未遍历的 key 值
  - 设置 `Post` 代码块为函数调用表达式 `mapiternext(hit)`，寻找下一个要遍历的 key 值

    ```go
    func walkRange(nrange *ir.RangeStmt) ir.Node {
        ...
        switch k := t.Kind(); {
        case k == types.TMAP
            ...
            nfor.Cond = ir.NewBinaryExpr(base.Pos, ir.ONE, ir.NewSelectorExpr(base.Pos, ir.ODOT, hit, keysym), typecheck.NodNil())

            fn = typecheck.LookupRuntime("mapiternext", th)
            nfor.Post = mkcallstmt1(fn, typecheck.NodAddr(hit))
            ...
        ...
        }
        ...
    }
    ```

<br>

- 更新 `for` 循环的 `Body` 代码块

  - 当 `v1 == nil` 时，说明不需要关心循环时的 key 与 value ，不需要额外修改 `Body` 代码块
  - 当 `v2 == nil` 时，说明迭代时，需要使用 key 值，在 `Body` 代码块中添加 `v1 = key`
  - 当迭代时，同时需要使用 key 与 value 时，在 `Body` 代码块中添加 `v1, v2 = key, elem`

    ```go
    func walkRange(nrange *ir.RangeStmt) ir.Node {
        ...
        switch k := t.Kind(); {
        case k == types.TMAP
            ...
            key := ir.NewStarExpr(base.Pos, typecheck.ConvNop(ir.NewSelectorExpr(base.Pos, ir.ODOT, hit, keysym), types.NewPtr(t.Key())))
            if v1 == nil {
                body = nil
            } else if v2 == nil {
                body = []ir.Node{rangeAssign(nrange, key)}
            } else {
                elem := ir.NewStarExpr(base.Pos, typecheck.ConvNop(ir.NewSelectorExpr(base.Pos, ir.ODOT, hit, elemsym), types.NewPtr(t.Elem())))
                body = []ir.Node{rangeAssign2(nrange, key, elem)}
            }
            ...
        ...
        }
        ...
    }
    ```

### 通道

#### 代码示例

可以对通道执行遍历操作，在循环执行期间，最终会调用到 `channel` 的读操作，所以会阻塞当前协程：

```go
a := make(chan int, 10)
for v := range a {
    block(v)
}
```

在经过编译器处理后，会转化为如下伪代码：

```go
a := make(chan int, 10)
var v1 int
ha := a
var hv1 int
var hb bool
for hv1, hb = <-ha; hb != false; hv1, hb = <-ha {
    v1 = hv1
    block(v1)
    hv1 = 0
}
```

#### 代码分析

- 初始化临时变量

  - 初始化迭代对象 `ha`，即 `a` 本身
  - 初始化迭代变量 `hv1`，类型为 `t.Elem()`，用于接收通道中的变量，并设置为已完成类型检查
  - 如果该变量包含指针，则将初始化语句添加在 `ForStmt` 语句的 `Init` 代码块中
  - 初始化迭代变量 `hb`，类型为 `BOOL`，用于存储通道接收的状态

    ```go
    func walkRange(nrange *ir.RangeStmt) ir.Node {
        ...
        switch k := t.Kind(); {
        case k == types.TCHAN:
            // order.stmt arranged for a copy of the channel variable.
            ha := a

            hv1 := typecheck.TempAt(base.Pos, ir.CurFunc, t.Elem())
            hv1.SetTypecheck(1)
            if t.Elem().HasPointers() {
                init = append(init, ir.NewAssignStmt(base.Pos, hv1, nil))
            }
            hb := typecheck.TempAt(base.Pos, ir.CurFunc, types.Types[types.TBOOL])
            ...
        ...
        }
        ...
    }
    ```

<br>

- 更新 `for` 循环的 `Cond` 代码块

  - 设置 `Cond` 代码块为二元表达式 `hb != false`，即接收通道成功
  - 初始化 `lhs` 切片、`rhs` 切片和赋值语句 `a`，实现从 `ha`，即通道中接收数据，并传递给 `hv1` 和 `hb`，即 `hv1, hb = <-ha`
  - 更新 `Cond` 代码块，将赋值语句 `a` 添加至 `Cond` 代码块的初始化逻辑中

    ```go
    func walkRange(nrange *ir.RangeStmt) ir.Node {
        ...
        switch k := t.Kind(); {
        case k == types.TCHAN:
            ...
            nfor.Cond = ir.NewBinaryExpr(base.Pos, ir.ONE, hb, ir.NewBool(base.Pos, false))
            lhs := []ir.Node{hv1, hb}
            rhs := []ir.Node{ir.NewUnaryExpr(base.Pos, ir.ORECV, ha)}
            a := ir.NewAssignListStmt(base.Pos, ir.OAS2RECV, lhs, rhs)
            a.SetTypecheck(1)
            nfor.Cond = ir.InitExpr([]ir.Node{a}, nfor.Cond)
            ...
        ...
        }
        ...
    }
    ```

<br>

- 更新 `for` 循环的 `Body` 代码块

  - 当 `v1 == nil` 时，说明不需要关心迭代时接收到的值，不需要额外修改 `Body` 代码块
  - 当迭代时，需要使用接收到的值时，在 `Body` 代码中添加 `v1 = hv1`
  - 在 `Body` 代码中添加 `hv1 = nil`，避免垃圾回收问题

    ```go
    func walkRange(nrange *ir.RangeStmt) ir.Node {
        ...
        switch k := t.Kind(); {
        case k == types.TCHAN
            ...
            if v1 == nil {
                body = nil
            } else {
                body = []ir.Node{rangeAssign(nrange, hv1)}
            }
            // Zero hv1. This prevents hv1 from being the sole, inaccessible
            // reference to an otherwise GC-able value during the next channel receive.
            // See issue 15281.
            body = append(body, ir.NewAssignStmt(base.Pos, hv1, nil))
        ...
        }
        ...
    }
    ```

### 字符串

#### 代码示例

可以对字符串实现遍历操作，相当于对 `[]rune` 切片进行遍历，其中 `v1` 代表每次遍历时的索引，`v2` 代表该索引对应的数据：

```go
a := "hello,world!"
for i, v := range a {
    block(i, v)
}
```

在经过编译器处理后，会转化为如下伪代码：

```go
a := "hello,world!"
var v1 int
var v2 rune
ha := a
for hv1 := 0; hv1 < len(ha); {
    hv1t := hv1
    hv2 := rune(ha[hv1])
    if hv2 < utf8.RuneSelf {
        hv1++
    } else {
        hv2, hv1 = decoderune(ha, hv1)
    }
    v1, v2 = hv1t, hv2
    block(v1, v2)
}
```

#### 代码分析

字符串迭代与数组、切片迭代差异不大，且会转为 `[]rune` 进行迭代，差异点在于 `rune` 类型数据的实际编码所占用的字节数，会影响每次迭代时实际的索引位置。

```go
func walkRange(nrange *ir.RangeStmt) ir.Node {
    ...
    switch k := t.Kind(); {
    case k == types.TSTRING:
        // order.stmt arranged for a copy of the string variable.
        ha := a

        hv1 := typecheck.TempAt(base.Pos, ir.CurFunc, types.Types[types.TINT])
        hv1t := typecheck.TempAt(base.Pos, ir.CurFunc, types.Types[types.TINT])
        hv2 := typecheck.TempAt(base.Pos, ir.CurFunc, types.RuneType)

        // hv1 := 0
        init = append(init, ir.NewAssignStmt(base.Pos, hv1, nil))

        // hv1 < len(ha)
        nfor.Cond = ir.NewBinaryExpr(base.Pos, ir.OLT, hv1, ir.NewUnaryExpr(base.Pos, ir.OLEN, ha))

        if v1 != nil {
            // hv1t = hv1
            body = append(body, ir.NewAssignStmt(base.Pos, hv1t, hv1))
        }

        // hv2 := rune(ha[hv1])
        nind := ir.NewIndexExpr(base.Pos, ha, hv1)
        nind.SetBounded(true)
        body = append(body, ir.NewAssignStmt(base.Pos, hv2, typecheck.Conv(nind, types.RuneType)))

        // if hv2 < utf8.RuneSelf
        nif := ir.NewIfStmt(base.Pos, nil, nil, nil)
        nif.Cond = ir.NewBinaryExpr(base.Pos, ir.OLT, hv2, ir.NewInt(base.Pos, utf8.RuneSelf))

        // hv1++
        nif.Body = []ir.Node{ir.NewAssignStmt(base.Pos, hv1, ir.NewBinaryExpr(base.Pos, ir.OADD, hv1, ir.NewInt(base.Pos, 1)))}

        // } else {
        // hv2, hv1 = decoderune(ha, hv1)
        fn := typecheck.LookupRuntime("decoderune")
        call := mkcall1(fn, fn.Type().ResultsTuple(), &nif.Else, ha, hv1)
        a := ir.NewAssignListStmt(base.Pos, ir.OAS2, []ir.Node{hv2, hv1}, []ir.Node{call})
        nif.Else.Append(a)

        body = append(body, nif)

        if v1 != nil {
            if v2 != nil {
                // v1, v2 = hv1t, hv2
                body = append(body, rangeAssign2(nrange, hv1t, hv2))
            } else {
                // v1 = hv1t
                body = append(body, rangeAssign(nrange, hv1t))
            }
        }
    ...
    }
    ...
}
```

## 常见问题

### Loopvar

> <https://tip.golang.org/wiki/LoopvarExperiment#what-is-the-problem-this-solves>

Go1.22 发布时，针对于循环中的变量赋值逻辑做了更改，考虑如下代码，在循环体中构建了新的函数，新函数中捕获了临时变量 `i`：

```go
func Print123() {
    var prints []func()
    for i := 1; i <= 3; i++ {
        prints = append(prints, func() { fmt.Println(i) })
    }
    for _, print := range prints {
        print()
    }
}
```

<br>

在 1.22 以前的版本中，上述程序最终会输出 `"4,4,4"`，其原因是变量 `i` 仅在 `for` 循环的 `Init` 代码块中进行声明，在 `Post` 代码块中自增时，仅仅修改了 `i` 的值，使其自增，但是每次循环中变量 `i` 的地址其实没有发生变化。

相当于 `prints` 切片中的所有函数，所捕获的均是同一个变量 `i`，且在循环结束后，`i` 最终的值为 `4`。

同样的问题也发生在 `range` 循环中：

```go
func Print123ForSlice() {
    arr := []int{1, 2, 3}
    newArr := []*int{}
    for _, v := range arr {
        newArr = append(newArr, &v)
    }
    for _, v := range newArr {
        fmt.Println(*v)
    }
}
```

<br>

上述函数的执行结果为 `"3,3,3"`，其原因也是变量 `v` 仅在 `for` 循环的 `Init` 代码块中进行声明，后续每次循环中重新对其赋值，所以添加在 `newArr` 中的三个元素，都是同一个地址。

与上述 `for` 循环不同的时，`range` 循环结束的条件是临时变量 `v1` 超过了切片 `arr` 的索引范围，所以最终临时变量 `v2`，即代码中的 `v`，最终的值是最后一次循环赋值的 `3`。

在以往，想要避免上述问题，需要使用 `:=` 来创建不同的实例，对于如上代码，可修改为：

```go
func Print123() {
    var prints []func()
    for i := 1; i <= 3; i++ {
        i := i
        prints = append(prints, func() { fmt.Println(i) })
    }
    for _, print := range prints {
        print()
    }
}

func Print123ForSlice() {
    arr := []int{1, 2, 3}
    newArr := []*int{}
    for _, v := range arr {
        v := v
        newArr = append(newArr, &v)
    }
    for _, v := range newArr {
        fmt.Println(*v)
    }
}
```

<br>

在 1.22 及以后的版本，编译器内部修改了这个赋值逻辑，即 [distinctVars()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/noder/writer.go#L1481) 方法默认会为每次循环，都独立创建不同的实例，同时也可以标记位来修改这个执行逻辑

```go
func (w *writer) distinctVars(stmt *syntax.ForStmt) bool {
    lv := base.Debug.LoopVar
    fileVersion := w.p.info.FileVersions[stmt.Pos().Base()]
    is122 := fileVersion == "" || version.Compare(fileVersion, "go1.22") >= 0

    // Turning off loopvar for 1.22 is only possible with loopvarhash=qn
    //
    // Debug.LoopVar values to be preserved for 1.21 compatibility are 1 and 2,
    // which are also set (=1) by GOEXPERIMENT=loopvar.  The knobs for turning on
    // the new, unshared, loopvar behavior apply to versions less than 1.21 because
    // (1) 1.21 also did that and (2) this is believed to be the likely use case;
    // anyone checking to see if it affects their code will just run the GOEXPERIMENT
    // but will not also update all their go.mod files to 1.21.
    //
    // -gcflags=-d=loopvar=3 enables logging for 1.22 but does not turn loopvar on for <= 1.21.

    return is122 || lv > 0 && lv != 3
}

```

### 动态修改切片长度

针对于如下代码，可以看到循环仅执行三次，并未永远执行下去，其原因是在将 `range` 循环构建为 `for` 循环时，在 `Init` 代码块中，就会存储当前切片长度，最终循环时，以该长度为准。

```go
func f() {
    arr := []int{1, 2, 3}
    for _, v := range arr {
        arr = append(arr, v*2)
    }
    fmt.Println(arr) // "[1 2 3 2 4 6]"
}
```

### 哈希表随机遍历

针对于如下代码，可以发现每次运行时，函数的打印结果并非始终一致，其原因是在将  `range` 循环构建为 `for` 循环时，在 `Init` 代码块中，会根据随机数初始化当次循环开始的节点，确保使用者不要严格依赖哈希桶中的顺序。

```go
func f() {
    hash := map[string]int{"1": 1, "2": 2, "3": 3}
    for k, v := range hash {
        println(k, v)
    }
}
```

## 参考

- <https://draveness.me/golang/docs/part2-foundation/ch05-keyword/golang-for-range/#%E6%95%B0%E7%BB%84%E5%92%8C%E5%88%87%E7%89%87>
- <https://tip.golang.org/wiki/LoopvarExperiment#what-is-the-problem-this-solves>
