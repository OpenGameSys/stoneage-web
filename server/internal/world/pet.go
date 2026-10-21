package world

import (
	"encoding/json"
	"strings"

	"sa/server/internal/battle"
	"sa/server/internal/proto"
)

// 宠物（原版宠物也是一个 Char，CHAR_TYPEPET；gmsv/char/pet.c、enemy.c、char_data.c）：
//   宠物栏      CHAR_MAXPETHAVE 5 格；出战的宠物是 CHAR_DEFAULTPET，待命的是 CHAR_WORKSTANDBYPET 的位元
//   产生        PET_createPetFromCharaIndex（捕获）：照抄敌人当时的数值
//   忠诚度      CHAR_complianceParameter 的 CHAR_WORKFIXAI
//   升级        CHAR_LevelUpCheck + CHAR_PetLevelUp（按 ALLOCPOINT 与 PETRANK 成长）

const (
	PetSlots    = 5  // CHAR_MAXPETHAVE
	maxStandby  = 4  // 原客户端 BattlePetStMenCnt < 4：待命的最多 4 只
	petNameMax  = 16 // lssproto_KN_recv：名字最多 16 个字节
	petSkillMax = 7  // CHAR_MAXPETSKILLHAVE
)

// Pet 是宠物栏里的一只宠物。Vital/Str/Tgh/Dex 是原版内部值（不像角色的点数，宠物的是成长次数 × 成长率，不一定是 100 的倍数）。
type Pet struct {
	TempNo     int    `json:"temp"`  // CHAR_PETID：敌人模板编号
	EnemyID    int    `json:"enemy"` // CHAR_PETENEMYID
	Name       string `json:"name"`
	FreeName   string `json:"free,omitempty"` // CHAR_USERPETNAME：主人取的名字
	Owner      int64  `json:"owner"`          // 捕获它的角色（CHAR_CDKEY；只有原主人能改名）
	Sprite     int    `json:"sprite"`
	Level      int    `json:"lv"`
	Exp        int    `json:"exp"`
	HP         int    `json:"hp"`
	Vital      int    `json:"vit"`
	Str        int    `json:"str"`
	Tgh        int    `json:"tgh"`
	Dex        int    `json:"dex"`
	Alloc      [4]int `json:"alloc"` // CHAR_ALLOCPOINT
	Rank       int    `json:"rank"`  // CHAR_PETRANK
	Attr       [4]int `json:"attr"`  // 地水火风 0..100
	Resist     [6]int `json:"resist"`
	Skills     [7]int `json:"skills"` // 技能编号，-1 为空
	Slot       int    `json:"slot"`   // CHAR_SLOT：可用的技能栏数
	ModAI      int    `json:"modai"`
	VariableAI int    `json:"vai"` // CHAR_VARIABLEAI（100 为 1 点忠诚度）
	Rare       int    `json:"rare"`
	Critical   int    `json:"cri"`
	Counter    int    `json:"ctr"`
	GetLevel   int    `json:"getlv"` // CHAR_PETGETLV：捕获时的等级
	LimitLevel int    `json:"limit,omitempty"`
	Trans      int    `json:"trans,omitempty"`
	EventPet   bool   `json:"ev,omitempty"` // CHAR_ENDEVENT：任务给的宠物（任务条件里写了 EV 的才认它）
	// Mag 是魔法熟练度与抗魔（CHAR_EARTH_EXP.. 等），没打过魔法的宠物为 nil
	Mag *battle.MagicSkill `json:"mag,omitempty"`
	// Items 是身上的道具（_PET_ITEM，7 格，petitem.go），都没有时为 nil
	Items []*Item `json:"items,omitempty"`
	// 宠物邮件（addressbook.go）：飞出去了（Away）、带着的道具
	Away     bool  `json:"away,omitempty"`
	MailItem *Item `json:"mailItem,omitempty"`
	// Guard 是家族守护兽（CHAR_PETFAMILY）
	Guard bool `json:"guard,omitempty"`
	// Born 是捕获时的体腕耐敏（扩展资料算实际成长用，petext.go）；旧存档没有
	Born *[4]int `json:"born,omitempty"`
}

