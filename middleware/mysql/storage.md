# Storage

## MySQL 中持久化存储的数据与日志

MySQL 中持久化的核心数据和日志包括以下内容：

| **类型** | **持久化内容** | **文件位置** |
|----------|--------------|-------------|
| **数据文件**      | - 用户数据（表记录、索引）<br>- 系统元数据（表结构、统计信息等）                             | `.ibd`（独立表空间）<br>`ibdata1`（系统表空间） |
| **Redo Log**     | - 物理日志：记录数据页的修改操作（如字节变化、B+树分裂等）<br>- 保证事务的持久性和崩溃恢复能力 | `ib_logfile0`, `ib_logfile1`（循环文件）       |
| **Undo Log**     | - 逻辑日志：记录事务修改前的旧数据版本，用于回滚和 MVCC                                      | `ibdata1`（默认系统表空间）或独立 Undo 表空间       |
| **Binlog**       | - 逻辑日志：记录所有数据修改的 SQL 语句或行变更（需手动开启）<br>- 用于主从复制和增量恢复           | `mysql-bin.000001` 等（二进制文件）            |

## 段、区、页、行的层级关系

InnoDB 存储引擎采用层级化的物理存储结构，具体如下：

### 行（Row）

- **最小数据单元**：单条记录（如一行用户数据）。
- **存储格式**：  
  - 包含用户定义的列数据、隐藏列（事务 ID `trx_id`、回滚指针 `roll_pointer`）。
  - 支持行格式（如 `COMPACT`、`DYNAMIC`）控制溢出页存储策略。

### 页（Page）

- **最小管理单元**：默认大小 **16KB**，是 InnoDB 读写磁盘和内存的基本单位。
- **核心类型**：  
  - **数据页**：存储行记录。
  - **索引页（B+树节点）**：存储索引键和指针。
  - **Undo 页**：存储 Undo Log。
  - **系统页**：存储元数据（如空间管理信息）。

### 区（Extent）

- **连续页的集合**：一个区包含 **64 个连续的页**（16KB × 64 = 1MB）。
- **作用**：  
  - 减少随机 I/O：为表或索引预分配连续空间（例如 B+树扩展时直接分配一个区）。
  - 提升顺序访问性能。

### 段（Segment）

- **逻辑容器**：一个段由多个区组成，用于管理特定类型的存储空间。
- **常见段类型**：  
  - **数据段**：存储表数据（B+树的叶子节点）。
  - **索引段**：存储索引数据（B+树的非叶子节点）。
  - **回滚段**：存储 Undo Log。

### 层级关系总结

```plaintext
Segment（段） → Extent（区，多个连续页） → Page（页，16KB） → Row（行）
```

## Buffer Pool 中的内容

Buffer Pool 是 InnoDB 的内存缓存区域，核心内容包括：

| **内容** | **描述** |
|-----------|-----------|
| **数据页**       | - 缓存从磁盘加载的表数据页和索引页（B+树节点）<br>- 修改后的数据页称为 **脏页（Dirty Page）** |
| **Change Buffer** | - 缓存对非唯一索引的修改（INSERT/UPDATE/DELETE）<br>- 减少随机 I/O，延迟索引更新   |
| **自适应哈希索引** | - 自动为频繁访问的索引页构建哈希索引，加速查询   |
| **锁信息**    | - 行级锁、表级锁的元数据（部分实现可能驻留 Buffer Pool）  |
| **Undo Log 页** | - 缓存 Undo Log 的修改，支持事务回滚和 MVCC   |
| **系统页**      | - 存储空间管理信息（如区、段的分配状态）           |

## 持久化与内存协作流程示例

### 场景：更新一行数据

1. **加载数据到 Buffer Pool**  
   - 若目标数据页不在内存，从磁盘加载到 Buffer Pool。
2. **生成 Undo Log**  
   - 将旧数据写入 Undo 页（Buffer Pool 中），并生成 Redo Log 记录 Undo Log 的修改。
3. **修改数据页**  
   - 更新 Buffer Pool 中的数据页，标记为脏页。
4. **事务提交**  
   - Redo Log 刷盘（根据 `innodb_flush_log_at_trx_commit` 配置）。
   - Binlog 刷盘（若启用并配置为同步）。
5. **异步刷脏页**  
   - 后台线程根据 Checkpoint 机制将脏页刷回磁盘。

