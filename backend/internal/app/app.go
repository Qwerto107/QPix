package app

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"net/url"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"qpix/internal/config"
	"qpix/internal/database"
	"qpix/internal/storage"
)

type App struct {
	cfg                         config.Config
	db                          *gorm.DB
	stores                      map[string]storage.Store
	processing                  chan struct{}
	loginLimits, uploadLimits   limiter
	passwordLimits, shareLimits limiter
	shareCreateLimits           limiter
	rateMu                      sync.RWMutex
	rateLimits                  map[string]rateLimitRule
	uploadMu                    sync.Mutex
	uploads                     map[string]int
	uploadCount                 int
	settingsMu                  sync.RWMutex
	publicURL                   string
	appURL                      string
	secureCookie                bool
	maxPixels                   int64
	maxUploadBytes              int64
	profiles                    map[string]database.StorageProfile
	storageUploads              map[string]int
	masterKey                   []byte
	defaultStorage              string
	processingSettings          processingSettings
	accessRules                 map[string]accessRule
	trustedProxies              []netip.Prefix
	policyMu                    sync.Mutex
	quotaMu                     sync.Mutex
	albumRulesMu                sync.Mutex
	albumRules                  map[string]cachedAlbumRule
	albumRulesCleanup           time.Time
	frontend                    http.Handler
}

type principal struct {
	Token          *tokenAuthorization
	TokenID        string
	TokenLastUsed  *time.Time
	TokenOperation string
	User           database.User
	Session        *database.Session
}
type protectedHandler func(http.ResponseWriter, *http.Request, principal)