// gbkLen 是字串在原版（GBK）里的字节数：中文 2 个字节。
func gbkLen(s string) int {
	n := 0
	for _, r := range s {
		if r < 0x80 {
			n++
		} else {
			n += 2
		}
	}
	return n
}

// petBag 是宠物栏：Battle 是出战的那一格（-1 没有），Standby 是待命的位元。
type petBag struct {
	Pets    [PetSlots]*Pet `json:"pets"`
	Battle  int            `json:"battle"`
	Standby int            `json:"standby"`
	Pool    []*Pet         `json:"pool,omitempty"` // 寄放在宠物店的宠物（CHAR_POOLPET，petshop.go）
	// 骑宠（ride.go）：骑着的那一格（CHAR_RIDEPET，-1 没有）、能骑的宠物等级上限（CHAR_LEARNRIDE，0 当作预设）、
	// 骑宠证书（CHAR_LOWRIDEPETS / 1 / 2 的位元）
	Ride      int       `json:"ride"`
	LearnRide int       `json:"learnride,omitempty"`
	Licence   [3]uint32 `json:"licence"`
}

func parsePets(s string) *petBag {
	b := &petBag{Battle: -1, Ride: -1}
	if s != "" {
		if err := json.Unmarshal([]byte(s), b); err != nil {
			return &petBag{Battle: -1, Ride: -1}
		}
	}
	if b.Battle < 0 || b.Battle >= PetSlots || b.Pets[b.Battle] == nil {
		b.Battle = -1
	}
	if b.Ride < 0 || b.Ride >= PetSlots || b.Pets[b.Ride] == nil {
		b.Ride = -1
	}
	return b
}

func (b *petBag) String() string {
	out, _ := json.Marshal(b)
	return string(out)
}

// free 是第一个空格（CHAR_getCharPetElement），满了回 -1。
func (b *petBag) free() int {
	for i, p := range b.Pets {
		if p == nil {
			return i
		}
	}
	return -1
}

// freeCount 是空格数（NPC 条件 rePET）。
func (b *petBag) freeCount() int {
	n := 0
	for _, p := range b.Pets {
		if p == nil {
			n++
		}
	}
	return n
}

// displayName：有主人取的名字就用它（CHAR_getUseName）。
func (p *Pet) displayName() string {
	if p.FreeName != "" {
		return p.FreeName
	}
	return p.Name
}

func (p *Pet) derive() battle.Stats {
	return battle.Derive(float64(p.Vital), float64(p.Str), float64(p.Tgh), float64(p.Dex))
}

// addVariableAI 是 CHAR_PetAddVariableAi：变动值限制在 ±100×100 之间。
func (p *Pet) addVariableAI(n int) {
	p.VariableAI = max(battle.MinVariableAI, min(battle.MaxVariableAI, p.VariableAI+n))
}

// petFromFighter 是 PET_createPetFromCharaIndex：把被捕获的敌人照当时的数值变成宠物（HP 也是当时的 HP）。
func petFromFighter(f *battle.Fighter, owner int64) *Pet {
	s := f.Seed
	p := &Pet{TempNo: s.TempNo, EnemyID: s.EnemyID, Name: f.Name, Owner: owner, Sprite: f.Sprite, Level: f.Level,
		HP: f.HP, Vital: int(f.Base[0]), Str: int(f.Base[1]), Tgh: int(f.Base[2]), Dex: int(f.Base[3]),
		Alloc: s.Alloc, Rank: s.Rank, Attr: f.Attr, Skills: s.Skills, Slot: s.Slot, ModAI: s.ModAI,
		Rare: f.Rare, Critical: s.Critical, Counter: s.Counter, GetLevel: f.Level, LimitLevel: s.LimitLevel}
	p.Born = &[4]int{p.Vital, p.Str, p.Tgh, p.Dex}
	for i := range p.Resist {
		p.Resist[i] = f.Resist[i+1]
	}
	return p
}

