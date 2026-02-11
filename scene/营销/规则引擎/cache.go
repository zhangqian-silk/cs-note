package main

import "sync"

type RuleCache struct {
	mu    sync.RWMutex
	rules map[string]Rule
}

func NewRuleCache() *RuleCache {
	return &RuleCache{rules: map[string]Rule{}}
}

func (c *RuleCache) RegisterRules(rules []Rule) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, rule := range rules {
		c.rules[rule.RuleID] = rule
	}
}

func RegisterDefaultRules(cache *RuleCache) {
	cache.RegisterRules(LoadRules())
}

func (c *RuleCache) GetAll() []Rule {
	c.mu.RLock()
	defer c.mu.RUnlock()
	results := make([]Rule, 0, len(c.rules))
	for _, rule := range c.rules {
		results = append(results, rule)
	}
	return results
}

func (c *RuleCache) Get(ruleID string) (Rule, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	rule, ok := c.rules[ruleID]
	return rule, ok
}
