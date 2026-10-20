package world

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"sa/server/internal/data"
	"sa/server/internal/proto"
)

// 名片与邮件（char/addressbook.c、char/petmail.c）：
//
//	名片匣    80 格（ADDRESSBOOK_MAX），交换用 0..78，79 留给精灵召唤（_ANGLE_EMAIL，这里没有）
//	交换名片  面前那一格开着"交换名片"、没在战斗、名片匣没满、两边还没互相有的玩家（自己的开关不用开）；
//	          几个人时开选单"和谁交换名片呢？"；两边点头（CHAR_ACTNOD）
//	在线      上线 / 下线时有我名片的人那一格跟着更新（ADDRESSBOOK_notifyLoginLogout）
//	邮件      "月/日 时:分|内容"；对方没有我的名片就退回；不在线的存起来，上线时送（原版 AC 的离线邮件实际上送不到，见 store.MailKeep）
//	宠物邮件  把宠物（可以带一件能宠物邮寄的道具）寄给在线的人：飞行时间 (10 − min(敏捷/25, 10)) × 3 秒，送到后同样时间飞回来自动收回；
//	          飞行中的宠物留在宠物栏里标成离开（Pet.Away），下线或重开也不会不见（原版先放在 777 号地图，30 分钟找不到主人就丢在地上）
//
// 邮件的历史原版只存在客户端（data\mail.dat），这里也一样（localStorage）。

const (
	addressMax     = 80
	addressUse     = 79 // 交换只用 0..78（findBlankEntry）
	serverNumber   = 1  // setup.cf servernumber：名片上"在线"的值
	cardSelectSeq  = 22 // CHAR_WINDOWTYPE_SELECTTRADECARD
	petMailMax     = 6  // CHAR_PETMAILNUMS：同时在外面的邮件宠物
	petMailCoolOff = 2 * time.Second
)

// addressEntry 是名片匣的一格（ADDRESSBOOK_entry）。
type addressEntry struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Level int    `json:"lv"`
	DP    int    `json:"dp"`
	Face  int    `json:"face"`
	Trans int    `json:"trans,omitempty"`
}

type addressBook [addressMax]*addressEntry

func parseAddressBook(s string) *addressBook {
	b := &addressBook{}
	if s != "" {
		var list []*addressEntry
		if json.Unmarshal([]byte(s), &list) == nil {
			copy(b[:], list)
		}
	}
	return b
}

func (b *addressBook) String() string {
	out, _ := json.Marshal(b[:])
	return string(out)
}

// find 是对方在 b 里的那一格，没有回 -1。
func (b *addressBook) find(id int64) int {
	for i, x := range b {
		if x != nil && x.ID == id {
			return i
		}
	}
	return -1
}

func (b *addressBook) blank() int {
	for i := 0; i < addressUse; i++ {
		if b[i] == nil {
			return i
		}
	}
	return -1
}

// cardOf 是 e 现在的名片内容。
func cardOf(e *entity) *addressEntry {
	c := e.char
	return &addressEntry{ID: c.ID, Name: c.Name, Level: c.Level, DP: c.DP, Face: c.Face, Trans: c.Trans}
}

// addressInfo 是一格在协议里的样子（AB / ABI 字串）。
func (w *World) addressInfo(b *addressBook, i int) proto.AddressEntry {
	x := b[i]
	if x == nil {
		return proto.AddressEntry{Slot: i}
	}
	out := proto.AddressEntry{Slot: i, Used: true, Name: x.Name, Level: x.Level, DP: x.DP, Face: x.Face, Trans: x.Trans}
	if w.players[int(x.ID)] != nil {
		out.Online = serverNumber
	}
	return out
}

func (w *World) sendAddressBook(e *entity) {
	msg := &proto.AddressBook{Entries: make([]proto.AddressEntry, addressMax)}
	for i := range e.book {
		msg.Entries[i] = w.addressInfo(e.book, i)
	}
	e.client.Send(msg)
}

func (w *World) sendAddressSlot(e *entity, i int) {
	e.client.Send(&proto.AddressUpdate{Entries: []proto.AddressEntry{w.addressInfo(e.book, i)}})
}

