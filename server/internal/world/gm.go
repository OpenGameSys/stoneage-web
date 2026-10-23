package world

import (
	"cmp"
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"sa/server/internal/battle"
	"sa/server/internal/data"
	"sa/server/internal/proto"
)

// gmMenu 是 GM 指令选单（客户端系统窗口的"GM 指令"）：只在 GM 上线时发给他，别的玩家的客户端里没有这份清单。
// Group 是选单左边的分页；Kinds 是每个参数的输入方式（客户端 gm.ts）：
//
//	""      直接打字
//	item    从全部道具里挑（GmListReq "item"）
//	enemy   从全部敌人里挑（"enemy"，给宠物用）
//	player  从这条线路上的玩家里挑（"player"）
//	pet     从自己的宠物栏挑（客户端自己有）
//	opt:a|b 只能选这几个
//
// 预设值 $floor / $x / $y 由客户端换成自己现在的位置，$vit / $str / $tgh / $dex、$earth.. 换成现在的数值。
var gmMenu = []proto.GmItem{
	{Group: "移动", Cmd: "/warp", Name: "传送", Args: []string{"楼层", "x", "y"}, Kinds: []string{"", "", ""}, Defaults: []string{"$floor", "$x", "$y"}, Hint: "传送到这条线路的任何地方"},
	{Group: "移动", Cmd: "/goto", Name: "去找玩家", Args: []string{"角色"}, Kinds: []string{"player"}, Hint: "传送到这条线路上某个玩家的身边"},
	{Group: "移动", Cmd: "/call", Name: "叫来玩家", Args: []string{"角色"}, Kinds: []string{"player"}, Hint: "把这条线路上的某个玩家传送到身边"},
	{Group: "移动", Cmd: "/where", Name: "现在位置", Hint: "楼层编号、名字、座标、是不是遇敌区域、这层有几个人"},
	{Group: "移动", Cmd: "/town", Name: "回村子", Args: []string{"村子"}, Kinds: []string{"opt:萨姆吉尔|玛丽娜丝|加加|卡鲁它那"}, Defaults: []string{"萨姆吉尔"},
		Hint: "传送到四个出身村落的村长家"},
	{Group: "移动", Cmd: "/town2", Name: "传送 2", Args: []string{"村庄"}, Kinds: []string{"opt:"}, Defaults: []string{""}, // 选项在 gmMenuItems 里按记录点填
		Hint: "传送到任一村庄的复活点（记录点 / 长老的位置，清单由记录点 NPC 的资料列出）"},
	{Group: "角色", Cmd: "/lv", Name: "设定等级", Args: []string{"等级"}, Kinds: []string{""}, Defaults: []string{"$level"}, Hint: "经验也改成那一级的起点（1..140）"},
	{Group: "角色", Cmd: "/exp", Name: "增加经验", Args: []string{"经验"}, Kinds: []string{""}, Defaults: []string{"10000"}, Hint: "照常升级、加点、加魅力"},
	{Group: "角色", Cmd: "/points", Name: "可分配点数", Args: []string{"点数"}, Kinds: []string{""}, Defaults: []string{"10"}, Hint: "升级加点用的点数（状态窗口里分配）"},
	{Group: "角色", Cmd: "/stat", Name: "设定状态", Args: []string{"体力", "腕力", "耐力", "敏捷"}, Kinds: []string{"", "", "", ""},
		Defaults: []string{"$vit", "$str", "$tgh", "$dex"}, Hint: "四项基本状态的点数（0..1000）"},
	{Group: "角色", Cmd: "/attr", Name: "设定属性", Args: []string{"地", "水", "火", "风"}, Kinds: []string{"", "", "", ""},
		Defaults: []string{"$earth", "$water", "$fire", "$wind"}, Hint: "每项 0..10，加起来不超过 10"},
	{Group: "角色", Cmd: "/charm", Name: "设定魅力", Args: []string{"魅力"}, Kinds: []string{""}, Defaults: []string{"100"}, Hint: "0..100"},
	{Group: "角色", Cmd: "/dp", Name: "设定 DP", Args: []string{"DP"}, Kinds: []string{""}, Defaults: []string{"1000"}},
	{Group: "角色", Cmd: "/trans", Name: "设定转生", Args: []string{"转生次数"}, Kinds: []string{"opt:0|1|2|3|4|5"}, Defaults: []string{"0"}, Hint: "只改转生次数（影响石币上限、骑乘等）"},
	{Group: "角色", Cmd: "/luck", Name: "设定运气", Args: []string{"运气"}, Kinds: []string{"opt:1|2|3|4|5"}, Defaults: []string{"5"}, Hint: "影响会心、捕获等（原版每个游戏日重抽）"},
	{Group: "角色", Cmd: "/hp", Name: "设定耐久力", Args: []string{"HP"}, Kinds: []string{""}, Defaults: []string{"1"}, Hint: "现在的 HP（不超过最大值），测补血用"},
	{Group: "角色", Cmd: "/mp", Name: "设定气力", Args: []string{"MP"}, Kinds: []string{""}, Defaults: []string{"0"}, Hint: "现在的 MP（不超过最大值）"},
	{Group: "角色", Cmd: "/heal", Name: "全部回复", Hint: "自己与宠物的 HP、MP 回满"},
	{Group: "物品", Cmd: "/item", Name: "给道具", Args: []string{"道具", "个数"}, Kinds: []string{"item", ""}, Defaults: []string{"", "1"}, Hint: "点\"选择\"从全部道具里挑，也可以直接打编号"},
	{Group: "物品", Cmd: "/gold", Name: "设定石币", Args: []string{"数量"}, Kinds: []string{""}, Defaults: []string{"1000000"}, Hint: "不超过这个转生次数的石币上限"},
	{Group: "物品", Cmd: "/fame", Name: "设定声望", Args: []string{"点数"}, Kinds: []string{""}, Defaults: []string{"1000"}},
	{Group: "物品", Cmd: "/give", Name: "送道具给人", Args: []string{"角色", "道具", "个数"}, Kinds: []string{"player", "item", ""}, Defaults: []string{"", "", "1"}, Hint: "给这条线路上的别的玩家"},
	{Group: "物品", Cmd: "/repair", Name: "修好装备", Hint: "身上、道具栏与宠物身上的道具耐久回满"},
	{Group: "物品", Cmd: "/bank", Name: "家族银行存款", Args: []string{"金额"}, Kinds: []string{""}, Defaults: []string{"100000"}, Hint: "家族银行的个人帐户"},
	{Group: "物品", Cmd: "/clearbag", Name: "清空道具栏", Args: []string{"确认"}, Kinds: []string{"opt:确定"}, Defaults: []string{"确定"}, Hint: "丢掉道具栏里的全部道具（装备着的不动）"},
	{Group: "宠物", Cmd: "/pet", Name: "给宠物", Args: []string{"敌人", "等级"}, Kinds: []string{"enemy", ""}, Defaults: []string{"", "1"}, Hint: "照捕获的方式给；点\"选择\"从全部敌人里挑"},
	{Group: "宠物", Cmd: "/petlv", Name: "宠物等级", Args: []string{"宠物", "等级"}, Kinds: []string{"pet", ""}, Defaults: []string{"", "30"}, Hint: "照升级的方式长上去（只能往上）"},
	{Group: "宠物", Cmd: "/petai", Name: "宠物忠诚", Args: []string{"宠物", "变动值"}, Kinds: []string{"pet", ""}, Defaults: []string{"", "10000"}, Hint: "忠诚度变动值，10000 = +100"},
	{Group: "宠物", Cmd: "/petskill", Name: "教宠物技能", Args: []string{"宠物", "技能栏", "技能"}, Kinds: []string{"pet", "opt:1|2|3|4|5|6|7", "skill"},
		Defaults: []string{"", "1", ""}, Hint: "放进第几个技能栏（栏数不够会加开）；点\"选择\"从全部技能里挑"},
	{Group: "宠物", Cmd: "/petname", Name: "宠物改名", Args: []string{"宠物", "名字"}, Kinds: []string{"pet", ""}, Hint: "不管是谁抓的都能改（最多 16 个字节）"},
	{Group: "宠物", Cmd: "/petrank", Name: "宠物档次", Args: []string{"宠物", "档次"}, Kinds: []string{"pet", "opt:0|1|2|3|4|5"}, Defaults: []string{"", "5"},
		Hint: "升级时的成长倍率：0 是 4.5~5.0 倍，5 是 5.5~6.0 倍"},
	{Group: "宠物", Cmd: "/petdel", Name: "删除宠物", Args: []string{"宠物"}, Kinds: []string{"pet"}, Hint: "直接从宠物栏拿掉（骑着的会先下来）"},
	{Group: "宠物", Cmd: "/learnride", Name: "骑乘等级", Args: []string{"可骑等级"}, Kinds: []string{"opt:40|80|120|200"}, Defaults: []string{"200"}, Hint: "骑乘训练师的课程：能骑几级以下的宠物"},
	{Group: "战斗", Cmd: "/encount", Name: "立即遇敌", Args: []string{"敌人", "等级"}, Kinds: []string{"enemy", ""}, Defaults: []string{"", ""},
		Hint: "不填敌人就按这里的遇敌区域开打；填了敌人编号就跟它单挑（哪里都行，等级不填用它的最低等级）"},
	{Group: "战斗", Cmd: "/noenc", Name: "不遇敌", Hint: "切换：走路不会遇敌"},
	{Group: "其他", Cmd: "/war", Name: "家族对战", Args: []string{"对战场", "设成"}, Kinds: []string{"", "opt:prepare|fight|end|clear|show"}, Defaults: []string{"1", "show"},
		Hint: "对战场 1..4 庄园、11..15 家族 PK 场：prepare 现在可进场、fight 马上开打、end 立刻判胜负、clear 清掉排程"},
	{Group: "其他", Cmd: "/event", Name: "任务旗标", Args: []string{"编号", "设成"}, Kinds: []string{"", "opt:done|now|clear"}, Defaults: []string{"", "done"}, Hint: "done 做完、now 进行中、clear 清掉"},
	{Group: "其他", Cmd: "/fm", Name: "家族资料", Args: []string{"项目", "数值"}, Kinds: []string{"opt:fame|fill|manor|gold", ""}, Defaults: []string{"fame", "1000"},
		Hint: "冒险声望、补满假族人、占有庄园（0 起，-1 放掉）、共同帐户"},
	{Group: "管理", Cmd: "/who", Name: "在线玩家", Hint: "列出这条线路上的玩家与所在楼层"},
	{Group: "管理", Cmd: "/look", Name: "查看玩家", Args: []string{"角色"}, Kinds: []string{"player"}, Hint: "等级、位置、HP、石币、声望、DP、宠物"},
	{Group: "管理", Cmd: "/mute", Name: "禁言", Args: []string{"角色", "分钟"}, Kinds: []string{"player", ""}, Defaults: []string{"", "10"}, Hint: "这段时间不能说话；0 分钟是解除（不存档，下线就没了）"},
	{Group: "管理", Cmd: "/announce", Name: "公告", Args: []string{"内容"}, Kinds: []string{""}, Hint: "发给这条线路上所有的人"},
	{Group: "管理", Cmd: "/kick", Name: "踢下线", Args: []string{"角色"}, Kinds: []string{"player"}},
	{Group: "管理", Cmd: "/setpw", Name: "改密码", Args: []string{"角色", "新密码"}, Kinds: []string{"player", ""}, Hint: "替在线玩家的账号改密码（8..64 位；给忘了安全码的人用）"},
	{Group: "管理", Cmd: "/setsecret", Name: "改安全码", Args: []string{"角色", "安全码"}, Kinds: []string{"player", ""}, Hint: "替在线玩家的账号改安全码（4..32 位）"},
}

