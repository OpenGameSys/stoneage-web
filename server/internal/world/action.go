package world

import (
	"strings"

	"sa/server/internal/data"
	"sa/server/internal/proto"
)

// 动作（callfromcli.c lssproto_AC_recv、char.c CHAR_sendAction、char_talk.c TalkAction、npc_action.c）：
//
//	动作编号是客户端的 ANIM_*：0 攻击 1 受伤 2 晕倒 3 站立 4 走动 5 坐下 6 挥手 7 高兴 8 生气 9 悲伤 10 防御 11 点头 12 投掷
//	做动作    位置要和服务端一致（不一致就不理），广播给附近的人（不回给自己）；动作留在身上（CHAR_WORKACTION），
//	          后来才看到的人也看到那个姿势，走路、开打、传送就清掉
//	说话      话里有"坐下""挥手"等字也做那个动作（_TALK_ACTION，这时自己也收到）
//	NPC       "看动作的人"（Action）面对面 1 格时按动作回一句参数里的话（attack、sit…）；猜拳赢了高兴、输了悲伤
//
// 打呼、冷场原版只在自己的画面上（不送服务端），在客户端做。

const (
	actAttack = iota
	actDamage
	actDown
	actStand
	actWalk
	actSit
	actHand
	actHappy
	actAngry
	actSad
	actGuard
	actNod
	actThrow
	actCount
)

// talkActions 是 TalkAction 的关键字，按原版的顺序找第一个。
var talkActions = []struct {
	word string
	act  int
}{{"坐下", actSit}, {"挥手", actHand}, {"点头", actNod}, {"高兴", actHappy}, {"生气", actAngry}, {"悲伤", actSad},
	{"站立", actStand}, {"走动", actWalk}, {"晕倒", actDown}, {"攻击", actAttack}, {"防御", actGuard}, {"受伤", actDamage}, {"投掷", actThrow}}

// actionWords 是 NPC_ActionWatch 的参数关键字（站立、走动没有）。
var actionWords = map[int]string{actAttack: "attack", actDamage: "damage", actDown: "down", actSit: "sit", actHand: "hand",
	actHappy: "pleasure", actAngry: "angry", actSad: "sad", actGuard: "guard", actNod: "nod", actThrow: "throw"}

// Action 是 lssproto_AC_recv。
func (w *World) Action(charID, x, y, act int) {
	w.Do(func() {
		e := w.players[charID]
		if e == nil || e.fight != nil || e.x != x || e.y != y || act < 0 || act >= actCount {
			return
		}
		w.doAction(e, act, false)
	})
}

// doAction 是 CHAR_sendAction：self 为 true 时也送给自己（TalkAction、猜拳）。
func (w *World) doAction(e *entity, act int, self bool) {
	e.act = act + 1
	msg := &proto.ObjAction{ID: e.id, Action: act}
	if self {
		e.client.Send(msg)
	}
	for _, o := range w.floors[e.floor] {
		if o == e {
			continue
		}
		if o.client != nil && o.visible[e.id] && o.fight == nil {
			o.client.Send(msg)
		}
		if o.npc != nil && o.npc.Function == "Action" {
			w.actionWatch(e, o, act)
		}
	}
}

// clearAction：走路、开打、传送时动作取消（CHAR_WORKACTION = -1）。
func (e *entity) clearAction() { e.act = 0 }

// talkAction 是 TalkAction：话里有动作的字就做那个动作。
func (w *World) talkAction(e *entity, text string) {
	for _, t := range talkActions {
		if strings.Contains(text, t.word) {
			w.doAction(e, t.act, true)
			return
		}
	}
}

// actionWatch 是 NPC_ActionWatch：面对面 1 格时回那个动作的那句（颜色照参数的 msgcol，原版读不到参数时是黄色）。
func (w *World) actionWatch(p, n *entity, act int) {
	key, ok := actionWords[act]
	if !ok || !inFront(p, n, 1) || !inFront(n, p, 1) {
		return
	}
	if msg, ok := data.ArgField(n.npc.Arg, key); ok {
		w.npcSay(p, n, msg)
	}
}