// petLoyalty 是 CHAR_complianceParameter 的宠物忠诚度（CHAR_WORKFIXAI）：
// (主人等级 × 主人魅力 × 1.1) / (宠物等级 × MODAI) × 100，最多 100；再加 VARIABLEAI / 100；
// 主人转生过时再加 (最高等级 − 主人等级) / 2；结果在 0..100。
func (w *World) petLoyalty(e *entity, p *Pet) int {
	modai := p.ModAI
	if modai <= 0 {
		modai = 100
	}
	charm := w.stats(e).Charm
	ai := int(float64(e.char.Level*charm) * 1.10 / float64(max(1, p.Level)*modai) * 100)
	ai = min(ai, 100)
	ai = int(float64(ai) + float64(p.VariableAI)*0.01)
	if e.char.Trans > 0 {
		ai += (MaxLevel - e.char.Level) / 2
	}
	return max(0, min(100, ai))
}

// petMaxLevel：宠物的等级上限（CHAR_LevelUpCheck：有 LIMITLEVEL 的到那一级为止）。
func petMaxLevel(p *Pet) int {
	if p.LimitLevel > 0 {
		return min(MaxLevel, p.LimitLevel)
	}
	return MaxLevel
}

// petGainExp 加经验并升级，返回升了几级（CHAR_LevelUpCheck + 每级 CHAR_PetLevelUp 与 AI_FIX_PETLEVELUP）。
func (w *World) petGainExp(p *Pet, exp int) int {
	p.Exp += exp
	ups := 0
	for p.Level < petMaxLevel(p) {
		next := w.game.Battle.NextLevelExp(p.Level)
		if next < 0 || p.Exp < next {
			break
		}
		p.Level++
		w.petLevelUp(p)
		p.addVariableAI(battle.AIFixPetLevelUp)
		ups++
	}
	return ups
}

// rankRand 是 CHAR_PetLevelUp 的 RankRandTbl：按档次决定成长倍率的范围（÷100）。
var rankRand = [6][2]int{{450, 500}, {470, 520}, {490, 540}, {510, 560}, {530, 580}, {550, 600}}

// petLevelUp 是 CHAR_PetLevelUp：ALLOCPOINT 的四项成长率各加上随机分配的 10 点，
// 乘上按档次随机的倍率（4.5..6.0），加到体腕耐敏上。
func (w *World) petLevelUp(p *Pet) {
	rank := p.Rank
	if rank < 0 || rank > 5 {
		rank = 0
	}
	var param [4]float64
	for i := 0; i < 10; i++ {
		param[w.rnd.IntN(4)]++
	}
	lo, hi := rankRand[rank][0], rankRand[rank][1]
	f := float64(lo+w.rnd.IntN(hi-lo+1)) * 0.01
	add := func(grow int, k int) int { return max(0, int(float64(grow)*f+param[k]*f)) }
	p.Vital += add(p.Alloc[0], 0)
	p.Str += add(p.Alloc[1], 1)
	p.Tgh += add(p.Alloc[2], 2)
	p.Dex += add(p.Alloc[3], 3)
}

// petFighter 把出战的宠物换成战斗数值，站在主人位置 +5。
func (w *World) petFighter(e *entity, idx, slot int) *battle.Fighter {
	p := e.pets.Pets[idx]
	d, attr := w.petDerive(p)
	for i := range attr {
		attr[i] = max(0, min(100, p.Attr[i]+attr[i]))
	}
	f := &battle.Fighter{Slot: slot, Name: p.displayName(), Sprite: p.Sprite, Level: p.Level,
		HP: max(0, min(p.HP, d.HP)), MaxHP: d.HP, Atk: d.Atk, Def: d.Def, Quick: d.Quick, Attr: attr,
		Base: [4]float64{float64(p.Vital), float64(p.Str), float64(p.Tgh), float64(p.Dex)},
		Pet:  true, CharID: int(e.char.ID), Loyalty: w.petLoyalty(e, p), Rare: p.Rare, Weapon: -1}
	if p.Mag == nil {
		p.Mag = &battle.MagicSkill{}
	}
	f.Mag = p.Mag
	f.TempNo = p.TempNo // 鞭炮按宠物模板认
	for i := range p.Resist {
		f.Resist[i+1] = p.Resist[i]
	}
	for i, id := range p.Skills {
		if id >= 0 && i < petSkillMax {
			f.Skills[i] = w.game.Battle.Skills[id]
		}
	}
	return f
}

