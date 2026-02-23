# 规则引擎示例

本目录提供一个面向营销场景的轻量级规则引擎实现，包含规则定义、条件编译与执行、事实数据 (Fact) 管理、互斥组与并行评估等能力。

如需更完整的行业背景与设计方案，可参考上层文档 [rule_engine.md](file:///d:/GitHubRepositories/cs-note/system_design/marketing/rule_engine/rule_engine.md)。

## 核心特性

- 规则优先级排序与互斥组控制
- 条件树编译与执行 (AND/OR/NOT + 多种比较操作符)
- Fact 支持路径访问与懒加载
- 规则分组并行评估，避免共享状态污染

## 目录结构

- engine.go：规则编译与执行引擎
- model.go：数据模型与 Fact 实现
- rule.go：示例规则集
- main.go：示例入口
- utils.go：通用比较与集合判断工具
- constants.go：领域枚举与常量
- cache.go：规则缓存

## 快速开始

在当前目录执行：

```bash
go run .
```

输出为规则命中的动作列表 (JSON)。

## 规则数据结构

规则由 Rule + Condition + Action 组成：

```json
{
	"rule_id": "RULE_XXX",
	"rule_name": "示例规则",
	"type": "pricing",
	"priority": 80,
	"mutex_group": "new_user_promo",
	"status": "active",
	"condition": {
		"operator": "AND",
		"children": [
			{ "field": "user.city", "operator": "in", "value": ["北京", "上海"] },
			{ "field": "cart.total_amount", "operator": "gte", "value": 300 }
		]
	},
	"actions": [
		{ "type": "benefit_send", "params": { "benefit_type": "coupon" } }
	]
}
```

## 条件操作符

- 逻辑操作符：AND / OR / NOT
- 比较操作符：eq / ne / gt / gte / lt / lte / in / contains / bitmask_all

## Fact 与懒加载

Fact 通过路径访问字段，例如 user.city。若某路径未在 data 中找到，可为该路径注册 loader，在首次访问时动态加载并缓存到 Fact 中。

```go
fact := NewFact(map[string]interface{}{})
fact.SetLoader("risk.user_blacklist", func() (interface{}, error) {
	return false, nil
})
```

## 并行评估

引擎支持按规则类型或自定义分组并行评估，组内共享独立的 Fact 副本，避免并发写冲突。

```go
results, err := engine.EvaluateParallel(fact, func(rule Rule) string {
	return rule.Type
})
```
