package storage

import (
	"context"
	"io"
	"strings"
	"testing"
)

func TestLocalContainmentAndAtomicWrite(t *testing.T) {
	local, err := NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"../outside", "/absolute", "C:\\outside", "a/../../outside", "a\\..\\outside", ""} {
		if err := local.Put(context.Background(), key, strings.NewReader("unsafe")); err == nil {
			t.Fatalf("不应允许路径 %q", key)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := local.Put(ctx, "cancelled", strings.NewReader("cancelled")); err == nil {
		t.Fatal("已取消的请求仍写入了文件")
	}
	if err := local.Put(context.Background(), "2026/10/image.png", strings.NewReader("original")); err != nil {
		t.Fatal(err)
	}
	reader, err := local.Open(context.Background(), "2026/10/image.png")
	if err != nil {
		t.Fatal(err)
	}
	content, err := io.ReadAll(reader)
	reader.Close()
	if err != nil || string(content) != "original" {
		t.Fatal("文件内容不一致")
	}
	if err := local.Delete(context.Background(), "2026/10/image.png"); err != nil {
		t.Fatal(err)
	}
	if err := local.Delete(context.Background(), "2026/10/image.png"); err != nil {
		t.Fatal("重复删除应当成功", err)
	}
}
