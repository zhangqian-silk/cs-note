package main

const (
	RuleTypeTargeting   = "targeting"
	RuleTypePricing     = "pricing"
	RuleTypeRiskControl = "risk_control"
	RuleTypeTask        = "task"
	RuleTypeTouch       = "touch"
	RuleTypeReco        = "reco"
	RuleTypeAfter       = "after"

	RuleStatusActive = "active"

	RuleMutexNewUserPromo = "new_user_promo"

	ConditionAnd        = "AND"
	ConditionOr         = "OR"
	ConditionEq         = "eq"
	ConditionGt         = "gt"
	ConditionGte        = "gte"
	ConditionLt         = "lt"
	ConditionLte        = "lte"
	ConditionIn         = "in"
	ConditionContains   = "contains"
	ConditionBitmaskAll = "bitmask_all"

	LevelMaskGold    = 2
	LevelMaskDiamond = 4
	LevelKeyGold     = "gold"
	LevelKeyDiamond  = "diamond"

	CouponMaskPlatform      = 1
	CouponMaskFullReduction = 2

	UserTagHighValue  = "high_value"
	UserCityBeijing   = "北京"
	UserCityShanghai  = "上海"
	RecoSceneBigPromo = "big_promo"

	ActionBenefitSend    = "benefit_send"
	ActionNotifyUser     = "notify_user"
	ActionPriceDiscount  = "price_discount"
	ActionCouponMutex    = "coupon_mutex"
	ActionReject         = "reject"
	ActionAddPoints      = "add_points"
	ActionUnlockBadge    = "unlock_badge"
	ActionRecoInsert     = "reco_insert"
	ActionRecoDownweight = "reco_downweight"
	ActionRefundApprove  = "refund_approve"
	ActionOk             = "ok"

	BenefitTypeCoupon       = "coupon"
	BenefitTypeFreeShipping = "free_shipping"

	CouponTemplateDouble11 = "CP_2024_11_11"

	CouponTypePlatform      = "platform_coupon"
	CouponTypeFullReduction = "full_reduction_coupon"

	RejectReasonCouponLimit    = "coupon_limit"
	RejectReasonBlacklist      = "blacklist"
	RejectReasonMessageFatigue = "message_fatigue"

	BadgeRookie = "rookie"

	NotifyChannelSms  = "sms"
	NotifyChannelPush = "push"

	RecoItemMainVenue = "main_venue"

	RefundModeAuto = "auto"
)
