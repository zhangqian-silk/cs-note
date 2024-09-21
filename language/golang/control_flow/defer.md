# Defer

`defer` 关键字可以用于推迟函数执行，当 `defer` 语句被执行时，跟在 `defer` 后面的函数会被加入至一个特定的栈中，直到包含该 `defer` 语句的函数执行完毕，`defer` 后的函数才会被执行。

`defer` 常被用于处理成对的操作，例如打开和关闭，连接和断开连接，加锁和释放锁等待。无论函数最终是正常执行 `return` 操作，还是发生 `panic` 导致异常结束，也无论函数有一个还是多个 `return` 语句，`defer` 后的语句都会被正常执行，大幅简化操作。

例如在处理文件时，通过 `Open()` 函数打开后，即可紧跟着使用 `defer` 关键字来调用 `Close()` 函数：

```go
func ReadFile(filename string) ([]byte, error) {
    f, err := os.Open(filename)
    if err != nil { return nil, err }
    defer f.Close()
    
    content, err := ReadAll(f)
    if err != nil { return nil, err }
    
    update(content)
    return content, nil
}
```

## 特性

### 参数传递

在通过 `defer` 执行函数时，虽然函数会被延迟执行，但是函数中的入参，是在执行 `defer` 语句时，就进行拷贝的（Go 中的只有值传递）。例如如下程序中，最终打印的结果为 "befor defer"，即执行 `defer` 语句时，变量 `s` 的值。

```go
func main() {
    s := "befor defer"
    defer fmt.Println(s)
    s = "after defer"
}
```

<br>

此时可以通过匿名函数，在函数体内部调用原本的函数，如下所示，此时打印结果为 "after defer"：

```go
func main() {
    s := "befor defer"
    defer func() { fmt.Println(s) }()
    s = "after defer"
}
```

<br>

在这种场景下，`defer` 语句仍然会拷贝所要执行的函数，但是此函数本身没有入参，而是在函数体内部引用了变量 `s`，故不会额外拷贝变量 `s`，打印结果以函数执行时，变量 `s` 的实际值为准。

### 执行顺序

当一个函数内部存在多个 `defer` 语句时，最终会按照声明顺序，亦即入栈顺序，后进先出，优先执行最后声明的语句，但是所有语句均会在该函数退出之前，执行完成。例如对于如下程序，当 `main()` 函数调用 `f()` 函数时：

- 会优先执行 `f()` 内的语句，即打印 "func"，
- 然后 `defer` 语句会按照倒序进行打印，即 "2"、"1"、"0"
- 函数 `f()` 中所有语句，包括`defer` 语句执行完成后，继续执行 `main()` 函数中的语句，打印 "main"：

```go
func f() {
    for i := 0; i < 3; i++ {
        defer fmt.Println(i)
    }
    fmt.Println("func")
}

func main() {
    f()
    fmt.Println("main")
}
```

### 执行时机

从用户代码来看，`defer` 语句的效果，类似于将其后面的函数拷贝一份，然后插入在 `return` 语句前面。

例如如下两个函数，我们也可以不使用 `defer` 语句，手动在各个 `return` 语句之前，手动调用 `Close()` 方法，其效果与使用 `defer` 语句，完全等价。

```go
func ReadFile(filename string) ([]byte, error) {
    f, err := os.Open(filename)
    if err != nil { return nil, err }
    defer f.Close()
    
    content, err := ReadAll(f)
    if err != nil { return nil, err }
    
    update(content)
    return content, nil
}

func ReadFileWithoutDefer(filename string) ([]byte, error) {
    f, err := os.Open(filename)
    if err != nil { return nil, err }
    
    content, err := ReadAll(f)
    if err != nil {
        f.Close()
        return nil, err
    }

    update(content)
    f.Close()
    return content, nil
}
```

<br>

但是在涉及到返回值时，`defer` 函数的表现与预期却并不相符。在如下两个函数中，如果直接执行 `f()`，最终返回的变量 `t` 的结果为 1，而如果通过 `defer` 语句，将 `f()` 推迟执行，最终返回的变量 `t` 的结果，实际为 0。

```go
func f1() int {
    t := 1
    f := func() { t += 1 }
    defer f()
    return t // 1
    
}

func f2() int {
    t := 1
    f := func() { t += 1 }
    f()
    return t // 2
}
```

<br>

此时，如果我们再额外引入一种场景，手动为函数的返回值声明参数名称，此时再通过 `defer` 语句延迟对返回值的修改，其实际结果却是修改生效：

```go
func f3() (r int) {
    r = 1
    defer func() { r += 10 }()

    return r // 11
}

func f4() (r int) {
    t := 1
    defer func() { r += 20 }()
    return t // 21
}
```

<br>

在底层实现上，`return` 语句实际对应了多条汇编语句，会先执行一段赋值操作，将返回值存在由 caller 预先创建的内存处，再结束函数调用。而 `defer` 语句真正执行的位置，是在赋值之后，结束调用之前。对于如上三种情况：

- `f1()` 在实际执行时，因为变量 `t` 是在函数内部创建的变量，所以会先将变量 `t` 拷贝至返回值真正的内存空间上，再执行 `defer` 语句对应的匿名函数去修改变量 `t`，然后结束函数调用。对于 caller 来说，其拿到的返回值，是执行 `defer` 语句之前所拷贝的值
- `f2()` 不做过多介绍，属于函数正常执行流程，与预期结果一致
- `f3()` 在实际执行时，变量 `r` 所对应的位置，已经是由 caller 所创建的函数返回值的内存空间，`r` 的初始化以及 `defer` 语句中的修改逻辑，都是直接作用在该函数函数值的内存空间上，故修改是生效的
- `f4()` 在实际执行时，与 `f1()` 类似，会先做赋值操作，将变量 `t` 的值拷贝至返回值真正的内存空间上，但是不同的是，`defer` 中的匿名函数，是对变量 `r`，即真正的返回值做修改，而非函数内的局部变量 `t`，故修改是生效的

## 参考

- <https://draveness.me/golang/docs/part2-foundation/ch05-keyword/golang-defer/>
- <https://juejin.cn/post/7169596824385224735>
