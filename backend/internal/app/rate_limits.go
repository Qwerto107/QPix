package app

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"
)

type rateLimitRule struct {
	Enabled       bool `json:"enabled"`
	Requests      int  `json:"requests"`
	WindowSeconds int  `json:"window_seconds"`
}

func defaultRateLimits() map[string]rateLimitRule {
	return map[string]rateLimitRule{
		"login":           {true, 15, 900},
		"share_unlock":    {true, 15, 900},
		"password_change": {true, 10, 900},
		"upload":          {true, 120, 60},
		"share_create":    {true, 30, 60},
	}
}

func validRateLimits(rules map[string]rateLimitRule) bool {
	defaults := defaultRateLimits()
	if len(rules) != len(defaults) {
		return false
	}
	for scope := range defaults {
		rule, ok := rules[scope]
		if !ok || rule.Requests < 1 || rule.Requests > 1_000_000 || rule.WindowSeconds < 1 || rule.WindowSeconds > 86400 {
			return false
		}
	}
	return true
}

func (a *App) rateLimiter(scope string) *limiter {
	switch scope {
	case "login":
		return &a.loginLimits
	case "share_unlock":
		return &a.shareLimits
	case "password_change":
		return &a.passwordLimits
	case "upload":
		return &a.uploadLimits
	case "share_create":
		return &a.shareCreateLimits
	default:
		panic("未知限频规则: " + scope)
	}
}

func (a *App) allowRate(w http.ResponseWriter, scope, key, message string) bool {
	// 持有配置读锁直至计数完成，避免更新配置后又写入旧窗口的计数。
	a.rateMu.RLock()
	rule := a.rateLimits[scope]
	var retry time.Duration
	if rule.Enabled {
		retry = a.rateLimiter(scope).retry(key, rule.Requests, time.Duration(rule.WindowSeconds)*time.Second)
	}
	a.rateMu.RUnlock()
	if retry <= 0 {
		return true
	}
	seconds := (retry + time.Second - 1) / time.Second
	w.Header().Set("Retry-After", strconv.FormatInt(int64(seconds), 10))
	fail(w, http.StatusTooManyRequests, message)
	return false
}

func (a *App) getRateLimits(w http.ResponseWriter, r *http.Request, p principal) {
	a.rateMu.RLock()
	rules := make(map[string]rateLimitRule, len(a.rateLimits))
	for scope, rule := range a.rateLimits {
		rules[scope] = rule
	}
	a.rateMu.RUnlock()
	respond(w, 200, map[string]any{"rules": rules, "defaults": defaultRateLimits()})
}

func (a *App) updateRateLimits(w http.ResponseWriter, r *http.Request, p principal) {
	var input struct {
		Rules map[string]rateLimitRule `json:"rules"`
	}
	if !decode(w, r, &input) {
		return
	}
	if !validRateLimits(input.Rules) {
		fail(w, 400, "须提供全部五类限频规则，次数范围为 1–1000000，时间窗口为 1–86400 秒")
		return
	}
	raw, _ := json.Marshal(input.Rules)
	a.rateMu.Lock()
	if err := saveSetting(a.db.WithContext(r.Context()), "rate_limits", string(raw)); err != nil {
		a.rateMu.Unlock()
		internal(w, err)
		return
	}
	// 数据库保存成功后才切换配置，只清空发生变化的规则。
	for scope, rule := range input.Rules {
		if a.rateLimits[scope] != rule {
			limit := a.rateLimiter(scope)
			limit.mu.Lock()
			limit.entries = nil
			limit.nextCleanup = time.Time{}
			limit.mu.Unlock()
		}
	}
	a.rateLimits = input.Rules
	a.rateMu.Unlock()
	a.getRateLimits(w, r, p)
}

func (a *App) loadRateLimits(raw string) error {
	var rules map[string]rateLimitRule
	if err := json.Unmarshal([]byte(raw), &rules); err != nil || !validRateLimits(rules) {
		return errors.New("限频策略配置无效，请检查 settings 中的 rate_limits")
	}
	a.rateLimits = rules
	return nil
}
