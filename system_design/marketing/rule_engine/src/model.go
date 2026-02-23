package main

import (
	"encoding/json"
	"strings"
)

type Fact struct {
	data    map[string]interface{}
	loaders map[string]func() (interface{}, error)
	loaded  map[string]bool
}

func NewFact(data map[string]interface{}) *Fact {
	if data == nil {
		data = map[string]interface{}{}
	}
	return &Fact{
		data:    data,
		loaders: map[string]func() (interface{}, error){},
		loaded:  map[string]bool{},
	}
}

func (f *Fact) SetLoader(path string, loader func() (interface{}, error)) {
	f.loaders[path] = loader
}

func (f *Fact) Clone() *Fact {
	cloned := NewFact(deepCopyMap(f.data))
	for k, v := range f.loaders {
		cloned.loaders[k] = v
	}
	return cloned
}

func (f *Fact) GetPath(path string) (interface{}, bool, error) {
	parts := strings.Split(path, ".")
	var current interface{} = f.data
	for i, part := range parts {
		m, ok := current.(map[string]interface{})
		if !ok {
			return nil, false, nil
		}
		if val, ok := m[part]; ok {
			current = val
			continue
		}
		keyPath := strings.Join(parts[:i+1], ".")
		loaded, err := f.loadInto(keyPath, m, part)
		if err != nil {
			return nil, false, err
		}
		if loaded {
			if val, ok := m[part]; ok {
				current = val
				continue
			}
		}
		return nil, false, nil
	}
	return current, true, nil
}

func (f *Fact) loadInto(keyPath string, parent map[string]interface{}, key string) (bool, error) {
	loader, ok := f.loaders[keyPath]
	if !ok {
		return false, nil
	}
	if f.loaded[keyPath] {
		return true, nil
	}
	val, err := loader()
	if err != nil {
		return false, err
	}
	parent[key] = val
	f.loaded[keyPath] = true
	return true, nil
}

func deepCopyMap(src map[string]interface{}) map[string]interface{} {
	if src == nil {
		return map[string]interface{}{}
	}
	dst := make(map[string]interface{}, len(src))
	for k, v := range src {
		dst[k] = deepCopyValue(v)
	}
	return dst
}

func deepCopySlice(src []interface{}) []interface{} {
	if src == nil {
		return nil
	}
	dst := make([]interface{}, len(src))
	for i, v := range src {
		dst[i] = deepCopyValue(v)
	}
	return dst
}

func deepCopyValue(v interface{}) interface{} {
	switch t := v.(type) {
	case map[string]interface{}:
		return deepCopyMap(t)
	case []interface{}:
		return deepCopySlice(t)
	default:
		return v
	}
}

type Rule struct {
	RuleID      string     `json:"rule_id"`
	RuleName    string     `json:"rule_name"`
	Description string     `json:"description"`
	Type        string     `json:"type"`
	Priority    int        `json:"priority"`
	MutexGroup  string     `json:"mutex_group"`
	Status      string     `json:"status"`
	Condition   *Condition `json:"condition"`
	Actions     []Action   `json:"actions"`
}

type Condition struct {
	Operator string      `json:"operator"`
	Field    string      `json:"field,omitempty"`
	Value    interface{} `json:"value,omitempty"`
	Children []Condition `json:"children,omitempty"`
}

type Action struct {
	Type   string                 `json:"type"`
	Params map[string]interface{} `json:"params"`
}

type Result struct {
	RuleID  string
	Actions []Action
}

func ParseRuleJSON(data string) (*Rule, error) {
	var rule Rule
	if err := json.Unmarshal([]byte(data), &rule); err != nil {
		return nil, err
	}
	return &rule, nil
}

func ParseRulesJSON(data string) ([]Rule, error) {
	var rules []Rule
	if err := json.Unmarshal([]byte(data), &rules); err != nil {
		return nil, err
	}
	return rules, nil
}
