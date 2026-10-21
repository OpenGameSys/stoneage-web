package data

import (
	"bufio"
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Warp 是一个传送点：踩到 (Floor, X, Y) 时传送到 (ToFloor, ToX, ToY)。
type Warp struct {
	Floor, X, Y       int
	ToFloor, ToX, ToY int
	Source            string
}

// NPC 是 .create 文件中的一个摆放块（npcgen）。Dir 已换成客户端方向。
type NPC struct {
	Floor     int
	X, Y      int // 出生点：出生矩形的中心（原版在矩形内随机，静态摆放时取中心）
	X1, Y1    int // 出生矩形（含边界）：borncenter 为 中心±宽高/2，borncorner 为两角
	X2, Y2    int
	Dir       int
	Sprite    int
	Name      string
	Type      string   // enemy= 的第一段，如 npcgen_warp、sb_dou
	Args      []string // enemy= 其余各段
	CreateNum int
	Source    string
	Function  string // 对话行为：TownPeople（随机说一句）、SignBoard（弹窗显示文字）、WindowHealer/Healer（护士回复）、
	// WarpMan（传送员）、Windowman（万事通）、SavePoint（记录点）、TimeMan（按时间出现的人）、ItemShop（道具店）、
	// PoolItemShop（寄放道具的店）、Dengon（传言板）、Bankman（家族银行）、Charm（美容师）、LuckyMan（占卜师）、Janken（猜拳），其余为空
	Text    string          // 村民与告示牌的对话内容（参数文件各行直接接起来）
	Arg     string          // NPC_Util_GetArgStr 的结果（参数文件各行以 "|" 接起来），用 ArgField 取值
	Windows map[int]*WinDef // 万事通：conff 指定的窗口设定档
	Family  int             // .create 的 family=（CHAR_FMINDEX）：庄园的 NPC 是第几个村（1..4）
}

// npcFunctions 是 npc/genout/npcgen.template（与 windowman 的 .template）中模板名到 functionset 的对应（只列出已实现对话的几种）。
var npcFunctions = map[string]string{"npcgen_man": "TownPeople", "npcgen_signboard": "SignBoard",
	"npcgen_winhealer": "WindowHealer", "npcgen_healer": "Healer", "npcgen_warpman": "WarpMan",
	"windowman": "Windowman", "npcgen_savepoint": "SavePoint", "npcgen_timeman": "TimeMan",
	"npcgen_shop": "ItemShop", "npcgen_limitshop": "ItemShop", "npcgen_poolitemshop": "PoolItemShop",
	"npcgen_dengon": "Dengon", "bankman": "Bankman", "npcgen_charm": "Charm", "luckyman": "LuckyMan", "npcgen_janken": "Janken",
	"bus": "Bus", "airplane": "Airplane"}

// Implemented 是已经做了对话的 functionset（名字照原版 npc/*.c 的 NPC_<名字>Talked）。
var Implemented = map[string]bool{"TownPeople": true, "SignBoard": true, "WindowHealer": true, "Healer": true, "WarpMan": true,
	"Windowman": true, "SavePoint": true, "TimeMan": true, "ItemShop": true, "PoolItemShop": true, "Dengon": true, "Bankman": true,
	"Charm": true, "LuckyMan": true, "Janken": true, "ExChangeMan": true, "NPCEnemy": true, "Quiz": true,
	"ItemchangeMan": true, "Mic": true, "Action": true, "PetShop": true, "PetSkillShop": true, "Riderman": true, "Duelranking": true,
	"Familyman": true, "FmDengon": true, "FmLetter": true, "FmHealer": true,
	"Bus": true, "Airplane": true, "ManorSman": true, "Scheduleman": true, "FMWarpMan": true, "FMPKMan": true, "FMPKCallMan": true}

// loadTemplates 读 npc 目录下所有 .template（npctemplate.c）：模板名 → functionset。
func loadTemplates(dir string) map[string]string {
	out := map[string]string{}
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".template") {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		name := ""
		for _, line := range strings.Split(GBK(b), "\n") {
			k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
			switch {
			case strings.HasPrefix(strings.TrimSpace(line), "{"):
				name = ""
			case ok && k == "templatename":
				name = v
			case ok && k == "functionset" && name != "":
				out[name] = v
			}
		}
		return nil
	})
	return out
}

// resolveArg 是 npc/npcutil.c NPC_Util_GetArgStr：参数里某段含 "file" 时，读取 npc 目录下 "file:" 之后的文件并把各行拼起来；
// 否则参数本身就是内容。
func resolveArg(npcDir string, args []string) string {
	for _, a := range args {
		if strings.Contains(a, "file") {
			_, name, ok := strings.Cut(a, ":")
			if !ok {
				break
			}
			b, err := os.ReadFile(filepath.Join(npcDir, filepath.FromSlash(name)))
			if err != nil {
				return ""
			}
			return strings.NewReplacer("\r", "", "\n", "").Replace(GBK(b))
		}
	}
	return strings.Join(args, "|")
}

// ServerDirToClient：原客户端收到服务端方向后都做 (dir + 3) % 8（netproc.cpp）。
func ServerDirToClient(d int) int { return ((d+3)%8 + 8) % 8 }

