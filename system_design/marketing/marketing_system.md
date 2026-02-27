# 营销系统

**摘要**：营销系统的核心任务，是在预算、体验与合规边界内持续提升增量收益。实现这一目标，关键不在于功能堆叠，而在于先建立一条从策略制定、执行触达到归因复盘的可追踪闭环，再通过预算上限、触达频控、资格准入、策略互斥、优先级和配额等控制面约束执行过程，从而让增长、成本与风险在同一框架下被统一管理。

**核心观点概览**：
- 营销系统的本质是“有限预算下的增量收益最大化”，而不是“功能堆叠最大化”。
- 业务对象统一建模是平台化前提，缺少统一对象会导致策略冲突、数据口径漂移和重复建设。
- 决策执行流程应内置仲裁与异常保障机制，避免多活动并行导致预算失控与资金风险。
- 智能化应服务于可解释收益提升，Uplift 可优先落地，RL/MTA 按边界逐步引入。

## 1. 营销系统要解决的业务问题与边界

### 1.1 目标函数

营销系统的统一目标函数可定义为：

$$\text{Objective} = \text{Incremental Revenue} - \text{Marketing Cost} - \text{Experience Loss}$$

- **Incremental Revenue**：由营销动作产生的增量转化收益，而非自然转化。
- **Marketing Cost**：券补贴、渠道成本、内容生产成本、执行成本。
- **Experience Loss**：过度触达、错误触达、品牌负反馈带来的长期损耗。

### 1.2 业务边界与硬约束

| 约束项 | 业务含义 | 常见失效模式 | 治理措施 |
| :--- | :--- | :--- | :--- |
| `BudgetCap` | 活动或策略预算上限 | 预算短时间快速耗尽，后续缺乏弹性 | 分层预算池、小时级配额、预算熔断 |
| `FreqCap` | 用户触达频次上限 | 用户疲劳、退订率上升 | 多窗口频控（日/周/月）、跨渠道去重 |
| `Eligibility` | 用户资格门槛 | 非目标用户获券 | 条件快照、规则版本化 |
| `Exclusion` | 互斥规则 | 多活动叠加补贴 | 互斥矩阵、优先级仲裁 |
| `Priority` | 策略优先级 | 高价值策略被低价值策略抢占 | 仲裁层统一排序、收益优先 |
| `Quota` | 配额限制（用户/渠道/时段） | 热点渠道过载或库存偏斜 | 分桶配额、动态回收 |

### 1.3 设计原则

- **增量优先**：所有策略以净增量收益为评估标准，避免为自然转化支付补贴。
- **约束内优化**：预算、频控、资格、互斥是硬约束，任何自动化策略不得突破。
- **可回放可审计**：关键决策必须保留版本、快照和 trace，支持复盘与归因校验。
- **分阶段演进**：先建立闭环，再推进平台化，最后引入复杂智能策略。

## 2. 核心业务对象模型与生命周期

### 2.1 对象定义与生命周期

| 对象 | 定义 | 主键示例 | 生命周期状态 | 状态迁移触发条件 |
| :--- | :--- | :--- | :--- | :--- |
| `Campaign` | 业务活动，定义目标、预算和时间窗口 | `campaign_id` | Draft -> Approved -> Running -> Paused -> Closed | 审批通过、到达开始时间、手动暂停、到达结束时间 |
| `Plan` | 活动内批次或阶段配置 | `plan_id` | Draft -> Scheduled -> Running -> Finished | 定时触发、手动启动、任务完成 |
| `Strategy` | 决策单元，绑定规则与动作 | `strategy_id` | Draft -> Online -> Offline | 发布、回滚、版本替换 |
| `Rule` | 条件表达式，描述 Eligibility/Exclusion | `rule_id` | Editing -> Active -> Deprecated | 编译成功、策略下线 |
| `Audience` | 人群定义与计算结果 | `audience_id` | Defining -> Materialized -> Expired | 圈选完成、时间窗过期 |
| `Offer` | 权益实体（券/积分/红包） | `offer_id` | Created -> Issuing -> Redeemable -> Expired | 库存上架、发放、核销、过期 |
| `TouchTask` | 触达任务（短信/Push/站内） | `touch_task_id` | Pending -> Sending -> Sent -> Failed/Cancelled | 调度执行、回执返回、重试失败 |
| `AttributionWindow` | 归因时间窗配置 | `attr_window_id` | Draft -> Active -> Archived | 口径生效、口径替换 |

