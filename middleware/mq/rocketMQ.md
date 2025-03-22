# RocketMQ

## 整体架构

RocketMQ 整体架构如下所示

![](images/2025-03-21-13-42-16.png)

**NameServer**

- RocketMR 的注册中心，负责 Broker 和路由信息的管理

**Broker Cluster**

- **Broker**：存储消息，分为 **Master**（读写）和 **Slave**（只读或故障切换）
- **Topic**：逻辑消息分类，分为多个 **MessageQueue**（类似 Kafka 的 Partition）
- **MessageQueue**：消息存储单元，分布在多个 Broker 上，避免单点故障

## 消息类型

**有序**

- 生产顺序：需要满足单一生产者，串行发送消息
  - 全局有序：Topic 仅一个 MessageQueue
  - 分区有序：相同业务键分配到同一队列
- 消费顺序：消费者串行消费

**延迟**

- 原生支持，内置 **18 个固定延迟等级**（1s/5s/10s/30s/1m/.../2h）
- 生产者发送时指定 `delayTimeLevel`，Broker 根据等级将消息暂存到延迟队列，到期后转发到目标 Topic

## 事务

RocketMQ 通过半消息和事务状态回查，保障事务的最终一致性。

**关键组件**

- **半消息（Half Message）**：暂存于 Broker 的特殊消息，消费者不可见
- **事务监听器（Transaction Listener）**：由生产者实现，用于执行本地事务和回查状态
- **事务回查线程**：Broker 主动询问生产者事务状态

**实现流程**

- **发送半消息**
  - 生产者发送消息到 Broker
  - Broker 将其标记为 `PREPARED` 状态（消费者不可见）
- **执行本地事务**
  - 生产者执行本地事务，并返回事务状态（COMMIT/ROLLBACK）
- **提交或回滚**
  - COMMIT：将消息转为 `COMMITTED` 状态，消费者可见
  - ROLLBACK：丢弃消息
- **事务状态回查**
  - 若 Broker 未收到确认（如网络超时），会定期向生产者回查事务状态
  - 生产者需实现 `checkLocalTransaction` 方法返回最终状态。

## 容错

**发送重试**

- **自动重试**
  - 消费失败后，消息进入 **重试队列（%RETRY%Topic）**，由 Broker 定时重投
  - 重试次数可配置（默认 16 次，间隔逐步增加：10s/30s/1m/2m/.../2h）
- **重试队列管理**
  - 每个 Consumer Group 有独立的重试队列
  - 超过最大重试次数后，消息进入死信队列

**死信队列**

- **原生支持**
  - 消息重试超过最大次数后，自动进入 **死信队列（%DLQ%Topic）**
  - 死信队列独立于原 Topic，需单独订阅和处理
- **管理功能**
  - 支持查询和重新投递死信消息（需人工干预）

### 持久化

**存储架构**

- RocketMQ 采用 CommitLog + ConsumeQueue 的混合存储结构，兼顾高效写入与快速检索
- CommitLog
  - 所有消息按顺序追加写入 CommitLog 文件（物理存储），确保顺序写盘的高性能
  - 单个 CommitLog 文件默认大小为 1GB，滚动生成新文件。
- ConsumeQueue
  - 逻辑队列，按 Topic 和 Queue 存储消息在 CommitLog 中的偏移量（物理位置）和元数据（Tag、消息大小等）
  - 消费者通过 ConsumeQueue 快速定位消息在 CommitLog 中的位置
- IndexFile
  - 可选索引文件，支持按消息 Key 或时间范围快速查询消息。

**刷盘策略**

- 同步刷盘（SYNC_FLUSH）：消息写入内存后，立即调用 fsync 强制刷盘，保证数据不丢失
- 异步刷盘（ASYNC_FLUSH）：消息写入内存后即返回成功，由后台线程定期批量刷盘（默认间隔 500ms）
- 利用 mmap 将 CommitLog 文件映射到内存，写入内存即代表写入 Page Cache

## 高可用

**主从架构**

- Broker 分为 Master 与 Slave
  - Master：处理读写请求，负责消息存储和同步到 Slave。
  - Slave：从 Master 异步 / 同步拉取数据，仅作为备份（默认不提供读服务）。

- 同步模式
  - 同步复制：Master 需等待 Slave 写入成功后才返回生产者确认（HA=SYNC）
  - 异步复制：Master 写入本地后立即返回确认，Slave 异步同步数据（默认模式）

**故障转移**

- NameServer
  - 去中心化的集群，无状态，集群间不通信，仅维护 Topic 路由信息（Broker 地址、队列分布）
  - Broker 定时向所有 NameServer 注册心跳，NameServer 宕机不影响集群能力
  - 客户端缓存路由表，NameServer 全部宕机时仍可短暂正常工作

- DLedger 模式（RocketMQ 4.5+）
  - 基于 Raft 协议实现多副本强一致性，支持自动选主和数据同步
  - Leader 选举：Master 宕机时，Slave 通过 Raft 协议选举新 Leader
  - 数据同步：所有写入需多数节点（N/2+1）确认，确保数据一致性

- 传统主从切换
  - 依赖运维工具（如 RocketMQ-Console）手动切换 Master，需人工介入

## Ref

- <https://rocketmq.apache.org/zh/docs/>
- <https://javaguide.cn/high-performance/message-queue/rocketmq-questions.html>