// LoadMapWarps 读取 mapwarp.txt：类型:?:楼层,x,y:目标楼层,x,y:?
func LoadMapWarps(path string) ([]Warp, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []Warp
	for _, line := range strings.Split(GBK(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line[0] == '#' {
			continue
		}
		p := strings.Split(line, ":")
		if len(p) < 4 {
			continue
		}
		from, to := ints(p[2], ","), ints(p[3], ",")
		if len(from) < 3 || len(to) < 3 {
			continue
		}
		out = append(out, Warp{from[0], from[1], from[2], to[0], to[1], to[2], "mapwarp.txt"})
	}
	return out, nil
}

// LoadNPCs 读取 dir 下所有 .create 文件。块格式：NPCCREATE 后若干 { key=value ... }。
func LoadNPCs(dir string) ([]NPC, error) {
	var paths []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".create") {
			paths = append(paths, p)
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	var out []NPC
	confs := map[string]map[int]*WinDef{}
	templates := loadTemplates(dir)
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		rel, _ := filepath.Rel(dir, p)
		for _, n := range parseCreate(GBK(b), filepath.ToSlash(rel)) {
			fn, ok := npcFunctions[n.Type]
			if !ok && Implemented[templates[n.Type]] { // 其余模板按 .template 的 functionset（NPCEnemy 的 sb_* 有六十几种）
				fn, ok = templates[n.Type], true
			}
			if ok {
				n.Function, n.Text, n.Arg = fn, resolveArg(dir, n.Args), argString(dir, n.Args)
			}
			if n.Function == "Windowman" || n.Function == "Bankman" || n.Function == "Riderman" || n.Function == "Familyman" || n.Function == "FmLetter" { // NPC_BankmanInit、NPC_RidermanInit 读一样格式的设定档
				// NPC_WindowmanInit：参数的 conff 指定设定档；读不到时这个 NPC 不做（原版初始化失败）
				name, _ := ArgField(n.Arg, "conff")
				if _, seen := confs[name]; !seen {
					confs[name], _ = LoadWindowConf(filepath.Join(dir, filepath.FromSlash(name)))
				}
				if n.Windows = confs[name]; n.Windows == nil {
					n.Function = ""
				}
			}
			out = append(out, n)
		}
	}
	return out, nil
}

func parseCreate(text, source string) []NPC {
	var out []NPC
	var cur map[string]string
	sc := bufio.NewScanner(bytes.NewReader([]byte(text)))
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || line[0] == '#' {
			continue
		}
		switch {
		case strings.HasPrefix(line, "{"):
			cur = map[string]string{}
		case strings.HasPrefix(line, "}"):
			if cur != nil {
				if n, ok := npcFrom(cur, source); ok {
					out = append(out, n)
				}
			}
			cur = nil
		case cur != nil:
			if k, v, ok := strings.Cut(line, "="); ok {
				cur[strings.TrimSpace(k)] = strings.TrimSpace(v)
			}
		}
	}
	return out
}

func npcFrom(kv map[string]string, source string) (NPC, bool) {
	floor, err := strconv.Atoi(kv["floorid"])
	if err != nil {
		return NPC{}, false
	}
	n := NPC{Floor: floor, Name: kv["name"], Source: source, CreateNum: 1}
	// npc/npccreate.c：borncenter = 中心 x,y,宽,高 → [中心-宽/2, 中心+宽/2]；borncorner = 两个角
	if bc := ints(kv["borncenter"], ","); len(bc) >= 2 {
		w, h := 0, 0
		if len(bc) >= 4 {
			w, h = bc[2], bc[3]
		}
		n.X1, n.X2, n.Y1, n.Y2 = bc[0]-w/2, bc[0]+w/2, bc[1]-h/2, bc[1]+h/2
	} else if bc := ints(kv["borncorner"], ","); len(bc) >= 4 {
		n.X1, n.X2, n.Y1, n.Y2 = min(bc[0], bc[2]), max(bc[0], bc[2]), min(bc[1], bc[3]), max(bc[1], bc[3])
	} else {
		return NPC{}, false
	}
	n.X, n.Y = (n.X1+n.X2)/2, (n.Y1+n.Y2)/2
	if d, err := strconv.Atoi(kv["dir"]); err == nil {
		n.Dir = ServerDirToClient(d)
	}
	n.Sprite, _ = strconv.Atoi(kv["graphicname"])
	n.Family, _ = strconv.Atoi(kv["family"])
	if c, err := strconv.Atoi(kv["createnum"]); err == nil {
		n.CreateNum = c
	}
	parts := strings.Split(kv["enemy"], "|")
	n.Type, n.Args = parts[0], parts[1:]
	return n, true
}

// NPCWarps 把 npcgen_warp 类型的 NPC 转成传送点：enemy=npcgen_warp|楼层|x|y。
// 原版在出生矩形内随机放一个传送 NPC；这里固定放在中心格。
func NPCWarps(npcs []NPC) []Warp {
	var out []Warp
	for _, n := range npcs {
		if n.Type != "npcgen_warp" || len(n.Args) < 3 {
			continue
		}
		fl, e1 := strconv.Atoi(n.Args[0])
		x, e2 := strconv.Atoi(n.Args[1])
		y, e3 := strconv.Atoi(n.Args[2])
		if e1 != nil || e2 != nil || e3 != nil {
			continue
		}
		out = append(out, Warp{n.Floor, n.X, n.Y, fl, x, y, n.Source})
	}
	return out
}

func ints(s, sep string) []int {
	var out []int
	for _, p := range strings.Split(s, sep) {
		v, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil {
			return out
		}
		out = append(out, v)
	}
	return out
}
