package world

import (
	"strings"

	"sa/server/internal/battle"
	"sa/server/internal/data"
)

// NPC 参数里与宠物有关的条件与动作（npc_exchangeman.c NPC_PetLvCheck / NPC_PetLvCheckType2 / NPC_EventAddPet / NPC_EventDelPet，
// npc_eventaction.c ActionCheckMyPet）。宠物都按模板编号（CHAR_PETID）认。
// 任务交换的条件写成 "PET<等级-模板编号[*只数]"，含 "EV" 的只认任务给的宠物（CHAR_ENDEVENT 为 1），否则只认其他宠物；
// 段里写了 Pet_Name 时还要宠物的名字（CHAR_getUseName）与参数的 Pet_Name 相同。
// 原版 GetEgg 要 _NPC_FUSION（没开），pet_skill（任务里教宠物技能）数据里没有用到，都没做。

// exPetCond 是解开的宠物条件。
type exPetCond struct {
	temp, count, level, flg int
	event                   bool
}

// parseExPet 解开 "PET<20-1234*2"：比较符照原版的顺序找 "<"、">"、"="（"!=" 也当作 "="）。
func parseExPet(c string) exPetCond {
	head, tail, _ := strings.Cut(c, "-")
	pc := exPetCond{count: 1, event: strings.Contains(c, "EV")}
	if id, n, ok := strings.Cut(tail, "*"); ok {
		pc.temp, pc.count = data.Atoi(id), data.Atoi(n)
	} else {
		pc.temp = data.Atoi(tail)
	}
	switch {
	case strings.Contains(c, "<"):
		_, v, _ := strings.Cut(head, "<")
		pc.flg, pc.level = exLess, data.Atoi(v)
	case strings.Contains(c, ">"):
		_, v, _ := strings.Cut(head, ">")
		pc.flg, pc.level = exGreater, data.Atoi(v)
	case strings.Contains(c, "="):
		_, v, _ := strings.Cut(head, "=")
		pc.flg, pc.level = exEq, data.Atoi(v)
	}
	return pc
}

// match 是 NPC_PetLvCheckType2：这只宠物符不符合（模板、是不是任务宠物、等级、名字）。
func (pc exPetCond) match(p *Pet, name string, needName bool) bool {
	return p != nil && p.TempNo == pc.temp && p.EventPet == pc.event && exCmp(pc.level, p.Level, pc.flg) &&
		(!needName || p.displayName() == name)
}

// exPetName 是段里的 Pet_Name（原版看到段里有这个关键字就要求名字对上，名字取 NPC 参数里第一个 Pet_Name）。
func exPetName(n *entity, b string) (string, bool) {
	if _, ok := data.ArgField(b, "Pet_Name"); !ok {
		return "", false
	}
	name, _ := data.ArgField(n.npc.Arg, "Pet_Name")
	return name, true
}

// exPetCheck 是 NPC_PetLvCheck：符合条件的宠物要有那么多只。
func exPetCheck(p *entity, c, name string, needName bool) bool {
	pc := parseExPet(c)
	n := 0
	for _, pet := range p.pets.Pets {
		if pc.match(pet, name, needName) {
			n++
		}
	}
	return n >= pc.count
}

// exDelPets 是 NPC_AcceptDel / NPC_EventAdd 的 DelPet：先确认每一项都有（NPC_PetLvCheck），再交出。
// 写了 "*只数" 的交出那么多只，没写的把符合的全部交出。EVDEL 按成立的那组 EVENT 里的 PET 条件。回传 false 表示交换不成立。
func (w *World) exDelPets(p, n *entity, b, spec, group string) bool {
	works := strings.Split(spec, ",")
	if strings.Contains(spec, "EVDEL") {
		works = strings.Split(group, "&")
	}
	name, needName := exPetName(n, b)
	for _, c := range works {
		if strings.Contains(c, "PET") && !exPetCheck(p, c, name, needName) {
			return false
		}
	}
	if p.fight != nil {
		return false
	}
	changed := false
	for _, c := range works {
		if !strings.Contains(c, "PET") {
			continue
		}
		pc := parseExPet(c)
		limit := -1
		if strings.Contains(c, "*") {
			limit = pc.count
		}
		for i := 0; i < PetSlots && limit != 0; i++ {
			if pet := p.pets.Pets[i]; pc.match(pet, name, needName) {
				w.giveUpPet(p, i)
				changed = true
				limit--
			}
		}
	}
	if changed {
		w.petsChanged(p)
	}
	return true
}

// giveUpPet 是 NPC_EventDelPet：交出第 i 格的宠物（骑着的先下来，出战的改为没有）。
func (w *World) giveUpPet(p *entity, i int) {
	pet := p.pets.Pets[i]
	if p.pets.Ride == i {
		w.dismount(p)
	}
	w.removePet(p, i)
	w.sysMsg(p, "交出"+pet.Name+"。", colorWhite)
}

// exGetPet 是 NPC_EventAddPet：宠物栏满了回 false（原版显示 PetFullMsg）；参数是敌人编号，有 "," 时随机一种；
// 接下委托（mode 0）给的是任务宠物（CHAR_ENDEVENT = 1），交换（mode 1）给的是一般宠物。
func (w *World) exGetPet(p *entity, spec string, mode int) bool {
	if p.pets.free() < 0 {
		return false
	}
	ids := strings.Split(spec, ",")
	en := w.game.Battle.Enemies[data.Atoi(ids[w.rnd.IntN(len(ids))])]
	if en == nil {
		return false
	}
	pet := w.petFromEnemy(en)
	pet.EventPet = mode == 0
	w.addPet(p, pet)
	w.sysMsg(p, "拿到"+pet.Name+"。", colorWhite)
	w.petsChanged(p)
	return true
}

// petFromEnemy 是 ENEMY_createPetFromEnemyIndex：等级在敌人的等级范围里随机，成长照 ENEMY_createEnemy，HP 补满，
// 经验是那一级的起点（CHAR_setMaxExpFromLevel）。没有捕获者，谁都能改名。
func (w *World) petFromEnemy(en *data.Enemy) *Pet {
	lv := en.LvMin + w.rnd.IntN(en.LvMax-en.LvMin+1)
	f := battle.NewEnemy(battle.EnemySpec{Enemy: en, Temp: w.game.Battle.Temps[en.TempNo], Level: lv}, battle.EnemySlot(0), w.game.Battle.Skills, w.rnd)
	pet := petFromFighter(f, 0)
	pet.GetLevel = 0
	pet.HP = w.petMaxHP(pet)
	if lv < len(w.game.Battle.LevelExp) {
		pet.Exp = w.game.Battle.LevelExp[lv]
	}
	return pet
}

// petCheck 是 ActionCheckMyPet（传送员、战斗 NPC 等的 "PET<等级-模板编号"）：第一只这个模板的宠物的等级拿来比。
func (w *World) petCheck(p *entity, level, flg, temp int) bool {
	if temp <= 0 {
		return false
	}
	for _, pet := range p.pets.Pets {
		if pet != nil && pet.TempNo == temp {
			return compare(level, pet.Level, flg)
		}
	}
	return false
}
