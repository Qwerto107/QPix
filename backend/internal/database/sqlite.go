package database

import (
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"gorm.io/gorm"
)

func sqliteDSN(path string) (string, error) {
	fullPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("解析 SQLite 文件路径失败：%w", err)
	}
	if err := os.MkdirAll(filepath.Dir(fullPath), 0700); err != nil {
		return "", fmt.Errorf("创建 SQLite 数据目录失败：%w", err)
	}
	// 对路径编码后再添加内部参数，文件名中的 #、% 等字符不能被解释为 URI 配置。
	uriPath := filepath.ToSlash(fullPath)
	if !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath // Windows 盘符使用 file:///C:/... 形式。
	}
	params := url.Values{
		"_pragma":   {"busy_timeout(5000)", "foreign_keys(1)", "synchronous(FULL)", "wal_autocheckpoint(1000)"},
		"_timezone": {"UTC"},
	}
	// 连接级参数由驱动在每次建连时应用，坏连接被替换后也不能丢失外键和等待策略。
	return (&url.URL{Scheme: "file", Path: uriPath, RawQuery: params.Encode()}).String(), nil
}

func optimizeSQLite(db *gorm.DB, startup bool) error {
	if db.Dialector.Name() != "sqlite" {
		return nil
	}
	query := "PRAGMA optimize"
	if startup {
		query = "PRAGMA optimize=0x10002"
	}
	if err := db.Exec(query).Error; err != nil {
		return fmt.Errorf("更新 SQLite 查询统计失败：%w", err)
	}
	return nil
}

// MaintainSQLite 复用应用的维护任务，不新增定时器，不执行会阻塞读写的强制收缩。
func MaintainSQLite(db *gorm.DB) error {
	if db.Dialector.Name() != "sqlite" {
		return nil
	}
	if err := optimizeSQLite(db, false); err != nil {
		return err
	}
	var busy, frames, checkpointed int
	if err := db.Raw("PRAGMA wal_checkpoint(PASSIVE)").Row().Scan(&busy, &frames, &checkpointed); err != nil {
		return fmt.Errorf("执行 SQLite WAL 检查点失败：%w", err)
	}
	// 外部读事务可使部分日志暂时保留；PASSIVE 不等待，留给后续维护继续处理。
	if busy != 0 || frames > checkpointed {
		slog.Debug("SQLite WAL 检查点暂未完成", "busy", busy, "frames", frames, "checkpointed", checkpointed)
	}
	return nil
}