### Buffer Pool 与磁盘的交互

```plaintext
磁盘文件（.ibd） ↔ Buffer Pool（数据页/索引页） ↔ 用户查询和事务操作
```

## 关键设计思想

1. **日志先行（WAL）**  
   - 通过 Redo Log 保证数据修改的持久性，避免直接刷脏页的高延迟。
2. **空间预分配**  
   - 通过区（Extent）预分配连续空间，减少 B+树动态扩展的随机 I/O。
3. **内存缓冲优化**  
   - Buffer Pool 减少磁盘访问，Change Buffer 优化非唯一索引的写入性能。
4. **多版本并发控制（MVCC）**  
   - 通过 Undo Log 的版本链实现非锁定读，提升并发性能。

## Redo Log 和 Binlog 的存储位置与内存管理

### 存储位置与物理结构

#### Redo Log

- **物理存储**：  
  - **文件形式**：Redo Log 存储在独立的循环文件中，默认文件名为 `ib_logfile0` 和 `ib_logfile1`（数量可配置）。  
  - **非段区页结构**：  
    Redo Log **不遵循段、区、页的层级结构**，而是直接以**顺序追加写入的物理文件**形式管理。每个文件大小固定（默认 48MB），写满后循环覆盖旧日志。  
  - **内容类型**：  
    - **物理日志**：记录数据页的字节级修改（如页号、偏移量、新值）。  
    - **逻辑日志**：部分复杂操作（如 B+树分裂）记录逻辑语义。

- **内存管理**：  
  - **redo log buffer**：  
    - 事务执行期间，Redo Log 先写入内存中的 **redo log buffer**（大小由 `innodb_log_buffer_size` 控制，默认 16MB）。  
    - 事务提交时，根据配置 `innodb_flush_log_at_trx_commit` 决定刷盘策略：  
      - `=1`：同步刷盘（默认，保证持久性）。  
      - `=0` 或 `=2`：延迟刷盘或仅写入 OS 缓存。  
  - **异步刷盘**：  
    后台线程定期将 redo log buffer 中的数据刷盘（即使事务未提交），避免 buffer 满时阻塞用户线程。

#### Binlog

- **物理存储**：  
  - **文件形式**：Binlog 存储在 MySQL 数据目录下，文件名为 `mysql-bin.000001`、`mysql-bin.000002` 等，按顺序递增生成。  
  - **非段区页结构**：  
    Binlog 是 MySQL 服务器层的逻辑日志，**不依赖存储引擎的段区页结构**，直接以**顺序写入的二进制文件**形式存储。  
  - **内容类型**：  
    - **逻辑日志**：记录 SQL 语句（Statement 模式）或行变更数据（Row 模式）。  
    - **事务完整性**：通过 `COMMIT` 标记保证事务的原子性。

- **内存管理**：  
  - **binlog cache**：  
    - 每个事务的 Binlog 先写入线程私有的 **binlog_cache**（内存缓存）。  
    - 缓存大小由 `binlog_cache_size` 控制（默认 32KB），超出时暂存到临时文件。  
  - **刷盘策略**：  
    - 事务提交时，根据 `sync_binlog` 配置决定刷盘策略：  
      - `=1`：同步刷盘（保证 Binlog 不丢失）。  
      - `=0` 或 `=N`：依赖 OS 缓存或每 N 次提交批量刷盘。  

### 与段区页结构的对比

| **维度**       | **段区页结构（数据文件）**                  | **Redo Log**                     | **Binlog**                     |
|----------------|--------------------------------------|----------------------------------|--------------------------------|
| **管理对象**    | 用户数据（表、索引）                       | 数据页的物理修改记录                 | 逻辑操作记录（SQL 或行变更）       |
| **存储层级**    | 段 → 区 → 页 → 行                      | 独立循环文件                       | 独立顺序文件                     |
| **写入方式**    | 随机写入（数据页分散）                     | 顺序追加写入                       | 顺序追加写入                     |
| **作用范围**    | InnoDB 存储引擎内部                     | InnoDB 引擎崩溃恢复                | 跨引擎的主从复制与数据恢复         |
| **内存缓存**    | Buffer Pool（数据页缓存）              | redo log buffer                  | binlog cache                   |

### 协作与差异总结

