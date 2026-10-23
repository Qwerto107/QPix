package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"qpix/internal/database"
	"qpix/internal/processing"
)

type imageView struct {
	database.Image
	URL          string `json:"url"`
	ThumbnailURL string `json:"thumbnail_url"`
	OwnerName    string `json:"owner_name,omitempty"`
	OwnerRole    string `json:"owner_role,omitempty"`
	OwnerID      string `json:"owner_id,omitempty"`
	CanManage    bool   `json:"can_manage"`
}

func (a *App) imageView(row database.Image) imageView {
	a.settingsMu.RLock()
	base := a.publicURL
	a.settingsMu.RUnlock()
	u := base + "/i/" + row.ID + "." + row.Format
	thumb := base + "/i/" + thumbnailFile(row)
	return imageView{Image: row, URL: u, ThumbnailURL: thumb}
}

func thumbnailFile(row database.Image) string {
	if row.ThumbKey == "" {
		return row.ID + "." + row.Format
	}
	return row.ID + "-thumb" + path.Ext(row.ThumbKey)
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
	all, ok := allScope(w, r, p)
	if !ok {
		return
	}
	q := tokenAlbumQuery(ownerQuery(a.db.WithContext(r.Context()).Model(&database.Image{}), p, all), p, "album_id", tokenRead).Where("state = ?", "ready")
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
	rows, pagination, err := paginate[database.Image](q, r, limit, recordOrder(r))
	if err != nil {
		paginationError(w, err)
		return
	}
	items := make([]imageView, 0, len(rows))
	ownerIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		ownerIDs = append(ownerIDs, row.UserID)
	}
	owners, err := usersByID(a.db.WithContext(r.Context()), ownerIDs)
	if err != nil {
		internal(w, err)
		return
	}
	for _, row := range rows {
		items = append(items, a.viewFor(row, p, owners[row.UserID]))
	}
	respond(w, 200, pagination.response(items))
}

