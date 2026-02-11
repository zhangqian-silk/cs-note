package main

import (
	"encoding/json"
	"fmt"
)

func main() {
	cache := NewRuleCache()
	RegisterDefaultRules(cache)

	engine := NewEngine(cache.GetAll())
	fact := Fact{
		"user": map[string]interface{}{
			"register_days": 5,
			"city":          "北京",
			"tags":          []interface{}{"high_value", "vip"},
		},
		"cart": map[string]interface{}{
			"total_amount": 320,
			"threshold":    150,
		},
	}

	results, err := engine.Evaluate(fact)
	if err != nil {
		panic(err)
	}
	bytes, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		panic(err)
	}
	fmt.Println(string(bytes))
}
