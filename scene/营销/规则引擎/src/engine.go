package main

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strings"
	"sync"
)

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

func (e *Engine) Evaluate(fact *Fact) ([]Result, error) {
	return e.evaluateRules(e.rules, fact)
}

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

func compareNumber(left, right interface{}, cmp func(a, b float64) bool) (bool, error) {
	// 数值比较的统一入口
	lf, ok := toFloat(left)
	if !ok {
		return false, errors.New("left is not number")
	}
	rf, ok := toFloat(right)
	if !ok {
		return false, errors.New("right is not number")
	}
	return cmp(lf, rf), nil
}

func toFloat(v interface{}) (float64, bool) {
	// 支持多种数值类型的归一化
	switch t := v.(type) {
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	case float64:
		return t, true
	case float32:
		return float64(t), true
	case jsonNumber:
		f, err := t.Float64()
		if err != nil {
			return 0, false
		}
		return f, true
	default:
		return 0, false
	}
}

type jsonNumber interface {
	// 兼容 json.Number 的抽象接口
	Float64() (float64, error)
}

func toUint64(v interface{}) (uint64, bool) {
	switch t := v.(type) {
	case int:
		if t < 0 {
			return 0, false
		}
		return uint64(t), true
	case int8:
		if t < 0 {
			return 0, false
		}
		return uint64(t), true
	case int16:
		if t < 0 {
			return 0, false
		}
		return uint64(t), true
	case int32:
		if t < 0 {
			return 0, false
		}
		return uint64(t), true
	case int64:
		if t < 0 {
			return 0, false
		}
		return uint64(t), true
	case uint:
		return uint64(t), true
	case uint8:
		return uint64(t), true
	case uint16:
		return uint64(t), true
	case uint32:
		return uint64(t), true
	case uint64:
		return t, true
	case float64:
		if t < 0 || math.Trunc(t) != t {
			return 0, false
		}
		return uint64(t), true
	case float32:
		if t < 0 || math.Trunc(float64(t)) != float64(t) {
			return 0, false
		}
		return uint64(t), true
	case jsonNumber:
		f, err := t.Float64()
		if err != nil {
			return 0, false
		}
		if f < 0 || math.Trunc(f) != f {
			return 0, false
		}
		return uint64(f), true
	default:
		return 0, false
	}
}

func isEqual(left, right interface{}) bool {
	// 先归一化数值，再进行深度比较
	return reflect.DeepEqual(normalizeNumber(left), normalizeNumber(right))
}

func normalizeNumber(v interface{}) interface{} {
	// 将数值统一转换为 float64，避免类型差异导致不等
	if f, ok := toFloat(v); ok {
		return f
	}
	return v
}

func bitmaskAll(left, right interface{}) (bool, error) {
	lv, ok := toUint64(left)
	if !ok {
		return false, errors.New("left is not integer for bitmask_all")
	}
	rv, ok := toUint64(right)
	if !ok {
		return false, errors.New("right is not integer for bitmask_all")
	}
	return (lv & rv) == rv, nil
}

func isIn(left, right interface{}) (bool, error) {
	// 判断 left 是否存在于列表 right 中
	rv := reflect.ValueOf(right)
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return false, errors.New("right is not list for in")
	}
	for i := 0; i < rv.Len(); i++ {
		if isEqual(left, rv.Index(i).Interface()) {
			return true, nil
		}
	}
	return false, nil
}

func contains(left, right interface{}) (bool, error) {
	// 支持字符串包含与数组包含两种语义
	switch l := left.(type) {
	case string:
		r, ok := right.(string)
		if !ok {
			return false, errors.New("right is not string for contains")
		}
		return strings.Contains(l, r), nil
	default:
		rv := reflect.ValueOf(left)
		if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
			return false, errors.New("left is not list for contains")
		}
		for i := 0; i < rv.Len(); i++ {
			if isEqual(rv.Index(i).Interface(), right) {
				return true, nil
			}
		}
		return false, nil
	}
}