1. **持久化目标不同**：  
   - **Redo Log**：保证事务的持久性（Durability），确保崩溃后数据页可恢复。  
   - **Binlog**：支持主从复制和基于时间点的数据恢复（Point-in-Time Recovery）。  

2. **写入时序**：  
   - **两阶段提交（2PC）**：  
     在启用 Binlog 时，事务提交需协调 Redo Log 和 Binlog：  
     1. 写入 Redo Log（Prepare 状态）。  
     2. 写入 Binlog。  
     3. 提交 Redo Log（Commit 状态）。  
   - 通过内部 XA 事务保证两者的一致性。

3. **性能优化差异**：  
   - **Redo Log**：顺序写入 + 异步刷脏页，避免随机 I/O。  
   - **Binlog**：逻辑日志体积较大（尤其是 Row 模式），高并发下可能成为瓶颈，需合理配置 `sync_binlog` 和 `binlog_format`。

### 示例流程：事务提交时的日志写入

1. **用户执行 UPDATE**：  
   - 数据页加载到 Buffer Pool，生成 Undo Log 和 Redo Log（内存中）。  
2. **事务提交**：  
   - **Redo Log**：写入 redo log buffer，根据 `innodb_flush_log_at_trx_commit` 刷盘。  
   - **Binlog**：写入 binlog cache，根据 `sync_binlog` 刷盘。  
3. **后台持久化**：  
   - Redo Log 对应的脏页由 Checkpoint 机制异步刷盘。  
   - Binlog 文件按需切换（通过 `max_binlog_size` 控制单个文件大小）。

### 设计思想

- **Redo Log**：  
  - **物理日志 + 顺序写入**：最小化崩溃恢复时间，避免数据页直接刷盘的开销。  
  - **内存缓冲**：通过 redo log buffer 合并多次 I/O，提升吞吐量。  
- **Binlog**：  
  - **逻辑日志 + 跨引擎**：提供与存储引擎无关的数据复制能力。  
  - **可读性**：支持人工解析（如 `mysqlbinlog` 工具），便于故障排查和数据恢复。  

通过 Redo Log 和 Binlog 的协作，MySQL 在保证数据可靠性的同时，实现了高效的事务处理与分布式数据同步能力。

## Redo Log Buffer、Binlog Cache 与 Buffer Pool 的内存管理与淘汰机制

### 内存区域的作用与大小控制

#### Buffer Pool

- **作用**：缓存数据页和索引页，减少磁盘 I/O。
- **大小控制**：  
  - 通过参数 `innodb_buffer_pool_size` 配置（建议设置为物理内存的 50%~80%）。
  - 支持动态调整（MySQL 5.7+），但需重启或在线调整（可能影响性能）。
- **数据淘汰策略**：  
  - **改进的 LRU 算法**：  
    Buffer Pool 的 LRU（Least Recently Used）链表分为两个子链表：  
    - **Young 子链表**：存储频繁访问的热数据。  
    - **Old 子链表**：存储新加载的冷数据（通过参数 `innodb_old_blocks_pct` 控制比例）。  
  - **冷热分离**：新数据页先进入 Old 子链表，只有被多次访问后才提升到 Young 子链表，避免全表扫描污染缓存。  
  - **淘汰触发条件**：  
    - Buffer Pool 空间不足时，优先淘汰 Old 子链表的冷数据页。  
    - 后台线程（如 `Page Cleaner`）异步刷脏页释放空间。

#### Redo Log Buffer

- **作用**：临时缓存事务生成的 Redo Log，减少磁盘 I/O 频率。
- **大小控制**：  
  - 通过参数 `innodb_log_buffer_size` 配置（默认 16MB，建议在高并发事务场景适当增大）。  
  - 固定大小，不支持动态调整，需重启生效。
- **数据淘汰策略**：  
  - **无显式淘汰**：Redo Log Buffer 按事务提交顺序刷盘，无需主动淘汰。  
  - **刷盘触发条件**：  
    - 事务提交时（根据 `innodb_flush_log_at_trx_commit` 配置）。  
    - 后台每秒定时刷盘。  
    - Redo Log Buffer 空间占用超过 75% 时强制刷盘。

#### Binlog Cache

- **作用**：缓存事务生成的 Binlog 日志，支持主从复制和数据恢复。
- **大小控制**：  
  - 通过参数 `binlog_cache_size` 配置（默认 32KB，建议根据事务大小调整）。  
  - 每个事务独占一个 Binlog Cache，超出时写入临时文件。  
