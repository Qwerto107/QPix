//go:build !with_frontend

package webui

import "io/fs"

// 分离开发时由 Vite 提供页面，不要求预先生成前端资源。
func Files() fs.FS { return nil }