func (w *World) bookChanged(e *entity) {
	e.char.AddressBook = e.book.String()
	w.saveProgress(e)
}

// notifyLoginLogout 是 ADDRESSBOOK_notifyLoginLogout：有 e 名片的人那一格更新（等级等也一起更新）；上线时把自己的名片匣按在线的人更新后送出。
func (w *World) notifyLoginLogout(e *entity, login bool) {
	card := cardOf(e)
	for _, o := range w.players {
		if o == e || o.book == nil {
			continue
		}
		if i := o.book.find(e.char.ID); i >= 0 {
			o.book[i] = card
			w.sendAddressSlot(o, i)
			o.char.AddressBook = o.book.String()
		}
	}
	if !login {
		return
	}
	for i, x := range e.book {
		if x == nil {
			continue
		}
		if o := w.players[int(x.ID)]; o != nil && o != e {
			e.book[i] = cardOf(o)
		}
	}
	e.char.AddressBook = e.book.String()
	w.sendAddressBook(e)
}

// CardExchange 是 lssproto_AAB_recv → ADDRESSBOOK_addEntry。
func (w *World) CardExchange(charID int) {
	w.Do(func() {
		e := w.players[charID]
		if e == nil || e.fight != nil {
			return
		}
		if e.book.blank() < 0 {
			w.sysMsg(e, "名片匣已满。", colorWhite)
			return
		}
		fx, fy := e.x+MOVE[e.dir][0], e.y+MOVE[e.dir][1]
		var cands []*entity
		found := false
		for _, o := range w.floors[e.floor] {
			if o == e || o.x != fx || o.y != fy {
				continue
			}
			if o.kind != "player" {
				continue
			}
			found = true
			if w.cardCandidate(e, o) {
				cands = append(cands, o)
			}
		}
		switch len(cands) {
		case 0:
			if found {
				w.sysMsg(e, "无法交换名片。", colorWhite)
			} else {
				w.sysMsg(e, "那里没有任何人。", colorWhite)
			}
		case 1:
			w.addAddressBook(e, cands[0])
		default:
			var sb strings.Builder
			sb.WriteString("1\n和谁交换名片呢？\n")
			ids := make([]int, len(cands))
			for i, c := range cands {
				sb.WriteString(c.name + "\n")
				ids[i] = c.id
			}
			e.window = &npcWindow{npc: 0, seq: cardSelectSeq, kind: "select", buttons: BtnCancel, ids: ids}
			e.client.Send(&proto.NpcWindow{ID: 0, Kind: "select", Buttons: BtnCancel, Seq: cardSelectSeq, Text: sb.String()})
		}
	})
}

// cardCandidate：o 可以和 e 交换名片（没在战斗、开着"交换名片"、名片匣没满、还没互相有）。
func (w *World) cardCandidate(e, o *entity) bool {
	return o.fight == nil && o.fs&fsCard != 0 && o.book.blank() >= 0 &&
		!(e.book.find(o.char.ID) >= 0 && o.book.find(e.char.ID) >= 0)
}

func (w *World) cardSelectReply(e *entity, win *npcWindow, button int, arg string) {
	n := data.Atoi(arg) - 1
	if button != 0 || n < 0 || n >= len(win.ids) {
		return
	}
	if o := w.players[win.ids[n]]; o != nil && w.cardCandidate(e, o) && chebyshev(e, o) <= 1 {
		w.addAddressBook(e, o)
	}
}

