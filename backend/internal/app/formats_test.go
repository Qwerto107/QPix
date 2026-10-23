package app

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"net/http"
	"os"
	"testing"

	"golang.org/x/image/bmp"
	"qpix/internal/database"
)

func testFormatPreviews(t *testing.T, a *App, c *testClient) {
	t.Helper()
	var encoded bytes.Buffer
	if err := bmp.Encode(&encoded, image.NewNRGBA(image.Rect(0, 0, 3, 3))); err != nil {
		t.Fatal(err)
	}
	var uploaded imageView
	if err := json.Unmarshal(c.upload(encoded.Bytes(), "duplicate.bmp", "", "", 201), &uploaded); err != nil {
		t.Fatal(err)
	}
	if uploaded.Name != uploaded.ID+".bmp" {
		t.Fatal("上传未默认使用随机文件名", uploaded.Name)
	}
	if uploaded.PreviewURL == "" || uploaded.PreviewURL == uploaded.URL {
		t.Fatal("BMP 缺少独立预览")
	}
	c.request("GET", uploaded.PreviewURL, nil, 200)
	c.request("GET", uploaded.ThumbnailURL, nil, 200)
	var stored database.Image
	if err := a.db.First(&stored, "id = ?", uploaded.ID).Error; err != nil {
		t.Fatal(err)
	}
	var share struct {
		ID  string `json:"id"`
		URL string `json:"url"`
	}
	if err := json.Unmarshal(c.request("POST", "/api/v1/shares", map[string]any{"kind": "image", "target_id": uploaded.ID, "password": "p"}, 201), &share); err != nil {
		t.Fatal(err)
	}
	token := share.URL[len(share.URL)-64:]
	c.request("POST", "/api/v1/public-shares/"+token+"/unlock", map[string]string{"password": "p"}, 200)
	var public struct {
		Items []imageView `json:"items"`
	}
	if err := json.Unmarshal(c.request("GET", "/api/v1/public-shares/"+token, nil, 200), &public); err != nil {
		t.Fatal(err)
	}
	if len(public.Items) != 1 || len(public.Items[0].PreviewURL) < 4 || public.Items[0].PreviewURL[:4] != "/si/" {
		t.Fatal("分享预览未使用分享网关")
	}
	visitor := &testClient{t: t, server: c.server, client: &http.Client{}}
	visitor.request("GET", public.Items[0].PreviewURL, nil, 404)
	c.request("GET", public.Items[0].PreviewURL, nil, 200)
	c.request("DELETE", "/api/v1/shares/"+share.ID, nil, 200)
	c.request("GET", public.Items[0].PreviewURL, nil, 404)
	c.request("DELETE", "/api/v1/shares/"+share.ID+"/record", nil, 200)
	c.request("POST", "/api/v1/images/batch-delete", map[string]any{"ids": []string{uploaded.ID}}, 200)
	c.request("GET", uploaded.PreviewURL, nil, 404)
	reader, openErr := a.getStore(stored.StorageID).Open(context.Background(), stored.PreviewKey)
	if reader != nil {
		reader.Close()
	}
	if !os.IsNotExist(openErr) {
		t.Fatal("删除未清理预览文件", openErr)
	}
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="4" height="4"><rect width="4" height="4" fill="green"/></svg>`)
	if err := json.Unmarshal(c.upload(svg, "icon.svg", "", "", 201, map[string]string{"random_name": "false"}), &uploaded); err != nil {
		t.Fatal(err)
	}
	request, _ := http.NewRequest("GET", c.server.URL+uploaded.URL, nil)
	response, err := c.client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.Header.Get("Content-Disposition") != "attachment" || response.Header.Get("Content-Security-Policy") != "sandbox; default-src 'none'; style-src 'unsafe-inline'" {
		t.Fatal("SVG 缺少独立执行限制")
	}
	c.request("POST", "/api/v1/images/batch-delete", map[string]any{"ids": []string{uploaded.ID}}, 200)
	c.upload(svg, "icon.svg", "", "", 400, map[string]string{"random_name": "invalid"})
}