func New(cfg config.Config, db *gorm.DB, stores map[string]storage.Store) (*App, error) {
	if stores["local"] == nil {
		return nil, errors.New("缺少本地存储驱动")
	}
	frontend, err := newFrontend(cfg.WebRoot)
	if err != nil {
		return nil, err
	}
	a := &App{cfg: cfg, db: db, stores: stores, processing: make(chan struct{}, 2), publicURL: cfg.PublicURL, maxUploadBytes: cfg.MaxUploadBytes}
	a.frontend = frontend
	a.appURL, a.secureCookie, a.maxPixels = cfg.AppURL, cfg.SecureCookie, cfg.MaxPixels
	var count int64
	if err := db.Model(&database.User{}).Count(&count).Error; err != nil {
		return nil, err
	}
	if count == 0 {
		if cfg.AdminPassword == "" || len(cfg.AdminPassword) > 72 {
			return nil, errors.New("首次启动请设置非空 QPIX_ADMIN_PASSWORD，最多 72 字节")
		}
		if strings.TrimSpace(cfg.AdminUsername) == "" || len(cfg.AdminUsername) > 64 {
			return nil, errors.New("管理员名称长度必须在 1–64 字节之间")
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(cfg.AdminPassword), bcrypt.DefaultCost)
		if err != nil {
			return nil, err
		}
		if err = db.Create(&database.User{ID: randomID(), Username: cfg.AdminUsername, PasswordHash: string(hash), Role: "admin", Enabled: true}).Error; err != nil {
			return nil, err
		}
	}
	if err := a.initPersonalAlbums(); err != nil {
		return nil, err
	}
	var setting database.Setting
	err = db.Where(map[string]any{"key": "public_url"}).First(&setting).Error
	if err == nil {
		a.publicURL = setting.Value
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if err = config.ValidateURL(a.publicURL); err != nil {
		return nil, err
	}
	if err = a.initRuntime(); err != nil {
		return nil, err
	}
	// 单实例启动时没有在途上传，清除异常退出遗留的容量预占记录。
	if err = db.Where("1 = 1").Delete(&database.UploadReservation{}).Error; err != nil {
		return nil, err
	}
	return a, nil
}

func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", a.health)
	mux.HandleFunc("POST /api/v1/auth/login", a.login)
	mux.HandleFunc("GET /api/v1/auth/me", a.protect(a.me, false))
	mux.HandleFunc("POST /api/v1/auth/logout", a.protect(a.logout, false))
	mux.HandleFunc("PUT /api/v1/auth/password", a.preparedWrite(a.changePassword, false, "write"))
	mux.HandleFunc("GET /api/v1/images", a.protect(a.listImages, true))
	mux.HandleFunc("POST /api/v1/images", a.protect(a.upload, true))
	mux.HandleFunc("PATCH /api/v1/images/{id}", a.secure(a.updateImage, true, "write"))
	mux.HandleFunc("POST /api/v1/images/batch-delete", a.secure(a.deleteImages, true, "write"))
	mux.HandleFunc("GET /api/v1/albums", a.protect(a.listAlbums, true))
	mux.HandleFunc("POST /api/v1/albums", a.secure(a.createAlbum, true, "write"))
	mux.HandleFunc("PATCH /api/v1/albums/{id}", a.secure(a.renameAlbum, true, "write"))
	mux.HandleFunc("DELETE /api/v1/albums/{id}", a.secure(a.deleteAlbum, true, "write"))
	mux.HandleFunc("PUT /api/v1/albums/{id}/access", a.secure(a.updateAlbumAccess, true, "write"))
	mux.HandleFunc("PUT /api/v1/albums/{id}/default", a.secure(a.setDefaultAlbum, true, "write"))
	mux.HandleFunc("POST /api/v1/images/batch-move", a.secure(a.moveImages, true, "write"))
	mux.HandleFunc("GET /api/v1/users", a.admin(a.listUsers, false))
	mux.HandleFunc("POST /api/v1/users", a.preparedWrite(a.saveUser, false, "admin-write"))
	mux.HandleFunc("PUT /api/v1/users/{id}", a.preparedWrite(a.saveUser, false, "admin-write"))
	mux.HandleFunc("DELETE /api/v1/users/{id}", a.admin(a.deleteUser, true))
	mux.HandleFunc("GET /api/v1/upload-options", a.protect(a.uploadOptions, true))
	mux.HandleFunc("GET /api/v1/shares", a.protect(a.listShares, true))
	mux.HandleFunc("POST /api/v1/shares", a.preparedWrite(a.createShare, true, "write"))
	mux.HandleFunc("DELETE /api/v1/shares/{id}", a.secure(a.revokeShare, true, "write"))
	mux.HandleFunc("DELETE /api/v1/shares/{id}/record", a.secure(a.deleteShareRecord, true, "write"))
	mux.HandleFunc("GET /api/v1/public-shares/{token}", a.publicShare)
	mux.HandleFunc("POST /api/v1/public-shares/{token}/unlock", a.unlockShare)
	mux.HandleFunc("GET /si/{token}/{file}", a.serveShareImage)
	mux.HandleFunc("GET /api/v1/tokens", a.protect(a.listTokens, false))
	mux.HandleFunc("POST /api/v1/tokens", a.secure(a.createToken, false, "write"))
	mux.HandleFunc("PUT /api/v1/tokens/{id}", a.secure(a.updateToken, false, "write"))
	mux.HandleFunc("DELETE /api/v1/tokens/{id}", a.secure(a.revokeToken, false, "write"))
	mux.HandleFunc("DELETE /api/v1/tokens/{id}/record", a.secure(a.deleteTokenRecord, false, "write"))
	mux.HandleFunc("GET /api/v1/stats", a.protect(a.stats, true))
	mux.HandleFunc("GET /api/v1/settings", a.admin(a.settings, false))
	mux.HandleFunc("PATCH /api/v1/settings", a.admin(a.updateSettings, true))
	mux.HandleFunc("GET /api/v1/storages", a.admin(a.listProfiles, false))
	mux.HandleFunc("POST /api/v1/storages", a.admin(a.createProfile, false))
	mux.HandleFunc("PUT /api/v1/storages/{id}", a.admin(a.updateProfile, false))
	mux.HandleFunc("DELETE /api/v1/storages/{id}", a.admin(a.deleteProfile, false))
	mux.HandleFunc("POST /api/v1/storages/{id}/default", a.admin(a.defaultProfile, false))
	mux.HandleFunc("POST /api/v1/storages/{id}/test", a.admin(a.testProfile, false))
	mux.HandleFunc("GET /api/v1/processing", a.admin(a.getProcessing, false))
	mux.HandleFunc("PUT /api/v1/processing", a.admin(a.updateProcessing, false))
	mux.HandleFunc("GET /api/v1/rate-limits", a.admin(a.getRateLimits, false))
	mux.HandleFunc("PUT /api/v1/rate-limits", a.admin(a.updateRateLimits, false))
	mux.HandleFunc("GET /api/v1/access", a.admin(a.listAccess, false))
	mux.HandleFunc("PUT /api/v1/access/{scope}", a.admin(a.updateAccess, false))
	mux.HandleFunc("GET /i/{file}", a.serveImage)
	if a.frontend != nil {
		mux.Handle("GET /", a.frontend)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				slog.Error("请求处理异常", "error", recovered, "stack", string(debug.Stack()))
				fail(w, 500, "服务暂时不可用")
			}
		}()
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' blob: http: https:; connect-src 'self'; object-src 'none'; base-uri 'self'; frame-ancestors 'none'")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		if !a.enforceAccess(w, r) {
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" && !a.sameOrigin(r) {
			fail(w, 403, "请求来源不被允许")
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func (a *App) sameOrigin(r *http.Request) bool {
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	a.settingsMu.RLock()
	appURL := a.appURL
	a.settingsMu.RUnlock()
	if appURL != "" {
		return origin == appURL
	}
	u, err := url.Parse(origin)
	return err == nil && u.Host == r.Host && (u.Scheme == "http" || u.Scheme == "https")
}

