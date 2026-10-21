package storage

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestS3ObjectContract(t *testing.T) {
	var mu sync.Mutex
	objects := map[string][]byte{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 Credential=fixture/") {
			t.Error("缺少 SigV4 签名")
		}
		if r.URL.Path != "/photos/qpix/2026/image.png" {
			t.Errorf("对象路径错误: %s", r.URL.Path)
		}
		switch r.Method {
		case "PUT":
			if r.Header.Get("Content-Type") != "image/png" {
				t.Error("Content-Type 错误")
			}
			data, err := io.ReadAll(r.Body)
			if err != nil {
				t.Error(err)
			}
			objects[r.URL.Path] = data
			w.WriteHeader(200)
		case "GET":
			data, ok := objects[r.URL.Path]
			if !ok {
				http.Error(w, "missing", 404)
				return
			}
			_, _ = w.Write(data)
		case "DELETE":
			delete(objects, r.URL.Path)
			w.WriteHeader(204)
		default:
			t.Errorf("意外方法: %s", r.Method)
			w.WriteHeader(405)
		}
	}))
	defer server.Close()
	store := NewS3(S3Config{Endpoint: server.URL, Region: "us-east-1", Bucket: "photos", Prefix: "/qpix/", AccessKey: "fixture", SecretKey: "fixture-secret", PathStyle: true})
	ctx := context.Background()
	key := "2026/image.png"
	if err := store.Put(ctx, key, bytes.NewReader([]byte("fixture-bytes"))); err != nil {
		t.Fatal(err)
	}
	reader, err := store.Open(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	content, err := io.ReadAll(reader)
	reader.Close()
	if err != nil || string(content) != "fixture-bytes" {
		t.Fatal("读取内容不同", err)
	}
	if err = store.Delete(ctx, key); err != nil {
		t.Fatal(err)
	}
	if err = store.Delete(ctx, key); err != nil {
		t.Fatal("重复删除失败", err)
	}
	if _, err = store.Open(ctx, key); err == nil {
		t.Fatal("已删除对象仍能读取")
	}
}
