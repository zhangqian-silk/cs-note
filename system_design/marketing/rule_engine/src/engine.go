package main

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Engine 管理规则编译与执行
type Engine struct {
	// 已编译规则集合，按优先级降序排列
	rules []compiledRule
}

type compiledRule struct {
	// 规则元数据
	meta      Rule
	// 条件执行器：输入事实，输出是否命中
	evaluator func(*Fact) (bool, error)
}

func NewEngine(rules []Rule) *Engine {
	// 复制规则，避免外部修改影响引擎内部状态
	copied := make([]Rule, len(rules))
	copy(copied, rules)
	// 按优先级降序排序，确保高优先级先执行
	sort.SliceStable(copied, func(i, j int) bool {
		return copied[i].Priority > copied[j].Priority
	})
	compiled := make([]compiledRule, 0, len(copied))
	for _, rule := range copied {
		// 预编译条件表达式为可执行函数
		eval, err := CompileCondition(rule.Condition)
		if err != nil {
			// 编译失败的规则直接跳过
			continue
		}
		compiled = append(compiled, compiledRule{meta: rule, evaluator: eval})
	}
	return &Engine{rules: compiled}
}

// Evaluate 顺序执行所有规则并返回命中结果
func (e *Engine) Evaluate(fact *Fact) ([]Result, error) {
	return e.evaluateRules(e.rules, fact)
}

// EvaluateParallel 按分组并行评估规则，组内使用独立 Fact 副本
func (e *Engine) EvaluateParallel(fact *Fact, groupKey func(Rule) string) ([]Result, error) {
	if groupKey == nil {
		groupKey = func(rule Rule) string {
			if rule.Type == "" {
				return "default"
			}
			return rule.Type
		}
	}
	groups := map[string][]compiledRule{}
	for _, rule := range e.rules {
		key := groupKey(rule.meta)
		groups[key] = append(groups[key], rule)
	}
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		results  []Result
		firstErr error
	)
	for _, rules := range groups {
		groupRules := rules
		wg.Add(1)
		go func() {
			defer wg.Done()
			groupFact := fact.Clone()
			groupResults, err := e.evaluateRules(groupRules, groupFact)
			mu.Lock()
			defer mu.Unlock()
			if err != nil && firstErr == nil {
				firstErr = err
				return
			}
			results = append(results, groupResults...)
		}()
	}
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	return results, nil
}

func (e *Engine) evaluateRules(rules []compiledRule, fact *Fact) ([]Result, error) {
	// 逐条执行规则并汇总命中结果
	var results []Result
	// 互斥组命中记录：同一组只能命中一次
	mutexHit := map[string]bool{}
	for _, rule := range rules {
		// 非激活规则直接跳过
		if rule.meta.Status != "" && strings.ToLower(rule.meta.Status) != "active" {
			continue
		}
		// 互斥组已命中则跳过
		if rule.meta.MutexGroup != "" && mutexHit[rule.meta.MutexGroup] {
			continue
		}
		matched, err := rule.evaluator(fact)
		if err != nil {
			return nil, err
		}
		if matched {
			// 记录命中结果
			results = append(results, Result{RuleID: rule.meta.RuleID, Actions: rule.meta.Actions})
			if rule.meta.MutexGroup != "" {
				// 标记互斥组命中
				mutexHit[rule.meta.MutexGroup] = true
			}
		}
	}
	return results, nil
}

func EvaluateCondition(condition *Condition, fact *Fact) (bool, error) {
	// 递归解释执行条件树
	if condition == nil {
		return true, nil
	}
	op := strings.ToUpper(condition.Operator)
	switch op {
	case "AND":
		if len(condition.Children) == 0 {
			return false, errors.New("AND requires children")
		}
		for i := range condition.Children {
			ok, err := EvaluateCondition(&condition.Children[i], fact)
			if err != nil {
				return false, err
			}
			if !ok {
				return false, nil
			}
		}
		return true, nil
	case "OR":
		if len(condition.Children) == 0 {
			return false, errors.New("OR requires children")
		}
		for i := range condition.Children {
			ok, err := EvaluateCondition(&condition.Children[i], fact)
			if err != nil {
				return false, err
			}
			if ok {
				return true, nil
			}
		}
		return false, nil
	case "NOT":
		if len(condition.Children) != 1 {
			return false, errors.New("NOT requires exactly one child")
		}
		ok, err := EvaluateCondition(&condition.Children[0], fact)
		if err != nil {
			return false, err
		}
		return !ok, nil
	default:
		// 叶子节点条件
		return evaluateLeaf(condition, fact)
	}
}