### 2.2 对象关系与一对多约束

- `Campaign` 1:N `Plan`：一个活动包含多个执行阶段。
- `Plan` 1:N `Strategy`：每个阶段可并行配置多条策略。
- `Strategy` 1:N `Rule`：一条策略可组合多个条件规则。
- `Strategy` 1:N `Offer`：一条策略可绑定多个权益梯度。
- `Strategy` 1:N `TouchTask`：同一策略可拆分多渠道触达任务。
- `Audience` 和 `Strategy` 是“引用关系”，同一人群可被多策略引用，但要受 `Exclusion` 与 `Priority` 仲裁。

<table style="width:100%; border-collapse:separate; border-spacing:0 8px; margin:12px 0;">
  <tr>
    <td style="padding:10px 12px; border:1px solid #bfdbfe; border-radius:10px; background:#eff6ff; font-weight:700; color:#1d4ed8;">Campaign</td>
    <td style="width:56px; text-align:center; color:#64748b; font-weight:700;">1:N</td>
    <td style="padding:10px 12px; border:1px solid #bfdbfe; border-radius:10px; background:#eff6ff; font-weight:700; color:#1d4ed8;">Plan</td>
    <td style="padding:10px 12px; color:#334155;">活动拆分为多个执行阶段</td>
  </tr>
  <tr>
    <td style="padding:10px 12px; border:1px solid #99f6e4; border-radius:10px; background:#ecfeff; font-weight:700; color:#0f766e;">Plan</td>
    <td style="width:56px; text-align:center; color:#64748b; font-weight:700;">1:N</td>
    <td style="padding:10px 12px; border:1px solid #99f6e4; border-radius:10px; background:#ecfeff; font-weight:700; color:#0f766e;">Strategy</td>
    <td style="padding:10px 12px; color:#334155;">每个阶段配置多条策略</td>
  </tr>
  <tr>
    <td style="padding:10px 12px; border:1px solid #c4b5fd; border-radius:10px; background:#f5f3ff; font-weight:700; color:#6d28d9;">Strategy</td>
    <td style="width:56px; text-align:center; color:#64748b; font-weight:700;">1:N</td>
    <td style="padding:10px 12px; border:1px solid #c4b5fd; border-radius:10px; background:#f5f3ff; font-weight:700; color:#6d28d9;">Rule / Offer / TouchTask</td>
    <td style="padding:10px 12px; color:#334155;">规则判断、权益动作、触达任务均由策略驱动</td>
  </tr>
  <tr>
    <td style="padding:10px 12px; border:1px solid #fde68a; border-radius:10px; background:#fffbeb; font-weight:700; color:#a16207;">Strategy</td>
    <td style="width:56px; text-align:center; color:#64748b; font-weight:700;">引用</td>
    <td style="padding:10px 12px; border:1px solid #fde68a; border-radius:10px; background:#fffbeb; font-weight:700; color:#a16207;">Audience / AttributionWindow</td>
    <td style="padding:10px 12px; color:#334155;">分别定义命中人群与归因口径</td>
  </tr>
</table>

### 2.3 对象级幂等键规范

| 场景 | 幂等键建议 | 说明 |
| :--- | :--- | :--- |
| 发券 | `campaign_id + strategy_id + user_id + offer_id + biz_date` | 防重复发放与跨重试重复扣库 |
| 触达 | `touch_task_id + user_id + channel + template_version` | 防重复推送、保障回执匹配 |
| 核销 | `offer_instance_id + order_id` | 防重复核销和补偿重复执行 |
| 归因入账 | `user_id + conversion_event_id + attr_window_id` | 保证一次转化只被一次口径计算 |