// addAddressBook 是 ADDRESSBOOK_addAddressBook：互相放进对方的名片匣。
func (w *World) addAddressBook(me, him *entity) {
	if him.book.blank() < 0 {
		w.sysMsg(me, "对方的名片匣已满。", colorWhite)
		return
	}
	if me.book.blank() < 0 {
		w.sysMsg(me, "名片匣已满。", colorWhite)
		return
	}
	iHad, heHad := me.book.find(him.char.ID) >= 0, him.book.find(me.char.ID) >= 0
	if !iHad {
		i := me.book.blank()
		me.book[i] = cardOf(him)
		if heHad {
			w.sysMsg(me, fmt.Sprintf("从%s得到名片。", him.name), colorWhite)
		} else {
			w.sysMsg(me, fmt.Sprintf("和%s交换名片 。", him.name), colorWhite)
		}
		w.sendAddressSlot(me, i)
		w.bookChanged(me)
	} else {
		w.sysMsg(me, fmt.Sprintf("给%s自己的名片。", him.name), colorWhite)
	}
	if !heHad {
		i := him.book.blank()
		him.book[i] = cardOf(me)
		if iHad {
			w.sysMsg(him, fmt.Sprintf("从%s得到名片。", me.name), colorWhite)
		} else {
			w.sysMsg(him, fmt.Sprintf("和%s交换名片 。", me.name), colorWhite)
		}
		w.sendAddressSlot(him, i)
		w.bookChanged(him)
	} else if !iHad {
		w.sysMsg(him, fmt.Sprintf("因为%s想要名片，所以将名片给他了。", me.name), colorWhite)
	}
	// 对方转向自己，两边点头
	him.dir = moveDir(sign(me.x-him.x), sign(me.y-him.y))
	w.doAction(me, actNod, true)
	w.doAction(him, actNod, true)
}

// CardDelete 是 lssproto_DAB_recv：删掉一格，重送整个名片匣。
func (w *World) CardDelete(charID, index int) {
	w.Do(func() {
		e := w.players[charID]
		if e == nil || index < 0 || index >= addressMax || e.book[index] == nil {
			return
		}
		e.book[index] = nil
		w.bookChanged(e)
		w.sendAddressBook(e)
	})
}

// mailBody 是存起来等对方上线的邮件。
type mailBody struct {
	From  int64  `json:"from"`
	Name  string `json:"name"`
	Date  string `json:"date"`
	Text  string `json:"text"`
	Color int    `json:"color,omitempty"`
}

func (w *World) mailDate() string {
	t := w.now()
	return fmt.Sprintf("%2d/%02d %2d:%02d", int(t.Month()), t.Day(), t.Hour(), t.Minute())
}

// MailSend 是 lssproto_MSG_recv → ADDRESSBOOK_sendMessage。
func (w *World) MailSend(charID, index int, text string) {
	w.Do(func() {
		e := w.players[charID]
		if e == nil || index < 0 || index >= addressMax || e.book[index] == nil || text == "" {
			return
		}
		text = cutBytes(text, 140) // MAIL_STR_LEN（原版只有客户端限制）
		x := e.book[index]
		if o := w.players[int(x.ID)]; o != nil {
			i := o.book.find(e.char.ID)
			if i < 0 {
				w.sysMsg(e, fmt.Sprintf("寄信件给%s，但由於%s 没有名片，所以信件又被退回来了。", x.Name, x.Name), colorYellow)
				return
			}
			o.client.Send(&proto.MailRecv{Index: i, Date: w.mailDate(), Text: text, PetSprite: -1})
			w.sysMsg(e, fmt.Sprintf("送信给%s 。", x.Name), colorWhite)
			return
		}
		body, _ := json.Marshal(mailBody{From: e.char.ID, Name: e.name, Date: w.mailDate(), Text: text})
		to := x.ID
		w.saves <- func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := w.db.PutMail(ctx, to, string(body)); err != nil {
				w.log.Error("存邮件失败", "to", to, "err", err)
			}
		}
		w.sysMsg(e, fmt.Sprintf("送信给%s 。", x.Name), colorWhite)
	})
}

// deliverStoredMails 是上线时的 MessageFlush：送出存着的邮件（对方已经不在名片匣里的就不送）。
func (w *World) deliverStoredMails(e *entity) {
	id := e.id
	w.saves <- func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		list, err := w.db.TakeMails(ctx, e.char.ID)
		if err != nil || len(list) == 0 {
			return
		}
		go w.Do(func() {
			e := w.players[id]
			if e == nil {
				return
			}
			for _, s := range list {
				var m mailBody
				if json.Unmarshal([]byte(s), &m) != nil {
					continue
				}
				if i := e.book.find(m.From); i >= 0 {
					e.client.Send(&proto.MailRecv{Index: i, Date: m.Date, Text: m.Text, PetSprite: -1})
				}
			}
		})
	}
}