func CompileCondition(condition *Condition) (func(*Fact) (bool, error), error) {
	// 将条件树编译为可执行函数，减少运行期开销
	if condition == nil {
		return func(*Fact) (bool, error) { return true, nil }, nil
	}
	op := strings.ToUpper(condition.Operator)
	switch op {
	case "AND":
		if len(condition.Children) == 0 {
			return nil, errors.New("AND requires children")
		}
		children := make([]func(*Fact) (bool, error), 0, len(condition.Children))
		for i := range condition.Children {
			fn, err := CompileCondition(&condition.Children[i])
			if err != nil {
				return nil, err
			}
			children = append(children, fn)
		}
		return func(fact *Fact) (bool, error) {
			for _, fn := range children {
				ok, err := fn(fact)
				if err != nil {
					return false, err
				}
				if !ok {
					return false, nil
				}
			}
			return true, nil
		}, nil
	case "OR":
		if len(condition.Children) == 0 {
			return nil, errors.New("OR requires children")
		}
		children := make([]func(*Fact) (bool, error), 0, len(condition.Children))
		for i := range condition.Children {
			fn, err := CompileCondition(&condition.Children[i])
			if err != nil {
				return nil, err
			}
			children = append(children, fn)
		}
		return func(fact *Fact) (bool, error) {
			for _, fn := range children {
				ok, err := fn(fact)
				if err != nil {
					return false, err
				}
				if ok {
					return true, nil
				}
			}
			return false, nil
		}, nil
	case "NOT":
		if len(condition.Children) != 1 {
			return nil, errors.New("NOT requires exactly one child")
		}
		child, err := CompileCondition(&condition.Children[0])
		if err != nil {
			return nil, err
		}
		return func(fact *Fact) (bool, error) {
			ok, err := child(fact)
			if err != nil {
				return false, err
			}
			return !ok, nil
		}, nil
	default:
		// 叶子节点条件编译
		return compileLeaf(condition)
	}
}

func compileLeaf(condition *Condition) (func(*Fact) (bool, error), error) {
	// 编译单条比较条件为函数
	if condition.Field == "" {
		return nil, errors.New("leaf condition requires field")
	}
	field := condition.Field
	operator := strings.ToLower(condition.Operator)
	value := condition.Value
	return func(fact *Fact) (bool, error) {
		left, ok, err := getByPath(fact, field)
		if err != nil {
			return false, err
		}
		if !ok {
			return false, nil
		}
		right, err := resolveValue(value, fact)
		if err != nil {
			return false, err
		}
		switch operator {
		case "eq":
			return isEqual(left, right), nil
		case "ne":
			return !isEqual(left, right), nil
		case "gt":
			return compareNumber(left, right, func(a, b float64) bool { return a > b })
		case "gte":
			return compareNumber(left, right, func(a, b float64) bool { return a >= b })
		case "lt":
			return compareNumber(left, right, func(a, b float64) bool { return a < b })
		case "lte":
			return compareNumber(left, right, func(a, b float64) bool { return a <= b })
		case "in":
			return isIn(left, right)
		case "contains":
			return contains(left, right)
		case "bitmask_all":
			return bitmaskAll(left, right)
		default:
			return false, fmt.Errorf("unsupported operator: %s", operator)
		}
	}, nil
}

func evaluateLeaf(condition *Condition, fact *Fact) (bool, error) {
	// 解释执行单条比较条件
	if condition.Field == "" {
		return false, errors.New("leaf condition requires field")
	}
	left, ok, err := getByPath(fact, condition.Field)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil
	}
	right, err := resolveValue(condition.Value, fact)
	if err != nil {
		return false, err
	}
	switch strings.ToLower(condition.Operator) {
	case "eq":
		return isEqual(left, right), nil
	case "ne":
		return !isEqual(left, right), nil
	case "gt":
		return compareNumber(left, right, func(a, b float64) bool { return a > b })
	case "gte":
		return compareNumber(left, right, func(a, b float64) bool { return a >= b })
	case "lt":
		return compareNumber(left, right, func(a, b float64) bool { return a < b })
	case "lte":
		return compareNumber(left, right, func(a, b float64) bool { return a <= b })
	case "in":
		return isIn(left, right)
	case "contains":
		return contains(left, right)
	case "bitmask_all":
		return bitmaskAll(left, right)
	default:
		return false, fmt.Errorf("unsupported operator: %s", condition.Operator)
	}
}

func resolveValue(value interface{}, fact *Fact) (interface{}, error) {
	// 支持 {"var": "path"} 形式的动态取值
	m, ok := value.(map[string]interface{})
	if !ok {
		return value, nil
	}
	ref, ok := m["var"].(string)
	if !ok || ref == "" {
		return value, nil
	}
	v, ok, err := getByPath(fact, ref)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("variable not found: %s", ref)
	}
	return v, nil
}

func getByPath(fact *Fact, path string) (interface{}, bool, error) {
	// 通过路径读取事实中的字段
	return fact.GetPath(path)
}

type PipelineContext struct {
	Result  bool
	Reason  string
	Fact    *Fact
	Results []Result
	Data    map[string]interface{}
}

type PipelineHandler interface {
	Name() string
	Handle(*PipelineContext) error
}

type Pipeline struct {
	handlers []PipelineHandler
}

func NewPipeline(handlers ...PipelineHandler) *Pipeline {
	return &Pipeline{handlers: handlers}
}

func (p *Pipeline) Execute(ctx *PipelineContext) error {
	if ctx == nil {
		return errors.New("pipeline context is nil")
	}
	if !ctx.Result {
		return nil
	}
	for _, handler := range p.handlers {
		err := handler.Handle(ctx)
		if err != nil {
			ctx.Result = false
			if ctx.Reason == "" {
				ctx.Reason = err.Error()
			}
			return err
		}
		if !ctx.Result {
			if ctx.Reason == "" {
				ctx.Reason = handler.Name()
			}
			return nil
		}
	}
	return nil
}
