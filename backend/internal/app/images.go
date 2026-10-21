package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
	"gorm.io/gorm"
	"qpix/internal/database"
	"qpix/internal/processing"
)

type imageView struct {
	database.Image
	URL          string `json:"url"`
	ThumbnailURL string `json:"thumbnail_url"`
	OwnerID      string `json:"owner_id,omitempty"`
	CanManage    bool   `json:"can_manage"`
}

func (a *App) imageView(row database.Image) imageView {
	a.settingsMu.RLock()
	base := a.publicURL
	a.settingsMu.RUnlock()
	u := base + "/i/" + row.ID + "." + row.Format
	thumb := u
	if row.ThumbKey != "" {
		thumb = base + "/i/" + row.ID + "-thumb.png"
	}
	return imageView{Image: row, URL: u, ThumbnailURL: thumb}
}

func (a *App) listImages(w http.ResponseWriter, r *http.Request, p principal) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	if page > 1_000_000 {
		fail(w, 400, "页码超出范围")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit < 1 {
		limit = 36
	}
	if limit > 100 {
		limit = 100
	}
	q := a.visibleImages(a.db.WithContext(r.Context()).Model(&database.Image{}), p).Where("state = ?", "ready")
	if r.URL.Query().Get("mine") == "true" {
		q = q.Where("user_id = ?", p.User.ID)
	}
	if owner := r.URL.Query().Get("user_id"); owner != "" && p.User.Role == "admin" {
		q = q.Where("user_id = ?", owner)
	}
	if album := r.URL.Query().Get("album_id"); album != "" {
		if album == "unfiled" {
			q = q.Where("album_id IS NULL")
		} else {
			q = q.Where("album_id = ?", album)
		}
	}
	if search := strings.TrimSpace(r.URL.Query().Get("q")); search != "" {
		if utf8.RuneCountInString(search) > 255 {
			fail(w, 400, "搜索内容过长")
			return
		}
		escaped := strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(strings.ToLower(search))
		q = q.Where("LOWER(name) LIKE ? ESCAPE '!'", "%"+escaped+"%")
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		internal(w, err)
		return
	}
	var rows []database.Image
	if err := q.Order("created_at DESC").Order("id DESC").Offset((page - 1) * limit).Limit(limit).Find(&rows).Error; err != nil {
		internal(w, err)
		return
	}
	items := make([]imageView, 0, len(rows))
	for _, row := range rows {
		items = append(items, a.viewFor(row, p))
	}
	respond(w, 200, map[string]any{"items": items, "total": total, "page": page, "limit": limit})
}