func (a *App) protect(next protectedHandler, allowToken bool) http.HandlerFunc {
	return a.secure(next, allowToken, "any")
}

func (a *App) admin(next protectedHandler, write bool) http.HandlerFunc {
	mode := "admin"
	if write {
		mode = "admin-write"
	}
	return a.secure(next, false, mode)
}

func (a *App) secure(next protectedHandler, allowToken bool, mode string) http.HandlerFunc {
	return a.secureWithPolicy(next, allowToken, mode, true)
}

// 昂贵的密码计算由处理函数在锁外完成，写入前必须调用 lockWrite 重新鉴权。
func (a *App) preparedWrite(next protectedHandler, allowToken bool, mode string) http.HandlerFunc {
	return a.secureWithPolicy(next, allowToken, mode, false)
}

func (a *App) secureWithPolicy(next protectedHandler, allowToken bool, mode string, autoLock bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := a.authenticate(w, r, allowToken, mode)
		if !ok {
			return
		}
		// 上传自行在提交阶段加锁；其他写接口先在锁外完成有界请求体接收。
		upload := r.Method == "POST" && r.URL.Path == "/api/v1/images"
		if r.Method != "GET" && r.Method != "HEAD" && !upload {
			if !bufferJSONBody(w, r) {
				return
			}
			if autoLock {
				locked, fresh, ok := a.lockWrite(w, r, allowToken, mode)
				if !ok {
					return
				}
				defer locked.release()
				w, p = locked, fresh
			}
		}
		if p.Token != nil {
			now := time.Now().UTC()
			cutoff := now.Add(-time.Minute)
			// 鉴权查询已取得上次使用时刻；大多数请求无需额外写库，并发首次使用由条件更新去重。
			if p.TokenLastUsed == nil || !p.TokenLastUsed.After(cutoff) {
				if err := a.db.WithContext(r.Context()).Model(&database.APIToken{}).
					Where("id = ? AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > ?)", p.TokenID, now).
					Where("last_used_at IS NULL OR last_used_at <= ?", cutoff).Update("last_used_at", now).Error; err != nil {
					internal(w, err)
					return
				}
			}
		}
		next(w, r, p)
	}
}

