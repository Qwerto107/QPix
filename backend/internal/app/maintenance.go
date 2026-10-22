package app

import (
	"context"
	"log/slog"
	"time"

	"qpix/internal/database"
)

// Maintain 清理过期会话并重试未完成的文件删除，状态保存在数据库中，进程重启后也不会丢失。
func (a *App) Maintain(ctx context.Context) {
	a.policyMu.RLock()
	defer a.policyMu.RUnlock()
	db := a.db.WithContext(ctx)
	if err := db.Where("expires_at < ?", time.Now().UTC()).Delete(&database.Session{}).Error; err != nil {
		slog.Error("清理过期会话失败", "error", err)
	}
	if err := db.Where("expires_at < ?", time.Now().UTC()).Delete(&database.ShareSession{}).Error; err != nil {
		slog.Error("清理过期分享会话失败", "error", err)
	}
	a.cleanupDeleted(ctx)
}

func (a *App) cleanupDeleted(ctx context.Context) {
	db := a.db.WithContext(ctx)
	var pending []database.Image
	if err := db.Where("state = ?", "deleting").Find(&pending).Error; err != nil {
		slog.Error("查询待删除图片失败", "error", err)
		return
	}
	for _, row := range pending {
		if ctx.Err() != nil {
			return
		}
		store := a.getStore(row.StorageID)
		if store == nil {
			slog.Error("待删除图片的存储驱动不可用", "image_id", row.ID, "storage_id", row.StorageID)
			continue
		}
		if err := store.Delete(ctx, row.Key); err != nil {
			slog.Error("重试删除原图失败", "image_id", row.ID, "error", err)
			continue
		}
		if row.ThumbKey != "" {
			if err := store.Delete(ctx, row.ThumbKey); err != nil {
				slog.Error("重试删除缩略图失败", "image_id", row.ID, "error", err)
				continue
			}
		}
		if err := db.Delete(&row).Error; err != nil {
			slog.Error("清理已删除图片记录失败", "image_id", row.ID, "error", err)
		}
	}
}