// GmCommand 是 GM 指令选单送来的指令。不是 GM 的角色送来的一律不执行（客户端本来就没有选单，只记一笔）。
func (w *World) GmCommand(charID int, text string) {
	w.Do(func() {
		e := w.players[charID]
		if e == nil {
			return
		}
		if !e.gm {
			if r := []rune(text); len(r) > 60 { // 别让人用 16KB 的垃圾刷日志
				text = string(r[:60]) + "…"
			}
			w.log.Warn("不是 GM 的角色送来 GM 指令", "char", e.name, "text", text)

			return
		}
		if text = strings.TrimSpace(text); !strings.HasPrefix(text, "/") || !w.gmCommand(e, text) {
			gmReply(e, "没有这个指令："+text)
		}
	})
}

// GmListReq 是 GM 窗口的"选择"要的清单：全部道具、全部敌人、这条线路上的玩家。只回给 GM。
func (w *World) GmListReq(charID int, kind string) {
	w.Do(func() {
		e := w.players[charID]
		if e == nil || !e.gm {
			return
		}
		out := &proto.GmList{Kind: kind, Entries: []proto.GmEntry{}}
		switch kind {
		case "item":
			for id, d := range w.game.Items {
				memo := []rune(strings.TrimSpace(d.Memo))
				if len(memo) > 18 {
					memo = append(memo[:18], '…')
				}
				out.Entries = append(out.Entries, proto.GmEntry{ID: id, Name: d.Name, Info: fmt.Sprintf("Lv%d %s", d.Level, string(memo)), Image: d.Image})
			}
		case "enemy":
			for id, en := range w.game.Battle.Enemies {
				name, info := en.Name, fmt.Sprintf("Lv%d-%d", en.LvMin, en.LvMax)
				if t := w.game.Battle.Temps[en.TempNo]; t != nil {
					name = t.Name
				}
				if en.PetFlag != 0 {
					info += " 可捕获"
				}
				out.Entries = append(out.Entries, proto.GmEntry{ID: id, Name: name, Info: info})
			}
		case "skill":
			for id, s := range w.game.Battle.Skills {
				memo := []rune(strings.TrimSpace(s.Comment))
				if len(memo) > 18 {
					memo = append(memo[:18], '…')
				}
				out.Entries = append(out.Entries, proto.GmEntry{ID: id, Name: s.Name, Info: string(memo)})
			}
		case "player":

			for _, p := range w.players {
				out.Entries = append(out.Entries, proto.GmEntry{ID: p.id, Name: p.name, Info: fmt.Sprintf("Lv%d 楼层 %d (%d,%d)", p.char.Level, p.floor, p.x, p.y)})
			}
		default:
			return
		}
		slices.SortFunc(out.Entries, func(a, b proto.GmEntry) int { return cmp.Compare(a.ID, b.ID) })
		e.client.Send(out)
	})
}