func (a *App) upload(w http.ResponseWriter, r *http.Request, p principal) {
	if !a.uploadLimits.allow(p.User.ID, 120, time.Minute) {
		w.Header().Set("Retry-After", "60")
		fail(w, 429, "上传过于频繁，请稍后再试")
		return
	}
	release, ok := a.beginUpload(p.User.ID)
	if !ok {
		w.Header().Set("Retry-After", "2")
		fail(w, 429, "上传请求过多，请稍后重试")
		return
	}
	defer release()
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
		var fileError *os.PathError
		if errors.As(err, &tooLarge) || errors.Is(err, multipart.ErrMessageTooLarge) {
			fail(w, 413, "上传文件超出大小限制")
		} else if errors.As(err, &fileError) {
			slog.Error("写入上传临时文件失败", "error", err)
			fail(w, 500, "服务器临时存储不可用，请稍后重试或联系管理员")
		} else {
			fail(w, 400, "上传内容不完整或 multipart/form-data 格式无效")
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
	// 慢速请求体只占用接收名额，完整收到文件后才占用图片处理名额。
	select {
	case a.processing <- struct{}{}:
		defer func() { <-a.processing }()
	default:
		w.Header().Set("Retry-After", "2")
		fail(w, 429, "图片处理中，请稍后重试")
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
	value := r.FormValue("album_id")
	if value == "" {
		value, err = a.defaultAlbum(r, p.User.ID)
		if err != nil {
			internal(w, err)
			return
		}
	}
	if !requireTokenPermission(w, p, &value, tokenUpload) {
		return
	}
	if !a.albumWritable(r, p, value) {
		fail(w, 400, "相册不存在或没有上传权限")
		return
	}
	albumID := &value
	file, err := header.Open()
	if err != nil {
		internal(w, err)
		return
	}
	defer file.Close()
	info, format, err := processing.ConfigReader(file)
	if err != nil || (format != "jpeg" && format != "png" && format != "gif" && format != "webp") {
		fail(w, 415, "仅支持有效的 JPEG、PNG、GIF、静态 WebP 图片")
		return
	}
	if info.Width <= 0 || info.Height <= 0 || int64(info.Width) > a.pixelLimit()/int64(info.Height) {
		fail(w, 413, "图片像素超出服务器设置的上限")
		return
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		internal(w, err)
		return
	}
	decoded, err := processing.DecodeReader(file, format)
	if err != nil {
		fail(w, 415, "图片内容不完整或格式暂不支持")
		return
	}
	if int64(decoded.Bounds().Dx())*int64(decoded.Bounds().Dy()) != int64(info.Width)*int64(info.Height) {
		fail(w, 415, "图片尺寸校验失败")
		return
	}
	a.settingsMu.Lock()
	storageID := r.FormValue("storage_id")
	if storageID == "" {
		storageID = a.defaultStorage
	}
	profile, exists := a.profiles[storageID]
	store := a.stores[storageID]
	defaults := a.processingSettings
	// 在途上传占用存储引用，替代覆盖网络 I/O 的设置读锁。
	if !exists || !profile.Enabled || store == nil {
		a.settingsMu.Unlock()
		fail(w, 400, "存储不存在或已停用")
		return
	}
	if a.storageUploads == nil {
		a.storageUploads = make(map[string]int)
	}
	a.storageUploads[storageID]++
	a.settingsMu.Unlock()
	defer func() {
		a.settingsMu.Lock()
		a.storageUploads[storageID]--
		if a.storageUploads[storageID] == 0 {
			delete(a.storageUploads, storageID)
		}
		a.settingsMu.Unlock()
	}()
	mode := r.FormValue("compression")
	if mode == "" || mode == "default" {
		mode = defaults.Mode
	}
	quality := defaults.Quality
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
	var data []byte
	if mode != "none" {
		switch {
		case format == "gif":
			status = "unsupported"
		case int64(info.Width)*int64(info.Height) > processing.MaxPixels:
			status = "pixel_limit"
		default:
			if _, err = file.Seek(0, io.SeekStart); err != nil {
				internal(w, err)
				return
			}
			original, readErr := io.ReadAll(file)
			if readErr != nil {
				internal(w, readErr)
				return
			}
			result := processing.Optimize(original, decoded, format, mode, quality)
			format, status = result.Format, result.Status
			if status == "optimized" {
				data = result.Data
				info.Width, info.Height = decoded.Bounds().Dx(), decoded.Bounds().Dy()
			}
		}
	}
	if format == "jpeg" {
		format = "jpg"
	}
	id := randomID()
	folder := time.Now().UTC().Format("2006/01/")
	size := header.Size
	var content io.Reader = file
	if data != nil {
		size = int64(len(data))
		content = bytes.NewReader(data)
	} else if _, err = file.Seek(0, io.SeekStart); err != nil {
		internal(w, err)
		return
	}
	row := database.Image{ID: id, UserID: p.User.ID, AlbumID: albumID, Name: name, StorageID: storageID, Key: folder + id + "." + format, Format: format, Size: size, OriginalSize: header.Size, Optimized: status == "optimized", CompressionStatus: status, Width: info.Width, Height: info.Height, State: "ready"}
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
	if err = store.Put(r.Context(), row.Key, content); err != nil {
		internal(w, err)
		return
	}
	thumb, thumbFormat, err := thumbnail(decoded, format, row.Size)
	if err != nil {
		slog.Warn("缩略图生成失败，使用原图", "image_id", id, "error", err)
	} else if len(thumb) > 0 {
		key := folder + id + "-thumb." + thumbFormat
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
	locked := a.lockPolicy(w)
	defer locked.release()
	w = locked
	// 接收和处理期间不阻塞管理操作；提交前以当前状态重新检查全部授权。
	p, ok = a.authenticate(w, r, true, "any")
	if !ok || !a.enforceAccess(w, r) {
		return
	}
	if !a.sameOrigin(r) {
		fail(w, 403, "请求来源不被允许")
		return
	}
	if !requireTokenPermission(w, p, albumID, tokenUpload) {
		return
	}
	if !a.albumWritable(r, p, *albumID) {
		fail(w, 400, "相册不存在或没有上传权限")
		return
	}
	limit = a.uploadLimit()
	if p.User.MaxFileBytes > 0 {
		limit = min(limit, p.User.MaxFileBytes)
	}
	if header.Size > limit || int64(info.Width) > a.pixelLimit()/int64(info.Height) {
		fail(w, 413, "图片超出当前大小或像素上限")
		return
	}
	a.settingsMu.RLock()
	enabled := a.profiles[storageID].Enabled
	a.settingsMu.RUnlock()
	if !enabled {
		fail(w, 409, "存储已停用，请重新选择存储")
		return
	}
	err = a.withUploadQuota(a.db.WithContext(r.Context()), p.User.ID, func(tx *gorm.DB, user database.User) error {
		var reserved int64
		if err := tx.Model(&database.UploadReservation{}).Select("COALESCE(SUM(bytes),0)").Where("user_id = ? AND id <> ?", p.User.ID, reservation).Scan(&reserved).Error; err != nil {
			return err
		}
		if user.QuotaBytes > 0 && row.Size > user.QuotaBytes-user.UsedBytes-reserved {
			return errQuota
		}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		if err := tx.Model(&database.User{}).Where("id = ?", row.UserID).UpdateColumn("used_bytes", gorm.Expr("used_bytes + ?", row.Size)).Error; err != nil {
			return err
		}
		return tx.Delete(&database.UploadReservation{}, "id = ?", reservation).Error
	})
	if err != nil {
		if errors.Is(err, errQuota) {
			fail(w, 409, err.Error())
		} else {
			internal(w, err)
		}
		return
	}
	committed = true
	locked.release()
	respond(w, 201, a.viewFor(row, p, p.User))
}

func (a *App) beginUpload(userID string) (func(), bool) {
	a.uploadMu.Lock()
	defer a.uploadMu.Unlock()
	// 接收总量有界，并限制单个账号，避免同一 Token 独占全站临时文件名额。
	if a.uploadCount >= 8 || a.uploads[userID] >= 2 {
		return nil, false
	}
	if a.uploads == nil {
		a.uploads = make(map[string]int)
	}
	a.uploads[userID]++
	a.uploadCount++
	return func() {
		a.uploadMu.Lock()
		defer a.uploadMu.Unlock()
		a.uploadCount--
		a.uploads[userID]--
		if a.uploads[userID] == 0 {
			delete(a.uploads, userID)
		}
	}, true
}

func thumbnail(source image.Image, format string, size int64) ([]byte, string, error) {
	bounds := source.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	// 小尺寸但体积大的图片仍生成缩略图，避免列表反复下载大块元数据或原图尾部数据。
	if width <= 480 && height <= 480 && size <= 128<<10 && format != "gif" {
		return nil, "", nil
	}
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
	if target.Opaque() {
		err := jpeg.Encode(&output, target, &jpeg.Options{Quality: 82})
		return output.Bytes(), "jpg", err
	}
	err := png.Encode(&output, target)
	return output.Bytes(), "png", err
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
	if len(updates) == 0 && input.AlbumID == nil {
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
	if input.AlbumID != nil {
		id := *input.AlbumID
		if id == "" {
			var err error
			id, err = a.defaultAlbum(r, row.UserID)
			if err != nil {
				internal(w, err)
				return
			}
		}
		var target database.Album
		if err := db.First(&target, "id = ? AND user_id = ?", id, row.UserID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				fail(w, 400, "请选择上传者自己的相册")
			} else {
				internal(w, err)
			}
			return
		}
		if row.AlbumID == nil || id != *row.AlbumID {
			if !requireTokenPermission(w, p, &id, tokenUpload) {
				return
			}
		}
		updates["album_id"] = id
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
	if p.Token != nil {
		ids := map[string]bool{}
		for _, id := range input.IDs {
			ids[id] = true
		}
		if len(rows) != len(ids) {
			fail(w, 403, "部分图片不在 Token 删除权限范围内")
			return
		}
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
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	if len(ids) > 0 {
		if err := db.Model(&database.Image{}).Where("id IN ?", ids).Update("state", "deleting").Error; err != nil {
			internal(w, err)
			return
		}
	}
	releasePolicy(w)
	for _, row := range rows {
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
		if err := a.removeImageRecord(db, row); err != nil {
			internal(w, err)
			return
		}
		deleted++
	}
	respond(w, 200, map[string]any{"deleted": deleted, "failed": failed})
}

func (a *App) serveImage(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "" {
		a.protect(a.serveTokenImage, true)(w, r)
		return
	}
	p, err := a.sessionPrincipal(r)
	if err != nil {
		internal(w, err)
		return
	}
	a.serveStoredImage(w, r, func(row database.Image) bool { return imageReadable(row, p) })
}

func (a *App) serveTokenImage(w http.ResponseWriter, r *http.Request, p principal) {
	a.serveStoredImage(w, r, func(row database.Image) bool { return imageReadable(row, p) })
}

func (a *App) serveStoredImage(w http.ResponseWriter, r *http.Request, permitted func(database.Image) bool) {
	w.Header().Set("Cache-Control", "private, no-store")
	file := r.PathValue("file")
	if len(file) < 36 || len(file) > 45 {
		http.NotFound(w, r)
		return
	}
	id := file[:32]
	var row database.Image
	// 同一查询加载图片与所属相册，权限和 Referer 均使用本次数据库快照，不缓存授权结果。
	err := a.db.WithContext(r.Context()).Joins("AlbumRef").
		Where(clause.Eq{Column: clause.Column{Table: clause.CurrentTable, Name: "id"}, Value: id}).
		Where(clause.Eq{Column: clause.Column{Table: clause.CurrentTable, Name: "state"}, Value: "ready"}).First(&row).Error
	if err != nil || !permitted(row) {
		http.NotFound(w, r)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/i/") {
		if row.AlbumID != nil && row.AlbumRef == nil && !a.cfg.DisableAccessRules {
			http.NotFound(w, r)
			return
		}
		if !a.enforceAlbumAccess(w, r, row.AlbumRef) {
			return
		}
	}
	key := row.Key
	contentType := "image/" + row.Format
	if row.Format == "jpg" {
		contentType = "image/jpeg"
	}
	if file == thumbnailFile(row) && row.ThumbKey != "" {
		key = row.ThumbKey
		contentType = "image/png"
		if path.Ext(key) == ".jpg" || path.Ext(key) == ".jpeg" {
			contentType = "image/jpeg"
		}
	} else if file != id+"."+row.Format {
		http.NotFound(w, r)
		return
	}
	store := a.getStore(row.StorageID)
	if store == nil {
		http.NotFound(w, r)
		return
	}
	etag := fmt.Sprintf("\"%s\"", file)
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("ETag", etag)
	// 权限与相册规则检查先于 304；匹配时不再读取本地文件或发起 S3 GetObject。
	if matchesETag(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	reader, err := store.Open(r.Context(), key)
	if err != nil {
		w.Header().Set("Cache-Control", "private, no-store")
		w.Header().Del("ETag")
		w.Header().Del("Content-Type")
		http.NotFound(w, r)
		return
	}
	defer reader.Close()
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

func matchesETag(header, etag string) bool {
	for _, value := range strings.Split(header, ",") {
		value = strings.TrimSpace(value)
		if value == "*" || strings.TrimPrefix(value, "W/") == etag {
			return true
		}
	}
	return false
}