## 3. 决策执行主链路

<table style="width:100%; table-layout:fixed; border-collapse:collapse; margin:14px 0; border:1px solid #e5e7eb; border-radius:12px; overflow:hidden;">
  <tr style="background:#f8fafc;">
    <td style="padding:12px 10px; border-right:1px solid #e5e7eb; text-align:center; font-weight:700; color:#1d4ed8;">① 事件输入</td>
    <td style="padding:12px 10px; border-right:1px solid #e5e7eb; text-align:center; font-weight:700; color:#1d4ed8;">② Audience 圈选</td>
    <td style="padding:12px 10px; border-right:1px solid #e5e7eb; text-align:center; font-weight:700; color:#1d4ed8;">③ 资格判定<br><span style="font-weight:500; color:#475569;">Eligibility / Exclusion</span></td>
    <td style="padding:12px 10px; text-align:center; font-weight:700; color:#1d4ed8;">④ 策略仲裁<br><span style="font-weight:500; color:#475569;">Priority / BudgetCap / Quota</span></td>
  </tr>
  <tr>
    <td colspan="4" style="padding:6px 0; text-align:center; color:#64748b; font-weight:700;">↓</td>
  </tr>
  <tr style="background:#f0fdfa;">
    <td style="padding:12px 10px; border-right:1px solid #ccfbf1; text-align:center; font-weight:700; color:#0f766e;">⑤ Offer 发放</td>
    <td style="padding:12px 10px; border-right:1px solid #ccfbf1; text-align:center; font-weight:700; color:#0f766e;">⑥ TouchTask 触达</td>
    <td style="padding:12px 10px; border-right:1px solid #ccfbf1; text-align:center; font-weight:700; color:#0f766e;">⑦ 转化事件采集</td>
    <td style="padding:12px 10px; text-align:center; font-weight:700; color:#0f766e;">⑧ AttributionWindow 归因</td>
  </tr>
  <tr>
    <td colspan="4" style="padding:6px 0; text-align:center; color:#64748b; font-weight:700;">↓</td>
  </tr>
  <tr style="background:#f5f3ff;">
    <td colspan="4" style="padding:14px 12px; text-align:center; font-weight:700; color:#6d28d9;">⑨ 收益回传与调参</td>
  </tr>
</table>

### 3.1 圈选与资格判定

- **业务问题**：人群不准会直接导致成本浪费。
- **机制**：基于 ID Mapping + 标签快照进行实时或准实时圈选；Eligibility 判断目标命中，Exclusion 判断禁止命中。
- **风险**：标签延迟导致资格误判。
- **指标**：人群规模偏差率、资格误判率、圈选耗时。

### 3.2 策略仲裁层

仲裁层是多活动并行下的核心控制面，统一处理：

- `Priority`：按业务价值和策略级别排序。
- `Exclusion`：执行互斥矩阵，禁止冲突策略同时命中。
- `BudgetCap`：预算不足时阻断发放，避免超预算。
- `Quota`：按渠道/时段/人群分桶控量。
- **异常保障机制**：仲裁服务超时或异常时，降级至保守策略（例如仅执行低补贴策略，或仅触达不发券）。

### 3.3 发放链路（Offer）

- **资格快照**：在发放时固化 Eligibility 结果与策略版本，便于审计和回放。
- **库存与扣减**：Redis Lua 原子扣减 + 热点 Key 分片，避免超卖。
- **冻结与核销对账**：发放生成冻结记录，核销后转实耗；超时未核销自动释放或过期结转。
- **补偿机制**：针对重试失败、回执丢失、跨服务超时等情况，提供反向补偿与人工保障流程。

### 3.4 触达链路（TouchTask）

- **疲劳度控制**：统一执行 `FreqCap`（用户/渠道/活动多窗口）。
- **渠道降级**：主渠道失败按策略降级（例如 Push -> 短信 -> 站内信）。
- **重复抑制**：基于幂等键和回执状态，防止重复触达。
- **质量目标**：在保证成功率前提下最小化骚扰成本。

