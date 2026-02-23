package main

var DefaultRules = []Rule{
	{
		RuleID:      "RULE_1024",
		RuleName:    "双11新人满减_高活用户专享",
		Description: "针对北京/上海的高价值新人，满300减50",
		Type:        RuleTypeTargeting,
		Priority:    100,
		MutexGroup:  RuleMutexNewUserPromo,
		Status:      RuleStatusActive,
		Condition: &Condition{
			Operator: ConditionAnd,
			Children: []Condition{
				{Field: "user.register_days", Operator: ConditionLte, Value: 7},
				{
					Operator: ConditionOr,
					Children: []Condition{
						{Field: "user.city", Operator: ConditionIn, Value: []interface{}{UserCityBeijing, UserCityShanghai}},
						{Field: "user.tags", Operator: ConditionContains, Value: UserTagHighValue},
					},
				},
				{Field: "cart.total_amount", Operator: ConditionGte, Value: 300},
			},
		},
		Actions: []Action{
			{Type: ActionBenefitSend, Params: map[string]interface{}{"benefit_type": BenefitTypeCoupon, "template_id": CouponTemplateDouble11, "count": 1}},
		},
	},
	{
		RuleID:      "RULE_2048",
		RuleName:    "高活用户免邮",
		Description: "高活用户满120免邮",
		Type:        RuleTypeTargeting,
		Priority:    90,
		MutexGroup:  RuleMutexNewUserPromo,
		Status:      RuleStatusActive,
		Condition: &Condition{
			Operator: ConditionAnd,
			Children: []Condition{
				{Field: "user.tags", Operator: ConditionContains, Value: UserTagHighValue},
				{Field: "cart.total_amount", Operator: ConditionGte, Value: 120},
			},
		},
		Actions: []Action{
			{Type: ActionBenefitSend, Params: map[string]interface{}{"benefit_type": BenefitTypeFreeShipping}},
		},
	},
	{
		RuleID:      "RULE_VAR",
		RuleName:    "动态门槛折扣",
		Description: "购物金额满足动态门槛",
		Type:        RuleTypePricing,
		Priority:    80,
		Status:      RuleStatusActive,
		Condition: &Condition{
			Field:    "cart.total_amount",
			Operator: ConditionGte,
			Value:    map[string]interface{}{"var": "cart.threshold"},
		},
		Actions: []Action{
			{Type: ActionOk, Params: map[string]interface{}{}},
		},
	},
	{
		RuleID:      "RULE_PRICE_1",
		RuleName:    "会员专享价",
		Description: "金牌95折，钻石88折",
		Type:        RuleTypePricing,
		Priority:    70,
		Status:      RuleStatusActive,
		Condition: &Condition{
			Operator: ConditionOr,
			Children: []Condition{
				{Field: "user.level_mask", Operator: ConditionBitmaskAll, Value: LevelMaskGold},
				{Field: "user.level_mask", Operator: ConditionBitmaskAll, Value: LevelMaskDiamond},
			},
		},
		Actions: []Action{
			{Type: ActionPriceDiscount, Params: map[string]interface{}{LevelKeyGold: 0.95, LevelKeyDiamond: 0.88}},
		},
	},
	{
		RuleID:      "RULE_PRICE_2",
		RuleName:    "促销叠加互斥",
		Description: "平台券与满减券不可同享",
		Type:        RuleTypePricing,
		Priority:    65,
		Status:      RuleStatusActive,
		Condition: &Condition{
			Operator: ConditionAnd,
			Children: []Condition{
				{Field: "cart.coupons_mask", Operator: ConditionBitmaskAll, Value: CouponMaskPlatform},
				{Field: "cart.coupons_mask", Operator: ConditionBitmaskAll, Value: CouponMaskFullReduction},
			},
		},
		Actions: []Action{
			{Type: ActionCouponMutex, Params: map[string]interface{}{"reject": CouponTypeFullReduction}},
		},
	},
	{
		RuleID:      "RULE_RISK_1",
		RuleName:    "频次控制",
		Description: "单用户单日领券不超过3张",
		Type:        RuleTypeRiskControl,
		Priority:    60,
		Status:      RuleStatusActive,
		Condition: &Condition{
			Field:    "risk.daily_coupon_count",
			Operator: ConditionGte,
			Value:    3,
		},
		Actions: []Action{
			{Type: ActionReject, Params: map[string]interface{}{"reason": RejectReasonCouponLimit}},
		},
	},
	{
		RuleID:      "RULE_RISK_2",
		RuleName:    "黑名单拦截",
		Description: "用户或设备在黑名单中直接拒绝",
		Type:        RuleTypeRiskControl,
		Priority:    59,
		Status:      RuleStatusActive,
		Condition: &Condition{
			Operator: ConditionOr,
			Children: []Condition{
				{Field: "risk.user_blacklist", Operator: ConditionEq, Value: true},
				{Field: "risk.device_blacklist", Operator: ConditionEq, Value: true},
			},
		},
		Actions: []Action{
			{Type: ActionReject, Params: map[string]interface{}{"reason": RejectReasonBlacklist}},
		},
	},
	{
		RuleID:      "RULE_TASK_1",
		RuleName:    "连续签到奖励",
		Description: "连续签到7天奖励100积分",
		Type:        RuleTypeTask,
		Priority:    50,
		Status:      RuleStatusActive,
		Condition: &Condition{
			Field:    "task.checkin_streak",
			Operator: ConditionGte,
			Value:    7,
		},
		Actions: []Action{
			{Type: ActionAddPoints, Params: map[string]interface{}{"points": 100}},
		},
	},
	{
		RuleID:      "RULE_TASK_2",
		RuleName:    "组合任务成就",
		Description: "完善资料且首次下单解锁新手勋章",
		Type:        RuleTypeTask,
		Priority:    49,
		Status:      RuleStatusActive,
		Condition: &Condition{
			Operator: ConditionAnd,
			Children: []Condition{
				{Field: "task.profile_completed", Operator: ConditionEq, Value: true},
				{Field: "task.first_order", Operator: ConditionEq, Value: true},
			},
		},
		Actions: []Action{
			{Type: ActionUnlockBadge, Params: map[string]interface{}{"badge": BadgeRookie}},
		},
	},
	{
		RuleID:      "RULE_TOUCH_1",
		RuleName:    "渠道路由降级",
		Description: "优先Push，未开通则降级短信",
		Type:        RuleTypeTouch,
		Priority:    45,
		Status:      RuleStatusActive,
		Condition: &Condition{
			Operator: ConditionAnd,
			Children: []Condition{
				{Field: "user.push_enabled", Operator: ConditionEq, Value: false},
				{Field: "user.phone_verified", Operator: ConditionEq, Value: true},
			},
		},
		Actions: []Action{
			{Type: ActionNotifyUser, Params: map[string]interface{}{"channel": NotifyChannelSms}},
		},
	},
	{
		RuleID:      "RULE_TOUCH_2",
		RuleName:    "疲劳度控制",
		Description: "24小时内最多2条营销消息",
		Type:        RuleTypeTouch,
		Priority:    44,
		Status:      RuleStatusActive,
		Condition: &Condition{
			Field:    "touch.message_count_24h",
			Operator: ConditionGte,
			Value:    2,
		},
		Actions: []Action{
			{Type: ActionReject, Params: map[string]interface{}{"reason": RejectReasonMessageFatigue}},
		},
	},
	{
		RuleID:      "RULE_RECO_1",
		RuleName:    "强插入口",
		Description: "主会场入口插入第3位",
		Type:        RuleTypeReco,
		Priority:    40,
		Status:      RuleStatusActive,
		Condition: &Condition{
			Field:    "reco.scene",
			Operator: ConditionEq,
			Value:    RecoSceneBigPromo,
		},
		Actions: []Action{
			{Type: ActionRecoInsert, Params: map[string]interface{}{"item": RecoItemMainVenue, "position": 3}},
		},
	},
	{
		RuleID:      "RULE_RECO_2",
		RuleName:    "商家降权",
		Description: "评分低于3.0的商家降权50%",
		Type:        RuleTypeReco,
		Priority:    39,
		Status:      RuleStatusActive,
		Condition: &Condition{
			Field:    "reco.merchant_score",
			Operator: ConditionLt,
			Value:    3.0,
		},
		Actions: []Action{
			{Type: ActionRecoDownweight, Params: map[string]interface{}{"weight": 0.5}},
		},
	},
	{
		RuleID:      "RULE_AFTER_1",
		RuleName:    "极速退款",
		Description: "信用分>700且退款金额<200自动通过",
		Type:        RuleTypeAfter,
		Priority:    30,
		Status:      RuleStatusActive,
		Condition: &Condition{
			Operator: ConditionAnd,
			Children: []Condition{
				{Field: "after.credit_score", Operator: ConditionGt, Value: 700},
				{Field: "after.refund_amount", Operator: ConditionLt, Value: 200},
			},
		},
		Actions: []Action{
			{Type: ActionRefundApprove, Params: map[string]interface{}{"mode": RefundModeAuto}},
		},
	},
	{
		RuleID:      "RULE_AFTER_2",
		RuleName:    "超时赔付",
		Description: "外卖超时30分钟赔付5元",
		Type:        RuleTypeAfter,
		Priority:    29,
		Status:      RuleStatusActive,
		Condition: &Condition{
			Field:    "after.delivery_delay_minutes",
			Operator: ConditionGte,
			Value:    30,
		},
		Actions: []Action{
			{Type: ActionBenefitSend, Params: map[string]interface{}{"benefit_type": BenefitTypeCoupon, "amount": 5}},
		},
	},
}

func LoadRules() []Rule {
	return append([]Rule{}, DefaultRules...)
}
