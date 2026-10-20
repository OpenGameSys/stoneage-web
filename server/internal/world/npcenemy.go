package world

import (
	"strconv"
	"strings"
	"sync"
	"time"

	"sa/server/internal/battle"
	"sa/server/internal/data"
	"sa/server/internal/proto"
)

// 战斗 NPC（npc_npcenemy.c，functionset NPCEnemy，281 个）：说话（或走到它身上）后与参数 enemyno 指定的敌人战斗，
// 打赢后按 dieact 把玩家传送到 warpfl/warpx/warpy，或让 NPC 消失 time 秒再出现。gym 是道场的弟子（Doujyou_GetEnemy）。
//
// 参数（NPC_Util_GetStrFromStrWithDelim / NPC_Util_GetNumFromStrWithDelim，都是子串比对）：
//   entype     0 只有走到身上会打、1 只有说话会打、2 两者都会（数据全是 2）
//   askbattlemsg1..6  开打前的是/否确认窗口；没有就直接开打
//   item / noitem     身上要有（没有）这些道具；B_evend / B_evnow 要做完（进行中）这些任务
//   onebattle:1       同一时间只能有一场（alreadymsg）；deniedmsg 是条件不合时说的话
//   startmsg / endmsg 开打时、打赢后说的话；steal:0/1 开打时 / 打赢后收走 item 指定的道具；additem 打赢后给的道具
//   dieact:0 打赢后消失 time 秒（预设 120）；dieact:1 打赢后传送（NEWNPCENEMY 的按 OVER 各段的 FREE 条件选 WARP）
//
// 没做的部分：组队（CHAR_talkToCliAndParty 只对自己说、只传送自己）、NEWTIME/Time_Msg（NPC_NPCEnemyWatch 的时间，数据没用到）、
// sktype（BATTLE_EnemyRandowSetSkill，数据没用到）、_NPC_REPLACEMENT 的 REPLACEMENT（Check_EnemyWarpMe，数据没用到）、
// 道场弟子的随机外形（NPC_NPCEnemyInit 的 gymbody：NPC 照 .create 的图号；战斗中第一个敌人用 NPC 的图号与名字）、
// 战斗类型 BATTLE_TYPE_BOSS_BATTLE（客户端放 BOSS 音乐用，协议里还没有）、道场的 norisk（还没有战败惩罚）。

const (
	seqNpcEnemyStart  = 281             // CHAR_WINDOWTYPE_NPCENEMY_START
	npcEnemyRevival   = 120             // NPC_ENEMY_REVIVALTIMA_DEFAULT（秒）
	npcEnemyMaxEnemy  = 10              // NPC_ENEMY_ENEMYNUMBER
	npcEnemyMaxGym    = 64              // MAX_DOUJYOUENEMY
	npcEnemyLoopEvery = 5 * time.Second // NPC_NPCEnemy_Dying 把 CHAR_LOOPINTERVAL 设为 5000
)

// npcEnemyDied 是 CHAR_WORK_DIETIME：*entity → 最后一次被打倒的时间。实体只在自己的世界协程里用，换线路的世界之间互不相干。
var npcEnemyDied sync.Map

// npcArgNum 是 NPC_Util_GetNumFromStrWithDelim：第一个含 key 的段（只看前 31 字节，char outstr[32]）里的第一串数字（textToInt）；
// 没有这段时是 -1。所以 "time=300" 也读得到 300。
func npcArgNum(arg, key string) int {
	for _, seg := range strings.Split(arg, "|") {
		seg = gbkCut(seg, 31)
		if !strings.Contains(seg, key) {
			continue
		}
		i := strings.IndexAny(seg, "0123456789")
		if i < 0 {
			return 0
		}
		j := i
		for j < len(seg) && seg[j] >= '0' && seg[j] <= '9' {
			j++
		}
		n, _ := strconv.Atoi(seg[i:j])
		return n
	}
	return -1
}