### 3.5 归因链路（AttributionWindow）

- 归因窗口必须与策略目标一致（拉新短窗、召回中窗、复购长窗）。
- 同一转化事件按主口径单归因，辅口径可用于分析但不用于主结算。
- 归因结果回流策略引擎与模型训练，形成闭环。

### 3.6 核心技术模块与团队职责边界

在大型互联网组织实践中，营销系统通常并非由单一团队或单一代码库完成，而是由多个平台团队协同交付。研发团队需要首先明确能力边界与结果责任归属。

| 技术模块 | 主要职责 | 典型责任团队 | 对外接口与产出 | 关键 SLA/验收 |
| :--- | :--- | :--- | :--- | :--- |
| 规则引擎（Rule Engine） | 解析与执行 Rule，支持策略版本和灰度 | 决策平台团队 | `evaluate(context)`、规则版本、命中明细 | 决策延迟、规则正确率、灰度回退时效 |
| 权益引擎（Offer Engine） | 库存、发放、冻结、核销、对账补偿 | 交易营销团队或权益平台团队 | 发券/核销 API、账户流水、对账结果 | 资损率、发放成功率、对账时延 |
| 低代码平台（Campaign Builder） | Campaign/Plan/Strategy 可视化编排与发布 | 运营平台团队 | 配置 DSL、发布单、审批流、版本快照 | 发布成功率、回退耗时、配置错误率 |
| 人群平台（Audience Service） | 人群定义、圈选、快照、同步 | CDP/数据产品团队 | 人群 ID、快照版本、覆盖率报告 | 圈选耗时、规模偏差率、更新时效 |
| 触达网关（Touch Gateway） | 多渠道发送、路由、频控、回执 | 消息平台团队 | 触达任务 API、回执流、渠道状态 | 触达成功率、到达延迟、退订率 |
| 归因与分析服务（Attribution Service） | AttributionWindow 归因、增量核算、报表 | 数据分析平台团队 | 归因结果、ROI 报表、口径字典 | 口径一致性、报表时延、重复归因率 |
| 模型服务（Model Serving） | 评分、策略推荐、模型版本治理 | 算法工程团队 | `score()`、特征版本、reason code | 可用性、评分延迟、版本可追溯性 |

### 3.7 跨团队接口契约（研发视角）

当规则引擎、权益引擎、低代码平台分别由不同团队负责时，接口契约要固定三个层次：

- **业务契约**：对象语义一致（Campaign/Plan/Strategy/Offer/TouchTask），口径一致（AttributionWindow）。
- **技术契约**：请求/响应字段、错误码、幂等键、超时与重试语义。
- **治理契约**：版本发布流程、回退规则、审计字段与追踪路径。

建议接口文档至少包含以下字段：

| 接口 | 必填字段（示例） | 幂等键 | 失败处理约定 |
| :--- | :--- | :--- | :--- |
| 规则评估接口 | `trace_id`、`strategy_id`、`user_id`、`context`、`rule_version` | `trace_id + strategy_id + user_id` | 超时降级到保守策略，返回 `reason_code` |
| 发券接口 | `campaign_id`、`strategy_id`、`user_id`、`offer_id`、`biz_time` | `campaign_id + strategy_id + user_id + offer_id + biz_date` | 先扣减后异步通知；失败走补偿队列 |
| 触达接口 | `touch_task_id`、`channel`、`template_id`、`user_id` | `touch_task_id + user_id + channel` | 渠道失败按路由降级，回执异步对齐 |
| 归因接口 | `conversion_event_id`、`user_id`、`attr_window_id` | `user_id + conversion_event_id + attr_window_id` | 重复归因直接拒绝并记录审计日志 |

### 3.8 跨团队联调与发布流程

1. 低代码平台发布 Strategy 初版配置，生成 `strategy_version`。  
2. 规则引擎编译 Rule 并输出可执行版本；联调验证命中明细。  
3. 权益引擎执行 Offer 库存与发放全流程压力测试，确认资金风险防线。  
4. 触达网关验证 FreqCap 与渠道降级策略。  
5. 归因服务预注册 AttributionWindow，确保报表口径已生效。  
6. 通过灰度发布单上线；异常时按“规则回退 -> 发券降级 -> 触达限流”顺序实施风险控制。  

