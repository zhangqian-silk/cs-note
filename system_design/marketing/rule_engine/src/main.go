package main

import (
	"fmt"
	"sort"
	"strings"
)

func main() {
	cache := NewRuleCache()
	RegisterDefaultRules(cache)

	rules := cache.GetAll()
	runTargetingScenario(rules)
	runPricingScenario(rules)
	runRiskControlScenario(rules)
	runTaskScenario(rules)
	runTouchScenario(rules)
	runRecoScenario(rules)
	runAfterScenario(rules)
	runPipelineScenario(rules)
}

type EligibilityHandler struct {
	engine *Engine
}

func (h EligibilityHandler) Name() string {
	return "eligibility"
}

func (h EligibilityHandler) Handle(ctx *PipelineContext) error {
	results, err := h.engine.Evaluate(ctx.Fact)
	if err != nil {
		return err
	}
	ctx.Results = results
	if len(results) == 0 {
		ctx.Result = false
		ctx.Reason = "no_rule_matched"
	}
	return nil
}

type BenefitHandler struct{}

func (h BenefitHandler) Name() string {
	return "benefit"
}

func (h BenefitHandler) Handle(ctx *PipelineContext) error {
	if ctx.Data == nil {
		ctx.Data = map[string]interface{}{}
	}
	actions := []Action{}
	for _, result := range ctx.Results {
		actions = append(actions, result.Actions...)
	}
	ctx.Data["actions"] = actions
	return nil
}

type NotifyHandler struct{}

func (h NotifyHandler) Name() string {
	return "notify"
}

func (h NotifyHandler) Handle(ctx *PipelineContext) error {
	if ctx.Data == nil {
		ctx.Data = map[string]interface{}{}
	}
	ctx.Data["notify"] = map[string]interface{}{"channel": NotifyChannelSms}
	return nil
}

func filterRulesByType(rules []Rule, ruleType string) []Rule {
	filtered := make([]Rule, 0, len(rules))
	for _, rule := range rules {
		if rule.Type == ruleType {
			filtered = append(filtered, rule)
		}
	}
	return filtered
}

type evaluationEntry struct {
	RuleID   string
	RuleName string
	Type     string
	Priority int
	Matched  bool
	Reason   string
	Actions  []Action
}

func buildEvaluationReport(rules []Rule, fact *Fact) []evaluationEntry {
	copied := make([]Rule, len(rules))
	copy(copied, rules)
	sort.SliceStable(copied, func(i, j int) bool {
		return copied[i].Priority > copied[j].Priority
	})
	mutexHit := map[string]bool{}
	report := make([]evaluationEntry, 0, len(copied))
	for _, rule := range copied {
		entry := evaluationEntry{
			RuleID:   rule.RuleID,
			RuleName: rule.RuleName,
			Type:     rule.Type,
			Priority: rule.Priority,
		}
		if rule.Status != "" && strings.ToLower(rule.Status) != RuleStatusActive {
			entry.Matched = false
			entry.Reason = "inactive"
			report = append(report, entry)
			continue
		}
		if rule.MutexGroup != "" && mutexHit[rule.MutexGroup] {
			entry.Matched = false
			entry.Reason = "skipped_mutex_group"
			report = append(report, entry)
			continue
		}
		matched, err := EvaluateCondition(rule.Condition, fact)
		if err != nil {
			entry.Matched = false
			entry.Reason = err.Error()
			report = append(report, entry)
			continue
		}
		if matched {
			entry.Matched = true
			entry.Reason = "matched"
			entry.Actions = rule.Actions
			if rule.MutexGroup != "" {
				mutexHit[rule.MutexGroup] = true
			}
		} else {
			entry.Matched = false
			entry.Reason = "condition_false"
		}
		report = append(report, entry)
	}
	return report
}

func runScenario(title string, rules []Rule, fact *Fact) {
	fmt.Println("=== " + title + " ===")
	printRules(rules)
	printFact(fact)
	printEvaluation(buildEvaluationReport(rules, fact))
	engine := NewEngine(rules)
	results, err := engine.Evaluate(fact)
	if err != nil {
		panic(err)
	}
	printResults(results)
}

