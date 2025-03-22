# Kafka

## 整体架构

Kafka 整体架构如下所示

![](images/2025-03-21-13-42-31.png)

**Control Plane**

- 负责管理 Kafka 集群的元数据，包括 Broker 注册、Topic 注册、负载均衡等
- 旧版本使用 ZooKeeper 实现，新版本使用基于 Raft 的 KRaft 协议实现，更加轻量化
- 新增 **Broker** 时，**Partition** 自动重新分配，实现负载均衡
- **Partition** Leader 故障时，从 ISR 中选举新 Leader

**Data Plane**

- **Broker**：Kafka 的服务节点，可看作是一个独立的 Kafka 实例，多个 Broker 组成一个集群
- **Topic**：逻辑上的消息分类，每个 Topic 可分为多个 **Partition**，方便水平拓展
- **Partition**：消息存储的最小单元，每个 Partition 在物理上对应一个日志文件
- **Replica**：每个 Partition 有多个副本，包括 **Leader**（处理读写）和 **Follower**（异步/同步复制数据），
- **ISR（In-Sync Replicas）**：与 Leader 数据同步的副本集合，触发故障转移时，控制面会从这部分集合中选举新 Leader

## 消息类型

**有序**

- **局部有序**：每个分区（Partition）内的消息按写入顺序严格有序，但不同分区之间的消息顺序无法保证
- **全局有序**：需要限制 Topic 保持单分区
- **消费有序**：消费者按分区顺序消费（单线程或分区内多线程）

**延迟**

默认不支持延迟消息，需要业务处理。

- **消费者过滤**：消息附带目标触发时间戳，消费者轮询过滤未到期的消息
- **外部调度**：结合外部系统记录延迟时间，到期后重新投递到 Kafka

## 事务

Kafka 在高版本中支持了事务消息，其主要目标是实现 Exactly-Once 语义，确保生产者和消费者在分布式环境下的原子性操作。

**关键组件**

- **事务协调器（Transaction Coordinator）**：Broker 中的组件，负责管理事务状态
- **事务日志（Transaction Log）**：存储事务元数据（如事务 ID、状态）的内部 Topic（`__transaction_state`）
- **生产者事务 ID（Transactional ID）**：唯一标识一个生产者实例，用于故障恢复和跨会话事务关联

**实现流程**

- **初始化事务**：生产者通过 `initTransactions()` 向协调器注册，获取事务 ID 并建立会话
- **开启事务**：生产者调用 `beginTransaction()`，标记事务开始
- **发送消息**：消息被发送到目标 Topic，但处于未提交状态，消费者不可见
  - isolation.level=read_committed 时，仅读取已提交的消息
  - isolation.level=read_uncommitted 时，读取所有消息（默认）
- **提交或回滚**
  - **提交**：生产者发送 `commitTransaction()` 请求，协调器将事务标记为提交，消息对消费者可见
  - **回滚**：生产者调用 `abortTransaction()`，协调器丢弃事务相关的消息
- **两阶段提交（2PC）**
  - **Prepare 阶段**：协调器将事务状态标记为 "Prepare Commit" 并持久化到事务日志
  - **Commit 阶段**：协调器向所有相关分区写入提交标记，使消息可见

**Exactly-Once 保障**

- 跨会话幂等
  - Producer 重启会导致 Producer ID 重置，重新发送时 PID 无法满足幂等性
  - 通过生产者事务 ID 可以进行保障

- 跨分区原子性
  - 多分区消息提交时可能部分成功、部分失败，但是生产者只能全部重新发送
  - 若分区恰好更换 Leader 且 Seq Num 未同步，同样无法满足幂等性
  - 通过 2PC 保障跨分区发送消息时的原子性

## 容错

**发送重试**

- **重试策略**：配置**重试次数**和**重试间隔**，应对网络抖动或 Broker 短暂不可用
- **幂等性**：支持按照 ProducerID 和 SequenceID 去重

**死信队列**

默认不支持死信队列，需要业务处理。

- 消费者将多次失败的消息转发到指定的 DLQ Topic
- 监控 DLQ 并人工处理异常消息

### 持久化

**刷盘**

- 消息默认异步刷盘，Kafka 将消息写入 Page Cache 后立即返回成功
- 依赖操作系统定期将缓存数据刷新到磁盘
- 可手动配置同步刷盘策略，如消息量或时间

**副本**

- 可通过 `replication.factor` 配置分区的副本数量，降低消息丢失风险
- 可通过 `min.insync.replicas` 参数配置消息写入多少个副本后，代表接受成功
- 一般可设置 `min.insync.replicas=replication.factor-1`，兼顾可用性

## 高可用

**分区副本**

- 每个分区有且仅有一个 Leader 副本，负责处理所有读写请求
  - 生产者和消费者均与 Leader 交互
- 其他副本均为 Follower，从 Leader 异步拉取（Pull）消息并持久化到本地日志，保持与 Leader 的数据同步
  - Follower 副本不对外提供服务，仅作为 Leader 的备份，用于故障时快速切换
- Kafka 维护一个 ISR（In-Sync Replicas）列表，包含所有与 Leader 保持同步的副本（包括 Leader 自身）
  - 同步条件：Follower 副本的 LEO（Log End Offset，最新消息位置）与 Leader 的 HW（High Watermark，已提交消息位置）差距在可接受范围内
  - 若 Follower 副本长时间未同步，会被移出 ISR 列表，避免拖慢整体同步效率

**故障恢复**

- 若 Leader 宕机，Controller 节点从 ISR（In-Sync Replicas）列表选举新 Leader
- 若 ISR 列表为空，根据配置，可能选举非同步副本

**拓展**

- 支持动态添加 Broker，分区自动均衡

## Ref

- <https://xiaolincoding.com/interview/win.html>
- <https://blog.bytebytego.com/p/why-is-kafka-so-fast-how-does-it>