// gbkCut 取字串在原版（GBK）里的前 n 个字节（不切开全角字）。
func gbkCut(s string, n int) string {
	w := 0
	for i, r := range s {
		d := 1
		if r > 0xff {
			d = 2
		}
		if w+d > n {
			return s[:i]
		}
		w += d
	}
	return s
}

// npcArgList 把 "a,b,c" 拆开（getStringFromIndexWithDelim 依次取段）；空字串一段都取不到。
func npcArgList(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}

// enemyNo 是 enemyno 的值。NPC_NPCEnemyInit 与 NPC_Util_getEnemy 用 64 字节的缓冲，超过的部分读不到。
func enemyNo(arg string, size int) []string {
	s, _ := data.ArgField(arg, "enemyno")
	return npcArgList(gbkCut(s, size-1))
}

// npcEnemyValid 是 NPC_NPCEnemyInit 的检查：初始化失败的 NPC 原版根本不会出现，这里让它不理人。
// enemyno 前 10 个都要是存在的敌人；有 steal 就要有 item；dieact:1（不是 NEWNPCENEMY）要有合法的 warpfl/warpx/warpy。
func (w *World) npcEnemyValid(n *entity) bool {
	arg, bd := n.npc.Arg, w.game.Battle
	if bd == nil {
		return false
	}
	ids := enemyNo(arg, 64)
	if _, ok := data.ArgField(arg, "enemyno"); !ok {
		return false
	}
	for i, s := range ids {
		if i >= npcEnemyMaxEnemy {
			break
		}
		if bd.Enemies[data.Atoi(s)] == nil {
			return false
		}
	}
	if _, ok := data.ArgField(arg, "steal"); ok {
		if _, ok := data.ArgField(arg, "item"); !ok {
			return false
		}
	}
	if npcArgNum(arg, "dieact") == 1 && !strings.Contains(arg, "NEWNPCENEMY") {
		fl, ok1 := data.ArgField(arg, "warpfl")
		x, ok2 := data.ArgField(arg, "warpx")
		y, ok3 := data.ArgField(arg, "warpy")
		if !ok1 || !ok2 || !ok3 || !w.validPoint(data.Atoi(fl), data.Atoi(x), data.Atoi(y)) {
			return false
		}
	}
	return true
}

// validPoint 是 MAP_IsValidCoordinate：楼层存在、坐标在地图内。
func (w *World) validPoint(floor, x, y int) bool {
	f := w.game.Floors[floor]
	return f != nil && x >= 0 && y >= 0 && x < f.W && y < f.H
}

// npcEnemyType 是 CHAR_WORK_ENCOUNTTYPE（entype：不是 1、2 的都当 0）。
func npcEnemyType(arg string) int {
	if t := npcArgNum(arg, "entype"); t == 1 || t == 2 {
		return t
	}
	return 0
}

// npcEnemyTalk 是 NPC_NPCEnemyTalked：要面向 NPC、它就在面前一格，而且 entype 允许说话开打。
func (w *World) npcEnemyTalk(p, n *entity) {
	if !w.npcEnemyValid(n) || !inFront(p, n, 1) || npcEnemyType(n.npc.Arg) < 1 {
		return
	}
	w.npcEnemyEncount(p, n, 1)
}

// npcEnemyBump 是 EVENY_npcEncount（玩家走向 CHAR_EVENT_ENEMY 的 NPC 时客户端发 EV）：e 走到战斗 NPC 所在的格子时，
// 照 char_walk.c 退回原来的格子，并以 mode 0 交给 NPC_NPCEnemy_Encount。回传有没有碰到。
func (w *World) npcEnemyBump(e *entity) bool {
	var n *entity
	for _, o := range w.floors[e.floor] {
		if o.npc != nil && o.npc.Function == "NPCEnemy" && o.x == e.x && o.y == e.y && !o.hidden() && w.npcEnemyValid(o) {
			n = o
			break
		}
	}
	if n == nil {
		return false
	}
	e.x, e.y = e.x-MOVE[e.dir][0], e.y-MOVE[e.dir][1]
	e.client.Send(&proto.PosCorrect{X: e.x, Y: e.y, Dir: e.dir})
	w.broadcastMove(e)
	w.npcEnemyEncount(e, n, 0)
	return true
}

