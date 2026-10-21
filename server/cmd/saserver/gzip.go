package main

import (
	"compress/gzip"
	"net/http"
	"path"
	"strings"
	"sync"
)

// gzipTypes 是值得压缩的静态文件（文字类）。图集 WebP、音频 Opus、index.bin.gz 本来就是压缩过的，不再压。
var gzipTypes = map[string]bool{".js": true, ".json": true, ".html": true, ".css": true, ".txt": true, ".sab": true, ".mjs": true, ".map": true}

var gzipPool = sync.Pool{New: func() any { w, _ := gzip.NewWriterLevel(nil, gzip.BestSpeed); return w }}

// gzipped 把文字类的静态文件按请求的 Accept-Encoding 用 gzip 送出（客户端脚本与 tiles.json 等约 1 MB → 290 KB）。
// 为了简单不处理 Range 请求：要压缩的文件把 Range 拿掉，整个送。
func gzipped(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !gzipTypes[strings.ToLower(path.Ext(r.URL.Path))] || !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}
		r.Header.Del("Range")
		gw := gzipPool.Get().(*gzip.Writer)
		gw.Reset(w)
		z := &gzipWriter{ResponseWriter: w, gz: gw}
		defer func() {
			if z.on {
				gw.Close()
			}
			gzipPool.Put(gw)
		}()
		next.ServeHTTP(z, r)
	})
}

// gzipWriter 在第一次写出时决定要不要压：只有 200 的回应压（304 / 404 照原样），并去掉 Content-Length。
type gzipWriter struct {
	http.ResponseWriter
	gz      *gzip.Writer
	on      bool
	started bool
}

func (z *gzipWriter) WriteHeader(code int) {
	if !z.started {
		z.started = true
		h := z.Header()
		h.Add("Vary", "Accept-Encoding")
		if code == http.StatusOK {
			z.on = true
			h.Set("Content-Encoding", "gzip")
			h.Del("Content-Length")
		}
	}
	z.ResponseWriter.WriteHeader(code)
}

func (z *gzipWriter) Write(b []byte) (int, error) {
	if !z.started {
		z.WriteHeader(http.StatusOK)
	}
	if z.on {
		return z.gz.Write(b)
	}
	return z.ResponseWriter.Write(b)
}
