package app

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"gorm.io/gorm"
	"qpix/internal/database"
)

// Maintain 清理过期会话并重试未完成的文件删除，状态保存在数据库中，进程重启后也不会丢失。
func (a *App) Maintain(ctx context.Context) {
	db := a.db.WithContext(ctx)
	cutoff := time.Now().UTC()
	for _, model := range []any{&database.Session{}, &database.ShareSession{}} {
		if err := deleteExpiredSessions(db, model, cutoff); err != nil {
			slog.Error("清理过期会话失败", "model", fmt.Sprintf("%T", model), "error", err)
		}
	}
	a.cleanupDeleted(ctx)
	if err := database.MaintainSQLite(db); err != nil {
		slog.Error("SQLite 后台维护失败", "error", err)
	}
}

// 按主键小批删除，缩短 MySQL 行锁和 SQLite 唯一连接的单次占用时间。
// 修改密码、Cookie 策略等操作仍在各自事务中原子撤销会话。
func deleteExpiredSessions(db *gorm.DB, model any, cutoff time.Time) error {
	for {
		var ids []string
		if err := db.Model(model).Where("expires_at < ?", cutoff).Limit(1000).Pluck("id", &ids).Error; err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		if err := db.Where("id IN ? AND expires_at < ?", ids, cutoff).Delete(model).Error; err != nil {
			return err
		}
	}
}

func (a *App) cleanupDeleted(ctx context.Context) {
	db := a.db.WithContext(ctx)
	var pending []database.Image
	if err := db.Where("state = ?", "deleting").Find(&pending).Error; err != nil {
		slog.Error("查询待删除图片失败", "error", err)
		return
	}
	a.cleanupImages(ctx, pending)
}

// 只清理已标记删除的记录快照；相册删除传入本相册图片，后台任务传入全部待清理图片。
func (a *App) cleanupImages(ctx context.Context, pending []database.Image) {
	db := a.db.WithContext(ctx)
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
		if row.PreviewKey != "" {
			if err := store.Delete(ctx, row.PreviewKey); err != nil {
				slog.Error("重试删除预览图失败", "image_id", row.ID, "error", err)
				continue
			}
		}
		if err := a.removeImageRecord(db, row); err != nil {
			slog.Error("清理已删除图片记录失败", "image_id", row.ID, "error", err)
		}
	}
}

func (a *App) removeImageRecord(db *gorm.DB, row database.Image) error {
	// 只有实际删除记录的事务才能扣减用量，避免接口与后台重试并发时重复扣减。
	return db.Transaction(func(tx *gorm.DB) error {
		result := tx.Where("id = ? AND state = ?", row.ID, "deleting").Delete(&database.Image{})
		if result.Error != nil || result.RowsAffected == 0 {
			return result.Error
		}
		return tx.Model(&database.User{}).Where("id = ?", row.UserID).
			UpdateColumn("used_bytes", gorm.Expr("CASE WHEN used_bytes >= ? THEN used_bytes - ? ELSE 0 END", row.Size, row.Size)).Error
	})
}
