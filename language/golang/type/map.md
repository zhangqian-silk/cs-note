# 哈希表

哈希表是一系列键值对的无序集合，维护了 key 和 value 的映射关系，其中 key 不能重复，还提供了常数时间复杂度的读写性能。

## 数据结构

### hamp

在 Golang 中，`map` 类型实质上就是哈希表。数据结构 [hamp](https://github.com/golang/go/blob/go1.22.0/src/runtime/map.go#L117) 如下所示：

- `count`：当前哈希表的大小，即哈希表中元素的数量，可以通过 `len` 函数获取
- `flags`：当前哈希表的状态，用于并发控制
- `B`：当前桶的数量，其中 $2^B=len(buckets)$
- `noverflow`：溢出桶的近似数量，当溢出桶数量较多时，会触发扩容操作
- `hash0`：哈希种子，增加随机性
- `buckets`：指向桶数组，数组元素类型为 `bmap`
- `oldBuckets`：扩容时，指向扩容前的桶数组
- `nevacuate`：扩容时的数据迁移的进度，小于该数值的桶已经完成了迁移操作
- `extra`：可选字段，用于保存溢出桶的地址，防止溢出桶被 GC 回收

```go
const (
    // flags
    iterator     = 1 // there may be an iterator using buckets
    oldIterator  = 2 // there may be an iterator using oldbuckets
    hashWriting  = 4 // a goroutine is writing to the map
    sameSizeGrow = 8 // the current map growth is to a new map of the same size
)

// A header for a Go map.
type hmap struct {
    count     int // # live cells == size of map.  Must be first (used by len() builtin)
    flags     uint8
    B         uint8  // log_2 of # of buckets (can hold up to loadFactor * 2^B items)
    noverflow uint16 // approximate number of overflow buckets; see incrnoverflow for details
    hash0     uint32 // hash seed

    buckets    unsafe.Pointer // array of 2^B Buckets. may be nil if count==0.
    oldbuckets unsafe.Pointer // previous bucket array of half the size, non-nil only when growing
    nevacuate  uintptr        // progress counter for evacuation (buckets less than this have been evacuated)

    extra *mapextra // optional fields
}
```

### bmap

哈希函数会将不同的输入值，映射到一个定长的输出值，输入值不同时，哈希值有可能相同，被称作哈希碰撞，但是当哈希值不同时，输入值一定不同，通过对比哈希值，则可以加快数据匹配效率。

Golang 在最终查询哈希表中数据时，会先通过低 B 位确认桶号，再通过高 8 位来进行初步数据匹配，以提高查询效率。

相对应的，桶的数据结构为 [bmap](https://github.com/golang/go/blob/go1.22.0/src/runtime/map.go#L151)，其中 `tophash` 则用来存储哈希值的高 8 位的数组，加速数据匹配，数组长度为 `bucketCnt`，即 8 位，表示一个桶最多存放 8 个元素。

```go
const (
    // Maximum number of key/elem pairs a bucket can hold.
    MapBucketCountBits = 3 // log2 of number of elements in a bucket.
    MapBucketCount     = 1 << MapBucketCountBits
)

const (
    bucketCnt     = abi.MapBucketCount
)

type bmap struct {
    tophash [bucketCnt]uint8
}
```

在某些特殊情况下，桶中的 `tophash` 还会被用来存储特定的[标记位](https://github.com/golang/go/blob/go1.22.0/src/runtime/map.go#L94)：

- `emptyRest`：当前位置以及后续位置，包括溢出桶，都是空值
- `emptyOne`：当前位置是空值
- `evacuatedX` & `evacuatedY`：数据已经发生了迁移，位于新桶的前半部分或后半部分
- `evacuatedEmpty`：当前位置是空值，且桶已经进行了数据迁移
- `minTopHash`：高位哈希值的最小值，如果最终计算出的哈希值小于该数值，则会加上 `minTopHash` 用作新的高位哈希值，用来和其他标记位做区分

```go
const (
    // Possible tophash values. We reserve a few possibilities for special marks.
    emptyRest      = 0 // this cell is empty, and there are no more non-empty cells at higher indexes or overflows.
    emptyOne       = 1 // this cell is empty
    evacuatedX     = 2 // key/elem is valid.  Entry has been evacuated to first half of larger table.
    evacuatedY     = 3 // same as above, but evacuated to second half of larger table.
    evacuatedEmpty = 4 // cell is empty, bucket is evacuated.
    minTopHash     = 5 // minimum tophash for a normal filled cell.
)
```

此外，因为哈希表中存储的元素的数据结构不固定，所以桶具体的数据结构在编译期才会动态确认，类型生成方法为 [MapBucketType](https://github.com/golang/go/blob/a10e42f219abb9c5bc4e7d86d9464700a42c7d57/src/cmd/compile/internal/reflectdata/reflect.go#L91)，最终的数据结构如下所示：

- `keys` & `elems`：最终存储的键值对的数组，数组内元素类型为 key 和 value 对应的类型，数组长度同样为 8 位，与 `tophash` 保持一致
- `overflow`：指向溢出桶，当发生哈希碰撞，且当前桶中元素已经超过 8 个时，会临时建立溢出桶，通过链表的方式进行维护

```go
// Builds a type representing a Bucket structure for
// the given map type. This type is not visible to users -
// we include only enough information to generate a correct GC
// program for it.
// Make sure this stays in sync with runtime/map.go.

//     A "bucket" is a "struct" {
//             tophash [abi.MapBucketCount]uint8
//             keys [abi.MapBucketCount]keyType
//             elems [abi.MapBucketCount]elemType
//             overflow *bucket
//         }
func MapBucketType(t *types.Type) *types.Type {
    ...
}
```

### mapextra

在 [MapBucketType()](https://github.com/golang/go/blob/go1.22.0/src/cmd/compile/internal/reflectdata/reflect.go#L91) 中，对于 `overflow` 的类型生成有如下一段逻辑

```go
func MapBucketType(t *types.Type) *types.Type {
    ...
    // If keys and elems have no pointers, the map implementation
    // can keep a list of overflow pointers on the side so that
    // buckets can be marked as having no pointers.
    // Arrange for the bucket to have no pointers by changing
    // the type of the overflow field to uintptr in this case.
    // See comment on hmap.overflow in runtime/map.go.
    otyp := types.Types[types.TUNSAFEPTR]
    if !elemtype.HasPointers() && !keytype.HasPointers() {
        otyp = types.Types[types.TUINTPTR]
    }
    overflow := makefield("overflow", otyp)
    ...
}
```

即对于 `bmap` 中的 `overflow` 字段，会自动根据哈希表中的元素类型，来生成相对应的指针类型，当 key 或者 value 包含指针时，`overflow` 为 `unsafe.Pointer` 类型，直接指向溢出桶。

而当 key 和 value 均不包含指针时，`overflow` 为 `uintptr` 类型，此时整个 `bmap` 不存在任何指针元素（对于 GC 来说，`uintptr` 不会被认为是引用类型），从而避免了 GC 时的扫描成本。

但是对于溢出桶来说，此时会存在 GC 的问题，故需要另外一个存储结构，直接引用这些溢出桶，即 `hamp` 中的一个可选的 `extra` 字段，数据类型为 [mapextra](https://github.com/golang/go/blob/go1.22.0/src/runtime/map.go#L134)：

```go
type mapextra struct {
    // If both key and elem do not contain pointers and are inline, then we mark bucket
    // type as containing no pointers. This avoids scanning such maps.
    // However, bmap.overflow is a pointer. In order to keep overflow buckets
    // alive, we store pointers to all overflow buckets in hmap.extra.overflow and hmap.extra.oldoverflow.
    // overflow and oldoverflow are only used if key and elem do not contain pointers.
    // overflow contains overflow buckets for hmap.buckets.
    // oldoverflow contains overflow buckets for hmap.oldbuckets.
    // The indirection allows to store a pointer to the slice in hiter.
    overflow    *[]*bmap
    oldoverflow *[]*bmap

    // nextOverflow holds a pointer to a free overflow bucket.
    nextOverflow *bmap
}
```

当 `bmap` 中不包含指针时，即 key 和 elem 均不包含指针，overflow 类型为 `uintptr`，`hmap.extra` 中的 `overflow` 和 `oldoverflow` 字段，会分别引用 `hmap.buckets` 和 `hmap.oldbuckets` 中的溢出桶。

`nextOverFlow` 字段，则用于在内存预分配时，引用溢出桶数组中首个可用的溢出桶，并在之后使用的过程中，保持动态更新

## 初始化

Golang 支持通过字面量或者 `make` 函数来初始化哈希表：

```go
m1 := map[string]int{
    "key_m1_1": 1,
    "key_m1_2": 2,
}

m2 := make(map[string]int)
m2["key_m2_1"] = 1
m2["key_m2_2"] = 2

m3 := make(map[string]int, 4)
```

当未指定元素数量，或元素数量小于一个桶中元素的数量（8 个）时，会调用 [makemap_small()](https://github.com/golang/go/blob/go1.22.0/src/runtime/map.go#L294) 来创建哈希表，此时仅会创建一个空的哈希表，并指定哈希种子，在编译时才会具体分配其他属性的内存空间。

```go
// makemap_small implements Go map creation for make(map[k]v) and
// make(map[k]v, hint) when hint is known to be at most bucketCnt
// at compile time and the map needs to be allocated on the heap.
func makemap_small() *hmap {
    h := new(hmap)
    h.hash0 = uint32(rand())
    return h
}
```

对于其他场景，最终会调用 [makemap()](https://github.com/golang/go/blob/go1.22.0/src/runtime/map.go#L305) 函数来创建哈希表：

- 先进行内存空间溢出判断，此时按照极限情况下，每个桶中仅分配一个元素来计算，如果有溢出风险，则将 `hint` 值置为 0，按照最小值来分配内存
- 初始化哈希表，并指定随机种子，与 `makemap_small` 逻辑一致
- 根据 `hint` 值，计算桶的个数，如果计算出的 B 大于 0，则预分配桶数组对应的内存空间

```go
// makemap implements Go map creation for make(map[k]v, hint).
// If the compiler has determined that the map or the first bucket
// can be created on the stack, h and/or bucket may be non-nil.
// If h != nil, the map can be created directly in h.
// If h.buckets != nil, bucket pointed to can be used as the first bucket.
func makemap(t *maptype, hint int, h *hmap) *hmap {
    mem, overflow := math.MulUintptr(uintptr(hint), t.Bucket.Size_)
    if overflow || mem > maxAlloc {
        hint = 0
    }

    if h == nil {
        h = new(hmap)
    }
    h.hash0 = uint32(rand())

    B := uint8(0)
    for overLoadFactor(hint, B) {
        B++
    }
    h.B = B

    if h.B != 0 {
        var nextOverflow *bmap
        h.buckets, nextOverflow = makeBucketArray(t, h.B, nil)
        if nextOverflow != nil {
            h.extra = new(mapextra)
            h.extra.nextOverflow = nextOverflow
        }
    }

    return h
}
```

### 负载因子

在计算桶的数量时，会综合考虑内存的利用率与查找时的效率，桶的数量过多，显然会存在内存浪费的情况，而桶的数量过少，当出现碰撞时，则会增加溢出桶的数量，增加每次查找时所需比较的元素数量，进而减少查找效率。

对于哈希表中桶与元素的数量关系，有个核心指标叫做负载因子（loadFactor），用元素个数除以哈希表的容量来表示，即$loadFactor = num_{elems} / num_{buckets}$。

对于 Golang 来说，负载因子又可以表示为 $loadFactor = num_{elems} / (2^B)$，此外，每个桶中最多可以容纳 8 个元素，负载因子的最大值也同样为 8。

对于负载因子具体的取值，可以参考注释中给出的[测试报告](https://github.com/golang/go/blob/go1.22.0/src/runtime/map.go#L33)：

- loadFactor：负载因子
- %overflow：溢出率
- bytes/entry：平均每对键值对消耗的字节数
- hitprobe：查找一个存在的 key 时，平均查找个数
- missprobe：查找一个不存在的 key 时，平均查找个数

```go
// Picking loadFactor: too large and we have lots of overflow
// buckets, too small and we waste a lot of space. I wrote
// a simple program to check some stats for different loads:
// (64-bit, 8 byte keys and elems)
//  loadFactor    %overflow  bytes/entry     hitprobe    missprobe
//        4.00         2.13        20.77         3.00         4.00
//        4.50         4.05        17.30         3.25         4.50
//        5.00         6.85        14.77         3.50         5.00
//        5.50        10.55        12.94         3.75         5.50
//        6.00        15.27        11.67         4.00         6.00
//        6.50        20.90        10.79         4.25         6.50
//        7.00        27.14        10.15         4.50         7.00
//        7.50        34.03         9.73         4.75         7.50
//        8.00        41.10         9.40         5.00         8.00
//
// %overflow   = percentage of buckets which have an overflow bucket
// bytes/entry = overhead bytes used per key/elem pair
// hitprobe    = # of entries to check when looking up a present key
// missprobe   = # of entries to check when looking up an absent key

const (
    // Maximum average load of a bucket that triggers growth is bucketCnt*13/16 (about 80% full)
    // Because of minimum alignment rules, bucketCnt is known to be at least 8.
    // Represent as loadFactorNum/loadFactorDen, to allow integer math.
    loadFactorDen = 2
    loadFactorNum = loadFactorDen * abi.MapBucketCount * 13 / 16
)
```

最终综合考虑各项指标，最终选择了 6.5 作为负载因子的默认值，并且为了方便进行整数运算，优化了相关常量的表达式。

### 内存分配

在初始化时，内存的预分配，也会根据负载因子来进行判断，在 [overLoadFactor()](https://github.com/golang/go/blob/go1.22.0/src/runtime/map.go#L1097) 函数中，当元素数量小于一个桶时，直接返回 `false`，此时 B 的值为 0，相对应的桶的数量为 $2^0=1$，足够容纳所有元素。

当元素数量大于一个桶时，是否超过负载因子的判断从 $(num_{elems} / (2^B)) > loadFactor$ 优化为了判断 $num_{elems} > (loadFactorNum * (2^B / loadFactorDen))$，会扩大 B 的值，直至满足负载因子。

```go
func makemap(t *maptype, hint int, h *hmap) *hmap {
    ...
    B := uint8(0)
    for overLoadFactor(hint, B) {
        B++
    }
    h.B = B
    ...
}

// overLoadFactor reports whether count items placed in 1<<B buckets is over loadFactor.
func overLoadFactor(count int, B uint8) bool {
    return count > bucketCnt && uintptr(count) > loadFactorNum*(bucketShift(B)/loadFactorDen)
}

// bucketShift returns 1<<b, optimized for code generation.
func bucketShift(b uint8) uintptr {
    return uintptr(1) << (b & (goarch.PtrSize*8 - 1))
}
```

在计算出合适的桶的数量后，将通过 [makeBucketArray()](https://github.com/golang/go/blob/go1.22.0/src/runtime/map.go#L346) 函数来创建桶数组：

```go
func makemap(t *maptype, hint int, h *hmap) *hmap {
    ...
    if h.B != 0 {
        var nextOverflow *bmap
        h.buckets, nextOverflow = makeBucketArray(t, h.B, nil)
        if nextOverflow != nil {
            h.extra = new(mapextra)
            h.extra.nextOverflow = nextOverflow
        }
    }

    return h
}
```

- 首先判断 B 的大小，如果 B 小于 4，说明整体数据量较少，溢出的概率较低，会省略溢出桶的分配，否则，将额外预创建 $2^{B-4}$ 个溢出桶

    ```go
    func makeBucketArray(t *maptype, b uint8, dirtyalloc unsafe.Pointer) (buckets unsafe.Pointer, nextOverflow *bmap) {
        base := bucketShift(b)
        nbuckets := base
        if b >= 4 {
            nbuckets += bucketShift(b - 4)
            sz := t.Bucket.Size_ * nbuckets
            up := roundupsize(sz, !t.Bucket.Pointers())
            if up != sz {
                nbuckets = up / t.Bucket.Size_
            }
        }
        ...
    }
    ```

- 然后判断 `dirtyalloc` 指针，如果为空，则直接创建对应数组，可以看到正常的桶与溢出桶，是在一起进行内存分配的，其空间地址是连续的
- 如果 `dirtyalloc` 指针非空，则说明前置已进行过初始化，只需将其中的数据清空即可

    ```go
    func makeBucketArray(t *maptype, b uint8, dirtyalloc unsafe.Pointer) (buckets unsafe.Pointer, nextOverflow *bmap) {
        ...
        if dirtyalloc == nil {
            buckets = newarray(t.Bucket, int(nbuckets))
        } else {
            buckets = dirtyalloc
            size := t.Bucket.Size_ * nbuckets
            if t.Bucket.Pointers() {
                memclrHasPointers(buckets, size)
            } else {
                memclrNoHeapPointers(buckets, size)
            }
        }
        ...
    }
    ```

- 最终通过分配的数组长度来判断是否存在溢出桶，并分别返回普通桶的数组地址和溢出桶的数组地址
- 除此以外，还额外将最后一个溢出桶的 `overflow` 指针，指向了第一个普通桶，其他溢出桶的 `overflow` 指针均为 `nil`，这个差异会用于判断当前是否还有空余的溢出桶

    ```go
    func makeBucketArray(t *maptype, b uint8, dirtyalloc unsafe.Pointer) (buckets unsafe.Pointer, nextOverflow *bmap) {
        ...
        if base != nbuckets {
            nextOverflow = (*bmap)(add(buckets, base*uintptr(t.BucketSize)))
            last := (*bmap)(add(buckets, (nbuckets-1)*uintptr(t.BucketSize)))
            last.setoverflow(t, (*bmap)(buckets))
        }
        return buckets, nextOverflow
    }
    ```

## 数据处理

### 新增 & 修改数据

新增或是修改数据，均通过 `m[k] = v` 的方式进行：

```go
m := map[string]int{"key_1": 1}
m["key_2"] = 2
m["key_1"] = 100
fmt.Println(m) // "map[key_1:100 key_2:2]"
```

在底层，则是通过 [mapassign()](https://github.com/golang/go/blob/go1.22.0/src/runtime/map.go#L579) 函数实现相关功能：

- 判断 key 是否已经存在，若存在则修改 value 值
- 若 key 不存在，则寻找可插入新数据的位置
- 若不存在可插入位置，则触发扩容或是新增溢出桶进行存储
- 最终返回 value 对象对应的指针，由函数调用方进行修改

#### [mapassign()](https://github.com/golang/go/blob/go1.22.0/src/runtime/map.go#L579) 函数

函数详细介绍如下：

- 预处理：
  - 校验哈希表本身的初始化
  - 校验哈希表的并发写问题，哈希表本身不是线程安全的
  - 生成 key 对应的哈希值
  - 设置 `hashWriting` 标记位
  - 校验桶数组，若不存在则进行初始化

    ```go
    func mapassign(t *maptype, h *hmap, key unsafe.Pointer) unsafe.Pointer {
        if h == nil {
            panic(plainError("assignment to entry in nil map"))
        }
        ...
        if h.flags&hashWriting != 0 {
            fatal("concurrent map writes")
        }
        hash := t.Hasher(key, uintptr(h.hash0))

        // Set hashWriting after calling t.hasher, since t.hasher may panic,
        // in which case we have not actually done a write.
        h.flags ^= hashWriting

        if h.buckets == nil {
            h.buckets = newobject(t.Bucket) // newarray(t.Bucket, 1)
        }
        ...
    }
    ```

- 确认哈希桶，声明或初始化一些关键变量
  - 桶号由哈希值的低 B 位来确认，`hash & (1<<B-1)` 最终的结果，等价于 `hash % 2^B`
  - 确认当前哈希表的扩容状态，如果正在进行扩容，则触发一次扩容操作，等待当前 hash 桶数据迁移完成（后文详细介绍）
  - 通过桶号获取桶的地址
  - 计算高位的哈希值，并即确保哈希值的最小值，兼容标记位逻辑
  - 声明 key 值索引的指针 `inserti`
  - 声明 key 和 value 的指针 `insertk` 和 `elem`

    ```go
    func mapassign(t *maptype, h *hmap, key unsafe.Pointer) unsafe.Pointer {
        ...
    again:
        bucket := hash & bucketMask(h.B)
        if h.growing() {
            growWork(t, h, bucket)
        }
        b := (*bmap)(add(h.buckets, bucket*uintptr(t.BucketSize)))
        top := tophash(hash)

        var inserti *uint8
        var insertk unsafe.Pointer
        var elem unsafe.Pointer
        ...
    }

    // bucketMask returns 1<<b - 1, optimized for code generation.
    func bucketMask(b uint8) uintptr {
        return bucketShift(b) - 1
    }

    // tophash calculates the tophash value for hash.
    func tophash(hash uintptr) uint8 {
        top := uint8(hash >> (goarch.PtrSize*8 - 8))
        if top < minTopHash {
            top += minTopHash
        }
        return top
    }
    ```

- 遍历哈希桶，优先寻找当前 key 和 value 对应的位置，否则寻找一个最靠前的可插入的位置
  - 若 `tophash` 不匹配，桶中当前位置为空，且 key 的索引的指针为空，则更新 key 与 value 相关指针，记录下该位置
  - 若 `tophash` 不匹配，桶中当前位置以及之后位置为空，则结束当前循环，否则继续进行循环
  - 若 `tophash` 匹配，则尝试匹配当前位置对应的 key 值，若不匹配，则继续进行循环
  - 若 `tophash` 匹配，且当前位置的 key 值也匹配，则说明匹配成功，更新 key 值，获取 value 的地址，并直接前往 `done` 所对应的代码块，最终返回 value 对应的指针（在函数外部进行更新）
  - 若当前哈希桶遍历结束（一个桶中最多 8 个元素），且仍存在溢出桶，则继续按照如上逻辑，遍历溢出桶，否则结束当前循环

    ```go
    func mapassign(t *maptype, h *hmap, key unsafe.Pointer) unsafe.Pointer {
        ...
    bucketloop:
        for {
            for i := uintptr(0); i < abi.MapBucketCount; i++ {
                if b.tophash[i] != top {
                    if isEmpty(b.tophash[i]) && inserti == nil {
                        inserti = &b.tophash[i]
                        insertk = add(unsafe.Pointer(b), dataOffset+i*uintptr(t.KeySize))
                        elem = add(unsafe.Pointer(b), dataOffset+abi.MapBucketCount*uintptr(t.KeySize)+i*uintptr(t.ValueSize))
                    }
                    if b.tophash[i] == emptyRest {
                        break bucketloop
                    }
                    continue
                }
                k := add(unsafe.Pointer(b), dataOffset+i*uintptr(t.KeySize))
                if t.IndirectKey() {
                    k = *((*unsafe.Pointer)(k))
                }
                if !t.Key.Equal(key, k) {
                    continue
                }
                // already have a mapping for key. Update it.
                if t.NeedKeyUpdate() {
                    typedmemmove(t.Key, k, key)
                }
                elem = add(unsafe.Pointer(b), dataOffset+abi.MapBucketCount*uintptr(t.KeySize)+i*uintptr(t.ValueSize))
                goto done
            }
            ovf := b.overflow(t)
            if ovf == nil {
                break
            }
            b = ovf
        }
    }
    ```

- 若如上循环结束时，未找到已经存在的 key 值，则执行插入逻辑，判断扩容和溢出桶
  - 执行插入逻辑前，优先判断是否需要进行扩容，若需要，则执行扩容逻辑，此时桶号会发生变化（桶号为低 B 位，但是 B 会发生变化），所以要回到 `again` 处重新查找桶号以及所要插入的位置
  - 若不需要扩容，且没有找到合适的插入位置，则创建新的溢出桶，并更新 key 和 value 相关的指针为新的溢出桶的首位

    ```go
    func mapassign(t *maptype, h *hmap, key unsafe.Pointer) unsafe.Pointer {
        ...
        // Did not find mapping for key. Allocate new cell & add entry.

        // If we hit the max load factor or we have too many overflow buckets,
        // and we're not already in the middle of growing, start growing.
        if !h.growing() && (overLoadFactor(h.count+1, h.B) || tooManyOverflowBuckets(h.noverflow, h.B)) {
            hashGrow(t, h)
            goto again // Growing the table invalidates everything, so try again
        }

        if inserti == nil {
            // The current bucket and all the overflow buckets connected to it are full, allocate a new one.
            newb := h.newoverflow(t, b)
            inserti = &newb.tophash[0]
            insertk = add(unsafe.Pointer(newb), dataOffset)
            elem = add(insertk, abi.MapBucketCount*uintptr(t.KeySize))
        }
        ...
    }
    ```

- 执行数据插入逻辑
  - 判断 key 和 value 是否是间接引用，若是，则创建对应类型的新的对象，并将地址保存至所要插入的位置处，然后更新 key 的指针为实际存储 key 对应地址的指针（value 对应的指针在函数 `done` 代码块中会统一处理）
  - 更新 key 对应的值（如果值未发生改变，则不做处理，对于 value，最终会返回 value 对应的指针，在外部进行更新）
  - 更新 key 的索引的值，即 `tophash` 的值
  - 修改哈希表中元素个数

    ```go
    func mapassign(t *maptype, h *hmap, key unsafe.Pointer) unsafe.Pointer {
        ...
        // store new key/elem at insert position
        if t.IndirectKey() {2
            kmem := newobject(t.Key)
            *(*unsafe.Pointer)(insertk) = kmem
            insertk = kmem
        }
        if t.IndirectElem() {
            vmem := newobject(t.Elem)
            *(*unsafe.Pointer)(elem) = vmem
        }
        typedmemmove(t.Key, insertk, key)
        *inserti = top
        h.count++
        ...
    }
    ```

- 最终参数处理
  - 判断并发写冲突
  - 清除 `hashWriting` 状态位，标记写入完成
  - 更新 value 指针，最终返回 value 实际存储的地址对应的指针，用于外部修改 value

    ```go
    func mapassign(t *maptype, h *hmap, key unsafe.Pointer) unsafe.Pointer {
        ...
    done:
        if h.flags&hashWriting == 0 {
            fatal("concurrent map writes")
        }
        h.flags &^= hashWriting
        if t.IndirectElem() {
            elem = *((*unsafe.Pointer)(elem))
        }
        return elem
    }
    ```

#### [newoverflow()](https://github.com/golang/go/blob/go1.22.0/src/runtime/map.go#L247) 函数

一个哈希桶中最多可以存放 8 个元素，当哈希冲突超过这个数值时，需要使用溢出桶来存放新增元素。当需要新增溢出桶时，会通过 [newoverflow()](https://github.com/golang/go/blob/go1.22.0/src/runtime/map.go#L247) 函数处理相关逻辑。

- 创建溢出桶
  - 优先判断哈希表中的 `extra.nextOverflow` 指针是否为空，若非空，则说明创建哈希表时，预分配的溢出桶还存在，可以直接使用
  - 若在使用了该溢出桶后，还有剩余，则动态更新 `extra.nextOverflow` 指针，指向下一个可用的溢出桶
  - 若当前溢出桶为预分配的最后一个，即 `overflow` 指针非空（初始化时，最后一个溢出桶的 `overflow` 指针会指向第一个普通桶，其他溢出桶的 `overflow` 指针为空），则将其 `overflow` 指针重新置为空值，用于后续链接其他溢出桶，并将哈希表中的 `extra.nextOverflow` 指针置为空值，表示当前预分配的溢出桶，已全部消耗
  - 若当前不存在可用的溢出桶，则直接创建

    ```go
    func (h *hmap) newoverflow(t *maptype, b *bmap) *bmap {
        var ovf *bmap
        if h.extra != nil && h.extra.nextOverflow != nil {
            // We have preallocated overflow buckets available.
            // See makeBucketArray for more details.
            ovf = h.extra.nextOverflow
            if ovf.overflow(t) == nil {
                // We're not at the end of the preallocated overflow buckets. Bump the pointer.
                h.extra.nextOverflow = (*bmap)(add(unsafe.Pointer(ovf), uintptr(t.BucketSize)))
            } else {
                // This is the last preallocated overflow bucket.
                // Reset the overflow pointer on this bucket,
                // which was set to a non-nil sentinel value.
                ovf.setoverflow(t, nil)
                h.extra.nextOverflow = nil
            }
        } else {
            ovf = (*bmap)(newobject(t.Bucket))
        }
        ...
    }

    ```

- 更新溢出桶相关配置
  - 更新哈希表的溢出桶计数，即 `noverflow` 字段，用于扩容等逻辑使用
  - 当哈希表中的 key 和 value 不包含指针时，额外修改 `extra.overflow` 字段，将该溢出桶添加进去，避免被 GC 回收
  - 最后将该溢出桶链接在原本的桶链表上，返回溢出桶指针

    ```go
    func (h *hmap) newoverflow(t *maptype, b *bmap) *bmap {
        ...
        h.incrnoverflow()
        if !t.Bucket.Pointers() {
            h.createOverflow()
            *h.extra.overflow = append(*h.extra.overflow, ovf)
        }
        b.setoverflow(t, ovf)
        return ovf
    }

    func (h *hmap) createOverflow() {
        if h.extra == nil {
            h.extra = new(mapextra)
        }
        if h.extra.overflow == nil {
            h.extra.overflow = new([]*bmap)
        }
    }
    ```

### 读取数据

可以通过哈希表的下标 `m[key]` 来获取指定 key 所对应的 value，返回值有两种形式：

- 当接受一个参数时，会返回读取结果，如果 key 不存在，则返回 value 元素类型的零值
- 当接受两个参数时，还会额外返回一个布尔值，标记 key 是否存在

    ```go
    m := map[string]int{"key_1": 1}
    res1 := m["key_1"]
    res2, ok := m["key_2"]
    fmt.Println(res1, res2, ok) // "1 0 false"
    ```

两种形式在底层实现分别为 [mapaccess1()](https://github.com/golang/go/blob/go1.22.0/src/runtime/map.go#L396) 和 [mapaccess2()](https://github.com/golang/go/blob/go1.22.0/src/runtime/map.go#L457)，唯一的差异点仅在于返回值。函数内部主要功能为查找 key 所对应的值，与写操作时的逻辑基本一致。

#### [mapaccess2()](https://github.com/golang/go/blob/go1.22.0/src/runtime/map.go#L457)

以 `mapaccess2()` 函数为例，详细介绍如下：

- 预处理
  - 判空处理，如果 map 未进行初始化，或内部元素数量为 0，则直接返回默认值
  - 标记位校验，判断并发读写问题

    ```go
    func mapaccess2(t *maptype, h *hmap, key unsafe.Pointer) (unsafe.Pointer, bool) {
        ...
        if h == nil || h.count == 0 {
            if err := mapKeyError(t, key); err != nil {
                panic(err) // see issue 23734
            }
            return unsafe.Pointer(&zeroVal[0]), false
        }
        if h.flags&hashWriting != 0 {
            fatal("concurrent map read and map write")
        }
        ...
    }
    ```

- 确认 key 对应的哈希值以及哈希桶
  - 计算 hash 值，并获取低 B 位哈希值和高 8 哈希值
  - 通过低 B 位哈希值获取哈希桶
  - 如果哈希桶正在扩容，则根据扩容类型，定位旧桶的地址，且如果旧桶中的数据未完成迁移，则将哈希桶地址修改为旧桶地址，从旧桶中查找
  - 需要注意的是，读操作不会等待扩容完成

    ```go
    func mapaccess2(t *maptype, h *hmap, key unsafe.Pointer) (unsafe.Pointer, bool) {
        ...
        hash := t.Hasher(key, uintptr(h.hash0))
        m := bucketMask(h.B)
        b := (*bmap)(add(h.buckets, (hash&m)*uintptr(t.BucketSize)))
        if c := h.oldbuckets; c != nil {
            if !h.sameSizeGrow() {
                // There used to be half as many buckets; mask down one more power of two.
                m >>= 1
            }
            oldb := (*bmap)(add(c, (hash&m)*uintptr(t.BucketSize)))
            if !evacuated(oldb) {
                b = oldb
            }
        }
        top := tophash(hash)
        ...
    }
    ```

- 遍历哈希桶，寻找对应的 key 值
  - 通过 `for` 循环遍历当前哈希桶以及溢出桶（溢出桶以链表的形式相互连接）
  - 在 `tophash` 不一致时，判断当前桶中的 `tophash` 是否为 `emptyRest` 状态位，如果是的话，说明后续空间均为空，直接结束循环
  - 在 `tophash` 一致时，对比 key 值，判断是否找到真正的 key，如果不一致则进行下一次循环
  - 在 `tophash` 一致，且 key 值一致时，返回相对应的 value 值

    ```go
    func mapaccess2(t *maptype, h *hmap, key unsafe.Pointer) (unsafe.Pointer, bool) {
        ...
    bucketloop:
        for ; b != nil; b = b.overflow(t) {
            for i := uintptr(0); i < abi.MapBucketCount; i++ {
                if b.tophash[i] != top {
                    if b.tophash[i] == emptyRest {
                        break bucketloop
                    }
                    continue
                }
                k := add(unsafe.Pointer(b), dataOffset+i*uintptr(t.KeySize))
                if t.IndirectKey() {
                    k = *((*unsafe.Pointer)(k))
                }
                if t.Key.Equal(key, k) {
                    e := add(unsafe.Pointer(b), dataOffset+abi.MapBucketCount*uintptr(t.KeySize)+i*uintptr(t.ValueSize))
                    if t.IndirectElem() {
                        e = *((*unsafe.Pointer)(e))
                    }
                    return e, true
                }
            }
        }
        return unsafe.Pointer(&zeroVal[0]), false
    }
    ```

#### For Range

此外，哈希表同样支持 `for range` 进行遍历：

- 每次遍历可以拿到对应的 key 和 value
- 遍历的顺序是无序的

    ```go
    m := map[string]int{"key_1": 1, "key_2": 2}
    for key, v := range m {
        fmt.Println(key, v)
    }
    // "key_2 2"
    // "key_1 1"
    ```

### 删除数据

对于 map 中特定 key 值的删除操作，通过 `delete` 函数实现，函数内部对于 key 是否存在等情况做了判断，会保障调用该函数后，map 中一定不存在指定的 key，该函数也没有返回值来标记操作结果。

```go
m := map[string]int{"key_1": 1, "key_2": 2}
fmt.Println(m) // "map[key_1:1 key_2:2]"
delete(m, "key_1")
fmt.Println(m) // "map[key_2:2]"
```

底层通过 [mapdelete()](https://github.com/golang/go/blob/go1.22.0/src/runtime/map.go#L696) 函数来实现相关能力，核心逻辑与读、写操作较为一致，均是通过哈希值定位哈希桶，再通过遍历找到 key 值，但是在找到 key 值并删除数据的基础上，还进行了很多优化处理。

#### [mapdelete()](https://github.com/golang/go/blob/go1.22.0/src/runtime/map.go#L696)

函数逻辑介绍如下所示：

- 预处理：
  - 判空处理
  - 并发标记位处理

    ```go
    func mapdelete(t *maptype, h *hmap, key unsafe.Pointer) {
        ...
        if h == nil || h.count == 0 {
            if err := mapKeyError(t, key); err != nil {
                panic(err) // see issue 23734
            }
            return
        }
        if h.flags&hashWriting != 0 {
            fatal("concurrent map writes")
        }
        ...
    }
    ```

- 参数初始化：
  - 初始化哈希值，并找到对应的桶号
  - 如果正则扩容，则等待扩容迁移操作完成
  - 确认哈希桶地址与高位哈希值

    ```go
    func mapdelete(t *maptype, h *hmap, key unsafe.Pointer) {
        ...
        hash := t.Hasher(key, uintptr(h.hash0))

        // Set hashWriting after calling t.hasher, since t.hasher may panic,
        // in which case we have not actually done a write (delete).
        h.flags ^= hashWriting

        bucket := hash & bucketMask(h.B)
        if h.growing() {
            growWork(t, h, bucket)
        }
        b := (*bmap)(add(h.buckets, bucket*uintptr(t.BucketSize)))
        bOrig := b
        top := tophash(hash)
        ...
    }
    ```

- 查找目标元素：
  - 哈希桶与溢出桶以链表的方式连接，故通过两层循环遍历所有数据
  - 遍历时优先比较高位哈希，不匹配时判断下此处的高位哈希是否为 `emptyRest` 状态位（表示当前以及后续均为空），如果是，则结束遍历，否则继续遍历操作
  - 高位哈希值匹配成功后，再真正比较 key 值是否相等，如果相等，则执行后续删除逻辑，否则继续遍历操作

    ```go
    func mapdelete(t *maptype, h *hmap, key unsafe.Pointer) {
        ...
    search:
        for ; b != nil; b = b.overflow(t) {
            for i := uintptr(0); i < abi.MapBucketCount; i++ {
                if b.tophash[i] != top {
                    if b.tophash[i] == emptyRest {
                        break search
                    }
                    continue
                }
                k := add(unsafe.Pointer(b), dataOffset+i*uintptr(t.KeySize))
                k2 := k
                if t.IndirectKey() {
                    k2 = *((*unsafe.Pointer)(k2))
                }
                if !t.Key.Equal(key, k2) {
                    continue
                }
                ...
            }
        }
        ...
    }
    ```

- 删除目标元素：
  - 对于 key，只对指针本身进行处理，最终的数据由 GC 进行清理，不包含指针的场景则不做处理，可以直接进行覆盖
  - 对于 value，除了清理指针以外，非指针的数据也会进行清空
  - 设置标记位，表示当前位置为空，可以直接写入
  - 值得注意的是，如果 key 值不包含指针，并没有进行清空处理
    - 在写入新元素的 key 时，使用 `typedmemmove()` 函数进行处理，函数内部对覆盖前后的数据有做判等处理
    - 此时，删除时优化了一次清理操作，推迟至写入时进行覆盖，如果下次写入的 key 值与该位置未清理的 key 值相等，则进一步优化一次覆盖操作
    - 对于 hashmap 来说，一方面本身对 key 做了 hash 处理，会尽可能减少冲突的可能性，所以写入同一个 hash 桶中的元素有一定相等的可能性
    - 另一方面从业务上讲，大部分 hashmap 实例中，key 的取值其实是可穷举的，进一步加大了 key 值重复的可能性
    - 而对于 value 而言，猜测是重复的概率极低，故没有通过上述方案进行处理

    ```go
    func mapdelete(t *maptype, h *hmap, key unsafe.Pointer) {
        ...
    search:
        for ; b != nil; b = b.overflow(t) {
            for i := uintptr(0); i < abi.MapBucketCount; i++ {
                ...
                // Only clear key if there are pointers in it.
                if t.IndirectKey() {
                    *(*unsafe.Pointer)(k) = nil
                } else if t.Key.Pointers() {
                    memclrHasPointers(k, t.Key.Size_)
                }
                e := add(unsafe.Pointer(b), dataOffset+abi.MapBucketCount*uintptr(t.KeySize)+i*uintptr(t.ValueSize))
                if t.IndirectElem() {
                    *(*unsafe.Pointer)(e) = nil
                } else if t.Elem.Pointers() {
                    memclrHasPointers(e, t.Elem.Size_)
                } else {
                    memclrNoHeapPointers(e, t.Elem.Size_)
                }
                b.tophash[i] = emptyOne
                ...
            }
        }
        ...
    }
    ```

- 上述操作中，将当前位置设为空值，但是还可以根据哈希桶中其他位置的实际情况，进行优化，首先判断当前元素是否为哈希桶中最后一个
  - 如果当前元素是当前桶中最后一个，后续溢出桶存在，且溢出桶中第一个元素的标记位不是 `emptyRest`，说明后面还有元素
  - 如果当前元素不是桶中最后一个，且后一个元素的标记位不是 `emptyRest`，同样说明后面还有其他元素
  - 除此以外，说明后面没有其他元素，即当前元素是最后一个

    ```go
    func mapdelete(t *maptype, h *hmap, key unsafe.Pointer) {
        ...
    search:
        for ; b != nil; b = b.overflow(t) {
            for i := uintptr(0); i < abi.MapBucketCount; i++ {
                ...
                // If the bucket now ends in a bunch of emptyOne states,
                // change those to emptyRest states.
                // It would be nice to make this a separate function, but
                // for loops are not currently inlineable.
                if i == abi.MapBucketCount-1 {
                    if b.overflow(t) != nil && b.overflow(t).tophash[0] != emptyRest {
                        goto notLast
                    }
                } else {
                    if b.tophash[i+1] != emptyRest {
                        goto notLast
                    }
                }
                ...
            }
        }
        ...
    }
    ```

- 针对最后一个元素
  - 将标记位重新设置为 `emptyRest`（表示当前以及后续均为空）
  - 之后再前向遍历相邻位置，如果是空值，即 `emptyOne`，则同样更新为 `emptyRest`

    ```go
    func mapdelete(t *maptype, h *hmap, key unsafe.Pointer) {
        ...
    search:
        for ; b != nil; b = b.overflow(t) {
            for i := uintptr(0); i < abi.MapBucketCount; i++ {
                ...
                for {
                    b.tophash[i] = emptyRest
                    if i == 0 {
                        if b == bOrig {
                            break // beginning of initial bucket, we're done.
                        }
                        // Find previous bucket, continue at its last entry.
                        c := b
                        for b = bOrig; b.overflow(t) != c; b = b.overflow(t) {
                        }
                        i = abi.MapBucketCount - 1
                    } else {
                        i--
                    }
                    if b.tophash[i] != emptyOne {
                        break
                    }
                }
                ...
            }
        }
        ...
    }
    ```

- 最终处理
  - 删除目标元素后，更新元素数量，如果当前 map 中元素数量为空，则重置 hash 种子
  - 进行并发校验，并修改 map 的写标记位

    ```go
    func mapdelete(t *maptype, h *hmap, key unsafe.Pointer) {
        ...
    search:
        for ; b != nil; b = b.overflow(t) {
            for i := uintptr(0); i < abi.MapBucketCount; i++ {
                ...
            notLast:
                h.count--
                // Reset the hash seed to make it more difficult for attackers to
                // repeatedly trigger hash collisions. See issue 25237.
                if h.count == 0 {
                    h.hash0 = uint32(rand())
                }
                break search
            }
        }

        if h.flags&hashWriting == 0 {
            fatal("concurrent map writes")
        }
        h.flags &^= hashWriting
    }
    ```

### 清空

对于将 map 进行清空，并没有提供额外的函数进行调用，只能通过如下两种方式实现：

- 修改 map 变量的引用，原本的变量被 GC 回收
- 循环调用 `delete` 函数

在直观对比上，方法一更简洁，但是会存在一定的资源浪费，方法二遍历操作会有额外耗时，但是实际上编译器会对方法二做一定优化，最终调用 [mapclear()](https://github.com/golang/go/blob/go1.22.0/src/runtime/map.go#L989) 函数进行处理，将 hashmap 相关的一系列标记位进行重置，例如：

- 修改 count 值为 0
- 修改所有位置的 tophash 值为 `emptyRest`
- 重置溢出桶相关逻辑
- 重置扩容相关逻辑

## 扩容

哈希表在初始化时，会根据容量来进行内存分配，但是在不断插入新数据后，不可避免的会导致性能的劣化，在 [mapassign()](https://github.com/golang/go/blob/go1.22.0/src/runtime/map.go#L579) 函数中有提到，在如下两种情况，会触发扩容操作：

- 负载因子过高（与初始化时判断 B 的大小的方法一致）
- 溢出桶过多（接近普通桶的数量）

    ```go
    func mapassign(t *maptype, h *hmap, key unsafe.Pointer) unsafe.Pointer {
        ...
        // If we hit the max load factor or we have too many overflow buckets,
        // and we're not already in the middle of growing, start growing.
        if !h.growing() && (overLoadFactor(h.count+1, h.B) || tooManyOverflowBuckets(h.noverflow, h.B)) {
            hashGrow(t, h)
            ...
        }
        ...
    }
    ```

在 [hashGrow()](https://github.com/golang/go/blob/go1.22.0/src/runtime/map.go#L1053) 函数内部执行扩容操作时，也会根据两种情况的不同，来进行不同的扩容操作：

- 增量扩容 & 负载因子过高：此时说明 map 内部元素的填充率较高，需要更大的容量
- 等量扩容 & 溢出桶过多：此时说明 map 内部元素数量可能并不多，但是由于其他原因，导致数据分布较为松散，溢出桶数量较多，比如不停的插入数据后再删除数据，此时重新调整元素位置即可

### [hashGrow()](https://github.com/golang/go/blob/go1.22.0/src/runtime/map.go#L1053)

- 设置标记位
  - 通过负载因子，区分增量扩容和等量扩容
  - 增量扩容会将 B 加 1(`bigger`)
  - 等量扩容容量不变，通过 `sameSizeGrow` 进行标识

    ```go
    func hashGrow(t *maptype, h *hmap) {
        // If we've hit the load factor, get bigger.
        // Otherwise, there are too many overflow buckets,
        // so keep the same number of buckets and "grow" laterally.
        bigger := uint8(1)
        if !overLoadFactor(h.count+1, h.B) {
            bigger = 0
            h.flags |= sameSizeGrow
        }
        ...
    }
    ```

- 修改桶数组
  - 将原本的桶保存至 `oldbuckets` 中
  - 分配新的桶数组与溢出桶（与初始化时一致）
  - 修改迭代器标记位，如果当前处于迭代中，标记使用原本的桶进行迭代

    ```go
    func hashGrow(t *maptype, h *hmap) {
        ...
        oldbuckets := h.buckets
        newbuckets, nextOverflow := makeBucketArray(t, h.B+bigger, nil)

        flags := h.flags &^ (iterator | oldIterator)
        if h.flags&iterator != 0 {
            flags |= oldIterator
        }
        ...
    }
    ```

- 提交扩容
  - 修改桶的数量与标记位
  - 修改新桶与旧桶的引用
  - 重置数据迁移进度与溢出桶的数量

    ```go
    func hashGrow(t *maptype, h *hmap) {
        ...
        // commit the grow (atomic wrt gc)
        h.B += bigger
        h.flags = flags
        h.oldbuckets = oldbuckets
        h.buckets = newbuckets
        h.nevacuate = 0
        h.noverflow = 0
        ...
    }
    ```

- 修改溢出桶相关数据
  - 将溢出桶保存至 `oldoverflow`
  - 修改溢出桶指针

    ```go
    func hashGrow(t *maptype, h *hmap) {
        ...
        if h.extra != nil && h.extra.overflow != nil {
            // Promote current overflow buckets to the old generation.
            if h.extra.oldoverflow != nil {
                throw("oldoverflow is not nil")
            }
            h.extra.oldoverflow = h.extra.overflow
            h.extra.overflow = nil
        }
        if nextOverflow != nil {
            if h.extra == nil {
                h.extra = new(mapextra)
            }
            h.extra.nextOverflow = nextOverflow
        }
    }
    ```

最后，需要注意的是，完整的扩容是个耗时操作，`hashGrow()` 函数仅仅完成了初始化的部分，数据的迁移操作会分散在之后的访问中通过 [growWork()](https://github.com/golang/go/blob/go1.22.0/src/runtime/map.go#L1140) 和 [evacuate()](https://github.com/golang/go/blob/go1.22.0/src/runtime/map.go#L1164) 进行处理，用以优化单次的性能。

```go
func hashGrow(t *maptype, h *hmap) {
    ...
    // the actual copying of the hash table data is done incrementally
    // by growWork() and evacuate().
}
```

### [growWork()](https://github.com/golang/go/blob/go1.22.0/src/runtime/map.go#L1140)

在写入和删除时，会通过 [growing()](https://github.com/golang/go/blob/go1.22.0/src/runtime/map.go#L1117) 函数判断当前是否处于扩容状态，进而调用 `growWork()` 方法进行处理，以 `mapassign()` 函数为例：

```go
func mapassign(t *maptype, h *hmap, key unsafe.Pointer) unsafe.Pointer {
    ...
    if h.growing() {
        growWork(t, h, bucket)
    }
    ...
}
```

是否处于扩容状态，即 `growing()` 函数的判断逻辑也很简单，只对 `oldbuckets` 进行判空处理，即数据迁移操作是否完成。

```go
// growing reports whether h is growing. The growth may be to the same size or bigger.
func (h *hmap) growing() bool {
    return h.oldbuckets != nil
}
```

对于 `growWork()` 函数，每次会触发两次数据迁移操作：

- 一次是迁移正在使用的桶，因为 B 值对于新桶、旧桶可能发生改变（区分增量扩容和等量扩容），所以需要计算下旧桶对应的桶号（即 `bucket&h.oldbucketmask()`）
- 一次是按迁移进度，迁移下一个桶（即 `h.nevacuate`）

    ```go
    func growWork(t *maptype, h *hmap, bucket uintptr) {
        // make sure we evacuate the oldbucket corresponding
        // to the bucket we're about to use
        evacuate(t, h, bucket&h.oldbucketmask())

        // evacuate one more oldbucket to make progress on growing
        if h.growing() {
            evacuate(t, h, h.nevacuate)
        }
    }
    ```

### [evacuate()](https://github.com/golang/go/blob/go1.22.0/src/runtime/map.go#L1164)

在数据迁移时，重点是区分迁移前后，哈希桶中元素的重新分配：

- 对于等量扩容来说，仅仅是将原本较为稀疏的数据，调整的更为紧凑，比如将溢出桶的数据放在普通桶中，桶号没有发生改变
- 对于增量扩容来说，因为 B 值扩大了，低位 hash 的位数也扩充了一位，根据这一位的取值是 0 或者 1，将老数据分为了两部分，高位为 0 时，桶号不变，也被称作 X 区，高位为 1 时，桶号改变，也被称作 Y 区

函数的细节介绍如下：

- 参数预处理：
  - 根据桶号 `oldbucket` 定位桶的地址
  - 计算扩容前的桶数（如果不是等量扩容，B 值要减一计算）
  - 判断当前桶是否完成数据迁移

    ```go
    func evacuate(t *maptype, h *hmap, oldbucket uintptr) {
        ...
        b := (*bmap)(add(h.oldbuckets, oldbucket*uintptr(t.BucketSize)))
        newbit := h.noldbuckets()
        if !evacuated(b) {
            ...
        }
        ...
    }
    ```

  - 对于桶的数量，如果不是等量扩容，则 B 值要减一判断

    ```go
    func (h *hmap) noldbuckets() uintptr {
        oldB := h.B
        if !h.sameSizeGrow() {
            oldB--
        }
        return bucketShift(oldB)
    }
    ```

  - 数据迁移状态，通过桶中首位元素的 tophash 进行判断

    ```go
    func evacuated(b *bmap) bool {
        h := b.tophash[0]
        return h > emptyOne && h < minTopHash
    }

    const (
        emptyOne       = 1 // this cell is empty
        evacuatedX     = 2 // key/elem is valid.  Entry has been evacuated to first half of larger table.
        evacuatedY     = 3 // same as above, but evacuated to second half of larger table.
        evacuatedEmpty = 4 // cell is empty, bucket is evacuated.
        minTopHash     = 5 // minimum tophash for a normal filled cell.
    )
    ```

- 迁移结构初始化
  - 迁移时所用数据结构为 `evacDst`，存储每个位置相关数据
  - 初始化 xy 容器，用于存储 x 区和 y 区数据

    ```go
    // evacDst is an evacuation destination.
    type evacDst struct {
        b *bmap          // current destination bucket
        i int            // key/elem index into b
        k unsafe.Pointer // pointer to current key storage
        e unsafe.Pointer // pointer to current elem storage
    }

    func evacuate(t *maptype, h *hmap, oldbucket uintptr) {
        ...
        if !evacuated(b) {
            ...
            // xy contains the x and y (low and high) evacuation destinations.
            var xy [2]evacDst
            ...
        }
        ...
    }
    ```

  - 初始化 x 区数据空间

    ```go
    func evacuate(t *maptype, h *hmap, oldbucket uintptr) {
        ...
        if !evacuated(b) {
            ...
            x := &xy[0]
            x.b = (*bmap)(add(h.buckets, oldbucket*uintptr(t.BucketSize)))
            x.k = add(unsafe.Pointer(x.b), dataOffset)
            x.e = add(x.k, abi.MapBucketCount*uintptr(t.KeySize))
            ...
        }
        ...
    }
    ```

  - 对于增量扩容，额外初始化 y 区数据空间，其中桶的地址是通过旧桶的索引(`oldbucket`)和旧桶的数量(`newbit`)计算而来

    ```go
    func evacuate(t *maptype, h *hmap, oldbucket uintptr) {
        ...
        if !evacuated(b) {
            ...
            if !h.sameSizeGrow() {
                // Only calculate y pointers if we're growing bigger.
                // Otherwise GC can see bad pointers.
                y := &xy[1]
                y.b = (*bmap)(add(h.buckets, (oldbucket+newbit)*uintptr(t.BucketSize)))
                y.k = add(unsafe.Pointer(y.b), dataOffset)
                y.e = add(y.k, abi.MapBucketCount*uintptr(t.KeySize))
            }
            ...
        }
        ...
    }
    ```

- 外层循环初始化
  - 外层循环处理哈希桶和所有溢出桶，初始化 key 与 value 指针

    ```go
    func evacuate(t *maptype, h *hmap, oldbucket uintptr) {
        ...
        if !evacuated(b) {
            ...
            for ; b != nil; b = b.overflow(t) {
                k := add(unsafe.Pointer(b), dataOffset)
                e := add(k, abi.MapBucketCount*uintptr(t.KeySize))
                ...
            }
            ...
        }
        ...
    }
    ```

- 内层循环初始化
  - 内循环处理每个桶内的数据
  - 处理空值，设置为 `evacuatedEmpty`，并继续循环
  - 重复进行数据迁移时，触发异常
  - 获取 key 的真实值（即 `k2`）

    ```go
    func evacuate(t *maptype, h *hmap, oldbucket uintptr) {
        ...
        if !evacuated(b) {
            ...
            for ; b != nil; b = b.overflow(t) {
                ...
                for i := 0; i < abi.MapBucketCount; i, k, e = i+1, add(k, uintptr(t.KeySize)), add(e, uintptr(t.ValueSize)) {
                    top := b.tophash[i]
                    if isEmpty(top) {
                        b.tophash[i] = evacuatedEmpty
                        continue
                    }
                    if top < minTopHash {
                        throw("bad map state")
                    }
                    k2 := k
                    if t.IndirectKey() {
                        k2 = *((*unsafe.Pointer)(k2))
                    }
                }
            }
            ...
        }
        ...
    }
    ```

- 对于增量扩容，计算迁移区域
  - 如果当前有迭代器正在使用，且 key 值本身和 hash 值不稳定，例如浮点数 NaN，此时因为 hash 值本身作用不大，故直接通过 tophash 值的最低位决定迁移至 x 区还是 y 区
    - 此外，对于这种场景还需要重新更新 tophash 值（与之前不同），引入额外的随机数，保障多次扩容后，这些特殊的 key 值可以均匀分布
  - 对于正常的 key 值（hash 结果固定）来说，根据 hash 值的第 `newbit` 位，决定迁移至 x 区还是 y 区
    - 增量扩容时，B 每次增加一位，容量扩充为原本的 2 倍，故 `newbit` 既可以表示原本的桶的数量，也可以表示桶号新增的那一位
    - 假定扩容前 B 是 4，共有 16 个桶，即 `0b10000` 个桶，扩容后 B 是 5，此时要获取到低位 hash 新增的那一位数据，直接与 `0b10000` 做与运算即可

    ```go
    func evacuate(t *maptype, h *hmap, oldbucket uintptr) {
        ...
        if !evacuated(b) {
            ...
            for ; b != nil; b = b.overflow(t) {
                ...
                for i := 0; i < abi.MapBucketCount; i, k, e = i+1, add(k, uintptr(t.KeySize)), add(e, uintptr(t.ValueSize)) {
                    ...
                    var useY uint8
                    if !h.sameSizeGrow() {
                        hash := t.Hasher(k2, uintptr(h.hash0))
                        if h.flags&iterator != 0 && !t.ReflexiveKey() && !t.Key.Equal(k2, k2) {
                            useY = top & 1
                            top = tophash(hash)
                        } else {
                            if hash&newbit != 0 {
                                useY = 1
                            }
                        }
                    }
                    ...
                }
            }
            ...
        }
        ...
    }
    ```

- 获取迁移的目标位置
  - 计算 `evacuatedX` 与 `evacuatedY` 的数量关系，保障 $evacuatedX + 1 = evacuatedY$
  - 更新旧桶中当前位置的 `tophash` 值，记录元素被迁移至 x 区还是 y 区
  - 通过 `useY` 获取要迁移的目标位置
  - 判断目标位置索引，如果溢出，则使用溢出桶

    ```go
    func evacuate(t *maptype, h *hmap, oldbucket uintptr) {
        ...
        if !evacuated(b) {
            ...
            for ; b != nil; b = b.overflow(t) {
                ...
                for i := 0; i < abi.MapBucketCount; i, k, e = i+1, add(k, uintptr(t.KeySize)), add(e, uintptr(t.ValueSize)) {
                    ...
                    if evacuatedX+1 != evacuatedY || evacuatedX^1 != evacuatedY {
                        throw("bad evacuatedN")
                    }

                    b.tophash[i] = evacuatedX + useY // evacuatedX + 1 == evacuatedY
                    dst := &xy[useY]                 // evacuation destination
                    
                    if dst.i == abi.MapBucketCount {
                        dst.b = h.newoverflow(t, dst.b)
                        dst.i = 0
                        dst.k = add(unsafe.Pointer(dst.b), dataOffset)
                        dst.e = add(dst.k, abi.MapBucketCount*uintptr(t.KeySize))
                    }
                    ...
                }
            }
            ...
        }
        ...
    }
    ```

- 迁移数据
  - 修改 tophash
    - `dst.i&(abi.MapBucketCount-1)` 防止越界
  - 迁移 key 和 value

    ```go
    func evacuate(t *maptype, h *hmap, oldbucket uintptr) {
        ...
        if !evacuated(b) {
            ...
            for ; b != nil; b = b.overflow(t) {
                ...
                for i := 0; i < abi.MapBucketCount; i, k, e = i+1, add(k, uintptr(t.KeySize)), add(e, uintptr(t.ValueSize)) {
                    ...
                    dst.b.tophash[dst.i&(abi.MapBucketCount-1)] = top // mask dst.i as an optimization, to avoid a bounds check
                    if t.IndirectKey() {
                        *(*unsafe.Pointer)(dst.k) = k2 // copy pointer
                    } else {
                        typedmemmove(t.Key, dst.k, k) // copy elem
                    }
                    if t.IndirectElem() {
                        *(*unsafe.Pointer)(dst.e) = *(*unsafe.Pointer)(e)
                    } else {
                        typedmemmove(t.Elem, dst.e, e)
                    }
                    ...
                }
            }
            ...
        }
        ...
    }
    ```

- 更新目标位置
  - 更新索引，指向下一个位置
  - 更新 key 和 value 指针，同样指向下一个位置
    - 更新操作有可能导致越界，会在迁移数据时，通过索引进行判断，越界时会更新为溢出桶的地址

    ```go
    func evacuate(t *maptype, h *hmap, oldbucket uintptr) {
        ...
        if !evacuated(b) {
            ...
            for ; b != nil; b = b.overflow(t) {
                ...
                for i := 0; i < abi.MapBucketCount; i, k, e = i+1, add(k, uintptr(t.KeySize)), add(e, uintptr(t.ValueSize)) {
                    ...
                    dst.i++
                    // These updates might push these pointers past the end of the
                    // key or elem arrays.  That's ok, as we have the overflow pointer
                    // at the end of the bucket to protect against pointing past the
                    // end of the bucket.
                    dst.k = add(dst.k, uintptr(t.KeySize))
                    dst.e = add(dst.e, uintptr(t.ValueSize))
                }
            }
            ...
        }
        ...
    }
    ```

- 迁移完成，最终处理
  - 如果旧桶没有被迭代器使用，且桶中包含指针，则清理 key 和 value 的内存空间
    - 注意不能清理 tophash 部分，标记位还有作用（`dataOffset` 表示 tophash 部分的偏移量）
  - 如果当前迁移为顺序迁移，则更新迁移进度

```go
func evacuate(t *maptype, h *hmap, oldbucket uintptr) {
    ...
    if !evacuated(b) {
        ...
        // Unlink the overflow buckets & clear key/elem to help GC.
        if h.flags&oldIterator == 0 && t.Bucket.Pointers() {
            b := add(h.oldbuckets, oldbucket*uintptr(t.BucketSize))
            // Preserve b.tophash because the evacuation
            // state is maintained there.
            ptr := add(b, dataOffset)
            n := uintptr(t.BucketSize) - dataOffset
            memclrHasPointers(ptr, n)
        }
    }

    if oldbucket == h.nevacuate {
        advanceEvacuationMark(h, t, newbit)
    }
}
```

### [advanceEvacuationMark()](https://github.com/golang/go/blob/go1.22.0/src/runtime/map.go#L1278)

hashmap 在扩容时，新的内存空间以及相关标识位都会完成初始化操作，但是数据的迁移是渐进式的操作，用于优化单次操作的性能，在每一次数据按顺序迁移完，会通过 `advanceEvacuationMark()` 函数来处理迁移进度相关的逻辑。

函数的逻辑如下所示：

- 迁移进度自增
- 循环判断后续位置是否完成迁移
  - 设置循环结束标记位
    - 桶数量较少时，以桶数量为准
    - 桶数量较多时，最多循环 1024 次
  - 循环判断当前桶是否完成数据迁移
    - 数据迁移每次会触发两次，一次是在使用的桶，一次是顺序迁移，所以有可能当前位置之后的桶已经完成了迁移
- 如果迁移完成，则移除相关数据
  - 清理旧桶
  - 清理旧溢出桶的相关数据
  - 移除扩容标记位

```go
func advanceEvacuationMark(h *hmap, t *maptype, newbit uintptr) {
    h.nevacuate++
    // Experiments suggest that 1024 is overkill by at least an order of magnitude.
    // Put it in there as a safeguard anyway, to ensure O(1) behavior.
    stop := h.nevacuate + 1024
    if stop > newbit {
        stop = newbit
    }
    for h.nevacuate != stop && bucketEvacuated(t, h, h.nevacuate) {
        h.nevacuate++
    }
    if h.nevacuate == newbit { // newbit == # of oldbuckets
        // Growing is all done. Free old main bucket array.
        h.oldbuckets = nil
        // Can discard old overflow buckets as well.
        // If they are still referenced by an iterator,
        // then the iterator holds a pointers to the slice.
        if h.extra != nil {
            h.extra.oldoverflow = nil
        }
        h.flags &^= sameSizeGrow
    }
}
```

## Ref

- <https://draveness.me/golang/docs/part2-foundation/ch03-datastructure/golang-hashmap/>
- <https://blog.csdn.net/Jeff_fei/article/details/134052696>
- <https://zhuanlan.zhihu.com/p/666635281>
