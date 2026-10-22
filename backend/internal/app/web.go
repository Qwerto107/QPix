package app

import (
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"qpix/internal/webui"
)

func newFrontend(directory string) (http.Handler, error) {
	var open func(string) (fs.File, error)
	if directory != "" {
		root, err := filepath.Abs(directory)
		if err != nil {
			return nil, err
		}
		// 外置目录仍限制符号链接边界，不能读取目录之外的文件。
		open = func(name string) (fs.File, error) { return os.OpenInRoot(root, name) }
	} else if embedded := webui.Files(); embedded != nil {
		open = embedded.Open
	} else {
		return nil, nil
	}
	index, err := open("index.html")
	if err != nil {
		return nil, fmt.Errorf("无法读取前端 index.html: %w", err)
	}
	info, err := index.Stat()
	index.Close()
	if err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("前端 index.html 必须是普通文件")
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
		file, err := open(name)
		if err != nil && os.IsNotExist(err) && !strings.HasPrefix(name, "assets/") && path.Ext(name) == "" {
			name = "index.html"
			file, err = open(name)
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
		content, ok := file.(io.ReadSeeker)
		if !ok {
			http.Error(w, "无法读取前端资源", http.StatusInternalServerError)
			return
		}
		http.ServeContent(w, r, name, info.ModTime(), content)
	}), nil
}
