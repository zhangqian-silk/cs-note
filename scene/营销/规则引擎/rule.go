package main

var DefaultRules = []Rule{
	{
		RuleID:      "RULE_1024",
		RuleName:    "双11新人满减_高活用户专享",
		Description: "针对北京/上海的高价值新人，满300减50",
		Priority:    100,
		MutexGroup:  "new_user_promo",
		Status:      "active",
		Condition: &Condition{
			Operator: "AND",
			Children: []Condition{
				{Field: "user.register_days", Operator: "lte", Value: 7},
				{
					Operator: "OR",
					Children: []Condition{
						{Field: "user.city", Operator: "in", Value: []interface{}{"北京", "上海"}},
						{Field: "user.tags", Operator: "contains", Value: "high_value"},
					},
				},
				{Field: "cart.total_amount", Operator: "gte", Value: 300},
			},
		},
		Actions: []Action{
			{Type: "benefit_send", Params: map[string]interface{}{"benefit_type": "coupon", "template_id": "CP_2024_11_11", "count": 1}},
		},
	},
	{
		RuleID:      "RULE_2048",
		RuleName:    "高活用户免邮",
		Description: "高活用户满120免邮",
		Priority:    90,
		MutexGroup:  "new_user_promo",
		Status:      "active",
		Condition: &Condition{
			Operator: "AND",
			Children: []Condition{
				{Field: "user.tags", Operator: "contains", Value: "high_value"},
				{Field: "cart.total_amount", Operator: "gte", Value: 120},
			},
		},
		Actions: []Action{
			{Type: "benefit_send", Params: map[string]interface{}{"benefit_type": "free_shipping"}},
		},
	},
	{
		RuleID:      "RULE_VAR",
		RuleName:    "动态门槛折扣",
		Description: "购物金额满足动态门槛",
		Priority:    80,
		Status:      "active",
		Condition: &Condition{
			Field:    "cart.total_amount",
			Operator: "gte",
			Value:    map[string]interface{}{"var": "cart.threshold"},
		},
		Actions: []Action{
			{Type: "ok", Params: map[string]interface{}{}},
		},
	},
	{
		RuleID:      "RULE_PRICE_1",
		RuleName:    "会员专享价",
		Description: "金牌95折，钻石88折",
		Priority:    70,
		Status:      "active",
		Condition: &Condition{
			Operator: "OR",
			Children: []Condition{
				{Field: "user.level", Operator: "eq", Value: "gold"},
				{Field: "user.level", Operator: "eq", Value: "diamond"},
			},
		},
		Actions: []Action{
			{Type: "price_discount", Params: map[string]interface{}{"gold": 0.95, "diamond": 0.88}},
		},
	},
	{
		RuleID:      "RULE_PRICE_2",
		RuleName:    "促销叠加互斥",
		Description: "平台券与满减券不可同享",
		Priority:    65,
		Status:      "active",
		Condition: &Condition{
			Operator: "AND",
			Children: []Condition{
				{Field: "cart.coupons", Operator: "contains", Value: "platform_coupon"},
				{Field: "cart.coupons", Operator: "contains", Value: "full_reduction_coupon"},
			},
		},
		Actions: []Action{
			{Type: "coupon_mutex", Params: map[string]interface{}{"reject": "full_reduction_coupon"}},
		},
	},
	{
		RuleID:      "RULE_RISK_1",
		RuleName:    "频次控制",
		Description: "单用户单日领券不超过3张",
		Priority:    60,
		Status:      "active",
		Condition: &Condition{
			Field:    "risk.daily_coupon_count",
			Operator: "gte",
			Value:    3,
		},
		Actions: []Action{
			{Type: "reject", Params: map[string]interface{}{"reason": "coupon_limit"}},
		},
	},
	{
		RuleID:      "RULE_RISK_2",
		RuleName:    "黑名单拦截",
		Description: "用户或设备在黑名单中直接拒绝",
		Priority:    59,
		Status:      "active",
		Condition: &Condition{
			Operator: "OR",
			Children: []Condition{
				{Field: "risk.user_blacklist", Operator: "eq", Value: true},
				{Field: "risk.device_blacklist", Operator: "eq", Value: true},
			},
		},
		Actions: []Action{
			{Type: "reject", Params: map[string]interface{}{"reason": "blacklist"}},
		},
	},
	{
		RuleID:      "RULE_TASK_1",
		RuleName:    "连续签到奖励",
		Description: "连续签到7天奖励100积分",
		Priority:    50,
		Status:      "active",
		Condition: &Condition{
			Field:    "task.checkin_streak",
			Operator: "gte",
			Value:    7,
		},
		Actions: []Action{
			{Type: "add_points", Params: map[string]interface{}{"points": 100}},
		},
	},
	{
		RuleID:      "RULE_TASK_2",
		RuleName:    "组合任务成就",
		Description: "完善资料且首次下单解锁新手勋章",
		Priority:    49,
		Status:      "active",
		Condition: &Condition{
			Operator: "AND",
			Children: []Condition{
				{Field: "task.profile_completed", Operator: "eq", Value: true},
				{Field: "task.first_order", Operator: "eq", Value: true},
			},
		},
		Actions: []Action{
			{Type: "unlock_badge", Params: map[string]interface{}{"badge": "rookie"}},
		},
	},
	{
		RuleID:      "RULE_TOUCH_1",
		RuleName:    "渠道路由降级",
		Description: "优先Push，未开通则降级短信",
		Priority:    45,
		Status:      "active",
		Condition: &Condition{
			Operator: "AND",
			Children: []Condition{
				{Field: "user.push_enabled", Operator: "eq", Value: false},
				{Field: "user.phone_verified", Operator: "eq", Value: true},
			},
		},
		Actions: []Action{
			{Type: "notify_user", Params: map[string]interface{}{"channel": "sms"}},
		},
	},
	{
		RuleID:      "RULE_TOUCH_2",
		RuleName:    "疲劳度控制",
		Description: "24小时内最多2条营销消息",
		Priority:    44,
		Status:      "active",
		Condition: &Condition{
			Field:    "touch.message_count_24h",
			Operator: "gte",
			Value:    2,
		},
		Actions: []Action{
			{Type: "reject", Params: map[string]interface{}{"reason": "message_fatigue"}},
		},
	},
	{
		RuleID:      "RULE_RECO_1",
		RuleName:    "强插入口",
		Description: "主会场入口插入第3位",
		Priority:    40,
		Status:      "active",
		Condition: &Condition{
			Field:    "reco.scene",
			Operator: "eq",
			Value:    "big_promo",
		},
		Actions: []Action{
			{Type: "reco_insert", Params: map[string]interface{}{"item": "main_venue", "position": 3}},
		},
	},
	{
		RuleID:      "RULE_RECO_2",
		RuleName:    "商家降权",
		Description: "评分低于3.0的商家降权50%",
		Priority:    39,
		Status:      "active",
		Condition: &Condition{
			Field:    "reco.merchant_score",
			Operator: "lt",
			Value:    3.0,
		},
		Actions: []Action{
			{Type: "reco_downweight", Params: map[string]interface{}{"weight": 0.5}},
		},
	},
	{
		RuleID:      "RULE_AFTER_1",
		RuleName:    "极速退款",
		Description: "信用分>700且退款金额<200自动通过",
		Priority:    30,
		Status:      "active",
		Condition: &Condition{
			Operator: "AND",
			Children: []Condition{
				{Field: "after.credit_score", Operator: "gt", Value: 700},
				{Field: "after.refund_amount", Operator: "lt", Value: 200},
			},
		},
		Actions: []Action{
			{Type: "refund_approve", Params: map[string]interface{}{"mode": "auto"}},
		},
	},
	{
		RuleID:      "RULE_AFTER_2",
		RuleName:    "超时赔付",
		Description: "外卖超时30分钟赔付5元",
		Priority:    29,
		Status:      "active",
		Condition: &Condition{
			Field:    "after.delivery_delay_minutes",
			Operator: "gte",
			Value:    30,
		},
		Actions: []Action{
			{Type: "benefit_send", Params: map[string]interface{}{"benefit_type": "coupon", "amount": 5}},
		},
	},
}

func LoadRules() []Rule {
	return append([]Rule{}, DefaultRules...)
}
