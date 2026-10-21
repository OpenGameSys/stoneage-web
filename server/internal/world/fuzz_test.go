package world

import (
	"fmt"
	"math"
	"math/rand/v2"
	"sort"
	"strings"
	"testing"

	"sa/server/internal/battle"
	"sa/server/internal/proto"
)

// dropClient 是什么都不留的连线（压力测试用，避免 fakeClient 把每条讯息都留在记忆体里）。
type dropClient struct{}

func (dropClient) Send(proto.Message) {}
func (dropClient) Kick(string)        {}

// 恶意客户端测试：每个客户端能呼叫的世界指令都用极端值（MinInt64、MaxInt64、-1、超大格位……）与怪字串轰炸，
// 在平常、对着 NPC、战斗中几种状态各打一轮，世界协程不能 panic（safe 拦下的也算失败）。
func TestHostileClientInputs(t *testing.T) {
	if testing.Short() {
		t.Skip("慢")
	}
	w, db, g := setup(t)
	ene := petEnemy(t, g, 1)
	n := findNPC(t, w, "WarpMan", "wpm_800_34_23")
	// 两个角色都用丢掉讯息的连线：十几万次指令会送出大量 MapData 等，留着会把记忆体吃光
	c := newChar(t, db, "hostile", n.floor, n.x+1, n.y)
	o := newChar(t, db, "victim", n.floor, n.x+1, n.y)
	w.Enter(c, dropClient{})
	w.Enter(o, dropClient{})
	id, other := int(c.ID), int(o.ID)
	slot := givePet(t, w, id, ene, 5)
	giveItem(w, id, 1)
	w.SetFlags(other, fsParty|fsDuel|fsTrade|fsCard)

	ints := []int{math.MinInt, math.MinInt + 1, -1 << 40, -100000, -2, -1, 0, 1, 2, 3, 4, 5, 6, 8, 9, 10, 15, 19, 20, 25, 26, 45, 53, 54, 55, 79, 80, 99, 100, 255, 1000,
		1 << 20, 1 << 31, 1 << 40, math.MaxInt - 1, math.MaxInt, id, other, slot, n.id}
	strs := []string{"", " ", "|", "||||", "\\", "%s%d", "\x00", "!!!!!!!!", "...", "R|P|-1", "R|P|99", "S|F|", "S|F|9999999999", "J|0", "M|1|2|3", "L|CHANGE|A|1||0",
		"B|-1|-1", "B|99999999999999999999|1", "D|1", strings.Repeat("长", 300), strings.Repeat("|", 500), "attack", "magic", "item", "capture", "escape", "guard", "pet",
		"none", "near", "party", "family", "world", "rest", "standby", "battle", "ride", "mail", "request", "accept", "cancel", "lock", "ok", "gold", "pet", "item"}
	rnd := rand.New(rand.NewPCG(1, 2))
	I := func() int { return ints[rnd.IntN(len(ints))] }
	S := func() string { return strs[rnd.IntN(len(strs))] }
	B := func() bool { return rnd.IntN(2) == 0 }

	calls := map[string]func(){
		"Move":             func() { w.Move(id, I(), I(), I()) },
		"Turn":             func() { w.Turn(id, I()) },
		"MapReq":           func() { f := I(); w.Do(func() { f = w.players[id].floor }); w.MapReq(id, f, I(), I(), I(), I()) },
		"MapReqAnyFloor":   func() { w.MapReq(id, I(), I(), I(), I(), I()) },
		"Talk":             func() { w.Talk(id, I()) },
		"Chat":             func() { w.Chat(id, S(), S(), I(), I()) },
		"GmCommand":        func() { w.GmCommand(id, "/"+S()+" "+S()) },
		"GmListReq":        func() { w.GmListReq(id, S()) },
		"QuestReq":         func() { w.QuestReq(id) },
		"GmDetailReq":      func() { w.GmDetailReq(id, S(), I()) },
		"ReturnHome":       func() { w.ReturnHome(id) },
		"BattleCommand":    func() { w.BattleCommand(id, S(), I(), I()) },
		"BattlePetCommand": func() { w.BattlePetCommand(id, I(), I()) },
		"PetState":         func() { w.PetState(id, I(), S()) },
		"PetRename":        func() { w.PetRename(id, I(), S()) },
		"PetDrop":          func() { w.PetDrop(id, I()) },
		"PetRide":          func() { w.PetRide(id, I()) },
		"ItemMove":         func() { w.ItemMove(id, I(), I()) },
		"Trade":            func() { w.Trade(id, S(), I(), I()) },
		"TradeOther":       func() { w.Trade(other, S(), I(), I()) },
		"Action":           func() { w.Action(id, I(), I(), I()) },
		"CardExchange":     func() { w.CardExchange(id) },
		"CardDelete":       func() { w.CardDelete(id, I()) },
		"MailSend":         func() { w.MailSend(id, I(), S()) },
		"PetMail":          func() { w.PetMail(id, I(), I(), I(), S()) },
		"Duel":             func() { w.Duel(id) },
		"Family":           func() { w.Family(id, S()+"|"+S()+"|"+S()) },
		"SetFlags":         func() { w.SetFlags(id, I()) },
		"PartyRequest":     func() { w.PartyRequest(id, I()) },
		"PartyKick":        func() { w.PartyKick(id, I()) },
		"BattleHelp":       func() { w.BattleHelp(id, B()) },
		"JoinBattle":       func() { w.JoinBattle(id) },
		"PetSkillUse":      func() { w.PetSkillUse(id, I(), I(), S()) },
		"PetItemEquip":     func() { w.PetItemEquip(id, I(), I(), I()) },
		"ItemUse":          func() { w.ItemUse(id, I(), I()) },
		"MagicUse":         func() { w.MagicUse(id, I(), I()) },
		"ItemDrop":         func() { w.ItemDrop(id, I()) },
		"GoldDrop":         func() { w.GoldDrop(id, I()) },
		"PickUp":           func() { w.PickUp(id, I()) },
		"StatUp":           func() { w.StatUp(id, I()) },
		"NpcWindowReply":   func() { w.NpcWindowReply(id, I(), I(), I(), S()) },
		"NpcWindowReplyN":  func() { w.NpcWindowReply(id, n.id, I(), I(), S()) },
	}
	names := make([]string, 0, len(calls))
	for k := range calls {
		names = append(names, k)
	}
	sort.Strings(names)

	// 让两个角色都还在线、站回 NPC 旁边（指令可能把人传走、踢下线、开打）
	reset := func(state string) {
		w.Do(func() {
			for _, who := range []int{id, other} {
				e := w.players[who]
				if e == nil {
					continue
				}
				if e.fight != nil {
					w.abandonBattle(e)
				}
				w.cancelTrade(e)
				if e.floor != n.floor || abs(e.x-n.x) > 1 || abs(e.y-n.y) > 1 {
					w.placeOnFloor(e, n.floor, n.x+1, n.y, 2)
				}
				e.char.Gold = 100000
			}
			e := w.players[id]
			if e == nil {
				return
			}
			if p := e.pets.Pets[slot]; p != nil {
				p.Away, e.pets.Ride = state == "mailpet", -1
				if state == "ride" {
					e.pets.Ride = slot
				}
			}
			if o := w.players[other]; o != nil && (state == "trade" || state == "party") {
				// 对方站在自己面前
				e.dir = 6
				o.x, o.y = e.x+MOVE[6][0], e.y+MOVE[6][1]
				if state == "trade" {
					w.tradeRequest(e)
				} else if e.party == nil {
					pt := &party{}
					pt.slots[0], pt.slots[1] = e, o
					e.party, o.party = pt, pt
				}
			}
			switch state {
			case "npc":
				w.warpmanOpen(e, n)
			case "battle":
				e.pets.Battle = slot
				w.startBattleWith(e, []*battle.Fighter{enemyFighter(w, ene, 1, battle.EnemySlot(0)), enemyFighter(w, ene, 1, battle.EnemySlot(1))}, nil, nil)
			}
		})
	}

	failures := map[string][]string{}
	for _, state := range []string{"idle", "npc", "battle", "trade", "party", "mailpet", "ride"} {

		for _, name := range names {
			for i := 0; i < 100; i++ {
				if i%25 == 0 {
					reset(state)
				}
				var gone bool
				w.Do(func() { gone = w.players[id] == nil })
				if gone {
					t.Logf("%s：角色被拿掉了（%s），跳过", name, state)
					break
				}
				before := w.Panics()
				calls[name]()
				if w.Panics() > before && len(failures[name]) < 3 {
					failures[name] = append(failures[name], state)
				}
			}
		}
	}
	// GM 也不能弄垮服务器：乱打 GM 指令
	w.Do(func() { w.players[id].gm = true })
	reset("idle")
	before := w.Panics()
	for i := 0; i < 1000; i++ {
		cmd := gmMenu[rnd.IntN(len(gmMenu))].Cmd
		var args []string
		for k := rnd.IntN(5); k > 0; k-- {
			if B() {
				args = append(args, fmt.Sprint(I()))
			} else {
				args = append(args, S())
			}
		}
		w.GmCommand(id, cmd+" "+strings.Join(args, " "))
		if w.Panics() > before {
			failures["GM "+cmd] = append(failures["GM "+cmd], strings.Join(args, " "))
			before = w.Panics()
		}
		if i%100 == 0 {
			reset("idle")
		}
	}
	for name, where := range failures {
		t.Errorf("%s 让世界协程 panic（%v）", name, where)
	}
}