func (a *App) authenticate(w http.ResponseWriter, r *http.Request, allowToken bool, mode string) (principal, bool) {
	db := a.db.WithContext(r.Context())
	var p principal
	if header := r.Header.Get("Authorization"); header != "" {
		if !allowToken || !strings.HasPrefix(header, "Bearer ") {
			fail(w, 401, "此接口不接受该凭据")
			return p, false
		}
		if !a.authenticateToken(w, r, &p) {
			return p, false
		}
	} else {
		cookie, err := r.Cookie("qpix_session")
		if err != nil {
			fail(w, 401, "请先登录")
			return p, false
		}
		var session database.Session
		if err = db.First(&session, "id = ? AND expires_at > ?", digest(cookie.Value), time.Now().UTC()).Error; err != nil {
			authError(w, err, "登录已失效，请重新登录")
			return p, false
		}
		if err = db.First(&p.User, "id = ?", session.UserID).Error; err != nil {
			authError(w, err, "账号不存在")
			return p, false
		}
		if r.Method != "GET" && r.Method != "HEAD" && subtle.ConstantTimeCompare([]byte(session.CSRF), []byte(r.Header.Get("X-CSRF-Token"))) != 1 {
			fail(w, 403, "安全校验失败，请刷新页面")
			return p, false
		}
		p.Session = &session
	}
	if !p.User.Enabled {
		fail(w, 401, "账号已停用")
		return p, false
	}
	if strings.HasPrefix(mode, "admin") && p.User.Role != "admin" {
		fail(w, 403, "此账号没有操作权限")
		return p, false
	}
	if p.Token != nil && !authorizeTokenRoute(w, r, &p) {
		return p, false
	}
	if !(r.Method == "POST" && r.URL.Path == "/api/v1/images") && !strings.HasPrefix(r.URL.Path, "/i/") {
		if p.User.Role == "admin" && p.Token == nil {
			if !a.enforceScope(w, r, "admin") {
				return p, false
			}
		}
	}
	return p, true
}

func authError(w http.ResponseWriter, err error, text string) {
	// 数据库暂时不可用不等于会话失效，避免前端因此清空有效登录。
	if errors.Is(err, gorm.ErrRecordNotFound) {
		fail(w, 401, text)
	} else {
		internal(w, err)
	}
}

func (a *App) health(w http.ResponseWriter, r *http.Request) {
	pool, err := a.db.DB()
	if err != nil || pool.PingContext(r.Context()) != nil {
		fail(w, 503, "数据库不可用")
		return
	}
	respond(w, 200, map[string]string{"status": "ok"})
}

func decode(w http.ResponseWriter, r *http.Request, dest any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dest); err != nil {
		fail(w, 400, "请求内容无效")
		return false
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		fail(w, 400, "请求必须包含一个 JSON 对象")
		return false
	}
	return true
}

func respond(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		slog.Debug("响应写入中断", "error", err)
	}
}
func fail(w http.ResponseWriter, status int, message string) {
	respond(w, status, map[string]string{"error": message})
}
func internal(w http.ResponseWriter, err error) {
	slog.Error("操作失败", "error", err)
	fail(w, 500, "操作失败，请稍后重试")
}
func randomID() string { return randomHex(16) }
func randomHex(size int) string {
	b := make([]byte, size)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Errorf("随机数生成失败：%w", err))
	}
	return hex.EncodeToString(b)
}
func digest(value string) string {
	result := sha256.Sum256([]byte(value))
	return hex.EncodeToString(result[:])
}

type window struct {
	count int
	until time.Time
}
type limiter struct {
	mu          sync.Mutex
	entries     map[string]window
	nextCleanup time.Time
}

// retry 返回需要等待的时长；零表示本次请求已获准。
func (l *limiter) retry(key string, limit int, duration time.Duration) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.entries == nil {
		l.entries = make(map[string]window)
	}
	now := time.Now()
	if !now.Before(l.nextCleanup) {
		for key, entry := range l.entries {
			if !entry.until.After(now) {
				delete(l.entries, key)
			}
		}
		l.nextCleanup = now.Add(time.Minute)
	}
	entry, exists := l.entries[key]
	// 达到容量时只拒绝新来源，不能使已有来源全部失效，也不能淘汰仍在限流的记录。
	if !exists && len(l.entries) >= 10000 {
		return l.nextCleanup.Sub(now)
	}
	if !entry.until.After(now) {
		entry = window{until: now.Add(duration)}
	}
	if entry.count >= limit {
		return entry.until.Sub(now)
	}
	entry.count++
	l.entries[key] = entry
	return 0
}