// npcEnemyCheck 是 NPC_NPCEnemy_Encount 的条件部分：item 全都有、noitem 全都没有（_ADD_NOITEM_BATTLE）、
// B_evend 全做完、B_evnow 全在进行中。道具是整个道具栏（含装备，CHAR_MAXITEMHAVE）。
func (w *World) npcEnemyCheck(p *entity, arg string) bool {
	if s, ok := data.ArgField(arg, "item"); ok {
		ids := npcArgList(s)
		if len(ids) == 0 {
			return false
		}
		for _, id := range ids {
			if !w.hasItem(p, data.Atoi(id)) {
				return false
			}
		}
	}
	if s, ok := data.ArgField(arg, "noitem"); ok {
		ids := npcArgList(s)
		if len(ids) == 0 {
			return false
		}
		for _, id := range ids {
			if w.hasItem(p, data.Atoi(id)) {
				return false
			}
		}
	}
	if s, ok := data.ArgField(arg, "B_evend"); ok {
		for _, ev := range npcArgList(s) {
			if !p.eventDone(data.Atoi(ev)) {
				return false
			}
		}
	}
	if s, ok := data.ArgField(arg, "B_evnow"); ok {
		for _, ev := range npcArgList(s) {
			if !p.eventNow(data.Atoi(ev)) {
				return false
			}
		}
	}
	return true
}

// npcEnemyBusy 是 onebattle 的检查：这个 NPC 开的战斗还在进行中（BattleArray[i].createindex == meindex）。
func (w *World) npcEnemyBusy(n *entity) bool {
	for _, e := range w.players {
		if e.fight != nil && e.fight.room.npc == n {
			return true
		}
	}
	return false
}

// npcEnemyEncount 是 NPC_NPCEnemy_Encount：mode 0 走到身上、1 说话。消失中不理人；entype 不允许、条件不合时说 deniedmsg；
// onebattle 的 NPC 正在和别人打时说 alreadymsg；有 askbattlemsg1 时先问是/否，否则直接开打（开不成也说 deniedmsg）。
func (w *World) npcEnemyEncount(p, n *entity, mode int) {
	if n.hidden() || p.fight != nil {
		return
	}
	arg := n.npc.Arg
	t := npcEnemyType(arg)
	if mode == 0 && t == 1 || mode == 1 && t == 0 || !w.npcEnemyCheck(p, arg) {
		w.npcEnemySay(p, n, "deniedmsg")
		return
	}
	if npcArgNum(arg, "onebattle") == 1 && w.npcEnemyBusy(n) {
		w.npcEnemySay(p, n, "alreadymsg")
		return
	}
	if msg, ok := data.ArgField(arg, "askbattlemsg1"); ok {
		// 第 2..6 行没有的也补一个换行；原版缓冲 512 字节，超过时什么都不做（"buffer over"）
		var sb strings.Builder
		sb.WriteString(msg)
		for i := 2; i < 7; i++ {
			sb.WriteByte('\n')
			if s, ok := data.ArgField(arg, "askbattlemsg"+strconv.Itoa(i)); ok {
				sb.WriteString(s)
			}
		}
		if gbkWidth(sb.String()) >= 512 {
			return
		}
		w.sendWindow(p, n, "message", BtnYesNo, seqNpcEnemyStart, npcText(sb.String()))
		return
	}
	if !w.npcEnemyBattleIn(p, n) {
		w.npcEnemySay(p, n, "deniedmsg")
	}
}