// cutBytes 把字串截到 GBK 的 n 个字节以内。
func cutBytes(s string, n int) string {
	used := 0
	for i, r := range s {
		w := 1
		if r >= 0x80 {
			w = 2
		}
		if used+w > n {
			return s[:i]
		}
		used += w
	}
	return s
}

// ---------------- 宠物邮件 ----------------

// PetMail 是 lssproto_PMSG_recv → PETMAIL_sendPetMail（检查的顺序照原版）。
func (w *World) PetMail(charID, index, petSlot, itemSlot int, text string) {
	w.Do(func() {
		e := w.players[charID]
		if e == nil || index < 0 || index >= addressMax {
			return
		}
		var item *Item
		if itemSlot >= 0 {
			if itemSlot < 9 || itemSlot >= len(e.inv) || e.inv[itemSlot] == nil {
				return
			}
			item = e.inv[itemSlot]
			if def := w.game.Items[item.ID]; def == nil || !def.CanPetMail {
				return
			}
		}
		if petSlot < 0 || petSlot >= PetSlots || e.pets.Pets[petSlot] == nil || e.book[index] == nil {
			return
		}
		p, x := e.pets.Pets[petSlot], e.book[index]
		say := func(s string, c int) { w.sysMsg(e, s, c) }
		switch {
		case p.Away:
			return
		case e.trade != nil:
			say("交易状态中无法传递宠物邮件。", colorYellow)
		case petSlot == e.pets.Ride:
			say("骑乘中的宠物无法传递宠物邮件！", colorYellow)
		case e.fight != nil:
			say("战斗状态中无法传递宠物邮件。", colorYellow)
		case strings.HasPrefix(p.Name, "*"):
			say("绑定宠物无法传递邮件！", colorYellow)
		case item != nil && strings.HasPrefix(w.itemName(item), "*"):
			say("绑定物品无法传递", colorYellow)
		case w.players[int(x.ID)] == nil:
			say("该玩家不在线上！", colorYellow)
		case e.floor >= 30017 && e.floor <= 30021:
			say("此地区不得寄送道具！", colorWhite)
		case w.players[int(x.ID)].floor >= 30017 && w.players[int(x.ID)].floor <= 30021:
			say("对方所在地区不得收寄送道具！", colorWhite)
		case e.floor >= 8200 && e.floor <= 8213:
			say("英雄战场不得寄送道具！", colorWhite)
		case isFamilyWarFloor(w.players[int(x.ID)].floor):
			say("对方在族战战场，不得寄送道具！", colorRed)
		case p.LimitLevel > 0:
			say("特殊宠物不能传递邮件喔！", colorYellow)
		case p.Guard:
			say("家族守护兽无法传递邮件喔！", colorYellow)
		case e.petMails > petMailMax:
			say("邮件中宠物过多，暂时无法传递宠物邮件！", colorYellow)
		case w.now().Before(e.petMailAt):
			say(fmt.Sprintf("需等待%d秒才能再次寄送宠物邮件！", int(e.petMailAt.Sub(w.now()).Seconds())+1), colorYellow)
		default:
			w.sendPetMail(e, petSlot, itemSlot, x, cutBytes(text, 140))
		}
	})
}

// isFamilyWarFloor 是族战的场地（1042、2032、3032、4032）。
func isFamilyWarFloor(f int) bool { return f == 1042 || f == 2032 || f == 3032 || f == 4032 }

// sendPetMail：宠物带着道具飞出去；飞行时间后送到，再过一样的时间飞回来。
func (w *World) sendPetMail(e *entity, petSlot, itemSlot int, to *addressEntry, text string) {
	p := e.pets.Pets[petSlot]
	if itemSlot >= 0 {
		p.MailItem, e.inv[itemSlot] = e.inv[itemSlot], nil
	}
	p.Away = true
	if e.pets.Battle == petSlot {
		e.pets.Battle = -1
	}
	e.pets.Standby &^= 1 << petSlot
	e.petMails++
	e.petMailAt = w.now().Add(petMailCoolOff)
	w.sysMsg(e, fmt.Sprintf("寄送宠物邮件(%s)给%s。", p.displayName(), to.Name), colorYellow)
	w.sysMsg(e, "系统自动为您存档!", colorRed)
	w.itemsChanged(e)
	w.petsChanged(e)
	d, _ := w.petDerive(p)
	delay := time.Duration((10-max(0, min(d.Quick/25, 10)))*3) * time.Second // PETMAIL_getIdleTime
	owner, date := e.char.ID, w.mailDate()
	time.AfterFunc(delay, func() {
		w.Do(func() {
			delivered := w.deliverPetMail(owner, p, to.ID, date, text)
			time.AfterFunc(delay, func() { w.Do(func() { w.petMailReturn(owner, p, delivered) }) })
		})
	})
}

