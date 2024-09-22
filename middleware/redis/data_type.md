# Data Type

在 Redis 的使用中，有五种常见的类型，即 `String`、`Hash`、`List`、`Set` 和 `Zset`，后续还额外支持了四种特殊场景会使用的类型 `BitMap`、`HyperLogLog`、`GEO` 和 `Stream`。通过丰富的数据类型，支持了不同的业务特征。

在底层的实现上，则分别对应了 SDS(Simple Dynamic String)、双向链表(linked list)、压缩列表(ziplist)、哈希表(hash table)、跳表(skiplist)、整数集合(intset)、快表(quicklist)、紧凑列表(listpack) 这几种数据结构。通过不同数据结构的选择，为上层的数据类型的提供了较好的读写性能。

对于具体的类型，比如 `Set`，在数据量较少时，会优先使用整数集合作为底层数据结构，数据量较大时，会使用哈希表作为底层数据结构

除此以外，Redis 还封装了对象类型，每一个键值对都分别对应了一个键对象和一个值对象。上述所提到的每一个类型，其实质都是对象类型，即 `String` 类型，其实是 `String` 对象。

可以通过对象中的类型字段，区分对象底层的真实类型，还可以通过对象中的编码字段，区分底层真实的数据结构。

## Object

`Object` 对于使用者来说是无感的，但却是 Redis 中最为核心的数据结构，每次在 Redis 中新创建一个键值对时，其实是创建了两个对象，一个用作 key，一个用作 value。

Redis 会根据用户使用的命令不同，以及真正输入的数据类型不同，最终决定对象的真实类型。

### 数据结构

