package provider

import (
	"encoding/json"
	"fmt"
	"strconv"
)

func findFirstNumber(v any, keys ...string) (float64, bool) {
	switch t := v.(type) {
	case map[string]any:
		for _, k := range keys {
			if raw, ok := t[k]; ok {
				if n, ok := toFloat(raw); ok {
					return n, true
				}
			}
		}
		for _, raw := range t {
			if n, ok := findFirstNumber(raw, keys...); ok {
				return n, true
			}
		}
	case []any:
		for _, item := range t {
			if n, ok := findFirstNumber(item, keys...); ok {
				return n, true
			}
		}
	}
	return 0, false
}

func toFloat(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case float32:
		return float64(t), true
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	case json.Number:
		n, err := t.Float64()
		return n, err == nil
	case string:
		n, err := strconv.ParseFloat(t, 64)
		return n, err == nil
	default:
		return 0, false
	}
}

func stringValue(v any) string {
	s, _ := v.(string)
	return s
}

func parseCLIJSON(stdout string) (map[string]any, error) {
	var m map[string]any
	if err := json.Unmarshal([]byte(stdout), &m); err != nil {
		return nil, fmt.Errorf("invalid cli json: %w", err)
	}
	return m, nil
}