func runTargetingScenario(rules []Rule) {
	fact := NewFact(map[string]interface{}{
		"user": map[string]interface{}{
			"register_days": 5,
			"city":          "北京",
			"tags":          []interface{}{UserTagHighValue, "vip"},
		},
		"cart": map[string]interface{}{
			"total_amount": 320,
		},
	})
	runScenario("targeting", filterRulesByType(rules, RuleTypeTargeting), fact)
}

func runPricingScenario(rules []Rule) {
	fact := NewFact(map[string]interface{}{
		"user": map[string]interface{}{
			"level_mask": LevelMaskGold,
		},
		"cart": map[string]interface{}{
			"total_amount": 200,
			"threshold":    150,
			"coupons_mask": CouponMaskPlatform + CouponMaskFullReduction,
		},
	})
	runScenario("pricing", filterRulesByType(rules, RuleTypePricing), fact)
}

func runRiskControlScenario(rules []Rule) {
	fact := NewFact(map[string]interface{}{
		"risk": map[string]interface{}{
			"daily_coupon_count": 3,
			"user_blacklist":     false,
			"device_blacklist":   true,
		},
	})
	runScenario("risk_control", filterRulesByType(rules, RuleTypeRiskControl), fact)
}

func runTaskScenario(rules []Rule) {
	fact := NewFact(map[string]interface{}{
		"task": map[string]interface{}{
			"checkin_streak":    3,
			"profile_completed": true,
			"first_order":       true,
		},
	})
	runScenario("task", filterRulesByType(rules, RuleTypeTask), fact)
}

func runTouchScenario(rules []Rule) {
	fact := NewFact(map[string]interface{}{
		"user": map[string]interface{}{
			"push_enabled":  false,
			"phone_verified": true,
		},
		"touch": map[string]interface{}{
			"message_count_24h": 1,
		},
	})
	runScenario("touch", filterRulesByType(rules, RuleTypeTouch), fact)
}

func runRecoScenario(rules []Rule) {
	fact := NewFact(map[string]interface{}{
		"reco": map[string]interface{}{
			"scene":          RecoSceneBigPromo,
			"merchant_score": 4.5,
		},
	})
	runScenario("reco", filterRulesByType(rules, RuleTypeReco), fact)
}

func runAfterScenario(rules []Rule) {
	fact := NewFact(map[string]interface{}{
		"after": map[string]interface{}{
			"credit_score":            650,
			"refund_amount":           100,
			"delivery_delay_minutes": 35,
		},
	})
	runScenario("after", filterRulesByType(rules, RuleTypeAfter), fact)
}

func runPipelineScenario(rules []Rule) {
	fact := NewFact(map[string]interface{}{
		"user": map[string]interface{}{
			"register_days": 5,
			"city":          "北京",
			"tags":          []interface{}{UserTagHighValue, "vip"},
		},
		"cart": map[string]interface{}{
			"total_amount": 320,
			"threshold":    150,
		},
	})
	runScenario("pipeline_rule_evaluation", rules, fact)
	engine := NewEngine(rules)
	pipeline := NewPipeline(
		EligibilityHandler{engine: engine},
		BenefitHandler{},
		NotifyHandler{},
	)
	ctx := &PipelineContext{
		Result: true,
		Fact:   fact,
	}
	err := pipeline.Execute(ctx)
	if err != nil {
		panic(err)
	}
	printPipelineOutput(ctx)
}

func printRules(rules []Rule) {
	fmt.Println("rules:")
	if len(rules) == 0 {
		fmt.Println("  (empty)")
		return
	}
	for _, rule := range rules {
		fmt.Printf("  - %s %s type=%s priority=%d status=%s mutex=%s\n", rule.RuleID, rule.RuleName, rule.Type, rule.Priority, rule.Status, rule.MutexGroup)
		fmt.Printf("    condition: %s\n", formatCondition(rule.Condition))
		if len(rule.Actions) == 0 {
			fmt.Println("    actions: (none)")
			continue
		}
		fmt.Printf("    actions: %s\n", formatActions(rule.Actions))
	}
}

