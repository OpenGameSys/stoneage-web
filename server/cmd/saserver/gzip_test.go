package main

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 文字类文件按 Accept-Encoding 压缩、内容不变；WebP 不压；不带 Accept-Encoding 时原样；目录不列出来。
func TestGzipStatic(t *testing.T) {
	dir := t.TempDir()
	big := strings.Repeat(`{"tiles":[1,2,3,4,5,6,7,8]}`, 2000)
	os.WriteFile(filepath.Join(dir, "tiles.json"), []byte(big), 0o644)
	os.WriteFile(filepath.Join(dir, "page.webp"), []byte("RIFFxxxxWEBP"), 0o644)
	os.MkdirAll(filepath.Join(dir, "sub"), 0o755)
	h := gzipped(http.FileServer(noListing{http.Dir(dir)}))
	srv := httptest.NewServer(h)
	defer srv.Close()
	get := func(p, enc string) *http.Response {
		req, _ := http.NewRequest("GET", srv.URL+p, nil)
		if enc != "" {
			req.Header.Set("Accept-Encoding", enc)
		}
		res, err := http.DefaultTransport.RoundTrip(req) // 不让 Go 自己解压
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	res := get("/tiles.json", "gzip")
	raw, _ := io.ReadAll(res.Body)
	if res.Header.Get("Content-Encoding") != "gzip" || len(raw) >= len(big)/10 {
		t.Fatalf("tiles.json 应 gzip：encoding=%q size=%d", res.Header.Get("Content-Encoding"), len(raw))
	}
	zr, err := gzip.NewReader(strings.NewReader(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	if body, _ := io.ReadAll(zr); string(body) != big {
		t.Fatal("解压后内容不同")
	}
	if res := get("/tiles.json", ""); res.Header.Get("Content-Encoding") != "" {
		t.Fatal("没要 gzip 就不该压")
	}
	if res := get("/page.webp", "gzip"); res.Header.Get("Content-Encoding") != "" {
		t.Fatal("WebP 不该压")
	}
	if res := get("/sub/", "gzip"); res.StatusCode != http.StatusNotFound {
		t.Fatalf("目录不该列出来：%d", res.StatusCode)
	}
	if res := get("/missing.json", "gzip"); res.StatusCode != http.StatusNotFound || res.Header.Get("Content-Encoding") != "" {
		t.Fatalf("404 不该压：%d %q", res.StatusCode, res.Header.Get("Content-Encoding"))
	}
}