func (a *App) upload(w http.ResponseWriter, r *http.Request, p principal) {
	if !a.uploadLimits.allow(p.User.ID, 120, time.Minute) {
		w.Header().Set("Retry-After", "60")
		fail(w, 429, "上传过于频繁，请稍后再试")
		return
	}
	// 同时限制接收与解码数量，避免大图并发放大内存和临时磁盘开销。
	select {
	case a.processing <- struct{}{}:
		defer func() { <-a.processing }()
	default:
		w.Header().Set("Retry-After", "2")
		fail(w, 429, "图片处理中，请稍后重试")
		return
	}
	limit := a.uploadLimit()
	if p.User.MaxFileBytes > 0 {
		limit = min(limit, p.User.MaxFileBytes)
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit+(64<<10))
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		if r.MultipartForm != nil {
			r.MultipartForm.RemoveAll()
		}
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			fail(w, 413, "上传文件超出大小限制")
		} else {
			fail(w, 400, "请使用 multipart/form-data 上传图片")
		}
		return
	}
	defer r.MultipartForm.RemoveAll()
	if len(r.MultipartForm.File) != 1 || len(r.MultipartForm.File["file"]) != 1 {
		fail(w, 400, "每个请求应包含一张图片，字段名为 file")
		return
	}
	header := r.MultipartForm.File["file"][0]
	if header.Size <= 0 || header.Size > limit {
		fail(w, 413, "图片为空或超出大小限制")
		return
	}
	reservation, err := a.reserveUpload(r, p.User.ID, header.Size)
	if err != nil {
		if errors.Is(err, errFileLimit) {
			fail(w, 413, err.Error())
		} else if errors.Is(err, errQuota) {
			fail(w, 409, err.Error())
		} else {
			internal(w, err)
		}
		return
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := a.db.WithContext(ctx).Delete(&database.UploadReservation{}, "id = ?", reservation).Error; err != nil {
			slog.Error("释放上传预占失败", "error", err)
		}
	}()
	name := strings.TrimSpace(header.Filename)
	if name == "" || utf8.RuneCountInString(name) > 255 {
		fail(w, 400, "图片名称长度必须在 1–255 个字符之间")
		return
	}
	var albumID *string
	if value := r.FormValue("album_id"); value != "" {
		if !a.albumWritable(r, p, value) {
			fail(w, 400, "相册不存在或没有上传权限")
			return
		}
		albumID = &value
	}
	file, err := header.Open()
	if err != nil {
		internal(w, err)
		return
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		internal(w, err)
		return
	}
	info, format, err := processing.Config(data)
	if err != nil || (format != "jpeg" && format != "png" && format != "gif" && format != "webp") {
		fail(w, 415, "仅支持有效的 JPEG、PNG、GIF、静态 WebP 图片")
		return
	}
	if info.Width <= 0 || info.Height <= 0 || int64(info.Width) > a.cfg.MaxPixels/int64(info.Height) {
		fail(w, 413, "图片像素超出限制（最多 2500 万像素）")
		return
	}
	decoded, err := processing.Decode(data, format)
	if err != nil {
		fail(w, 415, "图片内容不完整或格式暂不支持")
		return
	}
	if int64(decoded.Bounds().Dx())*int64(decoded.Bounds().Dy()) != int64(info.Width)*int64(info.Height) {
		fail(w, 415, "图片尺寸校验失败")
		return
	}
	a.settingsMu.RLock()
	locked := true
	defer func() {
		if locked {
			a.settingsMu.RUnlock()
		}
	}()
	storageID := r.FormValue("storage_id")
	if storageID == "" {
		storageID = a.defaultStorage
	}
	profile, exists := a.profiles[storageID]
	if !exists || !profile.Enabled {
		fail(w, 400, "存储不存在或已停用")
		return
	}
	mode := r.FormValue("compression")
	if mode == "" || mode == "default" {
		mode = a.processingSettings.Mode
	}
	quality := a.processingSettings.Quality
	if values, present := r.MultipartForm.Value["quality"]; present {
		if len(values) != 1 {
			fail(w, 400, "画质必须为 1–100 的整数")
			return
		}
		quality, err = strconv.Atoi(values[0])
		if err != nil {
			fail(w, 400, "画质必须为 1–100 的整数")
			return
		}
	}
	if !validProcessing(processingSettings{Mode: mode, Quality: quality}) {
		fail(w, 400, "压缩模式必须为 default、none、preserve 或 webp，画质必须为 1–100 的整数")
		return
	}
	status := "original"
	if mode != "none" {
		result := processing.Optimize(data, decoded, format, mode, quality)
		data, format, status = result.Data, result.Format, result.Status
		if status == "optimized" {
			info.Width, info.Height = decoded.Bounds().Dx(), decoded.Bounds().Dy()
		}
	}
	if format == "jpeg" {
		format = "jpg"
	}
	id := randomID()
	folder := time.Now().UTC().Format("2006/01/")
	row := database.Image{ID: id, UserID: p.User.ID, AlbumID: albumID, Name: name, StorageID: storageID, Key: folder + id + "." + format, Format: format, Size: int64(len(data)), OriginalSize: header.Size, Optimized: status == "optimized", CompressionStatus: status, Width: info.Width, Height: info.Height, State: "ready"}
	store := a.stores[row.StorageID]
	// 文件存储和数据库不共享事务；失败时补偿清理，清理失败保留日志供核对。
	committed := false
	defer func() {
		if !committed {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			for _, key := range []string{row.Key, row.ThumbKey} {
				if key != "" {
					if err := store.Delete(ctx, key); err != nil {
						slog.Error("上传失败后清理文件失败", "key", key, "error", err)
					}
				}
			}
		}
	}()
	if err = store.Put(r.Context(), row.Key, bytes.NewReader(data)); err != nil {
		internal(w, err)
		return
	}
	thumb, err := thumbnail(decoded)
	if err != nil {
		slog.Warn("缩略图生成失败，使用原图", "image_id", id, "error", err)
	} else {
		key := folder + id + "-thumb.png"
		if err = store.Put(r.Context(), key, bytes.NewReader(thumb)); err != nil {
			slog.Warn("缩略图保存失败，使用原图", "image_id", id, "error", err)
			// S3 响应丢失时对象可能已经写入，清理独立随机键避免留下孤立缩略图。
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			if cleanupErr := store.Delete(ctx, key); cleanupErr != nil {
				slog.Warn("缩略图补偿清理失败", "key", key, "error", cleanupErr)
			}
			cancel()
		} else {
			row.ThumbKey = key
		}
	}
	a.quotaMu.Lock()
	err = a.db.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		return tx.Delete(&database.UploadReservation{}, "id = ?", reservation).Error
	})
	a.quotaMu.Unlock()
	if err != nil {
		internal(w, err)
		return
	}
	committed = true
	a.settingsMu.RUnlock()
	locked = false
	respond(w, 201, a.viewFor(row, p))
}

func thumbnail(source image.Image) ([]byte, error) {
	bounds := source.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width > 480 || height > 480 {
		if width >= height {
			height = max(1, height*480/width)
			width = 480
		} else {
			width = max(1, width*480/height)
			height = 480
		}
	}
	target := image.NewNRGBA(image.Rect(0, 0, width, height))
	draw.ApproxBiLinear.Scale(target, target.Bounds(), source, bounds, draw.Src, nil)
	var output bytes.Buffer
	err := png.Encode(&output, target)
	return output.Bytes(), err
}

