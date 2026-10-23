package app

import (
	"bytes"
	"io"
	"net/http"
	"sync"
)

// 策略锁只覆盖重新鉴权与数据库修改，发送响应和存储 I/O 前必须释放。
type policyResponseWriter struct {
	http.ResponseWriter
	release func()
}

func (a *App) lockPolicy(w http.ResponseWriter) *policyResponseWriter {
	a.policyMu.Lock()
	return &policyResponseWriter{ResponseWriter: w, release: sync.OnceFunc(a.policyMu.Unlock)}
}

func (a *App) lockWrite(w http.ResponseWriter, r *http.Request, allowToken bool, mode string) (*policyResponseWriter, principal, bool) {
	locked := a.lockPolicy(w)
	// 接收请求体、计算密码及等待锁时，账号或凭据可能已变化，写入前读取最新状态。
	p, ok := a.authenticate(locked, r, allowToken, mode)
	if ok && !a.sameOrigin(r) {
		fail(locked, 403, "请求来源不被允许")
		ok = false
	}
	if !ok {
		locked.release()
	}
	return locked, p, ok
}

func (w *policyResponseWriter) WriteHeader(status int) {
	w.release()
	w.ResponseWriter.WriteHeader(status)
}

func (w *policyResponseWriter) Write(data []byte) (int, error) {
	w.release()
	return w.ResponseWriter.Write(data)
}

func (w *policyResponseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func releasePolicy(w http.ResponseWriter) {
	if locked, ok := w.(*policyResponseWriter); ok {
		locked.release()
	}
}

func bufferJSONBody(w http.ResponseWriter, r *http.Request) bool {
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<10))
	if err != nil {
		fail(w, 400, "请求内容无效或超出大小限制")
		return false
	}
	r.Body = io.NopCloser(bytes.NewReader(data))
	return true
}
