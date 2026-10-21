package app

import (
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

func newFrontend(directory string) (http.Handler, error) {
	if directory == "" {
		return nil, nil
	}
	root, err := filepath.Abs(directory)
	if err != nil {
		return nil, err
	}
	index, err := os.OpenInRoot(root, "index.html")
	if err != nil {
		return nil, fmt.Errorf("无法读取 QPIX_WEB_ROOT/index.html: %w", err)
	}
	info, err := index.Stat()
	index.Close()
	if err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("QPIX_WEB_ROOT/index.html 必须是普通文件")
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.Trim(strings.TrimPrefix(r.URL.Path, "/"), "/")
		// API 和图片路径不能回退为 HTML，避免掩盖接口错误和图片权限检查。
		for _, prefix := range []string{"api", "i", "si"} {
			if name == prefix || strings.HasPrefix(name, prefix+"/") {
				http.NotFound(w, r)
				return
			}
		}
		if name == "" {
			name = "index.html"
		}
		if !fs.ValidPath(name) {
			http.NotFound(w, r)
			return
		}
		for _, segment := range strings.Split(name, "/") {
			if strings.HasPrefix(segment, ".") {
				http.NotFound(w, r)
				return
			}
		}
		// OpenInRoot 同时约束 .. 和符号链接，静态站点不能读取目录外文件。
		file, err := os.OpenInRoot(root, name)
		if err != nil && os.IsNotExist(err) && !strings.HasPrefix(name, "assets/") && path.Ext(name) == "" {
			name = "index.html"
			file, err = os.OpenInRoot(root, name)
		}
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		if strings.HasPrefix(name, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		http.ServeContent(w, r, name, info.ModTime(), file)
	}), nil
}