// gmReply 是 GM 指令的回应（聊天栏，也列在 GM 窗口里）。
func gmReply(e *entity, s string) {
	e.client.Send(&proto.ChatMsg{Channel: "near", From: "GM", Text: s, Color: 4})
}

// gmCommand 是 GM 指令（对应原版 chatmagic.c 的 GM 指令；只有 config.yaml 的 gms 里的账号能用，聊天里打或从选单选，清单见 gmMenu）。
// 每个指令做完都回一句话（gmReply）。返回 true 表示已当作指令处理。
func (w *World) gmCommand(e *entity, text string) bool {
	f := strings.Fields(text)
	reply := func(format string, a ...any) { gmReply(e, fmt.Sprintf(format, a...)) }
	// num(i, lo, hi)：第 i 个参数，必须是 lo..hi 的整数
	bad := false
	num := func(i, lo, hi int) int {
		if i >= len(f) {
			bad = true
			return lo
		}
		n, err := strconv.Atoi(f[i])
		if err != nil || n < lo || n > hi {
			bad = true
			return lo
		}
		return n
	}
	usage := func() bool {
		for _, m := range gmMenu {
			if m.Cmd == f[0] {
				reply("用法：%s %s", m.Cmd, strings.Join(m.Args, " "))
				return true
			}
		}
		reply("用法不对：%s", text)
		return true
	}
	// 指名的玩家（这条线路上在线的，名字要完全一样）
	named := func() *entity {
		if len(f) < 2 {
			return nil
		}
		name := strings.Join(f[1:], " ")
		for _, p := range w.players {
			if p.name == name {
				return p
			}
		}
		reply("这条线路上没有 %s", name)
		return nil
	}
	// player(i)：第 i 个参数指名的玩家（多个参数的指令用，名字不能有空格）
	player := func(i int) *entity {
		if i >= len(f) {
			bad = true
			return nil
		}
		for _, p := range w.players {
			if p.name == f[i] {
				return p
			}
		}
		reply("这条线路上没有 %s", f[i])
		return nil
	}
	c := &e.char
	changed := func() { w.sendStatus(e); w.saveProgress(e) }
	switch f[0] {
	case "/warp":
		fl, x, y := num(1, 0, math.MaxInt32), num(2, 0, math.MaxInt32), num(3, 0, math.MaxInt32)
		if bad {
			return usage()
		}
		fo := w.game.Floors[fl]
		switch {
		case e.fight != nil:
			reply("战斗中不能传送")
		case fo == nil || !fo.In(x, y):
			reply("没有楼层 %d 的 (%d,%d)", fl, x, y)
		default:
			w.placeOnFloor(e, fl, x, y, e.dir)
			reply("传送到 %s (%d,%d)", fo.Name, x, y)
		}
	case "/goto", "/call":
		if len(f) < 2 {
			return usage()
		}
		t := named()
		if t == nil {
			return true
		}
		if t == e {
			reply("那是你自己")
			return true
		}
		from, to := e, t
		if f[0] == "/call" {
			from, to = t, e
		}
		if from.fight != nil {
			reply("%s 在战斗中", from.name)
			return true
		}
		w.placeOnFloor(from, to.floor, to.x, to.y, from.dir)
		if f[0] == "/call" {
			gmReply(t, "GM 把你叫了过来")
			reply("把 %s 叫到身边", t.name)
		} else {
			reply("来到 %s 身边", t.name)
		}
	case "/who":
		var b strings.Builder
		n := 0
		for _, p := range w.players {
			if n < 30 {
				fmt.Fprintf(&b, " %s(Lv%d 楼层%d)", p.name, p.char.Level, p.floor)
			}
			n++
		}
		reply("在线 %d 人：%s", n, b.String())
	case "/heal":
		w.healAll(e)
		reply("自己与宠物都回满了")
	case "/noenc":
		if e.noenemy > 0 {
			e.noenemy = 0
			reply("恢复遇敌")
		} else {
			e.noenemy = math.MaxInt32
			reply("不会遇敌了（再按一次恢复）")
		}
	case "/encount": // /encount [敌人编号 [等级]]：不带参数按遇敌区域；带编号就和那一种敌人单挑（测试没有图的敌人、技能等）
		if e.fight != nil {
			reply("已经在战斗中")
			return true
		}
		if len(f) > 1 {
			id := num(1, 0, math.MaxInt32)
			lv := 0
			if len(f) > 2 {
				lv = num(2, 1, MaxLevel)
			}
			en := w.game.Battle.Enemies[id]
			if bad || en == nil {
				reply("没有编号 %d 的敌人", id)
				return true
			}
			if lv <= 0 {
				lv = en.LvMin
			}
			s := battle.EnemySpec{Enemy: en, Temp: w.game.Battle.Temps[en.TempNo], Level: lv, DropRate: w.Rules.DropRate}
			w.startBattleWith(e, []*battle.Fighter{battle.NewEnemy(s, battle.EnemySlot(0), w.game.Battle.Skills, w.rnd)}, nil, nil)
			return true
		}
		a := w.game.Battle.AreaAt(e.floor, e.x, e.y)
		if a == nil {
			reply("这里不是遇敌区域")
			return true
		}
		w.startBattle(e, a)
	case "/announce":
		msg := strings.TrimSpace(strings.TrimPrefix(text, f[0]))
		if msg == "" {
			return usage()
		}
		cm := &proto.ChatMsg{Channel: "world", Text: "【公告】" + msg, Color: 4}
		for _, p := range w.players {
			p.client.Send(cm)
		}
		reply("公告已发给 %d 人", len(w.players))
	case "/setpw", "/setsecret": // 替在线玩家改账号的密码 / 安全码：明文在这里算成 MD5（客户端平时就是这么送的）
		t := player(1)
		if bad || len(f) < 3 {
			return usage()
		}
		if t == nil {
			return true
		}
		v := strings.Join(f[2:], " ")
		lo, hi, what := 8, 64, "密码"
		if f[0] == "/setsecret" {
			lo, hi, what = 4, 32, "安全码"
		}
		if n := len(v); n < lo || n > hi {
			reply("%s要 %d..%d 位", what, lo, hi)
			return true
		}
		sum := md5.Sum([]byte(v))
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		var err error
		if f[0] == "/setpw" {
			err = w.db.SetPassword(ctx, t.account, hex.EncodeToString(sum[:]))
		} else {
			err = w.db.SetSecret(ctx, t.account, hex.EncodeToString(sum[:]))
		}
		if err != nil {
			reply("改%s失败：%v", what, err)
			return true
		}
		w.log.Info("GM 改了玩家账号的"+what, "gm", e.name, "char", t.name, "account", t.account)
		gmReply(t, "GM 替你改了"+what)
		reply("%s（账号 %s）的%s已改", t.name, t.account, what)
	case "/kick":
		if len(f) < 2 {
			return usage()
		}
		t := named()
		if t == nil {
			return true
		}
		if t == e {
			reply("不能踢自己")
			return true
		}
		w.log.Info("GM 踢人", "gm", e.name, "char", t.name)
		t.client.Kick("被 GM 请下线")
		w.leave(t)
		reply("%s 已下线", t.name)

	case "/lv":
		n := num(1, 1, MaxLevel)
		if bad {
			return usage()
		}
		c.Level = n
		c.Exp = max(0, w.game.Battle.NextLevelExp(n-1)) // 经验改成这一级的起点
		changed()
		reply("等级设为 %d", n)
	case "/exp":
		n := num(1, 1, math.MaxInt32)
		if bad {
			return usage()
		}
		from := c.Level
		ups := w.gainExp(c, n)
		changed()
		if ups > 0 {
			reply("得到经验 %d，从 %d 级升到 %d 级", n, from, c.Level)
		} else {
			reply("得到经验 %d", n)
		}
	case "/points":
		n := num(1, 0, 10000)
		if bad {
			return usage()
		}
		c.Points = n
		changed()
		reply("可分配点数设为 %d（在状态窗口里分配）", n)
	case "/stat":
		v := [4]int{num(1, 0, 1000), num(2, 0, 1000), num(3, 0, 1000), num(4, 0, 1000)}
		if bad {
			return usage()
		}
		c.Vital, c.Str, c.Tgh, c.Dex = v[0], v[1], v[2], v[3]
		c.HP = -1
		changed()
		s := w.stats(e)
		reply("体力 %d、腕力 %d、耐力 %d、敏捷 %d → 耐久力 %d、攻击 %d、防御 %d、敏捷 %d", v[0], v[1], v[2], v[3], s.MaxHP, s.Atk, s.Def, s.Quick)
	case "/attr":
		v := [4]int{num(1, 0, 10), num(2, 0, 10), num(3, 0, 10), num(4, 0, 10)}
		if bad {
			return usage()
		}
		if v[0]+v[1]+v[2]+v[3] > 10 {
			reply("四项属性加起来不能超过 10")
			return true
		}
		c.Earth, c.Water, c.Fire, c.Wind = v[0], v[1], v[2], v[3]
		changed()
		reply("属性设为 地%d 水%d 火%d 风%d", v[0], v[1], v[2], v[3])
	case "/charm":
		n := num(1, 0, 100)
		if bad {
			return usage()
		}
		c.Charm = n
		changed()
		reply("魅力设为 %d", n)
	case "/dp":
		n := num(1, 0, maxDuelPoint)
		if bad {
			return usage()
		}
		c.DP = n
		changed()
		reply("DP 设为 %d", n)
	case "/trans":
		n := num(1, 0, 5)
		if bad {
			return usage()
		}
		c.Trans = n
		changed()
		reply("转生次数设为 %d", n)

	case "/item":
		id := num(1, 0, math.MaxInt32)
		count := 1
		if len(f) > 2 {
			count = num(2, 1, 20)
		}
		if bad {
			return usage()
		}
		def := w.game.Items[id]
		if def == nil {
			reply("没有编号 %d 的道具", id)
			return true
		}
		got := 0
		for ; got < count; got++ {
			if w.addItem(e.inv, w.newItem(def)) < 0 {
				break
			}
		}
		if got > 0 {
			w.itemsChanged(e)
		}
		switch {
		case got == 0:
			reply("道具栏已满，没有给%s", def.Name)
		case got < count:
			reply("得到 %s × %d（道具栏满了，少给 %d 个）", def.Name, got, count-got)
		default:
			reply("得到 %s × %d", def.Name, got)
		}
	case "/gold":
		n := num(1, 0, math.MaxInt32)
		if bad {
			return usage()
		}
		top := maxGold(c.Trans)
		c.Gold = min(n, top)
		w.itemsChanged(e)
		if n > top {
			reply("石币设为 %d（%d 转的上限）", c.Gold, c.Trans)
		} else {
			reply("石币设为 %d", c.Gold)
		}
	case "/fame":
		n := num(1, 0, math.MaxInt32/100)
		if bad {
			return usage()
		}
		c.Fame = 0
		w.earnFame(e, n*100)
		reply("声望设为 %d", c.Fame/100)

	case "/pet": // /pet 敌人编号 [等级]：照捕获的方式给一只宠物（enemy1.txt 的编号）
		id := num(1, 0, math.MaxInt32)
		lv := 0
		if len(f) > 2 {
			lv = num(2, 1, MaxLevel)
		}
		if bad {
			return usage()
		}
		en := w.game.Battle.Enemies[id]
		if en == nil {
			reply("没有编号 %d 的敌人", id)
			return true
		}
		if lv <= 0 {
			lv = en.LvMin
		}
		s := battle.EnemySpec{Enemy: en, Temp: w.game.Battle.Temps[en.TempNo], Level: lv}
		p := petFromFighter(battle.NewEnemy(s, battle.EnemySlot(0), w.game.Battle.Skills, w.rnd), e.char.ID)
		if slot := w.addPet(e, p); slot < 0 {
			reply("宠物栏满了")
		} else {
			w.petsChanged(e)
			reply("得到 %s（%d 级，第 %d 格）", p.Name, p.Level, slot+1)
		}
	case "/petlv": // /petlv 格位 等级：照升级的方式长到那一级
		slot, lv := num(1, 0, PetSlots-1), num(2, 1, MaxLevel)
		if bad {
			return usage()
		}
		p := w.petAt(e, slot)
		switch {
		case p == nil:
			reply("第 %d 格没有宠物", slot+1)
		case lv <= p.Level:
			reply("%s 已经 %d 级了（只能往上升）", p.displayName(), p.Level)
		default:
			ups := w.petGainExp(p, max(0, w.game.Battle.NextLevelExp(lv-1)-p.Exp))
			w.petsChanged(e)
			reply("%s 升了 %d 级，现在 %d 级", p.displayName(), ups, p.Level)
		}
	case "/petai": // /petai 格位 [变动值]：设定宠物的忠诚度变动值（CHAR_VARIABLEAI，预设 10000 = 忠诚度 +100）
		slot, v := num(1, 0, PetSlots-1), battle.MaxVariableAI
		if len(f) > 2 {
			v = num(2, -battle.MaxVariableAI, battle.MaxVariableAI)
		}
		if bad {
			return usage()
		}
		p := w.petAt(e, slot)
		if p == nil {
			reply("第 %d 格没有宠物", slot+1)
			return true
		}
		p.VariableAI = 0
		p.addVariableAI(v)
		w.petsChanged(e)
		reply("%s 的忠诚度为 %d", p.displayName(), w.petLoyalty(e, p))

	case "/war": // /war 对战场 prepare|fight|end|clear|show：把家族对战的排程拨到某个阶段
		id := num(1, 1, 99)
		if bad {
			return usage()
		}
		mode := "show"
		if len(f) > 2 {
			mode = f[2]
		}
		unlock := w.famLock()
		x := w.warOf(id)
		now := w.now().Unix()
		switch mode {
		case "prepare":
			if x.Flag == warNone || x.Flag == warDone {
				unlock()
				reply("对战场 %d 没有排程，先去约战。", id)
				return true
			}
			x.Flag, x.At, x.last = warScheduled, now, -1
		case "fight":
			if x.Flag == warNone || x.Flag == warDone {
				unlock()
				reply("对战场 %d 没有排程，先去约战。", id)
				return true
			}
			x.Flag, x.At, x.Prepare, x.last = warPrepare, now-60, 1, -1
			w.fam.warHome[id] = w
		case "end":
			if x.Flag == warFighting {
				if place := w.warPlaces()[id]; place != nil {
					a, b := w.warCount(x, place)
					winner := 0
					if a > b {
						winner = x.Host
					} else if b > a {
						winner = x.Guest
					}
					w.finishWar(x, place, winner)
				}
			}
		case "clear":
			w.resetWar(x)
		case "show":
		default:
			unlock()
			return usage()
		}
		w.saveWars()
		info := fmt.Sprintf("对战场 %d：flag %d，%s vs %s，at %s，prepare %d 分，win %d", id, x.Flag, x.HostName, x.GuestName, time.Unix(x.At, 0).Format("01-02 15:04"), x.Prepare, x.Win)
		unlock()
		reply("%s", info)

	case "/event": // /event 编号 [done|now|clear]：设定任务旗标（做完 / 进行中 / 清掉）
		n := num(1, 0, math.MaxInt32)
		if bad {
			return usage()
		}
		mode := "done"
		if len(f) > 2 {
			mode = f[2]
		}
		switch mode {
		case "now":
			e.setEventNow(n)
		case "clear":
			e.clearEvent(n)
		case "done":
			e.setEventDone(n)
		default:
			return usage()
		}
		w.saveProgress(e)
		w.flushQuests(e)
		reply("任务旗标 %d 设为 %s。做完：%s　进行中：%s", n, mode, c.EndEvents, c.NowEvents)
	case "/fm":
		n := num(2, math.MinInt32, math.MaxInt32)
		if bad {
			return usage()
		}
		reply("%s", w.gmFamily(e, f[1], n))

	case "/town2": // /town2 村庄：传送到那个村庄的复活点（gmTowns）
		name := strings.Join(f[1:], "") // 指令按空格切开了：村名里的空格（"漆黑的洞窟 地下7楼"）两边都去掉再比
		i := slices.IndexFunc(w.towns, func(t gmTown) bool { return strings.ReplaceAll(t.name, " ", "") == name })
		if i < 0 {
			names := make([]string, len(w.towns))
			for k, t := range w.towns {
				names[k] = t.name
			}
			reply("用法：/town2 村庄；村庄有：%s", strings.Join(names, "、"))
			return true
		}
		if e.fight != nil {
			reply("战斗中不能传送")
			return true
		}
		h := w.towns[i].at
		w.placeOnFloor(e, h.Floor, h.X, h.Y, e.dir)
		reply("传送到 %s 的复活点 (%d,%d)", w.towns[i].name, h.X, h.Y)
	case "/town":
		towns := []string{"萨姆吉尔", "玛丽娜丝", "加加", "卡鲁它那"}
		i := slices.Index(towns, strings.Join(f[1:], ""))
		if i < 0 {
			return usage()
		}
		h := data.Hometowns[i]
		if e.fight != nil {
			reply("战斗中不能传送")
			return true
		}
		w.placeOnFloor(e, h.Floor, h.X, h.Y, e.dir)
		reply("回到%s村的村长家", towns[i])
	case "/hp", "/mp":
		n := num(1, 0, math.MaxInt32)
		if bad {
			return usage()
		}
		maxHP, maxMP := w.maxHPMP(e)
		if f[0] == "/hp" {
			c.HP = max(1, min(n, maxHP))
			if c.HP == maxHP {
				c.HP = -1 // 满血
			}
			changed()
			reply("耐久力设为 %d / %d", min(max(1, n), maxHP), maxHP)
		} else {
			c.MP = min(n, maxMP)
			changed()
			reply("气力设为 %d / %d", c.MP, maxMP)
		}
	case "/repair":
		n := 0
		fix := func(it *Item) {
			if d := w.game.Items[it.ID]; it != nil && d != nil && d.Durability > 0 && (it.Dur != d.Durability || it.Crush != 0) {
				it.Dur, it.Crush = d.Durability, 0
				n++
			}
		}
		for _, it := range e.inv {
			if it != nil {
				fix(it)
			}
		}
		for _, p := range e.pets.Pets {
			if p != nil {
				for _, it := range p.Items {
					if it != nil {
						fix(it)
					}
				}
			}
		}
		w.itemsChanged(e)
		w.petsChanged(e)
		reply("修好了 %d 件道具", n)
	case "/bank":
		n := num(1, 0, fmMaxBank)
		if bad {
			return usage()
		}
		e.fmc.Bank = n
		c.Family = e.fmc.String()
		w.saveProgress(e)
		reply("家族银行个人帐户设为 %d", n)
	case "/petname":
		slot := num(1, 0, PetSlots-1)
		if bad || len(f) < 3 {
			return usage()
		}
		name := strings.Join(f[2:], " ")
		p := w.petAt(e, slot)
		switch {
		case p == nil:
			reply("第 %d 格没有宠物", slot+1)
		case gbkLen(name) > petNameMax || strings.ContainsAny(name, "|\\"):
			reply("名字太长（最多 16 个字节，中文算 2 个）或有不能用的字")
		default:
			old := p.displayName()
			p.FreeName = name
			w.petsChanged(e)
			reply("%s 改名为 %s", old, name)
		}
	case "/petrank":
		slot, r := num(1, 0, PetSlots-1), num(2, 0, 5)
		if bad {
			return usage()
		}
		p := w.petAt(e, slot)
		if p == nil {
			reply("第 %d 格没有宠物", slot+1)
			return true
		}
		p.Rank = r
		w.petsChanged(e)
		reply("%s 的档次设为 %d（之后每级成长 × %.1f~%.1f）", p.displayName(), r, float64(rankRand[r][0])/100, float64(rankRand[r][1])/100)
	case "/where":

		fo := w.game.Floors[e.floor]
		name := ""
		if fo != nil {
			name = fo.Name
		}
		enc := "不会遇敌"
		if w.game.Battle.AreaAt(e.floor, e.x, e.y) != nil {
			enc = "遇敌区域"
		}
		reply("楼层 %d %s (%d,%d)，%s，这层有 %d 个玩家", e.floor, name, e.x, e.y, enc, w.floorPlayers(e.floor))
	case "/luck":
		n := num(1, 1, 5)
		if bad {
			return usage()
		}
		e.char.Luck = n
		reply("运气设为 %d（到下一个游戏日会重抽）", n)
	case "/give": // /give 名字 道具编号 [个数]
		t := player(1)
		id := num(2, 0, math.MaxInt32)
		count := 1
		if len(f) > 3 {
			count = num(3, 1, 20)
		}
		if bad {
			return usage()
		}
		if t == nil {
			return true
		}
		def := w.game.Items[id]
		if def == nil {
			reply("没有编号 %d 的道具", id)
			return true
		}
		got := 0
		for ; got < count && w.addItem(t.inv, w.newItem(def)) >= 0; got++ {
		}
		if got == 0 {
			reply("%s 的道具栏满了", t.name)
			return true
		}
		w.itemsChanged(t)
		if t != e {
			gmReply(t, fmt.Sprintf("GM 给了你 %s × %d", def.Name, got))
		}
		reply("给了 %s %s × %d", t.name, def.Name, got)
	case "/clearbag":
		if len(f) < 2 || f[1] != "确定" {
			return usage()
		}
		n := 0
		for i := data.EquipSlots; i < data.ItemSlots; i++ {
			if e.inv[i] != nil {
				e.inv[i] = nil
				n++
			}
		}
		w.itemsChanged(e)
		reply("道具栏清空了（丢掉 %d 件）", n)
	case "/petskill": // /petskill 格位 技能栏(1..7) 技能编号
		slot, k, id := num(1, 0, PetSlots-1), num(2, 1, petSkillMax), num(3, 0, math.MaxInt32)
		if bad {
			return usage()
		}
		p, s := w.petAt(e, slot), w.game.Battle.Skills[id]
		switch {
		case p == nil:
			reply("第 %d 格没有宠物", slot+1)
		case s == nil:
			reply("没有编号 %d 的技能", id)
		default:
			p.Skills[k-1] = id
			p.Slot = max(p.Slot, k)
			w.petsChanged(e)
			reply("%s 的第 %d 个技能栏学会了 %s", p.displayName(), k, s.Name)
		}
	case "/petdel":
		slot := num(1, 0, PetSlots-1)
		if bad {
			return usage()
		}
		p := w.petAt(e, slot)
		switch {
		case p == nil:
			reply("第 %d 格没有宠物", slot+1)
		case e.fight != nil:
			reply("战斗中不能删除宠物")
		default:
			if e.pets.Ride == slot {
				w.dismount(e)
			}
			w.removePet(e, slot)
			w.petsChanged(e)
			reply("删除了 %s", p.displayName())
		}
	case "/learnride":
		n := num(1, 1, MaxLevel+60)
		if bad {
			return usage()
		}
		e.pets.LearnRide = n
		w.petsChanged(e)
		reply("可以骑乘 %d 级以下的宠物", n)
	case "/look":
		t := player(1)
		if bad {
			return usage()
		}
		if t == nil {
			return true
		}
		tc := &t.char
		var pets []string
		for _, p := range t.pets.Pets {
			if p != nil {
				pets = append(pets, fmt.Sprintf("%s(%d)", p.displayName(), p.Level))
			}
		}
		state := ""
		if t.fight != nil {
			state = "，战斗中"
		}
		maxHP, _ := w.maxHPMP(t)
		hp := tc.HP
		if hp < 0 {
			hp = maxHP
		}
		reply("%s：Lv%d %d转，楼层 %d (%d,%d)%s，HP %d/%d，石币 %d，声望 %d，DP %d，宠物 %s", t.name, tc.Level, tc.Trans, t.floor, t.x, t.y, state,
			hp, maxHP, tc.Gold, tc.Fame/100, tc.DP, strings.Join(pets, " "))
	case "/mute": // /mute 名字 分钟（0 解除）
		t := player(1)
		m := num(2, 0, 10080)
		if bad {
			return usage()
		}
		if t == nil {
			return true
		}
		if m == 0 {
			t.muteUntil = time.Time{}
			gmReply(t, "你的禁言解除了")
			reply("解除 %s 的禁言", t.name)
		} else {
			t.muteUntil = w.now().Add(time.Duration(m) * time.Minute)
			gmReply(t, fmt.Sprintf("你被 GM 禁言 %d 分钟", m))
			reply("%s 禁言 %d 分钟", t.name, m)
		}

	default:
		return false
	}
	return true
}