// npcEnemyReply 是 NPC_NPCEnemyWindowTalked：确认窗口按"是"就开打。
// 原版这时不再检查条件；这里再查一次（服务端权威：开着窗口时可能已经把道具丢了，或别人先开打了）。
func (w *World) npcEnemyReply(p, n *entity, win *npcWindow, button int, _ string) {
	if win.seq != seqNpcEnemyStart || button != BtnYes || n.hidden() || p.fight != nil || !w.npcEnemyValid(n) {
		return
	}
	arg := n.npc.Arg
	if !w.npcEnemyCheck(p, arg) {
		w.npcEnemySay(p, n, "deniedmsg")
		return
	}
	if npcArgNum(arg, "onebattle") == 1 && w.npcEnemyBusy(n) {
		w.npcEnemySay(p, n, "alreadymsg")
		return
	}
	w.npcEnemyBattleIn(p, n)
}

// npcEnemySay：参数里有 key 就让 NPC 说那句（CHAR_talkToCliAndParty / CHAR_talkToCli）。
func (w *World) npcEnemySay(p, n *entity, key string) {
	if msg, ok := data.ArgField(n.npc.Arg, key); ok {
		w.npcSay(p, n, msg)
	}
}

// npcEnemyBattleIn 是 NPC_NPCEnemy_BattleIn：复活时间没到不打；gym > 0 是道场（BATTLE_CreateVsEnemy mode 2），否则 mode 1。
// 开打后说 startmsg；steal:0 的这时就收走道具。
func (w *World) npcEnemyBattleIn(p, n *entity) bool {
	arg := n.npc.Arg
	if p.isFollower() {
		return false // BATTLE_CreateVsEnemy：队员不能开打（BATTLE_ERR_END）
	}
	if v, ok := npcEnemyDied.Load(n); ok && w.now().Before(v.(time.Time).Add(w.npcEnemyRevivalTime(n))) {
		return false
	}
	var enemies []*battle.Fighter
	if gym := npcArgNum(arg, "gym"); gym > 0 {
		enemies = w.doujyouEnemies(n, gym)
	} else {
		enemies = w.npcEnemies(arg)
	}
	if len(enemies) == 0 {
		return false // BATTLE_ERR_NOENEMY
	}
	p.window = nil
	w.startBattleWith(p, enemies, n, func(result string) {
		if result == "win" { // BATTLE_Finish：winside == -1（敌方全倒）才调用 WinFunc
			w.npcEnemyDying(p, n)
		}
	})
	w.npcEnemySay(p, n, "startmsg")
	if s, ok := data.ArgField(arg, "steal"); ok && data.Atoi(s) == 0 {
		w.npcEnemySteal(p, arg)
	}
	return true
}

// npcEnemyRevivalTime 是 CHAR_WORK_REVIVALTIME：参数 time（秒），没写是 120。
func (w *World) npcEnemyRevivalTime(n *entity) time.Duration {
	t := npcArgNum(n.npc.Arg, "time")
	if t == -1 {
		t = npcEnemyRevival
	}
	return time.Duration(t) * time.Second
}

// npcEnemies 是 NPC_Util_getEnemy（mode 1）：enemyno 前 10 个依次出场（不存在的跳过），等级在敌人的等级范围内随机。
// 大型敌人最多 5 只、只站前排：第 6 只以后的大型敌人和前五只里第一只普通敌人对调，前五只都是大型的就不出场。
func (w *World) npcEnemies(arg string) []*battle.Fighter {
	bd := w.game.Battle
	var specs []battle.EnemySpec
	big := 0
	for i, s := range enemyNo(arg, 64) {
		if i >= npcEnemyMaxEnemy {
			break
		}
		e := bd.Enemies[data.Atoi(s)]
		if e == nil || bd.Temps[e.TempNo] == nil {
			continue
		}
		spec := battle.EnemySpec{Enemy: e, Temp: bd.Temps[e.TempNo], Level: e.LvMin + w.rnd.IntN(e.LvMax-e.LvMin+1)}
		if spec.Temp.Size != 1 {
			specs = append(specs, spec)
			continue
		}
		if big >= 5 {
			continue
		}
		if len(specs) > 4 {
			j := -1
			for k := 0; k < 5; k++ {
				if specs[k].Temp.Size != 1 {
					j = k
					break
				}
			}
			if j < 0 {
				continue
			}
			specs = append(specs, specs[j])
			specs[j] = spec
		} else {
			specs = append(specs, spec)
		}
		big++
	}
	out := make([]*battle.Fighter, len(specs))
	for i, s := range specs {
		out[i] = battle.NewEnemy(s, battle.EnemySlot(i), bd.Skills, w.rnd)
	}
	return out
}

