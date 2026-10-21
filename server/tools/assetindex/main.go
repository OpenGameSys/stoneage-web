// assetindex 把导出资源的图像清单（assets/images/*.json）与动画（assets/anim/*.json）打包成一个 gzip 的二进制索引
// assets/index.bin.gz，客户端启动时一次读进内存，之后取图、取动画都不必再请求。另外列出"核心页"：
// 客户端源码里写到的界面图号（地图图号 20000..99999、高彩图号 ≥ 1000000）与动画编号所在的图集页，登录前先传进显存。
//
// 在 h5/server 下运行（资源重新导出或客户端改了界面图号之后）：go run ./tools/assetindex
//
// 格式（小端）：
//
//	"SAIX" u32 版本 1
//	图像：u32 块数；每块 u32 块号（图号 >> chunkShift）、u32 个数 n、n × 16 字节
//	      （u8 集合 0 i8 / 1 rgba / 255 没有这张图，u8 0，i16 页（−1 全透明），u16 x，u16 y，u16 w，u16 h，i16 锚点x，i16 锚点y）
//	动画：u32 个数；每个 u32 编号、u16 页数、u16 动作数、页数 × (u8 集合，u8 0，u16 页)，
//	      动作数 × (u8 方向，u8 种类，u16 ticks，u8 freeze，u8 0，u16 帧数，帧数 × (i32 图号，i16 x，i16 y，u16 事件，u16 0))
//	核心页：u32 个数，个数 × (u8 集合，u8 0，u16 页)
package main

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

type animJSON struct {
	ID      int `json:"id"`
	Actions []struct {
		Dir    int     `json:"dir"`
		Type   int     `json:"type"`
		Ticks  int     `json:"ticks"`
		Freeze int     `json:"freeze"`
		Frames [][]int `json:"frames"`
	} `json:"actions"`
	Pages []string `json:"pages"`
}

type pageKey struct{ set, page int }

func parsePage(s string) pageKey {
	set, n, _ := strings.Cut(s, "/")
	p, _ := strconv.Atoi(n)
	if set == "rgba" {
		return pageKey{1, p}
	}
	return pageKey{0, p}
}

func main() {
	assets := flag.String("assets", "../assets", "导出的资源目录")
	client := flag.String("client", "../client/src", "客户端源码（找界面用到的图号）")
	flag.Parse()
	var manifest struct {
		ChunkShift uint `json:"chunkShift"`
	}
	readJSON(filepath.Join(*assets, "manifest.json"), &manifest)
	var tiles struct {
		Tiles [][]int `json:"tiles"`
	}
	readJSON(filepath.Join(*assets, "tiles.json"), &tiles)
	tileIndex := map[int]int{}
	for _, t := range tiles.Tiles {
		tileIndex[t[0]] = t[1]
	}

	var out bytes.Buffer
	w := func(v any) { binary.Write(&out, binary.LittleEndian, v) }
	out.WriteString("SAIX")
	w(uint32(1))

	// 图像清单
	files, _ := filepath.Glob(filepath.Join(*assets, "images", "*.json"))
	slices.Sort(files)
	w(uint32(len(files)))
	images := map[int][]int{} // 图号 → [集合, 页, ...]（找核心页用）
	for _, f := range files {
		c, err := strconv.Atoi(strings.TrimSuffix(filepath.Base(f), ".json"))
		if err != nil {
			log.Fatalf("%s：%v", f, err)
		}
		var j struct {
			Img []json.RawMessage `json:"img"`
		}
		readJSON(f, &j)
		w(uint32(c))
		w(uint32(len(j.Img)))
		for i, raw := range j.Img {
			var e []int
			if string(raw) == "0" || json.Unmarshal(raw, &e) != nil || len(e) < 8 {
				w([16]byte{255})
				continue
			}
			images[c<<manifest.ChunkShift+i] = e
			w([2]uint8{uint8(e[0]), 0})
			w([7]int16{int16(e[1]), int16(e[2]), int16(e[3]), int16(e[4]), int16(e[5]), int16(e[6]), int16(e[7])})
		}
	}

	// 动画
	anims, _ := filepath.Glob(filepath.Join(*assets, "anim", "*.json"))
	slices.Sort(anims)
	w(uint32(len(anims)))
	animPages := map[int][]pageKey{}
	frames := 0
	for _, f := range anims {
		var a animJSON
		readJSON(f, &a)
		w(uint32(a.ID))
		w(uint16(len(a.Pages)))
		w(uint16(len(a.Actions)))
		for _, p := range a.Pages {
			k := parsePage(p)
			animPages[a.ID] = append(animPages[a.ID], k)
			w([2]uint8{uint8(k.set), 0})
			w(uint16(k.page))
		}
		for _, act := range a.Actions {
			w([2]uint8{uint8(act.Dir), uint8(act.Type)})
			w(uint16(act.Ticks))
			w([2]uint8{uint8(act.Freeze), 0})
			w(uint16(len(act.Frames)))
			for _, fr := range act.Frames {
				w(int32(fr[0]))
				w([2]int16{int16(fr[1]), int16(fr[2])})
				w([2]uint16{uint16(fr[3]), 0})
				frames++
			}
		}
	}

	// 核心页：客户端源码里的界面图号与动画编号
	core := map[pageKey]bool{}
	num := regexp.MustCompile(`\b\d{5,7}\b`)
	srcs, _ := filepath.Glob(filepath.Join(*client, "*.ts"))
	for _, f := range srcs {
		if strings.HasSuffix(f, ".gen.ts") || strings.HasSuffix(f, "soundtable.ts") || strings.HasSuffix(f, "albumdata.ts") {
			continue
		}
		b, _ := os.ReadFile(f)
		for _, m := range num.FindAll(b, -1) {
			n, _ := strconv.Atoi(string(m))
			idx := -1
			switch {
			case n >= 20000 && n < 100000:
				if i, ok := tileIndex[n]; ok {
					idx = i
				}
			case n >= 100000 && n < 140000:
				for _, k := range animPages[n] {
					core[k] = true
				}
			case n >= 1000000:
				idx = n
			}
			if e, ok := images[idx]; ok && e[1] >= 0 {
				core[pageKey{e[0], e[1]}] = true
			}
		}
	}
	keys := make([]pageKey, 0, len(core))
	for k := range core {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b pageKey) int { return (a.set-b.set)*100000 + a.page - b.page })
	w(uint32(len(keys)))
	coreBytes := 0
	for _, k := range keys {
		w([2]uint8{uint8(k.set), 0})
		w(uint16(k.page))
		if st, err := os.Stat(filepath.Join(*assets, "pages", map[int]string{0: "i8", 1: "rgba"}[k.set], fmt.Sprintf("%05d.webp", k.page))); err == nil {
			coreBytes += int(st.Size())
		}
	}

	var gz bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&gz, gzip.BestCompression)
	zw.Write(out.Bytes())
	zw.Close()
	dst := filepath.Join(*assets, "index.bin.gz")
	if err := os.WriteFile(dst, gz.Bytes(), 0o644); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%s：%d 块图像清单、%d 个动画（%d 帧）、%d 张核心页（下载 %.1f MB）；原始 %.1f MB，压缩后 %.1f MB\n",
		dst, len(files), len(anims), frames, len(keys), float64(coreBytes)/1e6, float64(out.Len())/1e6, float64(gz.Len())/1e6)
}

func readJSON(path string, v any) {
	b, err := os.ReadFile(path)
	if err != nil {
		log.Fatal(err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		log.Fatalf("%s：%v", path, err)
	}
}
