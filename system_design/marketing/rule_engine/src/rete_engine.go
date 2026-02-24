package main

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"
)

// ReteEngine 负责构建网络并对外提供规则评估入口
type ReteEngine struct {
	rules []Rule
}

// NewReteEngine 预排序规则，确保优先级语义与 Engine 一致
func NewReteEngine(rules []Rule) *ReteEngine {
	copied := make([]Rule, len(rules))
	copy(copied, rules)
	sort.SliceStable(copied, func(i, j int) bool {
		return copied[i].Priority > copied[j].Priority
	})
	return &ReteEngine{rules: copied}
}

// Evaluate 构建会话并插入单个事实完成评估
func (e *ReteEngine) Evaluate(fact *Fact) ([]Result, error) {
	session, err := newReteSession(e.rules)
	if err != nil {
		return nil, err
	}
	factID, err := session.InsertFact(fact)
	if err != nil {
		return nil, err
	}
	return session.ResultsForFact(factID), nil
}

// EvaluateParallel 按规则分组并行评估，每组独立会话避免状态冲突
func (e *ReteEngine) EvaluateParallel(fact *Fact, groupKey func(Rule) string) ([]Result, error) {
	if groupKey == nil {
		groupKey = func(rule Rule) string {
			if rule.Type == "" {
				return "default"
			}
			return rule.Type
		}
	}
	groups := map[string][]Rule{}
	for _, rule := range e.rules {
		key := groupKey(rule)
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
			engine := NewReteEngine(groupRules)
			groupResults, err := engine.Evaluate(groupFact)
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

// reteToken 表示事实在网络中的标识与载体
type reteToken struct {
	id   int
	fact *Fact
}

// reteOutput 接收来自上游节点的增量事件
type reteOutput interface {
	OnInsert(*reteToken)
	OnRetract(*reteToken)
}

// reteProducer 代表可向下游挂载输出的节点
type reteProducer interface {
	AddOutput(reteOutput)
}

// reteAlphaNode 负责单条件过滤与记忆匹配结果
type reteAlphaNode struct {
	key       string
	evaluator func(*Fact) (bool, error)
	memory    map[int]*reteToken
	outputs   []reteOutput
}

func (n *reteAlphaNode) AddOutput(output reteOutput) {
	n.outputs = append(n.outputs, output)
}

func (n *reteAlphaNode) OnFactInserted(token *reteToken) error {
	matched, err := n.evaluator(token.fact)
	if err != nil || !matched {
		return err
	}
	if n.memory == nil {
		n.memory = map[int]*reteToken{}
	}
	n.memory[token.id] = token
	for _, output := range n.outputs {
		output.OnInsert(token)
	}
	return nil
}

func (n *reteAlphaNode) OnFactRemoved(token *reteToken) {
	if n.memory == nil {
		return
	}
	if _, ok := n.memory[token.id]; !ok {
		return
	}
	delete(n.memory, token.id)
	for _, output := range n.outputs {
		output.OnRetract(token)
	}
}

// reteTrueNode 用于恒真条件的入口节点
type reteTrueNode struct {
	outputs []reteOutput
}

func (n *reteTrueNode) AddOutput(output reteOutput) {
	n.outputs = append(n.outputs, output)
}

func (n *reteTrueNode) OnFactInserted(token *reteToken) {
	for _, output := range n.outputs {
		output.OnInsert(token)
	}
}

func (n *reteTrueNode) OnFactRemoved(token *reteToken) {
	for _, output := range n.outputs {
		output.OnRetract(token)
	}
}

// reteBetaNode 实现 AND/OR 的增量合并与记忆
type reteBetaNode struct {
	op        string
	leftMem   map[int]*reteToken
	rightMem  map[int]*reteToken
	resultMem map[int]*reteToken
	outputs   []reteOutput
}

type reteJoinInput struct {
	node *reteBetaNode
	side string
}

func (i reteJoinInput) OnInsert(token *reteToken) {
	if i.side == "left" {
		i.node.insertLeft(token)
		return
	}
	i.node.insertRight(token)
}

func (i reteJoinInput) OnRetract(token *reteToken) {
	if i.side == "left" {
		i.node.retractLeft(token)
		return
	}
	i.node.retractRight(token)
}

func (n *reteBetaNode) AddOutput(output reteOutput) {
	n.outputs = append(n.outputs, output)
}

func (n *reteBetaNode) leftInput() reteOutput {
	return reteJoinInput{node: n, side: "left"}
}

func (n *reteBetaNode) rightInput() reteOutput {
	return reteJoinInput{node: n, side: "right"}
}

func (n *reteBetaNode) ensure() {
	if n.leftMem == nil {
		n.leftMem = map[int]*reteToken{}
	}
	if n.rightMem == nil {
		n.rightMem = map[int]*reteToken{}
	}
	if n.resultMem == nil {
		n.resultMem = map[int]*reteToken{}
	}
}

func (n *reteBetaNode) insertLeft(token *reteToken) {
	n.ensure()
	n.leftMem[token.id] = token
	if n.op == ConditionAnd {
		if _, ok := n.rightMem[token.id]; ok {
			n.emitInsert(token)
		}
		return
	}
	if n.op == ConditionOr {
		n.emitInsert(token)
	}
}

func (n *reteBetaNode) insertRight(token *reteToken) {
	n.ensure()
	n.rightMem[token.id] = token
	if n.op == ConditionAnd {
		if _, ok := n.leftMem[token.id]; ok {
			n.emitInsert(token)
		}
		return
	}
	if n.op == ConditionOr {
		n.emitInsert(token)
	}
}

func (n *reteBetaNode) retractLeft(token *reteToken) {
	if n.leftMem == nil {
		return
	}
	delete(n.leftMem, token.id)
	if n.op == ConditionAnd {
		if _, ok := n.rightMem[token.id]; ok {
			n.emitRetract(token)
		}
		return
	}
	if n.op == ConditionOr {
		if _, ok := n.rightMem[token.id]; ok {
			return
		}
		n.emitRetract(token)
	}
}

func (n *reteBetaNode) retractRight(token *reteToken) {
	if n.rightMem == nil {
		return
	}
	delete(n.rightMem, token.id)
	if n.op == ConditionAnd {
		if _, ok := n.leftMem[token.id]; ok {
			n.emitRetract(token)
		}
		return
	}
	if n.op == ConditionOr {
		if _, ok := n.leftMem[token.id]; ok {
			return
		}
		n.emitRetract(token)
	}
}

func (n *reteBetaNode) emitInsert(token *reteToken) {
	if n.resultMem == nil {
		n.resultMem = map[int]*reteToken{}
	}
	if _, ok := n.resultMem[token.id]; ok {
		return
	}
	n.resultMem[token.id] = token
	for _, output := range n.outputs {
		output.OnInsert(token)
	}
}

func (n *reteBetaNode) emitRetract(token *reteToken) {
	if n.resultMem == nil {
		return
	}
	if _, ok := n.resultMem[token.id]; !ok {
		return
	}
	delete(n.resultMem, token.id)
	for _, output := range n.outputs {
		output.OnRetract(token)
	}
}

// reteNotNode 负责 NOT 条件的增量维护
type reteNotNode struct {
	childMem  map[int]*reteToken
	resultMem map[int]*reteToken
	outputs   []reteOutput
	factExist func(int) bool
}

func (n *reteNotNode) AddOutput(output reteOutput) {
	n.outputs = append(n.outputs, output)
}

func (n *reteNotNode) OnInsert(token *reteToken) {
	if n.childMem == nil {
		n.childMem = map[int]*reteToken{}
	}
	n.childMem[token.id] = token
	if _, ok := n.resultMem[token.id]; ok {
		n.emitRetract(token)
	}
}

func (n *reteNotNode) OnRetract(token *reteToken) {
	if n.childMem != nil {
		delete(n.childMem, token.id)
	}
	if n.factExist != nil && !n.factExist(token.id) {
		return
	}
	if n.resultMem == nil {
		n.resultMem = map[int]*reteToken{}
	}
	if _, ok := n.resultMem[token.id]; ok {
		return
	}
	n.emitInsert(token)
}

func (n *reteNotNode) OnFactInserted(token *reteToken) {
	if n.childMem != nil {
		if _, ok := n.childMem[token.id]; ok {
			return
		}
	}
	if n.resultMem == nil {
		n.resultMem = map[int]*reteToken{}
	}
	if _, ok := n.resultMem[token.id]; ok {
		return
	}
	n.emitInsert(token)
}

func (n *reteNotNode) OnFactRemoved(token *reteToken) {
	if n.resultMem == nil {
		return
	}
	if _, ok := n.resultMem[token.id]; !ok {
		return
	}
	n.emitRetract(token)
}

func (n *reteNotNode) emitInsert(token *reteToken) {
	if n.resultMem == nil {
		n.resultMem = map[int]*reteToken{}
	}
	n.resultMem[token.id] = token
	for _, output := range n.outputs {
		output.OnInsert(token)
	}
}

func (n *reteNotNode) emitRetract(token *reteToken) {
	if n.resultMem == nil {
		return
	}
	if _, ok := n.resultMem[token.id]; !ok {
		return
	}
	delete(n.resultMem, token.id)
	for _, output := range n.outputs {
		output.OnRetract(token)
	}
}

// reteTerminal 表示规则的终结节点，用于写入议程
type reteTerminal struct {
	rule        Rule
	session     *reteSession
	activations map[int]struct{}
}

func (t *reteTerminal) OnInsert(token *reteToken) {
	if t.activations == nil {
		t.activations = map[int]struct{}{}
	}
	if _, ok := t.activations[token.id]; ok {
		return
	}
	t.activations[token.id] = struct{}{}
	t.session.addActivation(t.rule.RuleID, token.id)
}

func (t *reteTerminal) OnRetract(token *reteToken) {
	if t.activations == nil {
		return
	}
	if _, ok := t.activations[token.id]; !ok {
		return
	}
	delete(t.activations, token.id)
	t.session.removeActivation(t.rule.RuleID, token.id)
}

// reteSession 持有网络状态、事实表与规则激活议程
type reteSession struct {
	facts      map[int]*Fact
	nextID     int
	agenda     map[string]map[int]struct{}
	ruleByID   map[string]Rule
	ruleOrder  []Rule
	alphaNodes []*reteAlphaNode
	notNodes   []*reteNotNode
	trueNode   *reteTrueNode
}

// newReteSession 构建网络并准备会话状态
func newReteSession(rules []Rule) (*reteSession, error) {
	session := &reteSession{
		facts:    map[int]*Fact{},
		agenda:   map[string]map[int]struct{}{},
		ruleByID: map[string]Rule{},
	}
	builder := reteBuilder{
		alphaNodes: map[string]*reteAlphaNode{},
	}
	for _, rule := range rules {
		if rule.Status != "" && strings.ToLower(rule.Status) != RuleStatusActive {
			continue
		}
		session.ruleByID[rule.RuleID] = rule
		session.ruleOrder = append(session.ruleOrder, rule)
		root, err := builder.buildExpr(rule.Condition)
		if err != nil {
			continue
		}
		terminal := &reteTerminal{rule: rule, session: session}
		root.AddOutput(terminal)
	}
	session.trueNode = builder.trueNode
	session.notNodes = builder.notNodes
	session.alphaNodes = make([]*reteAlphaNode, 0, len(builder.alphaNodes))
	for _, alpha := range builder.alphaNodes {
		session.alphaNodes = append(session.alphaNodes, alpha)
	}
	for _, notNode := range session.notNodes {
		notNode.factExist = session.factExists
	}
	return session, nil
}

// InsertFact 插入事实并触发增量传播
func (s *reteSession) InsertFact(fact *Fact) (int, error) {
	id := s.nextID
	s.nextID++
	token := &reteToken{id: id, fact: fact}
	s.facts[id] = fact
	if s.trueNode != nil {
		s.trueNode.OnFactInserted(token)
	}
	for _, alpha := range s.alphaNodes {
		if err := alpha.OnFactInserted(token); err != nil {
			return id, err
		}
	}
	for _, notNode := range s.notNodes {
		notNode.OnFactInserted(token)
	}
	return id, nil
}

// UpdateFact 先撤回旧事实再插入新事实
func (s *reteSession) UpdateFact(id int, fact *Fact) error {
	if _, ok := s.facts[id]; !ok {
		return errors.New("fact not found")
	}
	s.RemoveFact(id)
	token := &reteToken{id: id, fact: fact}
	s.facts[id] = fact
	if s.trueNode != nil {
		s.trueNode.OnFactInserted(token)
	}
	for _, alpha := range s.alphaNodes {
		if err := alpha.OnFactInserted(token); err != nil {
			return err
		}
	}
	for _, notNode := range s.notNodes {
		notNode.OnFactInserted(token)
	}
	return nil
}

// RemoveFact 撤回事实并清理相关激活
func (s *reteSession) RemoveFact(id int) {
	fact, ok := s.facts[id]
	if !ok {
		return
	}
	token := &reteToken{id: id, fact: fact}
	if s.trueNode != nil {
		s.trueNode.OnFactRemoved(token)
	}
	for _, notNode := range s.notNodes {
		notNode.OnFactRemoved(token)
	}
	for _, alpha := range s.alphaNodes {
		alpha.OnFactRemoved(token)
	}
	delete(s.facts, id)
	s.clearActivations(id)
}

// ResultsForFact 按规则顺序输出命中结果并处理互斥组
func (s *reteSession) ResultsForFact(id int) []Result {
	results := []Result{}
	mutexHit := map[string]bool{}
	for _, rule := range s.ruleOrder {
		ids, ok := s.agenda[rule.RuleID]
		if !ok {
			continue
		}
		if _, ok := ids[id]; !ok {
			continue
		}
		if rule.MutexGroup != "" && mutexHit[rule.MutexGroup] {
			continue
		}
		results = append(results, Result{RuleID: rule.RuleID, Actions: rule.Actions})
		if rule.MutexGroup != "" {
			mutexHit[rule.MutexGroup] = true
		}
	}
	return results
}

func (s *reteSession) addActivation(ruleID string, factID int) {
	set, ok := s.agenda[ruleID]
	if !ok {
		set = map[int]struct{}{}
		s.agenda[ruleID] = set
	}
	set[factID] = struct{}{}
}

func (s *reteSession) removeActivation(ruleID string, factID int) {
	set, ok := s.agenda[ruleID]
	if !ok {
		return
	}
	delete(set, factID)
	if len(set) == 0 {
		delete(s.agenda, ruleID)
	}
}

func (s *reteSession) clearActivations(factID int) {
	for ruleID, set := range s.agenda {
		delete(set, factID)
		if len(set) == 0 {
			delete(s.agenda, ruleID)
		}
	}
}

func (s *reteSession) factExists(id int) bool {
	_, ok := s.facts[id]
	return ok
}

// reteBuilder 将条件树编译为 Rete 网络
type reteBuilder struct {
	alphaNodes map[string]*reteAlphaNode
	notNodes   []*reteNotNode
	trueNode   *reteTrueNode
}

// buildExpr 递归构建 Alpha/Beta/Not/True 节点并建立连接
func (b *reteBuilder) buildExpr(condition *Condition) (reteProducer, error) {
	if condition == nil {
		if b.trueNode == nil {
			b.trueNode = &reteTrueNode{}
		}
		return b.trueNode, nil
	}
	operator := strings.ToUpper(condition.Operator)
	switch operator {
	case ConditionAnd:
		if len(condition.Children) == 0 {
			return nil, errors.New("AND requires children")
		}
		var current reteProducer
		for i := range condition.Children {
			child, err := b.buildExpr(&condition.Children[i])
			if err != nil {
				return nil, err
			}
			if current == nil {
				current = child
				continue
			}
			node := &reteBetaNode{op: ConditionAnd}
			current.AddOutput(node.leftInput())
			child.AddOutput(node.rightInput())
			current = node
		}
		return current, nil
	case ConditionOr:
		if len(condition.Children) == 0 {
			return nil, errors.New("OR requires children")
		}
		var current reteProducer
		for i := range condition.Children {
			child, err := b.buildExpr(&condition.Children[i])
			if err != nil {
				return nil, err
			}
			if current == nil {
				current = child
				continue
			}
			node := &reteBetaNode{op: ConditionOr}
			current.AddOutput(node.leftInput())
			child.AddOutput(node.rightInput())
			current = node
		}
		return current, nil
	case "NOT":
		if len(condition.Children) != 1 {
			return nil, errors.New("NOT requires exactly one child")
		}
		child, err := b.buildExpr(&condition.Children[0])
		if err != nil {
			return nil, err
		}
		node := &reteNotNode{}
		child.AddOutput(node)
		b.notNodes = append(b.notNodes, node)
		return node, nil
	default:
		if condition.Field == "" {
			return nil, errors.New("leaf condition requires field")
		}
		key, err := alphaKey(condition)
		if err != nil {
			return nil, err
		}
		alpha, ok := b.alphaNodes[key]
		if !ok {
			evaluator, err := CompileCondition(condition)
			if err != nil {
				return nil, err
			}
			alpha = &reteAlphaNode{key: key, evaluator: evaluator}
			b.alphaNodes[key] = alpha
		}
		return alpha, nil
	}
}

// alphaKey 用于对等价叶子条件进行去重
func alphaKey(condition *Condition) (string, error) {
	raw, err := json.Marshal(condition.Value)
	if err != nil {
		return "", err
	}
	return condition.Field + "|" + strings.ToLower(condition.Operator) + "|" + string(raw), nil
}
