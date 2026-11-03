package engine

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
)

// integerValue — строгое число для десериализации снимка: как
// `typeof v === "number"` в TS. Строки, bool и null не принимаются
// (в отличие от numericValue, который приводит значения листа).
// Дробные значения усекаются — движок сам пишет только целые.
func integerValue(value any) (int, bool) {
	switch v := value.(type) {
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return 0, false
		}
		return int(v), true
	case float32:
		return int(v), true
	case int:
		return v, true
	case int64:
		return int(v), true
	case json.Number:
		parsed, err := v.Float64()
		if err != nil {
			return 0, false
		}
		return int(parsed), true
	default:
		return 0, false
	}
}

// numericValue приводит значение из JSON/листа к числу так же, как
// `typeof v === "number" ? v : Number(v)` в TS: строки парсятся, пустая
// строка/null дают 0, нечисловая строка — отказ. Как Number.isFinite в TS,
// ±Inf и NaN отклоняются. Отличие: шестнадцатеричные строки вроде "0x10"
// TS понимает (16), Go ParseFloat — нет; листы кампаний их не содержат.
func numericValue(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return finite(v)
	case float32:
		return finite(float64(v))
	case int:
		return float64(v), true
	case int8:
		return float64(v), true
	case int16:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	case uint:
		return float64(v), true
	case uint8:
		return float64(v), true
	case uint16:
		return float64(v), true
	case uint32:
		return float64(v), true
	case uint64:
		return float64(v), true
	case json.Number:
		parsed, err := v.Float64()
		if err != nil {
			return 0, false
		}
		return finite(parsed)
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			return 0, true
		}
		parsed, err := strconv.ParseFloat(trimmed, 64)
		if err != nil {
			return 0, false
		}
		return finite(parsed)
	case bool:
		if v {
			return 1, true
		}
		return 0, true
	case nil:
		return 0, true
	default:
		return 0, false
	}
}

// finite отбрасывает ±Inf и NaN — как Number.isFinite в TS.
func finite(value float64) (float64, bool) {
	if math.IsInf(value, 0) || math.IsNaN(value) {
		return 0, false
	}
	return value, true
}