// doujyouEnemies 是 Doujyou_GetEnemy（mode 2）：enemyno 里随机一个、enemypetno 里随机一个（有写的话），等级都是 gym；
// 第一个用 NPC 的形象与名字（BATTLE_CreateVsEnemy）。
// 原版 ENEMY_createEnemy 对道场的人形敌人另外随机属性、武器与技能（DoujyouRandomWeponSet / EnemyGymSkill），这里没做。
func (w *World) doujyouEnemies(n *entity, level int) []*battle.Fighter {
	bd := w.game.Battle
	pick := func(key string) *battle.EnemySpec {
		s, _ := data.ArgField(n.npc.Arg, key)
		var list []battle.EnemySpec
		for i, id := range npcArgList(gbkCut(s, 1023)) {
			if i >= npcEnemyMaxGym {
				break
			}
			if e := bd.Enemies[data.Atoi(id)]; e != nil && bd.Temps[e.TempNo] != nil {
				list = append(list, battle.EnemySpec{Enemy: e, Temp: bd.Temps[e.TempNo], Level: level})
			}
		}
		if len(list) == 0 {
			return nil
		}
		return &list[w.rnd.IntN(len(list))]
	}
	first := pick("enemyno")
	if first == nil {
		return nil
	}
	specs := []battle.EnemySpec{*first}
	if pet := pick("enemypetno"); pet != nil {
		specs = append(specs, *pet)
	}
	out := make([]*battle.Fighter, len(specs))
	for i, s := range specs {
		out[i] = battle.NewEnemy(s, battle.EnemySlot(i), bd.Skills, w.rnd)
	}
	out[0].Name, out[0].Sprite = n.name, n.sprite
	return out
}

// npcEnemySteal 是 NPC_NPCEnemy_StealItem：item 里每个编号收走道具栏（含装备）里第一件（整格）；第一个就没有时停下。
func (w *World) npcEnemySteal(p *entity, arg string) {
	s, ok := data.ArgField(arg, "item")
	if !ok {
		return
	}
	found := 0
	for _, id := range npcArgList(s) {
		want := data.Atoi(id)
		for i, it := range p.inv {
			if it != nil && it.ID == want {
				p.inv[i] = nil
				found++
				break
			}
		}
		if found == 0 {
			break
		}
	}
	if found > 0 {
		w.itemsChanged(p)
	}
}

// npcEnemyDying 是 NPC_NPCEnemy_Dying（打赢后的 WinFunc）：说 endmsg（NEWNPCENEMY 的在各段里说）、steal:1 收走道具、
// additem 给道具；dieact:0 让 NPC 消失，dieact:1 把玩家传送走。
func (w *World) npcEnemyDying(p, n *entity) {
	arg := n.npc.Arg
	newType := strings.Contains(arg, "NEWNPCENEMY")
	if !newType {
		w.npcEnemySay(p, n, "endmsg")
	}
	if s, ok := data.ArgField(arg, "steal"); ok && data.Atoi(s) == 1 {
		w.npcEnemySteal(p, arg)
	}
	if s, ok := data.ArgField(arg, "additem"); ok {
		if def := w.game.Items[data.Atoi(s)]; def != nil && w.addItem(p.inv, w.newItem(def)) >= 0 {
			w.itemsChanged(p)
			w.sysMsg(p, "获得"+def.Name, colorYellow)
		}
	}
	if npcArgNum(arg, "dieact") != 1 {
		w.npcEnemyHide(n)
		return
	}
	if !newType {
		fl, _ := data.ArgField(arg, "warpfl")
		x, _ := data.ArgField(arg, "warpx")
		y, _ := data.ArgField(arg, "warpy")
		w.warpTo(p, data.Atoi(fl), data.Atoi(x), data.Atoi(y))
		return
	}
	if pt, ok := w.npcEnemyCheckFree(p, n); ok && pt != [3]int{} {
		w.warpTo(p, pt[0], pt[1], pt[2])
	}
}