## 4. 三类高价值场景设计（拉新/促活/召回）

### 4.1 拉新场景

- **业务目标**：降低 CAC，提升首单转化率。
- **策略组合**：渠道投放 + 首单 Offer + 新手 TouchTask。
- **关键约束**：`BudgetCap`、`Eligibility`（新用户定义）、`Exclusion`（防与召回叠加）。
- **核心指标**：新增用户数、首单 CVR、渠道 ROI、CAC。
- **常见失败模式**：渠道作弊导致虚假新增，或首单补贴过深导致 ROI 为负。

### 4.2 促活场景

- **业务目标**：提升活跃频次与使用深度。
- **策略组合**：签到/任务激励 + 分层 Offer + 站内触达。
- **关键约束**：`FreqCap`、`Quota`（高峰期控量）、`Priority`（高价值用户优先）。
- **核心指标**：DAU/MAU、任务完成率、核销率、7 日留存。
- **常见失败模式**：频控失效造成疲劳，或激励强度不足导致参与率低。

### 4.3 召回场景

- **业务目标**：唤回沉默用户并恢复交易行为。
- **策略组合**：流失分层 Audience + 差异化 Offer + 多渠道 TouchTask。
- **关键约束**：`Eligibility`（流失定义）、`Exclusion`（与拉新互斥）、`BudgetCap`。
- **核心指标**：召回率、回流后 7/30 日留存、增量 GMV、召回 ROI。
- **常见失败模式**：归因窗口过短导致低估效果，或过长导致高估自然回流。

## 5. 智能化策略接入（Uplift 为主，RL/MTA 为辅）

### 5.1 Uplift：优先落地的增量决策模型

Uplift 的核心是估计个体处理效应：

$$Uplift = P(Y|T=1, X) - P(Y|T=0, X)$$

- **训练口径**：样本需包含处理组/对照组标签，目标转化口径与 `AttributionWindow` 对齐。
- **上线阈值**：仅对 Uplift 分数超过阈值且满足 `BudgetCap` 的用户触发补贴。
- **收益回传**：按策略版本回传“真实增量收益、补贴成本、净收益”，用于再训练与阈值更新。
- **适用边界**：适合券补贴和触达强干预场景；在样本稀疏或强策略干预变更频繁时应降级到规则策略。

### 5.2 RL/MTA：按边界接入，强调适用范围

- **RL 适用**：长周期、多步决策、明确状态转移的场景（如会员成长旅程）。
- **RL 降级**：当在线反馈延迟大、探索成本高时，回退为固定策略 + 小步人工调参。
- **MTA 适用**：用于渠道预算分配的辅助分析，不直接替代主归因结算口径。
- **MTA 降级**：数据缺失或路径稀疏时，回退到规则归因（首触/末触/位置衰减）。

### 5.3 策略引擎与模型服务接口（逻辑字段）

- **请求字段**：`user_id`、`campaign_id`、`strategy_id`、`feature_vector`、`context`、`budget_state`、`freq_state`。
- **响应字段**：`score`、`decision`（issue/hold/skip）、`recommended_offer_id`、`reason_code`、`model_version`。
- **审计字段**：`trace_id`、`rule_version`、`attr_window_id`、`timestamp`。

## 6. 营销特有工程难点与治理策略

### 6.1 资损（多发、错发、重复核销）

- **现象**：优惠券被重复发放或重复核销，导致资金损失。
- **根因**：幂等缺失、库存扣减与状态更新分离、回执不一致。
- **治理**：全链路幂等键、原子扣减、冻结与核销双账本、异常补偿任务。
- **观测指标**：资损率、重复发放率、重复核销率、补偿成功率。

### 6.2 策略冲突（多活动并行）