// petState 是宠物窗口显示的状态：battle 出战、standby 待命、rest 休息。
func (b *petBag) state(i int) string {
	switch {
	case b.Pets[i] != nil && b.Pets[i].Away:
		return "mail" // 宠物邮件飞出去了
	case i == b.Ride:
		return "ride"
	case i == b.Battle:
		return "battle"
	case b.Standby&(1<<i) != 0:
		return "standby"
	}
	return "rest"
}

// petInfo 是原版 CHAR_makeStatusString 的 "K" 字串（宠物窗口的内容）。
func (w *World) petInfo(e *entity, i int) proto.PetInfo {
	p := e.pets.Pets[i]
	d, _ := w.petDerive(p)
	next := 0
	if w.game.Battle != nil {
		next = max(0, w.game.Battle.NextLevelExp(p.Level))
	}
	info := proto.PetInfo{Slot: i, Name: p.Name, FreeName: p.FreeName, Sprite: p.Sprite, Level: p.Level, Exp: p.Exp,
		NextExp: next, HP: max(0, min(p.HP, d.HP)), MaxHp: d.HP, Atk: d.Atk, Def: d.Def, Agi: d.Quick,
		Loyalty: w.petLoyalty(e, p), Attrs: p.Attr[:], Trans: p.Trans, MaxSkill: p.Slot, Skills: []proto.PetSkill{},
		State: e.pets.state(i), CanRename: (p.Owner == 0 || p.Owner == e.char.ID) && !p.Guard, Items: w.petItems(p), Guard: p.Guard,
		Ext: w.petExt(p), RideInfo: w.rideInfo(e, p),

		// 骑乘钮：这个角色骑得上这只宠物才出现（原客户端按自己的人物与骑宠对照表决定；四色老虎要同色、证书骑宠要学过）
		Rideable: w.game.Ride.Sprite(e.char.Sprite, w.weaponSprite(e), p.Sprite, e.pets.Licence[0]) != 0}
	for k, id := range p.Skills {
		if s := w.game.Battle.Skills[id]; id >= 0 && s != nil {
			info.Skills = append(info.Skills, proto.PetSkill{Slot: k, ID: id, Name: s.Name, Memo: s.Comment, Field: s.Field, Target: s.Target, Func: s.Func, Option: s.Option})
		}
	}
	return info
}

func (w *World) sendPets(e *entity) {
	msg := &proto.Pets{Battle: e.pets.Battle, Ride: e.pets.Ride, Pets: []proto.PetInfo{}}
	for i, p := range e.pets.Pets {
		if p != nil {
			msg.Pets = append(msg.Pets, w.petInfo(e, i))
		}
	}
	e.client.Send(msg)
}

// petsChanged：宠物栏变了，序列化、通知并存档。
func (w *World) petsChanged(e *entity) {
	e.char.Pets = e.pets.String()
	w.sendPets(e)
	w.saveProgress(e)
}

// addPet 放进第一个空格，返回格位，满了回 -1。
func (w *World) addPet(e *entity, p *Pet) int {
	i := e.pets.free()
	if i >= 0 {
		e.pets.Pets[i] = p
	}
	return i
}