每个对象都由一个 [redisObject(robj)](https://github.com/redis/redis/blob/7.0.0/src/server.h#L845) 结构体进行表示。字段含义如下：

- `type`：对象的类型，如 `String`、`Hash` 等
- `encoding`：对象的编码方式，即底层数据结构，如 `SDS`、`hash table` 等
- `lru`：最近访问记录，[LRU_BITS](https://github.com/redis/redis/blob/7.0.0/src/server.h#L838) 值为 24
  - 对于 LRU（最近最少使用） 淘汰算法来说，记录 key 最后一次的访问时间戳
  - 对于 LFU（最近最不常用） 淘汰算法来说，高 16 位记录最后访问时间，低 8 位记录访问次数
- `refcount`：引用计数，用于 GC
- `ptr`：指向底层数据结构的指针

```C
typedef struct redisObject {
    unsigned type:4;
    unsigned encoding:4;
    unsigned lru:LRU_BITS; /* LRU time (relative to global lru_clock) or
                            * LFU data (least significant 8 bits frequency
                            * and most significant 16 bits access time). */
    int refcount;
    void *ptr;
} robj;
```

<br>

对象类型与编码类型的对应关系，如下所示，在目前的版本中(7.0)，压缩列表(ziplist)被紧凑列表(listpack)取代了，列表对象底层也改为使用快表(quicklist)来实现。

<table>
    <tr>
        <th>对象类型</th>
        <th>编码类型</th>
        <th>数据结构</th>
    </tr>
    <tr>
        <td rowspan="3"><a href="https://github.com/redis/redis/blob/7.0.0/src/server.h#L638">OBJ_STRING</a></td>
        <td><a href="https://github.com/redis/redis/blob/7.0.0/src/server.h#L826">OBJ_ENCODING_INT</a></td>
        <td>整数</td>
    </tr>
    <tr>
        <td><a href="https://github.com/redis/redis/blob/7.0.0/src/server.h#L833">OBJ_ENCODING_EMBSTR</a></td>
        <td>Embedded SDS</td>
    </tr>
    <tr>
        <td><a href="https://github.com/redis/redis/blob/7.0.0/src/server.h#L825">OBJ_ENCODING_RAW</a></td>
        <td>SDS</td>
    </tr>
    <tr>
        <td rowspan="3"><a href="https://github.com/redis/redis/blob/7.0.0/src/server.h#L639">OBJ_LIST</a></td>
        <td><a href="https://github.com/redis/redis/blob/7.0.0/src/server.h#L830">OBJ_ENCODING_ZIPLIST</a></td>
        <td>压缩列表（不再使用）</td>
    </tr>
    <tr>
        <td><a href="https://github.com/redis/redis/blob/7.0.0/src/server.h#L829">OBJ_ENCODING_LINKEDLIST</a></td>
        <td>双向链表（不再使用）</td>
    </tr>
    <tr>
        <td><a href="https://github.com/redis/redis/blob/7.0.0/src/server.h#L834">OBJ_ENCODING_QUICKLIST</a></td>
        <td>快表</td>
    </tr>
    <tr>
        <td rowspan="2"><a href="https://github.com/redis/redis/blob/7.0.0/src/server.h#L640">OBJ_SET</a></td>
        <td><a href="https://github.com/redis/redis/blob/7.0.0/src/server.h#L831">OBJ_ENCODING_INTSET</a></td>
        <td>整数集合</td>
    </tr>
    <tr>
        <td><a href="https://github.com/redis/redis/blob/7.0.0/src/server.h#L827">OBJ_ENCODING_HT</a></td>
        <td>哈希表</td>
    </tr>
    <tr>
        <td rowspan="3"><a href="https://github.com/redis/redis/blob/7.0.0/src/server.h#L641">OBJ_ZSET</a></td>
        <td><a href="https://github.com/redis/redis/blob/7.0.0/src/server.h#L830">OBJ_ENCODING_ZIPLIST</a></td>
        <td>压缩列表（不再使用）</td>
    </tr>
    <tr>
        <td><a href="https://github.com/redis/redis/blob/7.0.0/src/server.h#L836">OBJ_ENCODING_LISTPACK</a></td>
        <td>紧凑列表</td>
    </tr>
    <tr>
        <td><a href="https://github.com/redis/redis/blob/7.0.0/src/server.h#L832">OBJ_ENCODING_SKIPLIST</a></td>
        <td>跳表</td>
    </tr>
    <tr>
        <td rowspan="3"><a href="https://github.com/redis/redis/blob/7.0.0/src/server.h#L642">OBJ_HASH</a></td>
        <td><a href="https://github.com/redis/redis/blob/7.0.0/src/server.h#L830">OBJ_ENCODING_ZIPLIST</a></td>
        <td>压缩列表（不再使用）</td>
    </tr>
    <tr>
        <td><a href="https://github.com/redis/redis/blob/7.0.0/src/server.h#L836">OBJ_ENCODING_LISTPACK</a></td>
        <td>紧凑列表</td>
    </tr>
    <tr>
        <td><a href="https://github.com/redis/redis/blob/7.0.0/src/server.h#L827">OBJ_ENCODING_HT</a></td>
        <td>哈希表</td>
    </tr>
    <tr>
        <td><a href="https://github.com/redis/redis/blob/7.0.0/src/server.h#L656">OBJ_STREAM</a></td>
        <td><a href="https://github.com/redis/redis/blob/7.0.0/src/server.h#L835">OBJ_ENCODING_STREAM</a></td>
        <td>基数树</td>
    </tr>
</table>

### 常用命令

```shell
> rpush list_key 1 2 3 4 5 # 创建列表对象
(integer) 5

> type list_key # 获取对象类型
"list"

> object encoding list_key # 获取对象编码类型
"listpack"
```

## String

`String` 类型是 Redis 中最为基础的一个基本类型，业务层的所有非容器类型的数据，例如整数、浮点数、字符串、二进制数据等，在 Redis 中均是以 `String` 类型进行存储。

其编码方式可以是 `int`、`embstr` 或是 `raw`，其中 `embstr` 和 `rar`，底层的数据结构为 SDS(Simple Dynamic String)。

### 数据结构

SDS 底层的数据结构会根据实际所需大小，动态决定，以 [sdshdr8](https://github.com/redis/redis/blob/7.0.0/src/sds.h#L51) 为例：

- `len`：字符串当前的长度
- `alloc`：分配的字符串的总长度，即不包括结构体头部以及尾部的空终止符
- `flags`：标志位
- `buf`：存储字符串内容的字符数据

```c
struct __attribute__ ((__packed__)) sdshdr8 {
    uint8_t len; /* used */
    uint8_t alloc; /* excluding the header and null terminator */
    unsigned char flags; /* 3 lsb of type, 5 unused bits */
    char buf[];
};
```

<br>

出于节省的目的考虑，在 [sdshdr8](https://github.com/redis/redis/blob/7.0.0/src/sds.h#L51) 中，`len` 与 `alloc` 字段的类型均为 `uint8_t`，相对应的，[sdshdr16](https://github.com/redis/redis/blob/7.0.0/src/sds.h#L57)、[sdshdr32](https://github.com/redis/redis/blob/7.0.0/src/sds.h#L63) 和 [sdshdr64](https://github.com/redis/redis/blob/7.0.0/src/sds.h#L69) 中，`len` 与 `alloc` 字段的类型分别为 `uint16_t`、`uint32_t` 和 `uint64_t`。

不过在实际使用中，`String` 类型允许的最大长度，还远远不到 `uint64_t` 的范围，在 [checkStringLength()](https://github.com/redis/redis/blob/7.0.0/src/t_string.c#L40) 函数中，限制了最大的大小为 [proto_max_bulk_len](https://github.com/redis/redis/blob/7.0.0/src/config.c#L3046) 的大小，即 512MB

```c
standardConfig static_configs[] = {
    ...
    createLongLongConfig(
        "proto-max-bulk-len", NULL, 
        DEBUG_CONFIG | MODIFIABLE_CONFIG, 1024*1024, 
        LONG_MAX, server.proto_max_bulk_len, 
        512ll*1024*1024, MEMORY_CONFIG, NULL, NULL
    ), 
    ...
}

static int checkStringLength(client *c, long long size) {
    if (!mustObeyClient(c) && size > server.proto_max_bulk_len) {
        addReplyError(c,"string exceeds maximum allowed size (proto-max-bulk-len)");
        return C_ERR;
    }
    return C_OK;
}
```

<br>

相较于 C 语言原生字符串，SDS 主要由如下几个优势：

- 读取长度时，直接读取结构体的属性 `len`，时间复杂度为 O(1)
- 读取数据时，根据 `len` 来判断当前所要读取的长度，而非空白符，所以可以存储包含空白符的数据，即可以存储所有二进制数据
- 写入数据时，可以根据 `len` 和 `alloc` 两个关键字判断空间是否足够，不够的话还可以修改底层缓冲区数据，动态扩容

### 编码方式

- `int`：当存储的 value 是整数，且可以用 `long` 类型进行表示时，此时会直接将 [redisObject](https://github.com/redis/redis/blob/7.0.0/src/server.h#L845) 中的 `ptr`，从 `void *` 类型转为 `long` 类型，用于存储 value。

  - `void *` 与 `long` 占用的字节数始终相同，在 32 位机器为 $4$ 字节，在 64 位机器为 $8$ 字节

  - 此时结构体如下所示：

    <table style="width:100%; text-align:center;">
        <tr>
            <th colspan="3">redisObject</th>
        </tr>
        <tr>
            <td>type</td>
            <td>encoding</td>
            <td>ptr</td>
        </tr>
        <tr>
            <td>string</td>
            <td>int</td>
            <th>value</th>
        </tr>
    </table>

<br>

- `embstr`：当存储的 value 是字符串，且字符长度小于 [OBJ_ENCODING_EMBSTR_SIZE_LIMIT](https://github.com/redis/redis/blob/7.0.0/src/object.c#L119) 时，会采用 `embstr` 编码，针对 [redisObject](https://github.com/redis/redis/blob/7.0.0/src/server.h#L845) 与 [sdshdr8](https://github.com/redis/redis/blob/7.0.0/src/sds.h#L51) 仅进行一次内存分配，提高性能。

  - 在实际使用中，value 为大整数、浮点数、二进制数据等情况，均为被统一转为字符串来存储

  - 在 7.0 版本中，[OBJ_ENCODING_EMBSTR_SIZE_LIMIT](https://github.com/redis/redis/blob/7.0.0/src/object.c#L119) 的值为 44
  - 在 [createStringObject()](https://github.com/redis/redis/blob/7.0.0/src/object.c#L120) 函数中，会实现上述决策逻辑

    ```c
    #define OBJ_ENCODING_EMBSTR_SIZE_LIMIT 44
    robj *createStringObject(const char *ptr, size_t len) {
        if (len <= OBJ_ENCODING_EMBSTR_SIZE_LIMIT)
            return createEmbeddedStringObject(ptr,len);
        else
            return createRawStringObject(ptr,len);
    }
    ```

<br>

- `raw`：在其他情况下，均会使用 `raw` 编码类型，先分配 [redisObject](https://github.com/redis/redis/blob/7.0.0/src/server.h#L845) 的内存，再分配 [sdshdr8](https://github.com/redis/redis/blob/7.0.0/src/sds.h#L51) 的内存
  - 对于 `embstr` 来说，创建与释放均仅需要调用一次函数，且内存连续可以更好的利用 CPU 缓存，
  - 字符串长度在增长时，有可能会触发扩容逻辑，重新进行内存分配，有可能会不满足 `int` 和 `embstr` 的条件，出于简单考虑，规定 `int` 和 `embstr` 编码方式仅支持读，触发写逻辑时会将其转为 `raw` 编码类型再进行修改
<br>

- `size_limit`

  - 对于 [redisObject](https://github.com/redis/redis/blob/7.0.0/src/server.h#L845) 来说，在 64 位系统下，共占用 $16$ 字节

    <table style="width:100%; text-align:center;">
        <tr>
            <th></th>
            <th colspan="5">redisObject</th>
        </tr>
        <tr>
            <th>field</th>
            <td>type</td>
            <td>encoding</td>
            <td>lru</td>
            <td>refcount</td>
            <td>ptr</td>
        </tr>
        <tr>
            <th>type</th>
            <td>4 bit</td>
            <td>4 bit</td>
            <td>24 bit</td>
            <td>int</td>
            <td>void *</td>
        </tr>
        <tr>
            <th>byte</th>
            <td>0.5</td>
            <td>0.5</td>
            <td>3</td>
            <td>4</td>
            <td>8</td>
        </tr>
    </table>

  - 而对于 [sdshdr8](https://github.com/redis/redis/blob/7.0.0/src/sds.h#L51) 来说，在不考虑 `buf` 字段的前提下，共占用 $3$ 字节，此时假定 value 的长度为 $n$，则 buf 的长度为 value 加上空白符的长度即 $n + 1$ 字节

    <table style="width:100%; text-align:center;">
        <tr>
            <th></th>
            <th colspan="4">sdshdr8</th>
        </tr>
        <tr>
            <th>field</th>
            <td>len</td>
            <td>alloc</td>
            <td>flag</td>
            <td>buf</td>
        </tr>
        <tr>
            <th>type</th>
            <td>uint8_t</td>
            <td>uint8_t</td>
            <td>unsigned char</td>
            <td>char [ ]</td>
        </tr>
        <tr>
            <th>byte</th>
            <td>1</td>
            <td>1</td>
            <td>1</td>
            <td>n+1</td>
        </tr>
    </table>

  - 从内存分配和内存对齐的角度来说，64 位系统下，一次分配的内存为 $16$ 字节的倍数，且是 $2$ 的整数次幂，即 $16$ 字节、$32$ 字节、$64$ 字节，等等
  - 上述两个结构体占用的字节数为 $20 + n$，故较为合适的分配方式，是直接分配 $64$ 字节（$32$ 字节太小），此时 $n$ 的最大值即为 $64-20=44$

### 常用命令

> 官方文档：<https://redis.io/docs/latest/commands/?group=string>

- 基础操作：

    ```shell
    > set int_key 100 # 设置整数，key 值重复时会直接覆盖
    "OK"

    > get int_key # 获取 value，对于整数来说，最终返回的也是字符串类型
    "100"

    > append int_key _append # 附加新字符串，对于整数，会将其转为 raw 类型再修改
    (integer) 10             # 返回修改后的字符串长度

    > get int_key
    "100_append"

    > exists int_key # 判断 key 是否存在
    (integer) 1

    > strlen int_key # 获取字符串长度
    (integer) 10

    > DEL int_key # 删除
    (integer) 1
    ```

<br>

- range 操作

    ```shell
    > set string_key string_value # 设置字符串
    "OK"

    > setrange string_key 12 _edit # 从指定位置开始修改字符串
    (integer) 17

    > get string_key
    "string_value_edit"

    > setrange string_key 20 _out_of_range # 越界时，会自动扩容，并填充空白符 \x00
    (integer) 33

    > get string_key
    "string_value_edit\x00\x00\x00_out_of_range"

    > getrange string_key 0 11 # 获取指定索引内的数据
    "string_value"
    ```

<br>

- 批量操作

    ```shell
    > mset key1 value1 key2 value2 # 批量设置
    "OK"

    > mget key1 key2 # 批量读取
    1) "value1" 2) "value2"
    ```

<br>

- 计数器

    ```shell
    > set int_key 100
    "OK"

    > incr int_key # 将整数类型的值加一，并返回整数结果
    (integer) 101

    > incrby int_key 20 # 将整数类型的值加任意值，并返回整数结果
    (integer) 121

    > decr int_key # 将整数类型的值减一，并返回整数结果
    (integer) 120

    > decrby int_key 20 # 将整数类型的值减任意值，并返回整数结果
    (integer) 100
    ```

<br>

- 过期时间

  - `SETEX` 命令在 2.6.12 版本移除，被 `SET` 命令中的 `EX` 参数替代

    ```shell
    > set expire_key value ex 3600 # 设置键值对时，设置过期时间
    "OK"

    > setex expire_key2 3600 value # 设置键值对时，设置过期时间
    "OK"

    > expire expire_key 600 # 更新过期时间（要求 key 已存在）
    (integer) 1

    > ttl expire_key # 查看过期时间
    (integer) 593

    > ttl expire_key2 # 查看过期时间
    (integer) 3571
    ```

<br>

- 不存在时插入

  - `SETNX` 命令在 2.6.12 版本移除，被 `SET` 命令中的 `NX` 参数替代

    ```shell
    > setnx nx_key value # 不存在时插入，结果为 1 代表插入成功
    (integer) 1

    > setnx nx_key value # 不存在时插入，结果为 0 代表已经存在
    (integer) 0

    > set nx_key2 value nx # 不存在时插入，"OK" 代表插入成功，可结合其他关键字使用，比如过期时间
    "OK"

    > set nx_key2 value nx # 不存在时插入，nil 代表已经存在
    (nil)   
    ```

### 应用场景

- 分布式存储：在分布式架构下，某些临时数据需要维护在服务器内存，而不需要持久化存储时，可以通过 Redis 实现数据共享
<br>

- 缓存对象：常使用对象标识加主键一起作为 key 值

  - 缓存 json 字符串，由业务层处理序列化/反序列化逻辑

    ```shell
    > set user:1 '{"name":"silk", "age":18}'
    "OK"
    ```

  - 分属性进行缓存，避免了序列化开销，但是需要进行数据组装

    ```shell
    > mset user:1:name silk user:1:age 18
    "OK"
    ```

<br>

- 计数器：Redis 本身在处理命令时是单线程操作，不存在并发问题，所以适合于计数场景，比如页面 PV、点赞次数等
<br>

- 分布式锁：
  - 通过 `nx` 命令，可以实现加锁操作，插入成功代表加锁成功

    ```shell
    setnx lock_key unique_value
    ```

  - 通过设置过期时间，可以实现超时控制

    ```shell
    set lock_key unique_value nx ex 10
    ```

  - 通过删除 key 值，可以实现解锁操作，但是必须通过 lua 脚本，判断加锁、解锁来自同一个实例，然后再删除 key 值，确保操作的原子性

    ```lua
    if redis.call("get",KEYS[1]) == ARGV[1] then
        return redis.call("del",KEYS[1])
    else
        return 0
    end
    ```

## List

`List` 是最常见的容器类型之一，内部元素按照特定顺序进行排列，在使用时，可以从头部或尾部插入元素。`List` 内部的元素类型，均为 `String` 类型。

在老版本中，如果 `List` 中元素个数较少，且每个元素都小于特定值，会使用压缩列表(ziplist)作为底层数据结构，其他情况会使用双向链表(linked list)作为底层数据结构。

在 3.2 版本以后，`List` 仅会使用快表(quicklist)这一种数据结构。

### 常用命令

> 官方文档：<https://redis.io/docs/latest/commands/?group=list>

```shell
> rpush list_key r1 # 向队尾(right)插入一个元素，首次插入会新建列表元素
(integer) 1

> lpush list_key l2 l3 # 向队首(left)按序插入多个元素，即后插入的元素在左侧
(integer) 3

> rpush list_key r4 r5 # 向队尾(right)按序插入多个元素，即后插入的元素在右侧
(integer) 5

> lrange list_key 0 -1 # 打印列表内所有元素
1) "l3" 2) "l2" 3) "r1" 4) "r4" 5) "r5"

> lrange list_key 3 99 # 打印区间内的所有元素，无越界问题
1) "r4" 2) "r5"

> lrange list_key 98 99 # 打印区间内的所有元素，无越界问题
(empty list or set)

> lpop list_key # 从队首(left)弹出一个元素
"l3"

> rpop list_key # 从队尾(right)弹出一个元素
"r5"

> blpop list_key 10 # 从队首(left)弹出一个元素，最多阻塞等待 timeout 秒
1) "list_key" 2) "l2"

> brpop list_key 10 # 从队尾(right)弹出一个元素，最多阻塞等待 timeout 秒
1) "list_key" 2) "r4"

> rpush list_key v5 v6 v7
(integer) 4

> rpoplpush list_key list_key2 # 从列表一的队尾弹出一个元素，返回并同时添加至列表二的队首
"v7"

> brpoplpush list_key list_key2 10 # 最多阻塞等待 timeout 秒
"v6"
```

### 应用场景

- 消息队列

  - 消息队列是一种异步通信方式，在实现时，需要保障消息有序、不重复、不丢失。

    <br>

  - 有序：`List` 支持从队首或队尾进行读写，在使用时，控制生产者、消费者固定从不同的方向操作即可
    - 在消费者读取消息时，可以通过阻塞式命令来读取，例如 `brpop`，避免轮询操作

    <br>

  - 不重复：`List` 本身没有提供相关能力保障，需要业务层自行为每条消息生成一个全局 ID，自行进行保障
    - 比如使用 `setnx` 命令维护消费记录

    <br>

  - 不丢失：`List` 本身仅提供了消息的入队和出队能力，对于消息出队之后，但是因为网络或业务层问题导致消费行为中断的场景，没有办法进行保障
    - 从数据交互层面进行分析，其根本原因是缺少了 ack 机制
    - 所以可以将出队的数据，存储在另外一个队列中，当消费行为完成后，再将数据最终出队，完成删除操作
    - 通过 `rpoplpush` 和 `brpoplpush` 命令，可以完成上述操作，且过程是原子的

    <br>

  - 消息模型：消息队列有两种常见模型，点对点模型(P2P)与发布订阅模型(Pub/Sub)，但是 `List` 仅具备单播能力，不具备广播能力，即无法实现发布订阅模型

## Hash

`Hash` 类型是典型的 kv 存储容器，可以通过 hash 算法，对 key 实现分组操作，进而快速找到指定 key 值所对应的 value 元素，Redis 本身键值对的存储结构，也是依赖于哈希表。

在老版本中，如果 `Hash` 中元素个数小于 [hash-max-ziplist-entries](https://github.com/redis/redis/blob/7.0.0/src/config.c#L3055)，且每个元素大小都小于 [hash-max-ziplist-value](https://github.com/redis/redis/blob/7.0.0/src/config.c#L3059)，会使用压缩列表(ziplist)作为底层数据结构，其他情况会使用哈希表(hash table)作为底层数据结构。

在 7.0 版本中，压缩列表(ziplist)被彻底废弃，改由紧凑列表(listpack)来实现，相对应的，配置字段分别为 [hash-max-listpack-entries](https://github.com/redis/redis/blob/7.0.0/src/config.c#L3055) 和 [hash-max-listpack-value](https://github.com/redis/redis/blob/7.0.0/src/config.c#L3059)。

- 其中元素个数限制的默认值为 512 个，元素大小限制默认为 64 字节

```c
createSizeTConfig(
    "hash-max-listpack-entries", "hash-max-ziplist-entries", 
    MODIFIABLE_CONFIG, 0, 
    LONG_MAX, server.hash_max_listpack_entries, 
    512, INTEGER_CONFIG, NULL, NULL
)

createSizeTConfig(
    "hash-max-listpack-value", "hash-max-ziplist-value", 
    MODIFIABLE_CONFIG, 0, 
    LONG_MAX, server.hash_max_listpack_value, 
    64, MEMORY_CONFIG, NULL, NULL
)
```

### 常用命令

> 官方文档：<https://redis.io/docs/latest/commands/?group=hash>

```shell
> hset hash_key field1 10  # 向指定哈希表中设置新元素，哈希表不存在则自动创建
(integer) 1

> hset hash_key field1 100 # 向指定哈希表中设置新元素，filed 重复则更新
(integer) 0

> hget hash_key field1 # 获取哈希表中指定元素
"100"

> hmset hash_key field2 20 field3 30 # 向指定哈希表中批量设置新元素
"OK"

> hmget hash_key field2 field3 # 批量获取哈希表中指定元素
1) "20" 2) "30"

> hdel hash_key field2 # 删除哈希表中指定元素
(integer) 1

> hlen hash_key # 获取指定哈希表的长度
(integer) 2

> hgetall hash_key # 获取指定哈希表中所有数据
1) "field1" 2) "100" 3) "field3" 4) "30"

> hincrby hash_key field1 10 # 将哈希表中指定整数元素添加指定增量
(integer) 110
```

### 应用场景

- 缓存对象：常使用对象标识加主键一起作为 key 值

  - 直接将缓存对象当作 `Hash` 类型来存储，对象的属性名对应于 `Hash` 对象的 field 部分

    ```shell
    > hmset user:1 name silk age 18
    "OK"

    > hgetall user:1
    1) "name" 2) "silk" 3) "age" 4) "18"
    ```

  - 与 `String` 类型对比
    - `String`

        <table style="width:100%; text-align:center;">
            <tr>
                <th>Type</th>
                <th>Key</th>
                <th>value</th>
            </tr>
            <tr>
                <th rowspan="2">String</th>
                <td>user:1:name</td>
                <td>silk</td>
            </tr>
            <tr>
                <td>user:1:age</td>
                <td>18</td>
            </tr>
        </table>

    - `Hash`

        <table style="width:100%; text-align:center;">
            <tr>
                <th>Type</th>
                <th>Key</th>
                <th>field</th>
                <th>value</th>
            </tr>
            <tr>
                <th rowspan="3">Hash</th>
                <td rowspan="3">user:1</td>
            </tr>
            <tr>
                <td>name</td>
                <td>silk</td>
            </tr>
            <tr>
                <td>age</td>
                <td>18</td>
            </tr>
        </table>

<br>

- 购物车

  - 以用户 id 作为 key，以商品 id 作为 field，以商品数量作为 value

  - 用户添加商品：`hset cart:user:1 product:1 1`
  - 用户修改商品数量：`hincrby cart:user:1 product:1 1`
  - 购物车中商品总数：`hlen cart:user:1`
  - 删除商品：`hdel cart:user:1 product:1`
  - 获取购物车所有商品：`hgetall cart:user:1`

## Set

`Set` 中的数据是无序的，且不存在相同元素。其概念与数学中的集合较为类似，且支持交集、并集、差集等集合间操作。

如果 `Set` 中的元素都是整数，且元素数量小于 [set-max-intset-entries](https://github.com/redis/redis/blob/7.0.0/src/config.c#L3056) 会使用整数集合(intset)的编码方式，其他情况下，会使用哈希表(hash table)作为编码方式。

- 元素个数限制的默认值为 512 个

```c
createSizeTConfig(
    "set-max-intset-entries", NULL, MODIFIABLE_CONFIG, 0, 
    LONG_MAX, server.set_max_intset_entries, 
    512, INTEGER_CONFIG, NULL, NULL
)
```

### 常用命令

> 官方文档：<https://redis.io/docs/latest/commands/?group=set>

- 基础操作

  ```shell
  > sadd set_key 1 2 3 4 5 # 向集合中添加多个元素，集合不存在会自动创建
  (integer) 5              # 返回添加成功的个数

  > srem set_key 2 3 6 # 删除集合中的多个元素
  (integer) 2          # 返回删除成功的个数

  > scard set_key # 获取集合元素数量
  (integer) 3

  > smembers set_key # 获取集合所有元素
  1) "1" 2) "4" 3) "5"

  > sadd set_key 1 2 3 4 5 6 7 # 向集合中添加多个元素，忽略重复元素
  (integer) 4                  # 返回添加成功的个数

  > sismember set_key 3 # 判断某个元素是否在集合中
  (integer) 1

  > srandmember set_key 2 # 随机获取多个元素
  1) "2" 2) "6"

  > spop set_key 2 # 随机获取多个元素，并移除集合
  1) "4" 2) "5"

  > smembers set_key
  1) "1" 2) "2" 3) "3" 4) "6" 5) "7"
  ```

<br>

- 集合操作

  ```shell
  > sadd set_key1 1 2 3 4 5
  (integer) 5

  > sadd set_key2 1 3 5 7 9
  (integer) 5

  > sinter set_key1 set_key2 # 获取多个集合的交集
  1) "1" 2) "3" 3) "5"

  > sinterstore inter_res set_key1 set_key2 # 获取交集并存储
  (integer) 3                               # 新集合的元素数量

  > sunion set_key1 set_key2 # 获取多个集合的并集
  1) "1" 2) "2" 3) "3" 4) "4" 5) "5" 6) "7" 7) "9"

  > sunionstore union_res set_key1 set_key2 # 获取并集并存储
  (integer) 7                               # 新集合的元素数量

  > sdiff set_key1 set_key2 # 获取差集
  1) "2" 2) "4"

  > sdiff set_key2 set_key1 # 获取差集，集合的顺序会影响结果
  1) "7" 2) "9"

  > sdiffstore diff_res set_key1 set_key2 # 获取差集并存储
  (integer) 2                             # 新集合的元素数量
  ```

### 应用场景

适合需要对元素去重或者是做集合运算的场景。

- 点赞

  - 利用不可重复的特性，以文章 id 作为 key，以用户 id 作为 value

    <br>

  - 点赞：`sadd like:article:1 user:1`
  - 取消点赞：`srem like:article:1 user:1`
  - 获取点赞用户量：`scard like:article:1`
  - 获取所有点赞用户：`smembers like:article:1`
  - 判断用户是否点赞：`sismember like:article:1 user:1`
<br>

- 共同关注

  - 利用交集、差集的运算特性，以用户 id 作为 key，以关注的作者作为 value
    - 需要注意交集、差集的性能问题，避免阻塞主库

    <br>

  - 用户 1 关注：`sadd follow:user:1 author:1 author:2`
  - 用户 2 关注：`sadd follow:user:2 author:2 author:3`
  - 共同关注：`sinter follow:user:1 follow:user:2`
  - 向用户 1 推荐用户 2 的关注内容：`sdiff follow:user:2 follow:user:1`
<br>

- 抽奖

  - 利用不可重复的特性，以抽奖活动作为 key，以参与抽奖的用户 id 作为 value

    <br>

  - 更新参与用户：`sadd lucky:1 user:1 user:2 user:3`
  - 允许重复中奖
    - 抽取一个奖项：`srandmember lucky:1 1`
    - 抽取两个奖项：`srandmember lucky:1 2`
  - 不允许重复中奖
    - 抽取一个奖项：`spop lucky:1 1`
    - 抽取两个奖项：`spop lucky:1 2`

## Zset

与 `Set` 相比，`Zset` 中的元素在存储时，会额外附带一个用于排序的分值，在具备 `Set` 相关的所有特性的同时，还具备了有序性。

在老版本中，如果 `Zset` 中元素个数小于 [zset-max-ziplist-entries](https://github.com/redis/redis/blob/7.0.0/src/config.c#L3057)，且每个元素大小都小于 [zset-max-ziplist-value](https://github.com/redis/redis/blob/7.0.0/src/config.c#L3061)，会使用压缩列表(ziplist)作为底层数据结构，其他情况会使用跳表(sikplist)作为底层数据结构。

在 7.0 版本中，压缩列表(ziplist)被彻底废弃，改由紧凑列表(listpack)来实现，相对应的，配置字段分别为 [zset-max-listpack-entries](https://github.com/redis/redis/blob/7.0.0/src/config.c#L3057) 和 [zset-max-listpack-value](https://github.com/redis/redis/blob/7.0.0/src/config.c#L3061)。

- 其中元素个数限制的默认值为 128 个，元素大小限制默认为 64 字节

```c
createSizeTConfig(
    "zset-max-listpack-entries", "zset-max-ziplist-entries", 
    MODIFIABLE_CONFIG, 0, 
    LONG_MAX, server.zset_max_listpack_entries, 
    128, INTEGER_CONFIG, NULL, NULL
)

createSizeTConfig(
    "zset-max-listpack-value", "zset-max-ziplist-value", 
    MODIFIABLE_CONFIG, 0, 
    LONG_MAX, server.zset_max_listpack_value, 
    64, MEMORY_CONFIG, NULL, NULL
)
```

### 常用命令

> 官方文档：<https://redis.io/docs/latest/commands/?group=sorted-set>

- 基础操作

    ```shell
    > zadd zset_key 60 v1 80 v2 40 v3 50 v4 # 批量添加元素和对应分值
    (integer) 4

    > zrem zset_key v4 # 删除元素
    (integer) 1

    > zscore zset_key v2 # 获取元素分值
    "80"

    > zcard zset_key # 获取集合内元素数量
    (integer) 3

    > zincrby zset_key 30 v2 # 增加某一元素分值
    "110"
    ```

<br>

- 分数排序

  - `ZREVRANGE` 命令在 6.2.0 版本移除，被 `ZRANGE` 命令中的 `REV` 参数替代
  - `ZRANGEBYSCORE` 命令在 6.2.0 版本移除，被 `ZRANGE` 命令中的 `BYSCORE` 参数替代
  - `ZREVRANGEBYSCORE` 命令在 6.2.0 版本移除，被 `ZRANGE` 命令中的 `REV` 参数和 `BYSCORE` 参数替代

    ```shell
    > zadd zset_key2 80 v1 40 v2 50 v3 80 v4 80 v5 90 v6
    (integer) 6

    > zrange zset_key2 0 -1 # 获取所有元素（默认按分数从小到大排列）
    1) "v2" 2) "v3" 3) "v1" 4) "v4" 5) "v5" 6) "v6"

    > zrevrange zset_key2 0 2 withscores # 逆序获取第 0 个至第 2 个元素及其分数
    2) "v6" 2) "90" 3) "v5" 4) "80" 5) "v4" 6) "80"

    > zrangebyscore zset_key2 50 80 withscores limit 0 2 # 分页获取分数在指定区间内的元素
    4) "v3" 2) "50" 3) "v1" 4) "80"

    > zrevrangebyscore zset_key2 80 50 # 逆序获取分数在指定区间内的元素
    6) "v5" 2) "v4" 3) "v1" 4) "v3"

    >zrange zset_key2 80 50 byscore rev
    7) "v5" 2) "v4" 3) "v1" 4) "v3"
    ```

<br>

- Key 值排序
  - 当集合中所有元素得分相同时，按照 key 值进行排序，当得分不同时，结果是不可预测的
  - 指定最大值和最小值时，需要以 `[` 或 `(` 开头，表示包含和不包含
  - 字符 `-` 和 `+` 可用来表示无穷小和无穷大

  - `ZRANGEBYLEX` 命令在 6.2.0 版本移除，被 `ZRANGE` 命令中的 `BYLEX` 参数替代
  - `ZREVRANGEBYLEX` 命令在 6.2.0 版本移除，被 `ZRANGE` 命令中的 `REV` 参数和 `BYLEX` 参数替代

    ```shell
    > zadd zset_key3 0 v1 0 v3 0 v2 0 a4 0 c5 0 b6
    (integer) 6

    > zrangebylex zset_key3 - [v # 返回所有小于等于 'v' 的字符串
    1) "a4" 2) "b6" 3) "c5"

    > zrangebylex zset_key3 (a4 [v2 # 返回所有大于 'a4'，小于等于 'v2' 的字符串
    1) "b6" 2) "c5" 3) "v1" 4) "v2"

    > zrevrangebylex zset_key3 [v2 - # 逆序返回所有小于等于 'v2' 的字符串
    1) "v2" 2) "v1" 3) "c5" 4) "b6" 5) "a4"

    > zrange zset_key3 [b [m bylex # 返回所有大于等于 'b'，小于等于 'm' 的字符串
    1) "b6" 2) "c5"
    ```

### 应用场景

具备集合去重的特性，且适合需要根据元素权重进行排序，且权重易于修改的场景，或是根据字符本身进行排序的场景

- 排行榜
  - 根据权重进行排序，且权重易于修改，例如商品销量榜

    <br>

  - 初始化售卖数量：`zadd rank 200 product:1 50 product:2`
  - 商品新增售卖量：`zincrby rank 20 product:2`
  - 商品退货：`zincrby rank -10 product:2`
  - 获取商品销量：`zscore rank product:2`
  - 获取商品销量最高的三个元素及其销量：`zrange rank 0 2 withscores rev`
  - 获取销量在 50 和 200 以内的元素：`zrange rank 50 200 withscores byscore`
<br>

- 目录索引
  - 利用集合去重的特性，且按照元素本身进行排序，例如通讯录
  - 需要注意，集合中元素的分值必须相同

    <br>

  - 初始化通讯录：`zadd address_book 0 ZhangSan 0 LiSi`
  - 新增元素：`zadd address_book 0 WangWu`
  - 获取所有元素：`zrange address_book - + bylex`
  - 获取区间 `[A, B)` 中的元素：`zrange address_book [A (B bylex`

## BitMap

`BitMap` 是 Redis 内部实现的位图类型，是一串二进制数组，特别适合数据量大且连续的二值统计场景。

在实现上，复用了 `String` 类型，即将统计数据以二进制的方式存储在 `String` 中，所以也可以将 `String` 类型作为 `BitMap` 类型进行使用。需要注意 `String` 中的一位是一个字节，对应于 `BitMap` 中的八个比特。

### 常用命令

> 官方文档：<https://redis.io/docs/latest/commands/?group=bitmap>

- 基础操作

  ```shell
  > setbit bit_key 2 1 # 设置指定位置(第二位)为指定值(1)，key 不存在时会创建
  (integer) 0

  > setbit bit_key 2 1 # 设置指定位置(第二位)为指定值(1)，并返回设置前的数值
  (integer) 1

  > getbit bit_key 2 # 获取指定位置(第二位)的值
  (integer) 1

  > bitcount bit_key # 获取值为 1 的 bit 数
  (integer) 1

  > bitcount bit_key 0 1 # 获取指定范围的 byte 内，值为 1 的 bit 数
  (integer) 1

  > bitcount bit_key 0 1 bit # 获取指定范围的 bit 内，值为 1 的 bit 数
  (integer) 0

  > bitcount bit_key 0 4 bit
  (integer) 1

  > bitpos bit_key 1 # 返回第一次出现指定数值(1)的位置
  (integer) 2
  ```

<br>

- 运算操作
  - 需要注意，对于字符串 "1"，其对应的二进制数据为 "00110101"，其他字符同理

  ```shell
  > set bit_key1 12 # 二进制表示为 00110001 00110010
  "OK"

  > set bit_key2 34 # 二进制表示为 00110011 00110100
  "OK"

  > bitop and res bit_key1 bit_key2 # 位图间的与运算
  (integer) 2

  > get res         # 二进制表示为 00110001 00110000
  "10"

  > bitop or res bit_key1 bit_key2 # 位图间的或运算
  (integer) 2

  > get res         # 二进制表示为 00110011 00110110
  "36"

  > bitop xor res bit_key1 bit_key2 # 位图间的异或运算
  (integer) 2

  > get res         # 二进制表示为 00000010 00000110
  "\x02\x06"

  > bitop not res bit_key1 # 位图的取反运算，仅支持单个 key 进行操作
  (integer) 2

  > get res         # 二进制表示为 11001110 11001101
  "\xce\xcd"
  ```

### 应用场景

适合于针对海量连续场景下的二值数据统计

- 签到统计
  - 签到日期是连续的，签到状态是二值状态

  <br>

  - 用户 100 在 24 年 3 月 26 日签到：`setbit user:100:202403 25 1`
  - 检查用户是否签到：`getbit user:100:202403 25`
  - 获取用户某月签到次数：`bitcount user:100:202403`
  - 获取用户某月首次签到日期：`bitpos user:100:202403 1`
<br>

- 连续签到统计
  - 以日期作为 key 值，用户 ID 作为偏移量

  <br>

  - 用户 100 在 24 年 3 月 26 日签到：`setbit sign:20240326 99 1`
  - 获取 21 日至 23 日连续签到的用户：
    - 三个日期的 bitmap 取交集：`bitop and res sign:20240321 sign:20240322 sign:20240323`
    - 统计个数：`bitcount res`
<br>

- 统计在线用户
  - 用户 ID 可认为是连续的，在线状态是二值状态

  <br>

  - 用户 326 上线：`setbit online 325 1`
  - 用户是否在线：`getbit online 325`
  - 用户离线：`setbit online 325 0`
  - 统计在线用户总数：`bitcount online`

## HyperLogLog

> 基数(Cardinality)，在中文数学中又被称作势，参考：[wiki/势_(数学)](https://zh.wikipedia.org/wiki/%E5%8A%BF_(%E6%95%B0%E5%AD%A6))。
> 势（英语：Cardinality）在数学里是指如果存在着从集合A到集合B的双射，那么集合A与集合B等势，记为A~B。一个有限集的元素个数是一个自然数，势标志着该集合的大小。对于有限集，势为其元素的数量。比较无穷集里元素的多寡之方法，可在集合论里用集合的等势和某集合的势比另一个集合大这两个概念来达到目的。

`HyperLogLog` 是一种利用了统计算法的数据结构，主要依托于基于概率的基数统计算法，可以以固定的空间占用、极高的时间复杂度提供精确度极高的去重计数，且各数据结构间较为容易做合并计算。

在 Redis 中，每个 `HyperLogLog` 对象只占用 12 KB 内存，可以计算接近 2^64 个不同元素的基数，标准误算率是 0.81%。

### 数据结构

HyperLogLog 由 Philippe Flajolet 在 原始论文[《HyperLogLog: the analysis of a near-optimal cardinality estimation algorithm》](http://algo.inria.fr/flajolet/Publications/FlFuGaMe07.pdf) 中提出。Redis 中对 HLL 的三个 PFADD/PFCOUNT/PFMERGE，都是以 PF 开头，就是纪念 2011 年已经去世的 Philippe Flajolet 。

2013 年 Google 的 一篇论文[《HyperLogLog in Practice: Algorithmic Engineering of a State of The Art Cardinality Estimation Algorithm》](http://static.googleusercontent.com/media/research.google.com/en//pubs/archive/40671.pdf) 深入介绍了其实际实现和变体。

### 常用命令

> 官方文档：<https://redis.io/docs/latest/commands/?group=hyperloglog>

```shell
> pfadd hll_key v1 v2 v3 v4 # 批量添加元素
(integer) 1

> pfcount hll_key # 返回元素去重后的数量的估算值
(integer) 4

> pfadd hll_key2 v4 v5 v6
(integer) 1

> pfmerge res hll_key hll_key2 # 合并两个 hll 对象，并去重
"OK"

> pfcount res
(integer) 6
```

### 应用场景

- 海量用户时的 UV 计数
  - `HyperLogLog` 的优势在于仅花费 12 KB 内存，就可以近似计算 $2^64$ 个元素的基数，故天然适合用去重计数的场景

  <br>

  - 新增用户访问：`pfadd page:uv user:1`
  - 获取计数结果：`pfcount page:uv`

## GEO

`GEO` 类型顾名思义，是针对于地址信息定制的类型。

在内部实现上，复用了 `Zset` 的结构，通过 [GeoHash](https://en.wikipedia.org/wiki/Geohash) 这种编码方式，将用户的实际经纬度映射到的一个字符串，并将之作为权重信息。

这些字符串可以在一定程度上反映经纬度信息，将这些字符串排序后，相邻的字符串，在地理位置上也是相邻的。

但是将二维数据转化为一维数据时，会先将其按照规则的矩形进行拆分，再用曲线进行填充。一方面相邻的矩形的边界处，有可能地理位置相近，但是编码差距较大，而在曲线坐标突变处，有可能字符串相邻的点，实际地理位置差距较大，需要额外考虑这些临界问题。

### 常用命令

> 官方文档：<https://redis.io/docs/latest/commands/?group=geo>

```shell
> geoadd geo_key 116.40 39.92 v1 116.27 40.00 v2 # 新增数据
(integer) 2

> geopos geo_key v1 # 获取某一点的经纬度（浮点数存在精度问题）
1) 1) "116.39999896287918091"
   2) "39.9199990416181052"

> geodist geo_key v1 v2 km # 获取某两个元素间的距离
"14.2132"

> georadius geo_key 116.40 39.92 1 km # 获取给定坐标，附近指定距离(1km)内的元素
1) "v1"

> georadius geo_key 116.40 39.92 1 km withcoord # 返回目标元素的经纬度
1) 1) "v1"
   2) 1) "116.39999896287918091"
      2) "39.9199990416181052"

> georadius geo_key 116.40 39.92 100 km withdist desc # 逆序返回目标元素和指定元素的距离
1) 1) "v2"
   2) "14.2132"
2) 1) "v1"
   2) "0.0001"

> georadius geo_key 116.40 39.92 100 km withdist count 1 # 返回指定数量
1) 1) "v1"
   2) "0.0001"
```

### 应用场景

- 打车服务
  - 分区域将司机的坐标信息存储在一个集合中，用户打车时根据其实时坐标获取相关信息
  - 实际应用中，还需要考虑线（道路、河流等）和面（建筑物）的问题，并非仅考虑点的问题

  <br>

  - 存储汽车信息：`geoadd position:car 116.40 39.92 car:1`
  - 获取用户附近 10 km 最近的 5 个车辆：`georadius position:car 116.43 39.90 10 km count 5`

## Stream

`Stream` 是 Redis 在 5.0 新增的类型，针对于消息队列进行设计，支持自动生成消息的全局 ID、支持消息的 ack 能力、支持消息组模式等。

### 常用命令

> 官方文档：<https://redis.io/docs/latest/commands/?group=stream>

- 添加消息：

  <br>

  ```shell
  > xadd stream_key * idx 1 name jia # 添加一条消息，* 代表使用 redis 默认 ID
  "1726845349463-0" # 返回消息 id，前者为时间戳，后面为时间戳相同时的自增 id

  > xadd stream_key * idx 2 name yi # 每条消息可以包含多个键值对，即 field 和 value
  "1726845356176-0"

  > xadd stream_key * idx 3 name bing
  "1726845363024-0"

  > xadd stream_key_id 1 idx 1 name zi # 也可以手动指定 id(1)，但是 id 需要是递增的
  "1-0"  # 默认 id 第二段为 0，遵循 2-0 > 1-1 > 1-0 这种逻辑

  > xlen stream_key # 查看当前队列长度
  (integer) 3
  ```

<br>

- 读消息
  - 操作均为幂等操作，可以重复读取

  <br>

  ```shell
  > xrange stream_key 1726845356176 + # 查看 id 范围内的消息，同样 + 和 - 代表正负无穷
  1) 1) "1726845356176-0"
     1) 1) "idx" 2) "2" 3) "name" 4) "yi"
  2) 1) "1726845363024-0"
     1) 1) "idx" 2) "3" 3) "name" 4) "bing"

  > xread count 2 streams stream_key stream_key_id 0 0 # 从多个消息队列，分别读取 count(2) 条消息
  1) 1) "stream_key"
     2) 1) 1) "1726845349463-0"
          2) 1) "idx" 2) "1" 3) "name" 4) "jia"
        2) 1) "1726845356176-0"
          2) 1) "idx" 2) "2" 3) "name" 4) "yi"
  2) 1) "stream_key_id"
    2) 1) 1) "1-0"
          2) 1) "idx" 2) "1" 3) "name" 4) "zi"

  > xread block 1000 streams stream_key 1726845356176 # 从指定 id 之后开始读取，最多阻塞 1000 毫秒
  1) 1) "stream_key"
     2) 1) 1) "1726845363024-0"
          2) 1) "idx" 2) "3" 3) "name" 4) "bing"

  > xread count 2 streams stream_key + # + 代表读取最新的一条消息（count 指令无效）
  1) 1) "stream_key"
     2) 1) 1) "1726845363024-0"
          2) 1) "idx" 2) "3" 3) "name" 4) "bing"

  > xread count 2 block 1000 streams stream_key $ # $ 代表忽略已有消息，读取新写入的消息
  (nil)
  ```

<br>

- 删除消息
  - 一般用于真正消费后，再移除消息，即确认消费

  <br>

  ```shell
  > xdel stream_key 1726845356176
  (integer) 1

  > xrange stream_key - +
  1) 1) "1726845349463-0"
     1) 1) "idx" 2) "1" 3) "name" 4) "jia"
  2) 1) "1726845363024-0"
     1) 1) "idx" 2) "3" 3) "name" 4) "bing"
  ```

<br>

- 消费组
  - 同一个消费组内的多个消费者，不能消费同一条消息，可用于实现负载均衡
  - 多个消费组之间，可以消费同一条消息，可用于实现广播

  <br>

  ```shell
  # 创建一个消费组，从 id 为 0 之后的消息开始消费
  > xgroup create stream_key group1 0
  "OK"

  # 创建一个消费者进行读取，> 代表从未消费过的消息开始读取
  > xreadgroup group group1 consumer1 streams stream_key > 
  1) 1) "stream_key"
     2) 1) 1) "1726845349463-0"
           2) 1) "idx" 2) "1" 3) "name" 4) "jia"
        2) 1) "1726845363024-0"
           2) 1) "idx" 2) "3" 3) "name" 4) "bing"

  # 创建一个同组消费者进行读取，所有消息都被 consumer1 消费，返回空
  > xreadgroup group group1 consumer2 streams stream_key > 
  (nil)

  # 查看从 id 0 之后，所有消费者已经读取，但未进行确认的消息
  > xreadgroup group group1 consumer1 streams stream_key 0
  1) 1) "stream_key"
     2) 1) 1) "1726845349463-0"
           2) 1) "idx" 2) "1" 3) "name" 4) "jia"
        2) 1) "1726845363024-0"
           2) 1) "idx" 2) "3" 3) "name" 4) "bing"

  > xreadgroup group group1 consumer2 streams stream_key 0
  1) 1) "stream_key"
     2) (empty list or set)

  # 查看当前消费组读取但未确认的所有消息
  > xpending stream_key group1 
  1) "2"                # 消息数量
  2) "1726845349463-0"  # 消息最小 id
  3) "1726845363024-0"  # 消息最大 id
  4) 1) 1) "consumer1"  # 消费者
        1) "2"          # 该消费者读取但未确认的消息数量

  > xpending stream_key group1 - + 1 consumer1 
  1) 1) "1726845349463-0" # 未确认的消息 id
     2) "consumer1"       # 消费者名称
     3) "1995503"         # 未确认的时间
     4) "3"               # 消息被传递的次数

  # 确认某条消息，会从 pending 列表中移除
  > xack stream_key group1 1726845349463
  (integer) 1

  > xpending stream_key group1
  1) "1"
  2) "1726845363024-0"
  3) "1726845363024-0"
  4) 1) 1) "consumer1"
        2) "1"

  # 创建新消费组，从 id 为 0 之后的消息开始消费
  > xgroup create stream_key group2 0
  "OK"

  # 不同消费组间的读取操作，不受影响，count 限制读取数量
  > xreadgroup count 1 group group2 consumer1 streams stream_key > 
  1) 1) "stream_key"
     2) 1) 1) "1726845349463-0"
           2) 1) "idx" 2) "1" 3) "name" 4) "jia"

  # 继续读取操作，block 可以设置阻塞时间
  > xreadgroup block 1000 group group2 consumer1 streams stream_key >
  1) 1) "stream_key"
     2) 1) 1) "1726845363024-0"
           2) 1) "idx" 2) "3" 3) "name" 4) "bing"
  
  # 新建消费组 3，$ 代表忽略已有消息，从新消息开始读取
  > xgroup create stream_key group3 $
  "OK"

  # 未写入新消息时，读取为空
  > xreadgroup group group3 consumer1 streams stream_key >
  (nil)
  ```

### 应用场景

- 消息队列
  - 在点对点模型(P2P)的场景下，较之于 `List`，`Stream` 内置了 id 相关功能，并保障全局唯一
  - 在发布订阅模型(Pub/Sub)的场景下，`Stream` 利用消息组的能力也可以实现广播能力，且提供了 ack 机制
  - 较之于消息队列中间件，Redis 作为缓存，会存在数据丢失风险，以及消息积压带来的内存压力，需要结合使用场景选取合适的方案

  <br>

  - 添加消息，使用默认 id 保障有序：`xadd stream_key * name silk`
  - 创建消息组，起始 id 为 0：`xgroup create stream_key group1 0`
  - 消费者读取消息：`xreadgroup count 1 group group1 consumer1 streams stream_key >`
  - 消费者确认消费消息：`xack stream_key group1 12345`
  - 查看未处理完成的消息：`xpending stream_key group1`

## Ref

- 《Redis 设计与实现》（基于 Redis 3.0 版本）
- <https://xiaolincoding.com/redis/data_struct/command.html#string>
- <https://panzhongxian.cn/cn/2024/04/hll-simple-introduce/>
