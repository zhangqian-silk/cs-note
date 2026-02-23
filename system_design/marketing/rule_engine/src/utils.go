package main

import (
	"errors"
	"math"
	"reflect"
	"strings"
)

func compareNumber(left, right interface{}, cmp func(a, b float64) bool) (bool, error) {
	// 数值比较的统一入口
	lf, ok := toFloat(left)
	if !ok {
		return false, errors.New("left is not number")
	}
	rf, ok := toFloat(right)
	if !ok {
		return false, errors.New("right is not number")
	}
	return cmp(lf, rf), nil
}

func toFloat(v interface{}) (float64, bool) {
	// 支持多种数值类型的归一化
	switch t := v.(type) {
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	case float64:
		return t, true
	case float32:
		return float64(t), true
	case jsonNumber:
		f, err := t.Float64()
		if err != nil {
			return 0, false
		}
		return f, true
	default:
		return 0, false
	}
}

type jsonNumber interface {
	// 兼容 json.Number 的抽象接口
	Float64() (float64, error)
}

func toUint64(v interface{}) (uint64, bool) {
	switch t := v.(type) {
	case int:
		if t < 0 {
			return 0, false
		}
		return uint64(t), true
	case int8:
		if t < 0 {
			return 0, false
		}
		return uint64(t), true
	case int16:
		if t < 0 {
			return 0, false
		}
		return uint64(t), true
	case int32:
		if t < 0 {
			return 0, false
		}
		return uint64(t), true
	case int64:
		if t < 0 {
			return 0, false
		}
		return uint64(t), true
	case uint:
		return uint64(t), true
	case uint8:
		return uint64(t), true
	case uint16:
		return uint64(t), true
	case uint32:
		return uint64(t), true
	case uint64:
		return t, true
	case float64:
		if t < 0 || math.Trunc(t) != t {
			return 0, false
		}
		return uint64(t), true
	case float32:
		if t < 0 || math.Trunc(float64(t)) != float64(t) {
			return 0, false
		}
		return uint64(t), true
	case jsonNumber:
		f, err := t.Float64()
		if err != nil {
			return 0, false
		}
		if f < 0 || math.Trunc(f) != f {
			return 0, false
		}
		return uint64(f), true
	default:
		return 0, false
	}
}

func isEqual(left, right interface{}) bool {
	// 先归一化数值，再进行深度比较
	return reflect.DeepEqual(normalizeNumber(left), normalizeNumber(right))
}

func normalizeNumber(v interface{}) interface{} {
	// 将数值统一转换为 float64，避免类型差异导致不等
	if f, ok := toFloat(v); ok {
		return f
	}
	return v
}

func bitmaskAll(left, right interface{}) (bool, error) {
	lv, ok := toUint64(left)
	if !ok {
		return false, errors.New("left is not integer for bitmask_all")
	}
	rv, ok := toUint64(right)
	if !ok {
		return false, errors.New("right is not integer for bitmask_all")
	}
	return (lv & rv) == rv, nil
}

func isIn(left, right interface{}) (bool, error) {
	// 判断 left 是否存在于列表 right 中
	rv := reflect.ValueOf(right)
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return false, errors.New("right is not list for in")
	}
	for i := 0; i < rv.Len(); i++ {
		if isEqual(left, rv.Index(i).Interface()) {
			return true, nil
		}
	}
	return false, nil
}

func contains(left, right interface{}) (bool, error) {
	// 支持字符串包含与数组包含两种语义
	switch l := left.(type) {
	case string:
		r, ok := right.(string)
		if !ok {
			return false, errors.New("right is not string for contains")
		}
		return strings.Contains(l, r), nil
	default:
		rv := reflect.ValueOf(left)
		if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
			return false, errors.New("left is not list for contains")
		}
		for i := 0; i < rv.Len(); i++ {
			if isEqual(rv.Index(i).Interface(), right) {
				return true, nil
			}
		}
		return false, nil
	}
}