// gmFamily 是 /fm：给自己的家族做测试资料。
func (w *World) gmFamily(e *entity, what string, n int) string {
	defer w.famLock()()
	fm, _, _ := w.fmOf(e)
	if fm == nil {
		return "你没有家族"
	}
	switch what {
	case "fame":
		fm.Adv = max(0, fm.Adv+n*100)
	case "fill": // 假族人：不在线的正式族人（ID 用负数，不会和真的角色重复）
		for i := 1; i < fmMaxMember && n > 0; i++ {
			if fm.Members[i] == nil {
				id := -int64(fm.ID*100 + i)
				fm.Members[i] = &fmMember{ID: id, Name: fmt.Sprintf("族人%d", i), Level: 1, Duty: fmDutyMember}
				w.fam.byChar[id] = fm
				n--
			}
		}
		fm.recount()
	case "manor":
		if n < 0 {
			if i := w.fam.manorOf(fm); i >= 0 {
				w.fam.manors[i] = manorState{}
				w.fam.saveKV("manors", w.fam.manors)
				w.resyncFamily(fm)
			}
		} else if n < len(manorTable) {
			w.claimManor(fm, n)
		}
	case "gold":
		fm.Gold = max(0, min(fmMaxGold, n))
	default:
		return "用法：/fm fame 点数 | fill 人数 | manor 编号 | gold 数量"
	}
	w.fam.save(fm)
	return fmt.Sprintf("%s：%d 人（正式 %d），声望 %d，气势 %d，庄园 %d，共同帐户 %d", fm.Name, fm.num(), fm.joinNum(),
		fm.total()/100, fm.momentum()/100, w.fam.manorOf(fm), fm.Gold)
}

