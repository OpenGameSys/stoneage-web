package data

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// argString 是 npc/npcutil.c NPC_Util_GetArgStr：参数里某段含 "file" 时，读取 npc 目录下 "file:" 之后的文件，
// 各行去掉行尾换行后用 "|" 接起来（NPC_Util_MargeStrFromArgFile，上一段已以 "|" 结尾就不再加）；否则就是参数本身。
func argString(npcDir string, args []string) string {
	for _, a := range args {
		if !strings.Contains(a, "file") {
			continue
		}
		_, name, ok := strings.Cut(a, ":")
		if !ok {
			break
		}
		b, err := os.ReadFile(filepath.Join(npcDir, filepath.FromSlash(name)))
		if err != nil {
			return ""
		}
		var sb strings.Builder
		for _, line := range strings.Split(GBK(b), "\n") {
			line = strings.TrimRight(line, "\r\n")
			if s := sb.String(); s != "" && !strings.HasSuffix(s, "|") {
				sb.WriteByte('|')
			}
			sb.WriteString(line)
		}
		return sb.String()
	}
	return strings.Join(args, "|")
}

// ArgField 是 NPC_Util_GetStrFromStrWithDelim：按 "|" 切段，第一个"含有" key 的段取第一个 ":" 之后、下一个 ":" 之前的部分。
// 原版是子串比对（"FREE" 不会命中 "FreeMsg"，但 "WARP" 会命中 "NEWWARPMAN"），这里照做。
func ArgField(arg, key string) (string, bool) {
	for _, seg := range strings.Split(arg, "|") {
		if !strings.Contains(seg, key) {
			continue
		}
		parts := strings.Split(seg, ":")
		if len(parts) < 2 {
			continue // getStringFromIndexWithDelim 取不到第 2 段，继续找下一段
		}
		return parts[1], true
	}
	return "", false
}

// Atoi 是 C 的 atoi：跳过前导空白，读可选正负号与数字，读不到为 0。
func Atoi(s string) int {
	s = strings.TrimLeft(s, " \t")
	i, neg := 0, false
	if i < len(s) && (s[i] == '-' || s[i] == '+') {
		neg = s[i] == '-'
		i++
	}
	n := 0
	for ; i < len(s) && s[i] >= '0' && s[i] <= '9'; i++ {
		n = n*10 + int(s[i]-'0')
	}
	if neg {
		return -n
	}
	return n
}

// ---------------- 万事通（npc_windowman.c）的窗口设定档 ----------------

// 万事通窗口的按钮处理位置：ok, cancel, yes, no, prev, next，之后是选单的第 1..7 个 selected（buttonproc[13]）。
const (
	WinProcOK = iota
	WinProcCancel
	WinProcYes
	WinProcNo
	WinProcPrev
	WinProcNext
	WinProcSelect
	WinProcCount = 13
)

// WinButton 是一个按钮按下后的去向（buttonproc）：持有某道具时去 HaveItemGoto，没有某道具时去 NoItemGoto，否则去 Goto。
// -1 表示没有设定；HaveItem 设了但身上没有（或 NoItem 设了但身上有）时什么也不做。
type WinButton struct {
	Goto, HaveItem, HaveItemGoto, NoItem, NoItemGoto int
}

// WinDef 是设定档里的一个窗口（winno ... endwin）。Type 是 WINDOW_MESSAGETYPE_*（0 文字、2 选单），Buttons 为按钮位元。
type WinDef struct {
	No, Type, Buttons int
	Message           string
	Procs             [WinProcCount]*WinButton
	TakeGold          int // 骑乘训练师（npc_riderman.c）的学费
}

// 原版 WINDOW_MESSAGETYPE_*。
const (
	WinTypeMessage = 0
	WinTypeSelect  = 2
)

