package data

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// 骑宠的形象对照（ride/familyridefunction.lua，取自原版 ablua；gmsv 的 FamilyRideFunction / FamilyRideCheck 调用它）。
// 用的是 serv/ablua 那一份（与 C 源码相符、有 FamilyRideCheck）；data/ablua 里较旧的那份没有 FamilyRideCheck，宠物窗口永远不出现骑乘按钮。
// 这里只读 data() 里的表，按 FamilyRideFunction 的顺序查：
//   petlist + NewRideNoList   有骑宠证书（CHAR_LOWRIDEPETS 第 i 位）的宠物
//   CommonPetList + CommonRideNoList + PlayerColor  四色老虎：各颜色的人物只能骑对应颜色的那只
//   CommonPetList1            雷龙等：{骑乘形象, 人物原形象, 宠物原形象} 完全对上才行
// Pointpetlist（庄园骑宠，要家族）与 EquipagePetlist（镶嵌道具）还没有对应的系统，不读。

// Ride 是骑宠资料。
type Ride struct {
	RidePets     []int    // ridepet：宠物窗口显示骑乘按钮的宠物原形象（FamilyRideCheck）
	PetNames     []string // petname：ridepet 各宠物的名字（FreeRideQuery）
	PetList      []int    // petlist：证书骑宠，下标就是证书位元
	NewRideNo    [][]int  // NewRideNoList[人物编号-1][i]
	PlayerColor  [][]int  // PlayerColor[人物编号-1][颜色]
	CommonPets   []int    // CommonPetList
	CommonRideNo [][]int  // CommonRideNoList[人物编号-1][颜色]
	Common1      [][]int  // CommonPetList1：{骑乘形象, 人物原形象, 宠物原形象}
}

// luaTables 读出 "名字 = {...}" 形式的数字表（嵌套的表、字串略过不管，只收数字）。
func luaTables(src string) map[string][]any {
	out := map[string][]any{}
	// 去掉 -- 注释
	var b strings.Builder
	for _, line := range strings.Split(src, "\n") {
		if i := strings.Index(line, "--"); i >= 0 {
			line = line[:i]
		}
		b.WriteString(line + "\n")
	}
	s := b.String()
	for i := 0; i < len(s); i++ {
		if s[i] != '{' {
			continue
		}
		// 往回找 "名字 ="
		j := strings.LastIndexAny(s[:i], "\n;")
		head := strings.TrimSpace(s[j+1 : i])
		if !strings.HasSuffix(head, "=") {
			continue
		}
		name := strings.TrimSpace(strings.TrimSuffix(head, "="))
		if strings.ContainsAny(name, " \t(") {
			name = name[strings.LastIndexAny(name, " \t(")+1:]
		}
		v, end := luaTable(s, i)
		out[name] = v
		i = end
	}
	return out
}

// luaTable 从 s[i] 的 '{' 起读一个表，回传内容与结尾 '}' 的位置。
func luaTable(s string, i int) ([]any, int) {
	var out []any
	for i++; i < len(s); i++ {
		switch c := s[i]; {
		case c == '{':
			var v []any
			v, i = luaTable(s, i)
			out = append(out, v)
		case c == '}':
			return out, i
		case c == '"':
			k := strings.IndexByte(s[i+1:], '"')
			if k < 0 {
				return out, len(s)
			}
			out = append(out, s[i+1:i+1+k])
			i += k + 1
		case c == '-' || c >= '0' && c <= '9':
			k := i + 1
			for k < len(s) && s[k] >= '0' && s[k] <= '9' {
				k++
			}
			n, _ := strconv.Atoi(s[i:k])
			out = append(out, n)
			i = k - 1
		}
	}
	return out, i
}

func luaInts(v []any) []int {
	var out []int
	for _, x := range v {
		if n, ok := x.(int); ok {
			out = append(out, n)
		}
	}
	return out
}

func luaRows(v []any) [][]int {
	var out [][]int
	for _, x := range v {
		if r, ok := x.([]any); ok {
			out = append(out, luaInts(r))
		}
	}
	return out
}

// LoadRide 读 ride/familyridefunction.lua；没有这个档时回传空的资料（骑不了任何宠物）。
func LoadRide(dataDir string) *Ride {
	b, err := os.ReadFile(filepath.Join(dataDir, "ride", "familyridefunction.lua"))
	if err != nil {
		return &Ride{}
	}
	t := luaTables(GBK(b))
	var names []string
	for _, x := range t["petname"] {
		if s, ok := x.(string); ok {
			names = append(names, s)
		}
	}
	return &Ride{RidePets: luaInts(t["ridepet"]), PetNames: names, PetList: luaInts(t["petlist"]), NewRideNo: luaRows(t["NewRideNoList"]),
		PlayerColor: luaRows(t["PlayerColor"]), CommonPets: luaInts(t["CommonPetList"]), CommonRideNo: luaRows(t["CommonRideNoList"]),
		Common1: luaRows(t["CommonPetList1"])}
}

// Rideable 是 FamilyRideCheck：这种宠物（原形象）在骑宠名单里，宠物窗口才显示骑乘按钮。
func (r *Ride) Rideable(petSprite int) bool {
	for _, n := range r.RidePets {
		if n == petSprite {
			return true
		}
	}
	return false
}

// LicenceIndex：这种宠物（原形象）是不是要骑乘书的证书骑宠（FamilyRideFunction 的 petlist），是的话回传证书位元的下标，不是回 -1。
func (r *Ride) LicenceIndex(petSprite int) int {
	for i, n := range r.PetList {
		if n == petSprite && i < 32 {
			return i
		}
	}
	return -1
}

// playerNo 是 getNOindex：人物原形象 → 人物编号 1..12（100000 起每 20 个一种，100700 起每 10 个一种），100400 起的是 13，其余 -1。
func playerNo(base int) int {
	switch {
	case base >= 100000 && base < 100240:
		return (base-100000)/20 + 1
	case base >= 100700 && base < 100820:
		return (base-100700)/10 + 1
	case base >= 100400 && base < 100410:
		return 13
	}
	return -1
}

// Sprite 是 FamilyRideFunction 的形象部分：base 是人物原形象（CHAR_BASEBASEIMAGENUMBER），cur 是现在的形象，
// pet 是宠物原形象，licence 是证书位元（CHAR_LOWRIDEPETS）。骑不了时回 0。
func (r *Ride) Sprite(base, cur, pet int, licence uint32) int {
	switch {
	case cur == 101178 && pet == 101177, cur == 101177 && pet == 101178:
		return 101179
	}
	id := playerNo(base)
	if id < 1 {
		return 0
	}
	row := func(t [][]int, i int) int { // Lua 表越界时是 nil，当作骑不了
		if id-1 < len(t) && i < len(t[id-1]) {
			return t[id-1][i]
		}
		return 0
	}
	for i, n := range r.PetList {
		if n == pet && i < 32 && licence&(1<<i) != 0 {
			if s := row(r.NewRideNo, i); s > 0 {
				return s
			}
		}
	}
	for i := 0; id-1 < len(r.PlayerColor) && i < len(r.PlayerColor[0]); i++ {
		if (row(r.PlayerColor, i) == base || base >= 100700 && base < 108400) && i < len(r.CommonPets) && pet == r.CommonPets[i] {
			if s := row(r.CommonRideNo, i); s > 0 {
				return s
			}
		}
	}
	for _, c := range r.Common1 {
		if len(c) >= 3 && c[1] == base && c[2] == pet {
			return c[0]
		}
	}
	return 0
}