func (a *App) updateImage(w http.ResponseWriter, r *http.Request, p principal) {
	var input struct {
		Name    *string `json:"name"`
		AlbumID *string `json:"album_id"`
	}
	if !decode(w, r, &input) {
		return
	}
	updates := map[string]any{}
	if input.Name != nil {
		name := strings.TrimSpace(*input.Name)
		if name == "" || utf8.RuneCountInString(name) > 255 {
			fail(w, 400, "图片名称长度必须在 1–255 个字符之间")
			return
		}
		updates["name"] = name
	}
	if input.AlbumID != nil {
		if *input.AlbumID == "" {
			updates["album_id"] = nil
		} else {
			if !a.albumWritable(r, p, *input.AlbumID) {
				fail(w, 400, "相册不存在或没有上传权限")
				return
			}
			updates["album_id"] = *input.AlbumID
		}
	}
	if len(updates) == 0 {
		fail(w, 400, "没有可更新的内容")
		return
	}
	db := a.db.WithContext(r.Context())
	var row database.Image
	if err := a.manageableImages(db, p).First(&row, "id = ? AND state = ?", r.PathValue("id"), "ready").Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			fail(w, 404, "图片不存在")
		} else {
			internal(w, err)
		}
		return
	}
	if err := db.Model(&row).Updates(updates).Error; err != nil {
		internal(w, err)
		return
	}
	respond(w, 200, map[string]bool{"ok": true})
}

func (a *App) deleteImages(w http.ResponseWriter, r *http.Request, p principal) {
	var input struct {
		IDs []string `json:"ids"`
	}
	if !decode(w, r, &input) {
		return
	}
	if len(input.IDs) == 0 || len(input.IDs) > 100 {
		fail(w, 400, "每次请选择 1–100 张图片")
		return
	}
	db := a.db.WithContext(r.Context())
	var rows []database.Image
	if err := a.manageableImages(db, p).Where("id IN ?", input.IDs).Find(&rows).Error; err != nil {
		internal(w, err)
		return
	}
	deleted := 0
	failed := []string{}
	found := map[string]bool{}
	for _, row := range rows {
		found[row.ID] = true
	}
	for _, id := range input.IDs {
		if !found[id] {
			failed = append(failed, id)
		}
	}
	for _, row := range rows {
		if err := db.Model(&row).Update("state", "deleting").Error; err != nil {
			internal(w, err)
			return
		}
		store := a.getStore(row.StorageID)
		if store == nil {
			failed = append(failed, row.ID)
			continue
		}
		if err := store.Delete(r.Context(), row.Key); err != nil {
			failed = append(failed, row.ID)
			continue
		}
		if row.ThumbKey != "" {
			if err := store.Delete(r.Context(), row.ThumbKey); err != nil {
				failed = append(failed, row.ID)
				continue
			}
		}
		if err := db.Delete(&row).Error; err != nil {
			internal(w, err)
			return
		}
		deleted++
	}
	respond(w, 200, map[string]any{"deleted": deleted, "failed": failed})
}

func (a *App) serveImage(w http.ResponseWriter, r *http.Request) {
	a.policyMu.RLock()
	defer a.policyMu.RUnlock()
	p, err := a.sessionPrincipal(r)
	if err != nil {
		internal(w, err)
		return
	}
	a.serveStoredImage(w, r, func(db *gorm.DB) *gorm.DB { return a.visibleImages(db, p) })
}

func (a *App) serveStoredImage(w http.ResponseWriter, r *http.Request, permitted func(*gorm.DB) *gorm.DB) {
	w.Header().Set("Cache-Control", "private, no-store")
	file := r.PathValue("file")
	if len(file) < 36 || len(file) > 45 {
		http.NotFound(w, r)
		return
	}
	id := file[:32]
	var row database.Image
	if err := permitted(a.db.WithContext(r.Context())).First(&row, "id = ? AND state = ?", id, "ready").Error; err != nil {
		http.NotFound(w, r)
		return
	}
	key := row.Key
	contentType := "image/" + row.Format
	if row.Format == "jpg" {
		contentType = "image/jpeg"
	}
	if file == id+"-thumb.png" && row.ThumbKey != "" {
		key = row.ThumbKey
		contentType = "image/png"
	} else if file != id+"."+row.Format {
		http.NotFound(w, r)
		return
	}
	store := a.getStore(row.StorageID)
	if store == nil {
		http.NotFound(w, r)
		return
	}
	reader, err := store.Open(r.Context(), key)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer reader.Close()
	etag := fmt.Sprintf("\"%s\"", file)
	w.Header().Set("Content-Type", contentType)
	// 每次重新验证，删除图片后不会继续从浏览器强缓存中展示；外部 CDN 需另行清理。
	if w.Header().Get("Cache-Control") == "" {
		w.Header().Set("Cache-Control", "public, max-age=0, must-revalidate")
	}
	w.Header().Set("ETag", etag)
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	if key == row.Key {
		w.Header().Set("Content-Length", strconv.FormatInt(row.Size, 10))
	}
	if r.Method == "HEAD" {
		return
	}
	if _, err := io.Copy(w, reader); err != nil {
		slog.Debug("图片传输中断", "error", err)
	}
}
