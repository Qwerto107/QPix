//go:build with_frontend

package webui

import (
	"embed"
	"io/fs"
)

// 构建前仅复制前端产物，不能将源码、配置或上传文件嵌入程序。
//
//go:embed all:dist
var files embed.FS

func Files() fs.FS {
	root, err := fs.Sub(files, "dist")
	if err != nil {
		panic(err)
	}
	return root
}
