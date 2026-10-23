package app

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strings"

	"gorm.io/gorm/clause"
	"qpix/internal/database"
)

type ruleInput struct {
	Scope             string   `json:"scope"`
	IPEnabled         bool     `json:"ip_enabled"`
	IPs               []string `json:"ips"`
	RefererEnabled    bool     `json:"referer_enabled"`
	Referers          []string `json:"referers"`
	AllowEmptyReferer bool     `json:"allow_empty_referer"`
}
type accessRule struct {
	input    ruleInput
	networks []netip.Prefix
}

func parseNetwork(value string) (netip.Prefix, error) {
	if address, err := netip.ParseAddr(value); err == nil {
		address = address.Unmap()
		return netip.PrefixFrom(address, address.BitLen()), nil
	}
	prefix, err := netip.ParsePrefix(value)
	if err != nil || prefix.Addr().Is4In6() {
		return netip.Prefix{}, errors.New("请填写 IPv4、IPv6 或 CIDR，不要包含端口")
	}
	return prefix.Masked(), nil
}

var domainPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)*$`)

func compileRule(input ruleInput) (accessRule, error) {
	result := accessRule{input: input}
	if input.Scope == "upload" && (input.RefererEnabled || len(input.Referers) > 0) {
		return result, errors.New("上传接口只支持 IP 限制，不支持 Referer 配置")
	}
	if input.Scope != "admin" && input.Scope != "upload" && input.Scope != "images" {
		return result, errors.New("规则范围必须是 admin、upload 或 images")
	}
	if len(input.IPs) > 200 || len(input.Referers) > 200 {
		return result, errors.New("每类规则最多允许 200 项")
	}
	result.input.IPs, result.input.Referers = []string{}, []string{}
	for _, raw := range input.IPs {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		network, err := parseNetwork(value)
		if err != nil {
			return result, err
		}
		result.networks = append(result.networks, network)
		result.input.IPs = append(result.input.IPs, network.String())
	}
	for _, raw := range input.Referers {
		value := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(raw)), ".")
		if value == "" {
			continue
		}
		host := strings.TrimPrefix(value, "*.")
		if len(host) > 253 || !(domainPattern.MatchString(host) || validIPHost(host)) || (strings.HasPrefix(value, "*.") && validIPHost(host)) {
			return result, errors.New("Referer 请填写域名或 *.example.com，不包含协议、端口或路径；IPv6 不带方括号")
		}
		if address, err := netip.ParseAddr(value); err == nil {
			value = address.Unmap().String()
		}
		result.input.Referers = append(result.input.Referers, value)
	}
	if input.IPEnabled && len(result.networks) == 0 {
		return result, errors.New("启用 IP 白名单时至少填写一个 IP 或网段")
	}
	if input.RefererEnabled && len(result.input.Referers) == 0 {
		return result, errors.New("启用 Referer 白名单时至少填写一个域名")
	}
	return result, nil
}

func validIPHost(value string) bool { _, err := netip.ParseAddr(value); return err == nil }
func refererHost(value string) string {
	u, err := url.Parse(value)
	if err != nil || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if address, err := netip.ParseAddr(host); err == nil {
		return address.Unmap().String()
	}
	return host
}

func (rule accessRule) allows(address netip.Addr, referer string) bool {
	if rule.input.IPEnabled {
		found := false
		for _, network := range rule.networks {
			if address.IsValid() && network.Contains(address.Unmap()) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if !rule.input.RefererEnabled {
		return true
	}
	if referer == "" {
		return rule.input.AllowEmptyReferer
	}
	host := refererHost(referer)
	if host == "" {
		return false
	}
	for _, domain := range rule.input.Referers {
		if strings.HasPrefix(domain, "*.") {
			if strings.HasSuffix(host, "."+strings.TrimPrefix(domain, "*.")) {
				return true
			}
		} else if host == domain {
			return true
		}
	}
	return false
}

func (a *App) trusted(address netip.Addr) bool {
	a.settingsMu.RLock()
	defer a.settingsMu.RUnlock()
	for _, network := range a.trustedProxies {
		if network.Contains(address) {
			return true
		}
	}
	return false
}
func (a *App) requesterIP(r *http.Request) netip.Addr {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	address, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}
	}
	address = address.Unmap()
	if !a.trusted(address) {
		return address
	}
	forwarded := r.Header.Get("X-Forwarded-For")
	if forwarded == "" {
		return address
	}
	chain := strings.Split(forwarded, ",")
	// 只有当前一跳可信才继续向左解析，忽略客户端自行伪造的更早前缀。
	for i := len(chain) - 1; i >= 0 && a.trusted(address); i-- {
		address, err = netip.ParseAddr(strings.TrimSpace(chain[i]))
		if err != nil {
			return netip.Addr{}
		}
		address = address.Unmap()
	}
	return address
}

func (a *App) limiterIP(r *http.Request) string {
	address := a.requesterIP(r)
	// IPv6 按 /64 计数，避免同一网段轮换地址快速填满限流表。
	if address.Is6() {
		return netip.PrefixFrom(address, 64).Masked().String()
	}
	return address.String()
}

func requestScope(r *http.Request) string {
	// 统一认证入口在识别真实账号角色后检查网络规则，不能预先套用管理员规则。
	if strings.HasPrefix(r.URL.Path, "/api/v1/auth/") {
		return ""
	}
	if strings.HasPrefix(r.URL.Path, "/si/") || strings.HasPrefix(r.URL.Path, "/api/v1/public-shares/") {
		return ""
	}
	if strings.HasPrefix(r.URL.Path, "/i/") {
		return "images"
	}
	if r.URL.Path == "/api/v1/images" && r.Method == "POST" {
		return "upload"
	}
	for _, path := range []string{"/api/v1/images", "/api/v1/albums", "/api/v1/tokens", "/api/v1/shares", "/api/v1/stats", "/api/v1/upload-options"} {
		if r.URL.Path == path || strings.HasPrefix(r.URL.Path, path+"/") {
			return ""
		}
	}
	if strings.HasPrefix(r.URL.Path, "/api/") && r.URL.Path != "/api/v1/health" {
		return "admin"
	}
	return ""
}

func (a *App) enforceAccess(w http.ResponseWriter, r *http.Request) bool {
	if a.cfg.DisableAccessRules {
		return true
	}
	scope := requestScope(r)
	if scope == "" {
		return true
	}
	// 图片查到所属相册后才计算 Referer 覆盖，最外层保留全局 IP 限制。
	if strings.HasPrefix(r.URL.Path, "/i/") {
		a.settingsMu.RLock()
		rule := a.accessRules["images"]
		a.settingsMu.RUnlock()
		rule.input.RefererEnabled = false
		if !rule.allows(a.requesterIP(r), r.Header.Get("Referer")) {
			fail(w, 403, "请求不满足图片 IP 规则")
			return false
		}
		return true
	}
	return a.enforceScope(w, r, scope)
}

func (a *App) enforceScope(w http.ResponseWriter, r *http.Request, scope string) bool {
	if a.cfg.DisableAccessRules {
		return true
	}
	a.settingsMu.RLock()
	rule := a.accessRules[scope]
	a.settingsMu.RUnlock()
	if scope == "images" && (rule.input.IPEnabled || rule.input.RefererEnabled) {
		w.Header().Set("Cache-Control", "private, no-store")
	}
	if !rule.allows(a.requesterIP(r), r.Header.Get("Referer")) {
		fail(w, 403, "请求不满足访问规则（"+scope+"）")
		return false
	}
	return true
}

func (a *App) listAccess(w http.ResponseWriter, r *http.Request, p principal) {
	a.settingsMu.RLock()
	items := []ruleInput{}
	for _, scope := range []string{"admin", "upload", "images"} {
		rule := a.accessRules[scope].input
		items = append(items, rule)
	}
	a.settingsMu.RUnlock()
	address := a.requesterIP(r)
	ip := ""
	if address.IsValid() {
		ip = address.String()
	}
	respond(w, 200, map[string]any{"items": items, "requester_ip": ip, "referer_host": refererHost(r.Header.Get("Referer")), "bypassed": a.cfg.DisableAccessRules})
}

func (a *App) updateAccess(w http.ResponseWriter, r *http.Request, p principal) {
	var input ruleInput
	if !decode(w, r, &input) {
		return
	}
	if input.Scope != "" && input.Scope != r.PathValue("scope") {
		fail(w, 400, "规则范围不一致")
		return
	}
	input.Scope = r.PathValue("scope")
	rule, err := compileRule(input)
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	if input.Scope == "admin" && !rule.allows(a.requesterIP(r), r.Header.Get("Referer")) {
		fail(w, 400, "此规则会阻止当前管理请求，请先允许当前 IP 和 Referer")
		return
	}
	ips, _ := json.Marshal(rule.input.IPs)
	referers, _ := json.Marshal(rule.input.Referers)
	row := database.AccessRule{Scope: input.Scope, IPEnabled: input.IPEnabled, IPs: string(ips), RefererEnabled: input.RefererEnabled, Referers: string(referers), AllowEmptyReferer: input.AllowEmptyReferer}
	a.settingsMu.Lock()
	if err = a.db.WithContext(r.Context()).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "scope"}}, DoUpdates: clause.AssignmentColumns([]string{"ip_enabled", "ips", "referer_enabled", "referers", "allow_empty_referer"})}).Create(&row).Error; err != nil {
		a.settingsMu.Unlock()
		internal(w, err)
		return
	}
	a.accessRules[input.Scope] = rule
	a.settingsMu.Unlock()
	respond(w, 200, rule.input)
}
