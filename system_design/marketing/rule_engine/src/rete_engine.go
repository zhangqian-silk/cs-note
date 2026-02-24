package main

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"
)

type ReteEngine struct {
	rules      []reteRule
	alphaNodes map[string]reteAlphaNode
}

type reteRule struct {
	meta Rule
	expr reteExpr
}

type reteAlphaNode struct {
	key       string
	evaluator func(*Fact) (bool, error)
}

type reteContext struct {
	alphaCache map[string]reteAlphaResult
	alphaNodes map[string]reteAlphaNode
}

type reteAlphaResult struct {
	value bool
	err   error
	ok    bool
}

type reteExpr interface {
	Eval(*reteContext, *Fact) (bool, error)
}

type reteConst struct {
	value bool
}

func (r reteConst) Eval(_ *reteContext, _ *Fact) (bool, error) {
	return r.value, nil
}

type reteAlphaRef struct {
	key string
}

func (r reteAlphaRef) Eval(ctx *reteContext, fact *Fact) (bool, error) {
	if ctx.alphaCache == nil {
		ctx.alphaCache = map[string]reteAlphaResult{}
	}
	if cached, ok := ctx.alphaCache[r.key]; ok && cached.ok {
		return cached.value, cached.err
	}
	alpha, ok := ctx.alphaNodes[r.key]
	if !ok {
		return false, errors.New("alpha node not found")
	}
	value, err := alpha.evaluator(fact)
	ctx.alphaCache[r.key] = reteAlphaResult{value: value, err: err, ok: true}
	return value, err
}

type reteAnd struct {
	children []reteExpr
}

func (r reteAnd) Eval(ctx *reteContext, fact *Fact) (bool, error) {
	for _, child := range r.children {
		ok, err := child.Eval(ctx, fact)
		if err != nil {
			return false, err
		}
		if !ok {
			return false, nil
		}
	}
	return true, nil
}

type reteOr struct {
	children []reteExpr
}

func (r reteOr) Eval(ctx *reteContext, fact *Fact) (bool, error) {
	for _, child := range r.children {
		ok, err := child.Eval(ctx, fact)
		if err != nil {
			return false, err
		}
		if ok {
			return true, nil
		}
	}
	return false, nil
}

type reteNot struct {
	child reteExpr
}

func (r reteNot) Eval(ctx *reteContext, fact *Fact) (bool, error) {
	ok, err := r.child.Eval(ctx, fact)
	if err != nil {
		return false, err
	}
	return !ok, nil
}

func NewReteEngine(rules []Rule) *ReteEngine {
	copied := make([]Rule, len(rules))
	copy(copied, rules)
	sort.SliceStable(copied, func(i, j int) bool {
		return copied[i].Priority > copied[j].Priority
	})
	alphaNodes := map[string]reteAlphaNode{}
	compiled := make([]reteRule, 0, len(copied))
	for _, rule := range copied {
		expr, err := buildReteExpr(rule.Condition, alphaNodes)
		if err != nil {
			continue
		}
		compiled = append(compiled, reteRule{meta: rule, expr: expr})
	}
	return &ReteEngine{rules: compiled, alphaNodes: alphaNodes}
}

func (e *ReteEngine) Evaluate(fact *Fact) ([]Result, error) {
	return e.evaluateRules(e.rules, fact)
}

func (e *ReteEngine) EvaluateParallel(fact *Fact, groupKey func(Rule) string) ([]Result, error) {
	if groupKey == nil {
		groupKey = func(rule Rule) string {
			if rule.Type == "" {
				return "default"
			}
			return rule.Type
		}
	}
	groups := map[string][]reteRule{}
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

func (e *ReteEngine) evaluateRules(rules []reteRule, fact *Fact) ([]Result, error) {
	var results []Result
	mutexHit := map[string]bool{}
	ctx := &reteContext{
		alphaCache: map[string]reteAlphaResult{},
		alphaNodes: e.alphaNodes,
	}
	for _, rule := range rules {
		if rule.meta.Status != "" && strings.ToLower(rule.meta.Status) != RuleStatusActive {
			continue
		}
		if rule.meta.MutexGroup != "" && mutexHit[rule.meta.MutexGroup] {
			continue
		}
		matched, err := rule.expr.Eval(ctx, fact)
		if err != nil {
			return nil, err
		}
		if matched {
			results = append(results, Result{RuleID: rule.meta.RuleID, Actions: rule.meta.Actions})
			if rule.meta.MutexGroup != "" {
				mutexHit[rule.meta.MutexGroup] = true
			}
		}
	}
	return results, nil
}

func buildReteExpr(condition *Condition, alphaNodes map[string]reteAlphaNode) (reteExpr, error) {
	if condition == nil {
		return reteConst{value: true}, nil
	}
	operator := strings.ToUpper(condition.Operator)
	switch operator {
	case ConditionAnd:
		if len(condition.Children) == 0 {
			return nil, errors.New("AND requires children")
		}
		children := make([]reteExpr, 0, len(condition.Children))
		for i := range condition.Children {
			child, err := buildReteExpr(&condition.Children[i], alphaNodes)
			if err != nil {
				return nil, err
			}
			children = append(children, child)
		}
		return reteAnd{children: children}, nil
	case ConditionOr:
		if len(condition.Children) == 0 {
			return nil, errors.New("OR requires children")
		}
		children := make([]reteExpr, 0, len(condition.Children))
		for i := range condition.Children {
			child, err := buildReteExpr(&condition.Children[i], alphaNodes)
			if err != nil {
				return nil, err
			}
			children = append(children, child)
		}
		return reteOr{children: children}, nil
	case "NOT":
		if len(condition.Children) != 1 {
			return nil, errors.New("NOT requires exactly one child")
		}
		child, err := buildReteExpr(&condition.Children[0], alphaNodes)
		if err != nil {
			return nil, err
		}
		return reteNot{child: child}, nil
	default:
		if condition.Field == "" {
			return nil, errors.New("leaf condition requires field")
		}
		key, err := alphaKey(condition)
		if err != nil {
			return nil, err
		}
		if _, ok := alphaNodes[key]; !ok {
			evaluator, err := CompileCondition(condition)
			if err != nil {
				return nil, err
			}
			alphaNodes[key] = reteAlphaNode{key: key, evaluator: evaluator}
		}
		return reteAlphaRef{key: key}, nil
	}
}

func alphaKey(condition *Condition) (string, error) {
	raw, err := json.Marshal(condition.Value)
	if err != nil {
		return "", err
	}
	return condition.Field + "|" + strings.ToLower(condition.Operator) + "|" + string(raw), nil
}