// deliverPetMail 是 PETMAIL_sendPetmail：对方没有我的名片就退回；道具给对方（道具栏满了放在脚边）。
func (w *World) deliverPetMail(owner int64, p *Pet, to int64, date, text string) bool {
	o := w.players[int(to)]
	if o == nil || o.fight != nil || o.trade != nil {
		return false
	}
	i := o.book.find(owner)
	ownerName := ""
	if e := w.players[int(owner)]; e != nil {
		ownerName = e.name
	}
	if i < 0 {
		w.sysMsg(o, fmt.Sprintf("%s的%s 终於来了！由於对方没有您的名片，所以信件被退回了。", ownerName, p.displayName()), colorWhite)
		return false
	}
	if o.floor >= 8200 && o.floor <= 8213 {
		w.sysMsg(o, "英雄战场不得寄送道具！", colorWhite)
		return false
	}
	msg := &proto.MailRecv{Index: i, Date: date, Text: text, PetSprite: p.Sprite, PetLevel: p.Level, PetName: p.displayName(), ItemImage: -1}
	if it := p.MailItem; it != nil {
		if def := w.game.Items[it.ID]; def != nil {
			msg.ItemImage = def.Image
		}
		p.MailItem = nil
		if w.addItem(o.inv, it) < 0 {
			w.groundItem(o, it) // pm_putground
		}
		w.itemsChanged(o)
	}
	o.client.Send(msg)
	return true
}

// petMailReturn 是 PETMAIL_returnMail：回到主人身边、带回没送出的道具，自动收回（主人不在线时等上线，见 returnAwayPets）。
func (w *World) petMailReturn(owner int64, p *Pet, delivered bool) {
	e := w.players[int(owner)]
	if e == nil {
		return
	}
	slot := -1
	for i, x := range e.pets.Pets {
		if x == p {
			slot = i
		}
	}
	if slot < 0 {
		return
	}
	e.petMails = max(0, e.petMails-1)
	if delivered {
		w.sysMsg(e, p.displayName()+" 回来了！", colorWhite)
	} else {
		w.sysMsg(e, p.displayName()+" 回来了！似乎无法寄送信件。", colorWhite)
	}
	w.returnPet(e, p)
	w.sysMsg(e, "拾回邮件宠物 "+p.displayName(), colorYellow)
}

// returnPet 把飞回来的宠物与它带回的道具还给主人。
func (w *World) returnPet(e *entity, p *Pet) {
	p.Away = false
	if it := p.MailItem; it != nil {
		p.MailItem = nil
		if w.addItem(e.inv, it) < 0 {
			w.groundItem(e, it)
		}
		w.itemsChanged(e)
	}
	w.petsChanged(e)
}

// returnAwayPets：上线时还在外面的邮件宠物（下线或重开时还没飞回来的）都回来。
func (w *World) returnAwayPets(e *entity) {
	for _, p := range e.pets.Pets {
		if p != nil && p.Away {
			w.returnPet(e, p)
			w.sysMsg(e, p.displayName()+" 回来了！", colorWhite)
		}
	}
}

// groundItem 把道具放在 e 脚边（道具栏满了的时候）。
func (w *World) groundItem(e *entity, it *Item) {
	def := w.game.Items[it.ID]
	if def == nil {
		return
	}
	if x, y, ok := w.dropTile(e, true, "item", "gold"); ok {
		w.putGround(e.floor, x, y, "item", def.Name, def.Image, &ground{item: it, owner: e.char.ID})
	}
}
