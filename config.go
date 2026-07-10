package main

import (
	"encoding/base64"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/mortal/cpa-xai-quota-guard/internal/xaiquota"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
	"gopkg.in/yaml.v3"
)

func configDefaults() xaiquota.Config {
	return xaiquota.Defaults()
}

func configFields() []pluginapi.ConfigField {
	return []pluginapi.ConfigField{
		{Name: "enabled", Type: pluginapi.ConfigFieldTypeBoolean, Description: "插件总开关（默认关闭）"},
		{Name: "tick_seconds", Type: pluginapi.ConfigFieldTypeNumber, Description: "到期恢复扫描周期(秒)"},
		{Name: "max_reset_seconds", Type: pluginapi.ConfigFieldTypeNumber, Description: "允许的最大重置等待(秒)，超过则不禁用"},
		{Name: "management_url", Type: pluginapi.ConfigFieldTypeString, Description: "CPA 管理 API 基址"},
		{Name: "management_key", Type: pluginapi.ConfigFieldTypeString, Description: "CPA X-Management-Key（敏感，不回显）"},
		{Name: "state_path", Type: pluginapi.ConfigFieldTypeString, Description: "状态持久化 JSON 路径"},
	}
}

func parseConfigFromReconfigure(request []byte) xaiquota.Config {
	cfg := configDefaults()
	if len(request) == 0 {
		return cfg
	}
	var raw map[string]any
	if err := json.Unmarshal(request, &raw); err != nil {
		return cfg
	}
	if yamlBytes, ok := extractYAMLBytes(raw); ok {
		applyYAMLConfig(&cfg, yamlBytes)
		return cfg
	}
	configMap := raw
	if nested, ok := raw["config"].(map[string]any); ok {
		configMap = nested
	}
	applyConfigMap(&cfg, configMap)
	return cfg
}

func extractYAMLBytes(raw map[string]any) ([]byte, bool) {
	v, ok := raw["config_yaml"]
	if !ok || v == nil {
		return nil, false
	}
	switch t := v.(type) {
	case string:
		if decoded, err := base64.StdEncoding.DecodeString(t); err == nil {
			return decoded, true
		}
		return []byte(t), true
	case []byte:
		return t, true
	default:
		return nil, false
	}
}

func applyYAMLConfig(cfg *xaiquota.Config, yamlBytes []byte) {
	var m map[string]any
	if err := yaml.Unmarshal(yamlBytes, &m); err != nil {
		return
	}
	applyConfigMap(cfg, m)
}

func applyConfigMap(cfg *xaiquota.Config, m map[string]any) {
	if m == nil {
		return
	}
	if v, ok := asBool(m["enabled"]); ok {
		cfg.Enabled = v
	}
	if v, ok := asFloat(m["tick_seconds"]); ok && v > 0 {
		cfg.TickSeconds = v
	}
	if v, ok := asFloat(m["max_reset_seconds"]); ok && v > 0 {
		cfg.MaxResetSeconds = v
	}
	if v, ok := asString(m["management_url"]); ok {
		cfg.ManagementURL = strings.TrimSpace(v)
	}
	if v, ok := asString(m["management_key"]); ok {
		cfg.ManagementKey = strings.TrimSpace(v)
	}
	if v, ok := asString(m["state_path"]); ok && strings.TrimSpace(v) != "" {
		cfg.StatePath = strings.TrimSpace(v)
	}
}

func asBool(v any) (bool, bool) {
	switch t := v.(type) {
	case bool:
		return t, true
	case string:
		s := strings.ToLower(strings.TrimSpace(t))
		if s == "true" || s == "1" || s == "yes" {
			return true, true
		}
		if s == "false" || s == "0" || s == "no" {
			return false, true
		}
	case float64:
		return t != 0, true
	}
	return false, false
}

func asFloat(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case int:
		return float64(t), true
	case json.Number:
		n, err := t.Float64()
		return n, err == nil
	case string:
		n, err := strconv.ParseFloat(strings.TrimSpace(t), 64)
		return n, err == nil
	}
	return 0, false
}

func asString(v any) (string, bool) {
	switch t := v.(type) {
	case string:
		return t, true
	default:
		return "", false
	}
}