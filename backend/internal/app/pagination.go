package app

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"qpix/internal/database"
)

var errCursor = errors.New("分页游标无效，请刷新列表")

type listPagination struct {
	Total      *int64 `json:"total"`
	Page       int    `json:"page"`
	Limit      int    `json:"limit"`
	HasMore    bool   `json:"has_more"`
	NextCursor string `json:"next_cursor"`
}

type listCursor struct {
	ID    string `json:"id"`
	Value string `json:"value"`
	Order string `json:"order"`
}

// 游标只决定起点，所有请求仍重新应用账号、相册和 Token 权限过滤。
// 首页计数，后续页仅取 limit+1 条；旧客户端继续使用页码分页。
func paginate[T database.Image | database.Share | database.APIToken](q *gorm.DB, r *http.Request, limit int, order string) ([]T, listPagination, error) {
	page, _ := listPage(r)
	meta := listPagination{Page: page, Limit: limit}
	cursorMode := r.URL.Query().Get("pagination") == "cursor"
	raw := r.URL.Query().Get("cursor")
	col := strings.Fields(order)[0]
	if cursorMode && col != "created_at" && col != "name" && col != "size" {
		return nil, meta, errCursor
	}
	if !cursorMode || raw == "" {
		var total int64
		if err := q.Session(&gorm.Session{}).Count(&total).Error; err != nil {
			return nil, meta, err
		}
		meta.Total = &total
	}
	if cursorMode {
		meta.Page = 1
		if raw != "" {
			cursor, value, err := parseCursor(raw, order, col)
			if err != nil {
				return nil, meta, err
			}
			if strings.Contains(order, " ASC,") {
				// 主排序升序时 ID 仍降序，保持与旧列表相同的稳定顺序。
				q = q.Where(col+" >= ? AND ("+col+" > ? OR id < ?)", value, value, cursor.ID)
			} else {
				q = q.Where("("+col+", id) < (?, ?)", value, cursor.ID)
			}
		}
	} else {
		q = q.Offset((page - 1) * limit)
	}
	var rows []T
	if err := q.Order(order).Limit(limit + 1).Find(&rows).Error; err != nil {
		return nil, meta, err
	}
	meta.HasMore = len(rows) > limit
	if meta.HasMore {
		rows = rows[:limit]
		if cursorMode {
			cursor := cursorFor(rows[len(rows)-1], col)
			cursor.Order = order
			encoded, _ := json.Marshal(cursor)
			meta.NextCursor = base64.RawURLEncoding.EncodeToString(encoded)
		}
	}
	return rows, meta, nil
}

func parseCursor(raw, order, col string) (listCursor, any, error) {
	var cursor listCursor
	if len(raw) > 2048 {
		return cursor, nil, errCursor
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || json.Unmarshal(decoded, &cursor) != nil || cursor.Order != order || cursor.ID == "" || len(cursor.ID) > 32 {
		return cursor, nil, errCursor
	}
	switch col {
	case "created_at":
		value, err := time.Parse(time.RFC3339Nano, cursor.Value)
		if err != nil {
			return cursor, nil, errCursor
		}
		return cursor, value, nil
	case "size":
		value, err := strconv.ParseInt(cursor.Value, 10, 64)
		if err != nil || value < 0 {
			return cursor, nil, errCursor
		}
		return cursor, value, nil
	case "name":
		if len(cursor.Value) > 1020 {
			return cursor, nil, errCursor
		}
		return cursor, cursor.Value, nil
	}
	return cursor, nil, errCursor
}

func cursorFor(row any, col string) listCursor {
	switch row := row.(type) {
	case database.Image:
		value := row.CreatedAt.UTC().Format(time.RFC3339Nano)
		if col == "name" {
			value = row.Name
		} else if col == "size" {
			value = strconv.FormatInt(row.Size, 10)
		}
		return listCursor{ID: row.ID, Value: value}
	case database.APIToken:
		value := row.CreatedAt.UTC().Format(time.RFC3339Nano)
		if col == "name" {
			value = row.Name
		}
		return listCursor{ID: row.ID, Value: value}
	case database.Share:
		return listCursor{ID: row.ID, Value: row.CreatedAt.UTC().Format(time.RFC3339Nano)}
	}
	return listCursor{}
}

func paginationError(w http.ResponseWriter, err error) {
	if errors.Is(err, errCursor) {
		fail(w, 400, err.Error())
	} else {
		internal(w, err)
	}
}

func (meta listPagination) response(items any) any {
	return struct {
		Items any `json:"items"`
		listPagination
	}{items, meta}
}