- **现象**：同一用户同时命中多策略，补贴叠加不可控。
- **根因**：缺统一仲裁层、互斥关系分散在各系统。
- **治理**：集中仲裁服务、`Priority` 排序、`Exclusion` 互斥矩阵、预算预占。
- **观测指标**：冲突命中率、仲裁拒绝率、优先级反转次数。

### 6.3 频控疲劳（触达过度）

- **现象**：触达成功率看似正常，但退订和投诉上升，长期转化下降。
- **根因**：只按单渠道频控，未做跨活动和跨渠道去重。
- **治理**：统一 `FreqCap` 服务、跨渠道频控、模板多样化、冷却期策略。
- **观测指标**：退订率、投诉率、触达后转化衰减、用户疲劳指数。

### 6.4 归因偏差（高估或低估营销效果）

- **现象**：ROI 波动异常，策略迭代方向失真。
- **根因**：归因窗口不一致、自然流量未扣除、重复归因。
- **治理**：统一 `AttributionWindow`、主口径单归因、增量口径审计、回放校验。
- **观测指标**：口径偏差率、重复归因率、增量收益稳定性。

### 6.5 通用 HA/事务在营销场景的差异点

营销场景比一般业务更敏感的原因在于“资金 + 时效 + 并发”三重约束并存。LDC、多活、TCC、Saga 可作为通用能力，但在营销中应重点关注：

- 秒级峰值下的预算和库存一致性。
- 异步链路中的幂等与补偿闭环。
- 故障降级时优先保护资金安全而非触达覆盖率。

## 7. 指标体系：从结果指标到调参动作

### 7.1 指标分层

- **结果指标**：增量 GMV、活动 ROI、LTV 提升。
- **过程指标**：领取率、核销率、触达成功率、归因命中率。
- **风控指标**：资损率、冲突命中率、退订率、预算偏差率。

### 7.2 指标异常 -> 诊断 -> 动作矩阵

| 异常指标 | 诊断方向 | 典型动作 |
| :--- | :--- | :--- |
| 核销率下降 | 券门槛过高、发放时机偏移、人群漂移 | 下调门槛、调整触达时段、收缩 Audience |
| 领取率高但核销率低 | 高领取低使用行为、场景不匹配 | 缩短有效期、改为分层 Offer、增加使用门槛 |
| 触达成功率下降 | 渠道拥塞、模板限流、路由异常 | 启用渠道降级、切换模板、扩容发送配额 |
| 退订率上升 | `FreqCap` 失效、文案重复 | 收紧频控、增加冷却期、更新素材策略 |
| CAC 上升 | 渠道质量下降、拉新补贴过深 | 下调渠道出价、提高 Eligibility、压缩补贴 |
| 增量 ROI 下降 | 自然流量补贴、归因偏差 | 强化 Exclusion、重设 AttributionWindow、回收预算 |
| 资损率上升 | 幂等失败、对账滞后、补偿漏跑 | 强化幂等校验、缩短对账周期、补偿任务告警 |
| 预算消耗过快 | 高优先级策略过宽、Quota 失衡 | 限制高成本策略流量、按时段重配 Quota |
| 冲突命中率上升 | 互斥矩阵缺失或未更新 | 更新 Exclusion 规则、提高仲裁覆盖率 |
| 人群规模误差扩大 | 标签延迟、ID 映射漂移 | 修复数据延迟、重算人群、校准 ID Mapping |

## 8. 分阶段建设路线图（MVP -> 平台化 -> 智能化）

### 8.1 三阶段能力路线图

| 阶段 | 阶段目标 | 必做能力 | 可选能力 | 暂缓能力 | 退出条件 |
| :--- | :--- | :--- | :--- | :--- | :--- |
| MVP | 完成发券与触达闭环建设 | 对象模型最小集、Eligibility、基础 BudgetCap/FreqCap、发放幂等、基础归因 | 简单人群分层、模板管理 | RL、复杂 MTA | 连续 2 个周期 ROI 为正且资损可控 |
| 平台化 | 支撑多活动并行与运营效率提升 | 仲裁层（Priority/Exclusion/Quota）、低代码编排、对账补偿、指标监测面板 | 策略仿真、动态配额 | 全自动预算分配 | 多活动并行下稳定运行，冲突率受控 |
| 智能化 | 提升增量收益和自动化水平 | Uplift 在线决策、收益回传闭环、策略自动调参 | RL 局部试点、MTA 辅助预算优化 | 全量自治决策 | 增量收益持续提升且可解释 |