// npcEnemyCheckFree 是 NPC_NPCEnemy_CheckFree（_NEW_WARPMAN）：以 OVER 分开的各段里，找第一个含 NEWEVENT、FREE 条件成立、
// 而且写了 WARP 的段：说那段的 endmsg，WARP 以 ";" 分开的几个点随机取一个（楼层不对时用第一个），执行那段的动作。
// CHECKPARTY:FALSE（只传送队伍）要有组队，这里没做，当作一般的情形。
func (w *World) npcEnemyCheckFree(p, n *entity) ([3]int, bool) {
	for _, b := range strings.Split(n.npc.Arg, "OVER") {
		if !strings.Contains(b, "NEWEVENT") {
			continue
		}
		free, ok := data.ArgField(b, "FREE")
		if !ok || !w.passCheck(p, free) {
			continue
		}
		if msg, ok := data.ArgField(b, "endmsg"); ok {
			w.npcSay(p, n, msg)
		}
		spec, ok := data.ArgField(b, "WARP")
		if !ok {
			continue
		}
		var pts [][3]int
		for _, s := range strings.Split(spec, ";") {
			if s == "" || len(pts) == 15 {
				break
			}
			v := append(strings.Split(s, ","), "", "")
			pts = append(pts, [3]int{data.Atoi(v[0]), data.Atoi(v[1]), data.Atoi(v[2])})
		}
		if len(pts) == 0 {
			return [3]int{}, true
		}
		i := w.rnd.IntN(len(pts))
		if pts[i][0] <= 0 {
			i = 0
		}
		w.runEventAction(p, b) // Action_RunDoEventAction
		return pts[i], true
	}
	return [3]int{}, false
}

// npcEnemyHide 是 NPC_NPCEnemy_Dying 的 dieact:0：图号设为 0（这里用 TimeMan 的消失图号，hidden() 为真）、从大家的视野里拿掉，
// 记下 CHAR_WORK_DIETIME，改由 NPC_NPCEnemyLoop 每 5 秒检查复活。已经消失的只更新时间。
func (w *World) npcEnemyHide(n *entity) {
	npcEnemyDied.Store(n, w.now())
	if n.hidden() {
		return
	}
	n.sprite = timeManHidden
	for _, o := range w.floors[n.floor] {
		if o.client != nil && o.visible[n.id] {
			delete(o.visible, n.id)
			o.client.Send(&proto.ObjDisappear{IDs: []int{n.id}})
		}
	}
	w.npcEnemyArm(n, w.npcEnemyRevivalTime(n)+time.Second)
}

func (w *World) npcEnemyArm(n *entity, d time.Duration) {
	time.AfterFunc(d, func() { w.Do(func() { w.npcEnemyLoop(n) }) })
}

// npcEnemyLoop 是 NPC_NPCEnemyLoop：过了 DIETIME + REVIVALTIME 就换回原来的图号（CHAR_WORK_BASEIMGBAK）重新出现；还没到就 5 秒后再看。
func (w *World) npcEnemyLoop(n *entity) {
	if !n.hidden() {
		return
	}
	v, ok := npcEnemyDied.Load(n)
	if ok && !w.now().After(v.(time.Time).Add(w.npcEnemyRevivalTime(n))) {
		w.npcEnemyArm(n, npcEnemyLoopEvery)
		return
	}
	n.sprite = n.npc.Sprite
	w.announce(n)
}
