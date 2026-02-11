package main

import "encoding/json"

type Fact map[string]interface{}

type Rule struct {
	RuleID      string     `json:"rule_id"`
	RuleName    string     `json:"rule_name"`
	Description string     `json:"description"`
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
