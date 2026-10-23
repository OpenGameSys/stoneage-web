package world

import (
	"fmt"
	"hash/fnv"
	"slices"
)

// mapVerAlgo：通行的算法（data/mapset.go Walkable 等）改了就把它加一，所有楼层的版本一起变
const mapVerAlgo = "walk-v1"

// floorVer 是一层楼的地图资料版本：地块、物件、传送点与通行规则（mapset）的杂凑。客户端把收到的 MapData 存在浏览器
// （IndexedDB，client/src/mapcache.ts），下次进这一层时版本一样就直接用，不再向服务端要（MapData 占流量的大半）；
// 资料换过（换地图、改 mapset、加减传送点）版本就变，旧的作废。第一次用到时才算，之后记着。
func (w *World) floorVer(floor int) string {
	if v, ok := w.floorVers[floor]; ok {
		return v
	}
	f := w.game.Floors[floor]
	if f == nil {
		return ""
	}
	h := fnv.New64a()
	h.Write([]byte(mapVerAlgo))
	buf := make([]byte, 0, 2*len(f.Tiles)+2*len(f.Objs)+64)
	for _, v := range f.Tiles {
		buf = append(buf, byte(v), byte(v>>8))
	}
	for _, v := range f.Objs {
		buf = append(buf, byte(v), byte(v>>8))
	}
	h.Write(buf)
	var warps [][2]int
	for p := range w.game.Warps[floor] {
		warps = append(warps, p)
	}
	slices.SortFunc(warps, func(a, b [2]int) int {
		if a[0] != b[0] {
			return a[0] - b[0]
		}
		return a[1] - b[1]
	})
	for _, p := range warps {
		fmt.Fprintf(h, "w%d,%d;", p[0], p[1])
	}
	// 通行规则只看这层用到的图号（整张 mapset 太大，每层都算浪费）
	used := map[int]bool{}
	for _, v := range f.Tiles {
		used[int(v)] = true
	}
	for _, v := range f.Objs {
		used[int(v)] = true
	}
	nums := make([]int, 0, len(used))
	for n := range used {
		nums = append(nums, n)
	}
	slices.Sort(nums)
	for _, n := range nums {
		a := w.game.Tiles[n]
		fmt.Fprintf(h, "t%d:%d,%v;", n, a.Walkable, a.HaveHeight)
	}
	v := fmt.Sprintf("%016x", h.Sum64())
	if w.floorVers == nil {
		w.floorVers = map[int]string{}
	}
	w.floorVers[floor] = v
	return v
}