### 8.2 团队最小配置建议

| 角色 | MVP | 平台化 | 智能化 |
| :--- | :--- | :--- | :--- |
| 产品经理 | 1 | 1-2 | 2 |
| 运营 | 1-2 | 2-3 | 3+ |
| 后端工程师 | 2 | 3-4 | 4-5 |
| 算法工程师 | 0-1 | 1-2 | 2-3 |
| 数据分析 | 1 | 1-2 | 2 |

### 8.3 跨团队协作模型（RACI）

| 事项 | 业务研发 | 规则引擎团队 | 权益引擎团队 | 低代码平台团队 | 数据/归因团队 | 运营 |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| 活动方案评审 | A/R | C | C | C | C | R |
| Strategy 配置发布 | C | C | C | A/R | C | R |
| Rule 逻辑上线 | C | A/R | I | C | I | C |
| Offer 发放与核销链路上线 | C | I | A/R | I | C | C |
| 触达策略与频控上线 | A/R | C | I | C | I | C |
| ROI 报表与归因口径发布 | C | I | I | I | A/R | C |
| 大促值守与风险处置决策 | A/R | R | R | R | R | C |

注：`R` 负责执行（Responsible），`A` 最终负责（Accountable），`C` 咨询协作（Consulted），`I` 被通知（Informed）。

## 9. 附录：技术选型速查

### 9.1 规则引擎速查

| 方案 | 表达能力 | 热更新与版本管理 | 运行性能 | 营销场景建议 |
| :--- | :--- | :--- | :--- | :--- |
| Drools | 高（复杂条件+CEP） | 强 | 高 | 大规模复杂规则，需强治理 |
| LiteFlow | 中高（流程编排） | 中高 | 中高 | 运营流程编排优先 |
| Grule | 中高（Go 生态） | 中 | 高 | Go 技术栈的高并发服务 |
| 自研引擎 | 可定制 | 可定制 | 极高 | 规则量与并发都极高时 |

### 9.2 消息队列速查

| 方案 | 顺序/事务能力 | 延迟任务能力 | 扩展性 | 营销场景建议 |
| :--- | :--- | :--- | :--- | :--- |
| RocketMQ | 强（含事务消息） | 中高 | 高 | 实时触发、发券链路 |
| Kafka | 中（分区有序） | 低 | 极高 | 行为日志与离线分析 |
| RabbitMQ | 中 | 高 | 中 | 触达延迟任务、复杂路由 |
| Pulsar | 中高 | 高 | 高 | 多租户与跨域场景 |

### 9.3 存储速查

| 场景 | 推荐方案 | 关键能力 | 营销决策相关关注点 |
| :--- | :--- | :--- | :--- |
| 用户画像点查 | HBase | 大规模 KV 点查 | 低延迟读取与行级更新 |
| 人群圈选 | ClickHouse + BitMap | 位图集合运算 | 组合条件计算速度与成本 |
| 规则与配置 | MySQL/TiDB | 事务一致性 | 版本管理、审批发布一致性 |
| 会话与频控状态 | Redis Cluster | 高 QPS 与丰富数据结构 | 原子计数、过期策略、热点分片 |
| 素材与模板 | OSS/MinIO | 对象存储 | 版本管理、审核链路与检索效率 |

## 结语

营销系统建设应始终遵循同一节奏：先定义目标函数和边界，再统一对象模型，随后打通决策执行闭环，最后逐步引入智能化。技术选型的价值不在于“先进”，而在于是否稳定支撑 `BudgetCap/FreqCap/Eligibility/Exclusion/Priority/Quota` 这些核心业务约束，并持续提升可解释的增量收益。
