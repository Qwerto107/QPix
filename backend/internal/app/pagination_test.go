package app

import (
	"fmt"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"
	"qpix/internal/database"
)

func TestCursorPagination(t *testing.T) {
	db, err := database.Open("sqlite", filepath.Join(t.TempDir(), "pages.db"))
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	defer pool.Close()
	if err := database.Migrate(db); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 9; i++ {
		row := database.Image{ID: fmt.Sprintf("%032d", i), UserID: "owner", State: "ready", Name: fmt.Sprintf("图片%d.png", i/3), Size: int64(i / 2), CreatedAt: time.Date(2026, 1, 1, 0, 0, i/3, 123456000, time.UTC)}
		// 同一时间线混合正负时区，绑定与读取后仍须按 UTC 稳定翻页。
		row.CreatedAt = row.CreatedAt.In(time.FixedZone("test", (i%3-1)*8*3600))
		if i == 8 {
			row.UserID = "other"
		}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	// 用实际执行的查询检查后续页没有退回 COUNT/OFFSET。
	var statements []string
	if err := db.Callback().Query().After("gorm:query").Register("qpix:cursor-queries", func(tx *gorm.DB) {
		statements = append(statements, strings.ToLower(tx.Statement.SQL.String()))
	}); err != nil {
		t.Fatal(err)
	}
	defer db.Callback().Query().Remove("qpix:cursor-queries")
	for _, col := range []string{"created_at", "name", "size"} {
		for _, dir := range []string{"ASC", "DESC"} {
			t.Run(col+dir, func(t *testing.T) {
				order := col + " " + dir + ", id DESC"
				query := func() *gorm.DB {
					return db.Model(&database.Image{}).Where("user_id = ? AND state = ?", "owner", "ready")
				}
				var expected []string
				if err := query().Order(order).Pluck("id", &expected).Error; err != nil {
					t.Fatal(err)
				}
				var got []string
				cursor := ""
				for n := 0; n < 5; n++ {
					r := httptest.NewRequest("GET", "/api/v1/images?pagination=cursor&cursor="+url.QueryEscape(cursor), nil)
					statements = nil
					rows, meta, err := paginate[database.Image](query(), r, 3, order)
					if err != nil {
						t.Fatal(err)
					}
					if cursor == "" {
						if meta.Total == nil || *meta.Total != 8 {
							t.Fatal("首页计数错误")
						}
					} else if meta.Total != nil || len(statements) != 1 || strings.Contains(statements[0], "offset") || strings.Contains(statements[0], "count(") {
						t.Fatalf("后续页重复计数或偏移扫描：%v", statements)
					}
					for _, row := range rows {
						got = append(got, row.ID)
					}
					if !meta.HasMore {
						break
					}
					cursor = meta.NextCursor
				}
				if !reflect.DeepEqual(got, expected) {
					t.Fatalf("重复排序值导致遗漏或越权：got %v want %v", got, expected)
				}
			})
		}
	}
	r := httptest.NewRequest("GET", "/api/v1/images?pagination=cursor&cursor=invalid", nil)
	if _, _, err := paginate[database.Image](db.Model(&database.Image{}), r, 3, "created_at DESC, id DESC"); err != errCursor {
		t.Fatal("未拒绝无效游标")
	}
}