// PetState 是宠物窗口点状态（原客户端按 休息 → 待命 → 出战 → 休息 轮换）：
// 出战用 lssproto_KS（PET_SelectBattleEntryPet：倒下的不能出战，只能有一只），待命用 lssproto_SPET（最多 4 只）。
// 战斗中不能改。
func (w *World) PetState(charID, slot int, state string) {
	w.Do(func() {
		e := w.players[charID]
		if e == nil || e.fight != nil || slot < 0 || slot >= PetSlots || e.pets.Pets[slot] == nil || e.pets.Pets[slot].Away {
			return
		}
		b := e.pets
		bit := 1 << slot
		if slot == b.Ride {
			// 原客户端点骑着的宠物的状态钮：先下来（"R|P|-1"）再改成休息
			w.dismount(e)
			w.petsChanged(e)
			return
		}
		switch state {
		case "rest":
			b.Standby &^= bit
			if b.Battle == slot {
				b.Battle = -1
			}
		case "standby":
			n := 0
			for i := 0; i < PetSlots; i++ {
				if b.Standby&(1<<i) != 0 && i != slot {
					n++
				}
			}
			if n >= maxStandby {
				break
			}
			b.Standby |= bit
			if b.Battle == slot {
				b.Battle = -1
			}
		case "battle":
			if b.Pets[slot].HP <= 0 {
				break
			}
			b.Battle = slot
			b.Standby &^= bit
		}
		w.petsChanged(e)
	})
}

// PetRename 是 lssproto_KN_recv + CHAR_inputUserPetName：只有捕获它的人能改名，最多 16 个字节；空字串回到原名。
func (w *World) PetRename(charID, slot int, name string) {
	w.Do(func() {
		e := w.players[charID]
		if e == nil || slot < 0 || slot >= PetSlots || e.pets.Pets[slot] == nil {
			return
		}
		p := e.pets.Pets[slot]
		if p.Owner != 0 && p.Owner != e.char.ID {
			w.sysMsg(e, "无法为宠物命名。", colorYellow)
			return
		}
		if p.Guard {
			w.sysMsg(e, "家族守护兽无法修改名字！", colorYellow)
			return
		}
		name = strings.TrimSpace(name)
		if gbkLen(name) > petNameMax || strings.ContainsAny(name, "|\\") {
			return
		}
		p.FreeName = name
		w.petsChanged(e)
	})
}

// PetDrop 是 lssproto_DP（丢出宠物）：放到地上（dropPet），30 秒内丢的人可以捡回来，之后就消失。
func (w *World) PetDrop(charID, slot int) {
	w.Do(func() {
		e := w.players[charID]
		if e == nil || e.busy() || slot < 0 || slot >= PetSlots || e.pets.Pets[slot] == nil || e.pets.Pets[slot].Away {
			return
		}
		if e.pets.Pets[slot].Guard {
			w.sysMsg(e, "家族守护兽无法丢出！", colorYellow)
			return
		}
		w.dropPet(e, slot) // 放到地上（ground.go）
	})
}

// petsHurt 是 NPC_PetHealerCheck：有宠物的 HP 不满。
func (w *World) petsHurt(e *entity) bool {
	for _, p := range e.pets.Pets {
		if p != nil && p.HP != w.petMaxHP(p) {
			return true
		}
	}
	return false
}

// healPets 是 NPC_WindowHealerAllHeal 里宠物的部分：全部回复到最大 HP。
func (w *World) healPets(e *entity) {
	changed := false
	for _, p := range e.pets.Pets {
		if p != nil && p.HP != w.petMaxHP(p) {
			p.HP, changed = w.petMaxHP(p), true
		}
	}
	if changed {
		w.petsChanged(e)
	}
}

// parseMagic 读 characters.magic（空字串为都是 0）。
func parseMagic(s string) *battle.MagicSkill {
	m := &battle.MagicSkill{}
	if s != "" {
		_ = json.Unmarshal([]byte(s), m)
	}
	return m
}

func magicString(m *battle.MagicSkill) string {
	if m == nil || *m == (battle.MagicSkill{}) {
		return ""
	}
	out, _ := json.Marshal(m)
	return string(out)
}