- **数据淘汰策略**：  
  - **无显式淘汰**：Binlog Cache 在事务提交后清空。  
  - **溢出处理**：  
    - 若 Binlog Cache 空间不足，数据写入临时文件（磁盘），性能下降。  
    - 监控指标 `Binlog_cache_disk_use` 可统计溢出次数。

### 内存空间满时的处理机制

#### *Buffer Pool 满

- **后果**：  
  - 新数据页加载时触发 LRU 淘汰，频繁淘汰冷数据可能导致缓存命中率下降。  
  - 脏页刷盘压力增大，后台线程频繁刷盘，可能阻塞用户线程。  
- **应对措施**：  
  - 增大 `innodb_buffer_pool_size`。  
  - 优化查询，减少全表扫描（避免冷数据污染 Young 子链表）。  
  - 监控 `Innodb_buffer_pool_reads`（直接磁盘读取次数）和 `Innodb_buffer_pool_wait_free`（等待空闲页次数）。

#### Redo Log Buffer 满

- **后果**：  
  - 事务提交时强制同步刷盘（即使配置为 `innodb_flush_log_at_trx_commit=0`）。  
  - 高并发场景下，频繁刷盘导致事务提交延迟上升。  
- **应对措施**：  
  - 增大 `innodb_log_buffer_size`。  
  - 优化事务逻辑，减少单事务日志量。  
  - 监控 `Innodb_log_waits`（等待 Redo Log Buffer 空间次数）。

#### Binlog Cache 满

- **后果**：  
  - Binlog 写入临时文件（磁盘），导致事务提交延迟上升。  
  - 频繁磁盘 I/O 影响整体性能。  
- **应对措施**：  
  - 增大 `binlog_cache_size`。  
  - 使用 Row 模式时，减少单事务修改的数据量。  
  - 监控 `Binlog_cache_disk_use`（Binlog Cache 溢出次数）。

### 监控与优化建议

#### 关键监控指标

| **内存区域**     | **监控指标**                          | **说明**                          |
|------------------|-------------------------------------|-----------------------------------|
| **Buffer Pool**  | `Innodb_buffer_pool_reads`         | 直接磁盘读取次数（缓存未命中）          |
|                  | `Innodb_buffer_pool_wait_free`     | 等待空闲页次数（Buffer Pool 不足）    |
| **Redo Log**     | `Innodb_log_waits`                 | 等待 Redo Log Buffer 空间次数        |
| **Binlog**       | `Binlog_cache_disk_use`            | Binlog Cache 溢出到磁盘的次数         |

#### 优化建议

- **Buffer Pool**：  
  - 设置为物理内存的 50%~80%，避免频繁淘汰。  
  - 使用 `innodb_old_blocks_time` 控制冷数据提升阈值（默认 1s）。  
- **Redo Log Buffer**：  
  - 高并发事务场景（如秒杀）建议增大至 64MB~128MB。  
  - 避免长事务（减少 Redo Log 堆积）。  
- **Binlog Cache**：  
  - 根据平均事务大小调整 `binlog_cache_size`（例如单事务 1MB 日志则设为 2MB）。  
  - 启用 `binlog_row_image=MINIMAL`（Row 模式只记录变更列）减少日志体积。

### 内存管理流程图

```plaintext
事务提交流程：
1. 事务生成 Redo Log → 写入 Redo Log Buffer
   - 若 Buffer 满，强制刷盘 → 事务延迟上升
2. 事务生成 Binlog → 写入 Binlog Cache
   - 若 Cache 满，写入临时文件 → I/O 压力增大
3. 事务提交时：
   - Redo Log 按配置策略刷盘
   - Binlog 按 `sync_binlog` 配置刷盘
4. Buffer Pool 管理：
   - 新数据页加载 → 若 Buffer Pool 满，触发 LRU 淘汰
   - 脏页由后台线程异步刷盘
```

### 总结

- **Buffer Pool**：通过 LRU 淘汰冷数据页，优先保障热数据缓存。  
- **Redo Log Buffer**：依赖事务提交或后台线程刷盘，无显式淘汰。  
- **Binlog Cache**：事务提交后清空，溢出时写入磁盘临时文件。  
- **内存满的代价**：事务延迟上升、磁盘 I/O 压力增大，需通过监控指标提前优化配置。