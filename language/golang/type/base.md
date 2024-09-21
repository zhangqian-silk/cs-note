# 基础类型

## 整数

整数类型可以分为如下几种：

1. 明确声明了大小的类型，其中有符号整数用最高位 bit 来表示正负，一个 `n-bit` 有符号整数对应的数域为 $-2^{n-1}$ 到 $2^{n-1}-1$，无符号整数对应的数域为 是 0 到 $2^n-1$：
    | Size | Signed | Unsigned |
    | :-: | :-: | :-: |
    | 8 bits | int8 | uint8 |
    | 16 bits | int16 | uint16 |
    | 32 bits | int32 | uint32 |
    | 64 bits | int64 | uint64 |

2. 针对于 CPU 平台的类型，`int` 和 `uint`，可能是 32 bits 或者 64 bits。

3. Unicode 字符所使用的 `rune` 类型，与 `int32` 等价，需要 4 bytes 来支持所有字符。`byte` 类型与 `uint8` 等价，但是 `byte` 一般用作数据的单位，而非用来表示具体数值。

4. 用于存储指针数值的无符号整型 `uintptr`，大小同样取决于操作系统的位数，但是足以容纳指针。

需要注意的是，各种类型虽然存在等价关系，但是在实际使用中，各类型间仍然是截然不同的，例如不能将一个 `int32` 类型的值与 `int16` 的值相加，此时必须进行类型强转：

```go
var apples int32 = 1
var oranges int16 = 2
var compote int = apples + oranges // compile error
var compote = int(apples) + int(oranges)
```

## 浮点数

浮点数包括 `float32` 与 `float64` 两种，符合 IEEE754 浮点数国际标准定义。

浮点数能够表示的数值范围很大，其中 `float32` 的数域约为 1.4e-45 至 3.4e38，`float64` 的范围约为 4.9e-324 至 1.8e308，但是受限于有效 bit，`float32` 的精度约为 6 个十进制数，`float64` 的精度约为 15 个十进制数。