// LoadWindowConf 读取万事通的设定档（NPC_Windowman_readData）。每行 "键=值"（值取第一个 "=" 到下一个 "=" 之间），
// 行首空白去掉，# 开头与空行略过。按钮段从 okpressed / selected 等开始，到 endbutton 结束。
func LoadWindowConf(path string) (map[int]*WinDef, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	wins := map[int]*WinDef{}
	var cur *WinDef
	var btn *WinButton
	selects := 0
	sc := bufio.NewScanner(strings.NewReader(GBK(b)))
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if line == "" || line[0] == '#' {
			continue
		}
		line = strings.TrimLeft(strings.ReplaceAll(line, "\t", " "), " ")
		parts := strings.Split(line, "=")
		if len(parts) < 2 {
			continue
		}
		key, val := strings.ToLower(parts[0]), parts[1]
		if key == "winno" {
			cur, btn, selects = &WinDef{No: Atoi(val), Type: -1, Buttons: -1}, nil, 0
			continue
		}
		if cur == nil {
			continue
		}
		if btn != nil {
			switch key {
			case "gotowin":
				btn.Goto = Atoi(val)
			case "checkhaveitem":
				btn.HaveItem = Atoi(val)
			case "haveitemgotowin":
				btn.HaveItemGoto = Atoi(val)
			case "checkdonthaveitem":
				btn.NoItem = Atoi(val)
			case "donthaveitemgotowin":
				btn.NoItemGoto = Atoi(val)
			case "endbutton":
				btn = nil
			}
			continue
		}
		proc := -1
		switch key {
		case "wintype":
			cur.Type = Atoi(val)
		case "takegold":
			cur.TakeGold = Atoi(val)
		case "buttontype":
			cur.Buttons = windowButtons(val)
		case "message":
			// 原版按目前长度判断：还是空字串时直接放入（开头的空行因此不算一行），否则先补换行再接上
			if cur.Message == "" {
				cur.Message = val
			} else {
				cur.Message += "\n" + val
			}
		case "okpressed":
			proc = WinProcOK
		case "cancelpressed":
			proc = WinProcCancel
		case "yespressed":
			proc = WinProcYes
		case "nopressed":
			proc = WinProcNo
		case "prevpressed":
			proc = WinProcPrev
		case "nextpressed":
			proc = WinProcNext
		case "selected":
			proc = WinProcSelect + selects
			selects++
		case "endwin":
			if cur.Type >= 0 && cur.Buttons >= 0 && cur.Message != "" {
				wins[cur.No] = cur
			}
			cur = nil
		}
		if proc >= 0 && proc < WinProcCount {
			btn = &WinButton{Goto: -1, HaveItem: -1, HaveItemGoto: -1, NoItem: -1, NoItemGoto: -1}
			cur.Procs[proc] = btn
		}
	}
	return wins, sc.Err()
}

// windowButtons 是 NPC_Windowman_restoreButtontype："ok|cancel|yes|no|prev|next" 组合成位元，都不是时按数字解读（"none" 为 0）。
func windowButtons(s string) int {
	bits := 0
	for _, p := range strings.Split(s, "|") {
		switch strings.ToLower(p) {
		case "ok":
			bits |= 1 << 0
		case "cancel":
			bits |= 1 << 1
		case "yes":
			bits |= 1 << 2
		case "no":
			bits |= 1 << 3
		case "prev":
			bits |= 1 << 4
		case "next":
			bits |= 1 << 5
		}
	}
	if bits == 0 {
		bits = Atoi(s)
	}
	return bits
}

// ---------------- 长老与记录点（char_data.c elders[]、npc_savepoint.c） ----------------

// baseElders 是 char_data.c 的 elders[] 初值：四个村庄的村长家与 7770 号地图。其余编号由记录点 NPC 的 Born 设定（CHAR_ElderSetPosition）。
var baseElders = []Hometown{{1006, 15, 22}, {2006, 20, 16}, {3006, 21, 16}, {4006, 14, 20}, {7770, 9, 10}}

// MaxElders 是 elders[] 的大小（MAXELDERS）。
const MaxElders = 128

// loadElders 建立长老编号 → 记录位置的表：先放 elders[] 初值，再用每个记录点 NPC 的 "ID" 与 "Born:楼层,x,y" 覆盖。
func loadElders(npcs []NPC) map[int]Hometown {
	out := map[int]Hometown{}
	for i, h := range baseElders {
		out[i] = h
	}
	for _, n := range npcs {
		if n.Function != "SavePoint" {
			continue
		}
		id, ok := ArgField(n.Arg, "ID")
		if !ok {
			continue
		}
		born, _ := ArgField(n.Arg, "Born")
		p := strings.Split(born, ",")
		if len(p) < 3 {
			continue
		}
		e := Atoi(id)
		if e >= len(Hometowns) && e < MaxElders { // ELDERINDEXSTART 之前是村庄长老，不能覆盖
			out[e] = Hometown{Atoi(p[0]), Atoi(p[1]), Atoi(p[2])}
		}
	}
	return out
}