// floorPlayers 是这层楼上的玩家数（/where）。
func (w *World) floorPlayers(floor int) int {
	n := 0
	for _, p := range w.players {
		if p.floor == floor {
			n++
		}
	}
	return n
}

// gmTown 是 GM /town2 的一个目的地：村庄名（复活点所在楼层的名字）与位置。
type gmTown struct {
	name string
	at   data.Hometown
}

// gmTowns 列出所有村庄的复活点：先是 data.Game.Elders（char_data.c 的 elders[] 初值 + 每个记录点 NPC 的 Born），
// 按长老编号排序、同一个位置只列一次，名字用那一层楼的名字（重名就加上编号）；
// 再补上名字像村庄 / 城镇（结尾是 村、城、园、渔村，不是民房 / 店 / 医院 / 庄园 / 地下 / 回忆里的那种）而且有 NPC、却没有记录点的楼层
// （奇努伊村、达那村、蒙达村、伊甸园、弗烈顿火城、亚伊欧风城……），落点是护士 / 道具店 / 村民之一的旁边。
func gmTowns(game *data.Game) []gmTown {
	ids := make([]int, 0, len(game.Elders))
	for id := range game.Elders {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	var out []gmTown
	seen := map[data.Hometown]bool{}
	names := map[string]int{}
	for _, id := range ids {
		h := game.Elders[id]
		if seen[h] || game.Floors[h.Floor] == nil {
			continue
		}
		seen[h] = true
		name := cleanFloorName(game.Floors[h.Floor].Name) // 有的楼层名尾巴带坏字节
		if name == "" {
			name = fmt.Sprintf("楼层%d", h.Floor)
		}
		names[name]++
		if names[name] > 1 {
			name = fmt.Sprintf("%s%d", name, names[name])
		}
		out = append(out, gmTown{name: name, at: h})
	}
	// 没有记录点的村庄 / 城镇：按楼层编号
	floors := make([]int, 0, len(game.Floors))
	for id := range game.Floors {
		floors = append(floors, id)
	}
	slices.Sort(floors)
	listed := map[int]bool{}
	for _, t := range out {
		listed[t.at.Floor] = true
	}
	for _, id := range floors {
		name := cleanFloorName(game.Floors[id].Name)
		if listed[id] || !townLikeName(name) || len(game.NPCs[id]) == 0 {
			continue
		}
		n := townAnchorNPC(game.NPCs[id])
		if n == nil {
			continue
		}
		at := data.Hometown{Floor: id, X: n.X, Y: n.Y}
		if f := game.Floors[id]; game.Tiles.Walkable(f, n.X, n.Y+1) {
			at.Y++ // 站到 NPC 前面一格
		}
		names[name]++
		if names[name] > 1 {
			name = fmt.Sprintf("%s%d", name, names[name])
		}
		listed[id] = true
		out = append(out, gmTown{name: name, at: at})
	}
	return out
}

// cleanFloorName 去掉楼层名尾巴的坏字节与空白。
func cleanFloorName(s string) string {
	return strings.TrimFunc(s, func(r rune) bool { return r == utf8.RuneError || !unicode.IsGraphic(r) || unicode.IsSpace(r) })
}

// townLikeName：名字像一个村庄 / 城镇本身（不是它里面的某间房子或别的时代的回忆）。
func townLikeName(name string) bool {
	for _, bad := range []string{"家", "店", "医院", "民房", "庄园", "地下", "通路", "年前", "宿舍", "官邸", "训练", "游乐", "园区", "大陆", "柜台", "聊天", "PK", "道场", "事务所", "大厅"} {
		if strings.Contains(name, bad) {
			return false
		}
	}
	for _, suf := range []string{"村", "城", "园", "渔村"} {
		if strings.HasSuffix(name, suf) {
			return true
		}
	}
	return false
}

// townAnchorNPC 挑落点旁边的 NPC：护士 > 记录点 > 道具店 > 传送员 > 村民 > 随便一个。
func townAnchorNPC(npcs []data.NPC) *data.NPC {
	for _, fn := range []string{"WindowHealer", "SavePoint", "ItemShop", "WarpMan", "TownPeople", ""} {
		for i := range npcs {
			if (fn == "" || npcs[i].Function == fn) && npcs[i].Name != "" {
				return &npcs[i]
			}
		}
	}
	return nil
}

// gmMenuItems 是送给 GM 的指令选单：gmMenu 加上开机才知道的选项（/town2 的村庄清单）。
func (w *World) gmMenuItems() []proto.GmItem {
	items := slices.Clone(gmMenu)
	for i := range items {
		if items[i].Cmd == "/town2" {
			names := make([]string, len(w.towns))
			for k, t := range w.towns {
				names[k] = t.name
			}
			items[i].Kinds = []string{"opt:" + strings.Join(names, "|")}
			if len(names) > 0 {
				items[i].Defaults = []string{names[0]}
			}
		}
	}
	return items
}
