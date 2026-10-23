package app

import (
	"encoding/json"
	"time"

	"qpix/internal/database"
)

type cachedAlbumRule struct {
	referers   string
	allowEmpty bool
	rule       accessRule
	expires    time.Time
}

func (a *App) compiledAlbumRule(album database.Album) (accessRule, error) {
	now := time.Now()
	a.albumRulesMu.Lock()
	cached, exists := a.albumRules[album.ID]
	if exists && now.Before(cached.expires) && cached.referers == album.Referers && cached.allowEmpty == album.AllowEmptyReferer {
		a.albumRulesMu.Unlock()
		return cached.rule, nil
	}
	a.albumRulesMu.Unlock()
	var refs []string
	if err := json.Unmarshal([]byte(album.Referers), &refs); err != nil {
		return accessRule{}, err
	}
	rule, err := compileRule(ruleInput{Scope: "images", RefererEnabled: true, Referers: refs, AllowEmptyReferer: album.AllowEmptyReferer})
	if err != nil {
		return rule, err
	}
	a.albumRulesMu.Lock()
	defer a.albumRulesMu.Unlock()
	if a.albumRules == nil {
		a.albumRules = make(map[string]cachedAlbumRule)
	}
	if !now.Before(a.albumRulesCleanup) {
		for id, entry := range a.albumRules {
			if !now.Before(entry.expires) {
				delete(a.albumRules, id)
			}
		}
		a.albumRulesCleanup = now.Add(time.Minute)
	}
	// 仅缓存编译结果，始终与本次查询的规则内容比对；容量满时不缓存新相册，不影响鉴权。
	if _, exists := a.albumRules[album.ID]; exists || len(a.albumRules) < 1024 {
		a.albumRules[album.ID] = cachedAlbumRule{referers: album.Referers, allowEmpty: album.AllowEmptyReferer, rule: rule, expires: now.Add(time.Minute)}
	}
	return rule, nil
}

func (a *App) invalidateAlbumRule(id string) {
	a.albumRulesMu.Lock()
	delete(a.albumRules, id)
	a.albumRulesMu.Unlock()
}