func printFact(fact *Fact) {
	fmt.Println("fact:")
	printValue("  ", fact.data)
}

func printEvaluation(report []evaluationEntry) {
	fmt.Println("evaluation:")
	if len(report) == 0 {
		fmt.Println("  (empty)")
		return
	}
	for _, entry := range report {
		fmt.Printf("  - %s %s type=%s priority=%d matched=%t reason=%s\n", entry.RuleID, entry.RuleName, entry.Type, entry.Priority, entry.Matched, entry.Reason)
		if len(entry.Actions) > 0 {
			fmt.Printf("    actions: %s\n", formatActions(entry.Actions))
		}
	}
}

func printResults(results []Result) {
	fmt.Println("matched_results:")
	if len(results) == 0 {
		fmt.Println("  (empty)")
		return
	}
	for _, result := range results {
		fmt.Printf("  - %s actions=%s\n", result.RuleID, formatActions(result.Actions))
	}
}

func printPipelineOutput(ctx *PipelineContext) {
	fmt.Println("pipeline_output:")
	fmt.Printf("  result: %t\n", ctx.Result)
	fmt.Printf("  reason: %s\n", ctx.Reason)
	if len(ctx.Data) == 0 {
		fmt.Println("  data: (empty)")
		return
	}
	fmt.Println("  data:")
	printValue("    ", ctx.Data)
}

func printValue(indent string, value interface{}) {
	switch v := value.(type) {
	case map[string]interface{}:
		if len(v) == 0 {
			fmt.Println(indent + "(empty)")
			return
		}
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, key := range keys {
			val := v[key]
			switch val.(type) {
			case map[string]interface{}:
				fmt.Printf("%s%s:\n", indent, key)
				printValue(indent+"  ", val)
			default:
				fmt.Printf("%s%s: %s\n", indent, key, formatValue(val))
			}
		}
	default:
		fmt.Println(indent + formatValue(v))
	}
}

func formatCondition(condition *Condition) string {
	if condition == nil {
		return "true"
	}
	op := strings.ToUpper(condition.Operator)
	switch op {
	case "AND", "OR":
		if len(condition.Children) == 0 {
			return op + "()"
		}
		parts := make([]string, 0, len(condition.Children))
		for i := range condition.Children {
			parts = append(parts, formatCondition(&condition.Children[i]))
		}
		return "(" + strings.Join(parts, " "+op+" ") + ")"
	case "NOT":
		if len(condition.Children) == 0 {
			return "NOT()"
		}
		return "NOT (" + formatCondition(&condition.Children[0]) + ")"
	default:
		return condition.Field + " " + condition.Operator + " " + formatValue(condition.Value)
	}
}

func formatValue(value interface{}) string {
	switch v := value.(type) {
	case string:
		return v
	case []interface{}:
		items := make([]string, 0, len(v))
		for _, item := range v {
			items = append(items, formatValue(item))
		}
		return "[" + strings.Join(items, ", ") + "]"
	case map[string]interface{}:
		if ref, ok := v["var"]; ok {
			return "var(" + formatValue(ref) + ")"
		}
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, key := range keys {
			parts = append(parts, key+"="+formatValue(v[key]))
		}
		return "{" + strings.Join(parts, ", ") + "}"
	default:
		return fmt.Sprintf("%v", v)
	}
}

func formatActions(actions []Action) string {
	if len(actions) == 0 {
		return "(none)"
	}
	items := make([]string, 0, len(actions))
	for _, action := range actions {
		params := formatParams(action.Params)
		if params == "" {
			items = append(items, action.Type)
			continue
		}
		items = append(items, action.Type+"("+params+")")
	}
	return strings.Join(items, "; ")
}

func formatParams(params map[string]interface{}) string {
	if len(params) == 0 {
		return ""
	}
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+formatValue(params[key]))
	}
	return strings.Join(parts, ", ")
}

