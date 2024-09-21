# 字符串

字符串是一个不可变的字节序列，通常也会被认为是采用了 UTF-8 编码的 Unicode 码点(`rune`)序列，即可通过 `len` 函数获取字符串中的**字节数目**。
> 注意 `len` 函数返回的不是字符数目，在填充的字符为非 ASCII 字符时，每个字符会占据多个字节，此时第 i 个字节不一定是第 i 个字符。

## 底层结构

字符串的底层数据结构包含了指向字节数组的指针和数组长度，在 `runtime` 包中，可以看到类似的内部使用的 `string` 的结构体 [stringStruct](https://github.com/golang/go/blob/go1.22.0/src/runtime/string.go#L232)：

```go
type stringStruct struct {
    str unsafe.Pointer
    len int
}
```

而在 `reflect` 包中，可以看到 `string` 结构体在运行时的表现形式 [stringHeader](https://github.com/golang/go/blob/go1.22.0/src/reflect/value.go#L2840)：

```go
// StringHeader is the runtime representation of a string.
// ...
// Deprecated: Use unsafe.String or unsafe.StringData instead.
type StringHeader struct {
    Data uintptr
    Len  int
}
```

在老版本中，借助于上面暴露出的结构体，开发者可以实现 `string` 与 `slice` 的零拷贝的转换，但是因为 `Data` 的实际类型为 `uintprt`，没有任何类型校验，往往会导致非预期的行为产生，在高版本（Go 1.20）中，上述结构体被标记废弃，并在 `unsafe` 包中提供了类似能力，但是提供了类型安全的构建 `sting` 的方法 [String](https://github.com/golang/go/blob/go1.22.0/src/unsafe/unsafe.go#L262)：

```go
// String returns a string value whose underlying bytes
// start at ptr and whose length is len.
// ...
// Since Go strings are immutable, the bytes passed to String
// must not be modified afterwards.
func String(ptr *byte, len IntegerType) string
```

需要注意的是，在 Go 中，字符串是不允许被修改的，只允许重新赋值。这种设计保障了多线程操作以及底层字符数组数据共享时的安全性，而数据共享一方面节约了内存，另一方面在字符串复制以及获取子串时，能带来更高的性能。

在如下代码中，我们声明了字符串 `s`，并将其赋值给了字符串 `t`，可以发现他们在底层的字符数据上是共享的。当我们修改了 `s` 时，字符数组的首地址发生了改变，但是字符串 `t` 没有任何改变，可以证明在修改字符串时，实质是修改字符数组的指针，指向另外一处的字符数组，并非字符数组本身发生了改变。

```go
s := "Hello, world!"
fmt.Printf("%p\n", unsafe.StringData(s)) // "0x6c1b55"

t := s
fmt.Printf("%p\n", unsafe.StringData(t)) // "0x6c1b55" (与s一致)

s = "Hello"
fmt.Printf("s:%s, %p", s, unsafe.StringData(s)) // "s:Hello, 0x6c0af2"
fmt.Printf("t:%s, %p", t, unsafe.StringData(t)) // "t:Hello, world!, 0x6c1b55"
```

## 字面量

字符串的字面量支持双引号与反引号两种方式：

- 前者被称作解释字符串（Interpreted string literals）与其他语言类似，用于声明单行字符串，对于特殊字符，需要转义处理。适合需要使用转义字符的场景。

- 后者被称作原生字符串（Raw string literals），则没有转义操作，字符串内容会被原样输出，可以包括任意字符（除了反引号本身）。适合包含大量特殊字符或多行文本，需要原样输出的场景，例如正则表达式，网址链接，json 字符串等。

例如，在需要换行操作时，解释字符串需要拼接 `\n` 来实现效果，原生字符串直接换行即可；在声明 json 字符串时，解释字符串需要对特殊字符 `"` 进行转义，原生字符串同样直接声明即可：

```go
s := "Hello\nworld!"
t := `Hello
world!`

s = "{\"key\": \"value\"}"
t = `{"key": "value"}`
```

### 解析方式

在扫描器中的 [next()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/syntax/scanner.go#L88) 函数中，可以看到编译器会针对于双引号和反引号，分别调用不同的字符串的处理逻辑：

```go
func (s *scanner) next() {
    ...
    switch s.ch {
    ...
    case '"': 
        s.stdString()

    case '`': 
        s.rawString()
    ...
    }
    ...
}
```

在 [stdString()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/syntax/scanner.go#L674) 函数中，会循环读取后续所有字符，直至遇到下一个双引号，期间如果遇到转义字符会进行特殊处理：

```go
func (s *scanner) stdString() {
    ok := true
    s.nextch()

    for {
        if s.ch == '"' {
            s.nextch()
            break
        }
        if s.ch == '\\' {
            s.nextch()
            if !s.escape('"') {
                ok = false
            }
            continue
        }
        if s.ch == '\n' {
            s.errorf("newline in string")
            ok = false
            break
        }
        if s.ch < 0 {
            s.errorAtf(0, "string not terminated")
            ok = false
            break
        }
        s.nextch()
    }

    s.setLit(StringLit, ok)
}
```

在 [rawString()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/syntax/scanner.go#L706) 函数中，不会做任何额外的校验逻辑，会读取两个反引号间的所有内容，最终最终字符串的值：

```go
func (s *scanner) rawString() {
    ok := true
    s.nextch()

    for {
        if s.ch == '`' {
            s.nextch()
            break
        }
        if s.ch < 0 {
            s.errorAtf(0, "string not terminated")
            ok = false
            break
        }
        s.nextch()
    }
    // We leave CRs in the string since they are part of the
    // literal (even though they are not part of the literal
    // value).

    s.setLit(StringLit, ok)
}
```

## 索引

可通过索引操作 `s[i]` 返回第 i 个字节的字节值，注意：

  1. 索引不能越界，即 $0 \leq i < len(s)$；
  2. 字符串不能修改。

```go
s := "Hello, world!"
fmt.Println(len(s)) // "12"
fmt.Println(s[0], s[7]) // "72 119" ('H' and 'w')
s[0] = "h" // compile error
```

也可通过 `s[i:j]` 生成 s 第 i 个字节到第 j 个字节（不包含 j）的子串，满足 $0 \leq i \leq j \leq len(s)$。当没有指定 i 或者 j 时，默认值分别为 `0` 和 `len(s)`。同样得益于字符串不允许被修改的特性，所有子串均可以安全地共享相同的字符数组，避免了额外的开销：

```go
s := "Hello, world!"
fmt.Printf("s:%s, %p\n", s, unsafe.StringData(s)) // "s:Hello, world!, 0x281b56"

s0 := s[:4]
fmt.Printf("s[:4]:%s, %p\n", s0, unsafe.StringData(s0)) // "s[:4]:Hell, 0x281b56" (与s一致)

s1 := s[1:4]
fmt.Printf("s[1:4]:%s, %p\n", s1, unsafe.StringData(s1)) // "s[1:4]:ell, 0x281b57" (偏移量为 57-56=1)

s2 := s[2:4]
fmt.Printf("s[2:4]:%s, %p\n", s2, unsafe.StringData(s2)) // "s[2:4]:ll, 0x281b58" (偏移量为 58-56=2)
```

## 运算

字符串默认支持了一些运算符，即可以使用 `<`, `<=`, `>`, `>=` 和 `==` 对字符串逐字节进行比较：

```go
s := "abcde"
t := "bbcde"
fmt.Println(s <= t) // "true"
```

也支持使用 `+` 来进行字符串拼接：

```go
s := "Hello"
s += ", world!"
fmt.Println(s) // "Hello, world!"
```

## Byte Slice

字符串的不可变性，保障了安全和性能，但是当我们需要一些字符串的拼接等逻辑时，则会多次触发字符串的赋值，导致了没必要的内存开销。

例如我们需要针对于一个整数字符串，每隔三个字符插入一个逗号分隔符，在仅使用 `string` 类时，每次循环都需要构建新的 `string` 结构体：

```go
func addComma(s string) string {
    n := len(s)
    if n <= 3 {
        return s
    }

    commaIndex := n % 3
    if commaIndex == 0 {
        commaIndex = 3
    }

    result := s[:commaIndex]
    for i := commaIndex; i < n; i += 3 {
        result += "," + s[i:i+3]
    }

    return result
}
```

相比之下，我们可以直接使用字节数组，即 `[]byte` 来满足我们修改字符串的需求，在 json 序列化等场景广泛使用。

使用 `[]byte` 重写以上功能，在每次调用 `append()` 方法进行字符拼接操作时，如果切片本身的空间足够大，则不会触发任何额外的内存分配，且我们可以更前置的计算所需空间来初始化一个较为合理的切片大小：

```go
func addComma(s string) string {
    n := len(s)
    if n <= 3 {
        return s
    }

    result := make([]byte, 0, n+n/3)

    r := n % 3
    if r == 0 {
        r = 3
    }

    result = append(result, s[:r]...)
    for i := r; i < n; i += 3 {
        result = append(result, ',')
        result = append(result, s[i:i+3]...)
    }

    return string(result)
}
```

### 类型转换

从底层数据结构上讲，字符串与字节切片底层都是字节数组，故而他们之间可以方便地相互转换：

```go
s := "hello"
b := []byte(s)
t := string(b)
```

但是在确保底层数据一致的同时，还需要保障字节切片的可变性与字符串的不变性，以及不会互相影响，所以一般都会创建一个原有字节数组的副本，这会带来额外的开销。

- [slicebytetostring()](https://github.com/golang/go/blob/go1.22.0/src/runtime/string.go#L81) 函数如下所示：

    ```go
    func slicebytetostring(buf *tmpBuf, ptr *byte, n int) string {
        if n == 0 {
            return ""
        }
        ...
        if n == 1 {
            p := unsafe.Pointer(&staticuint64s[*ptr])
            if goarch.BigEndian {
                p = add(p, 7)
            }
            return unsafe.String((*byte)(p), 1)
        }

        var p unsafe.Pointer
        if buf != nil && n <= len(buf) {
            p = unsafe.Pointer(buf)
        } else {
            p = mallocgc(uintptr(n), nil, false)
        }
        memmove(p, unsafe.Pointer(ptr), uintptr(n))
        return unsafe.String((*byte)(p), n)
    }
    ```

- [stringtoslicebyte()](https://github.com/golang/go/blob/go1.22.0/src/runtime/string.go#L166) 函数如下所示：

    ```go
    func stringtoslicebyte(buf *tmpBuf, s string) []byte {
        var b []byte
        if buf != nil && len(s) <= len(buf) {
            *buf = tmpBuf{}
            b = buf[:len(s)]
        } else {
            b = rawbyteslice(len(s))
        }
        copy(b, s)
        return b
    }
    ```

在上述代码中，可以发现均用到了一个名为 `tmpBuf` 的变量，实质是一个预先创建的长度为 32 的数组，当所需空间较小时，会直接使用该缓冲区，当所需空间大于该长度时，会额外进行一次内存分配。

```go
const tmpStringBufSize = 32
type tmpBuf [tmpStringBufSize]byte
```

### 零拷贝转换

在某些场景下，我们将 `[]byte` 强转为 `string`，仅仅是因为类型要求，并不会涉及到可变与不可变的差异，此时编译器会做一定的优化，通过共享底层字节数据的方式来生成临时的字符串，避免额外的性能开销，在 [slicebytetostringtmp()](https://github.com/golang/go/blob/go1.22.0/src/runtime/string.go#L150) 函数中提供了实现，同时也备注了几种常见的场景：

- 作为字典的 key 使用；
- 字符串拼接；
- 字符串比较。

```go
// slicebytetostringtmp returns a "string" referring to the actual []byte bytes.
//
// Callers need to ensure that the returned string will not be used after
// the calling goroutine modifies the original slice or synchronizes with
// another goroutine.
//
// The function is only called when instrumenting
// and otherwise intrinsified by the compiler.
//
// Some internal compiler optimizations use this function.
//   - Used for m[T1{... Tn{..., string(k), ...} ...}] and m[string(k)]
//     where k is []byte, T1 to Tn is a nesting of struct and array literals.
//   - Used for "<"+string(b)+">" concatenation where b is []byte.
//   - Used for string(b)=="foo" comparison where b is []byte.
func slicebytetostringtmp(ptr *byte, n int) string {
    return unsafe.String(ptr, n)
}
```

此外，在业务场景中，当我们能够确保 `string` 与 `[]byte` 在共享底层字节数组时的安全性时，也可以手动实现如上的零拷贝方案来提升性能。

Go 1.20 版本，[unsafe](https://github.com/golang/go/blob/go1.22.0/src/unsafe/unsafe.go) 包中新增了接口用于获取 `string` 和 `slice` 底层的数据指针和构造方案：

```go
func Slice(ptr *ArbitraryType, len IntegerType) []ArbitraryType
func SliceData(slice []ArbitraryType) *ArbitraryType
func String(ptr *byte, len IntegerType) string
func StringData(str string) *byte
```

此时，零拷贝的方案实现如下：

```go
func StringToBytes(s string) []byte {
    return unsafe.Slice(unsafe.StringData(s), len(s))
}

func BytesToString(b []byte) string {
    return unsafe.String(unsafe.SliceData(b), len(b))
}
```

而在 1.20 之前的版本，则需要使用反射的方案进行处理，[reflect](https://github.com/golang/go/blob/go1.22.0/src/reflect/value.go#L2840) 包定义了 `string` 和 `slice` 的运行时的结构：

```go
// Deprecated: Use unsafe.String or unsafe.StringData instead.
type StringHeader struct {
    Data uintptr
    Len  int
}

// Deprecated: Use unsafe.Slice or unsafe.SliceData instead.
type SliceHeader struct {
    Data uintptr
    Len  int
    Cap  int
}
```

我们可以通过手动构造如上结构体，实现类型的转换：

```go
func StringToBytes(s string) []byte {
    stringHeader := (*reflect.StringHeader)(unsafe.Pointer(&s))
    bh := reflect.SliceHeader{
        Data: stringHeader.Data,
        Len:  stringHeader.Len,
        Cap:  stringHeader.Len,
    }
    return *(*[]byte)(unsafe.Pointer(&bh))
}
func BytesToString(b []byte) string{
    sliceHeader := (*reflect.SliceHeader)(unsafe.Pointer(&b))
    sh := reflect.StringHeader{
        Data: sliceHeader.Data,
        Len:  sliceHeader.Len,
    }
    return *(*string)(unsafe.Pointer(&sh))
}
```

## 参考

- <https://draveness.me/golang/docs/part2-foundation/ch03-datastructure/golang-string/>
