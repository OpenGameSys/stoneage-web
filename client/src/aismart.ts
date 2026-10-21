// 智能自动战斗（原版没有，额外新功能；原版 ai_setting 的 AI 模式已经拿掉，自动战斗一律走这里）。设定在 ai.ts，窗口在 aisetting.ts。
// 设定里的行动计划（人物 / 宠物各有第一回合与之后每回合）："智能判断"照下面的估值挑；固定的普通攻击 / 防御 / 精灵 / 宠物技能
// 在没有紧急的事（补血、复活、解状态、逃跑、抓宠）时照做，对象按设定（自己、宠物、主人、敌人……；"自动"按范围选），
// 找不到那个精灵 / 技能、MP 不够或对象不在时回到智能判断。
//
// 每回合把能做的事都列出来、各自估一个"值"，挑最大的，而不是照固定顺序：
//   1. 估计这回合每个我方的人会挨多少：每个能动的敌人的威胁（学到的平均伤害，没学到时按等级估）平均分到我方，
//      再加上最凶的那个的一半（可能集中打一个人）。会被打死、或 HP 低于"补血"设定的百分比的，按缺多少与身份（自己 > 队友 > 宠物）排先后。
//   2. 补血：两个人以上要补而且有补全体的精灵就补全体；否则挑够补满的最小的那个（精灵按 option 的回复量、道具按说明里的数字，肉优先）。
//   3. 复活倒下的队友 / 宠物；解掉队友 / 宠物不能动的状态（麻、眠、石、障）或快被毒死的毒（精灵的 option 对得上状态才用）。
//   4. MP 不够放最便宜的补血精灵时，有回复气力的道具就喝。
//   5. 打不过（逃得掉的战斗）：自己会被打死又补不了血、或敌人两回合的伤害超过我方剩下的 HP 而我方两回合打不完，就逃跑。
//   6. 进攻：普通攻击每个敌人、攻击精灵的每个范围（单人 / 一排 / 全体）各估一个值 =
//      预计伤害（不超过对方剩下的 HP）+ 打得倒时加上它的威胁 × 2（少一个人打我们）+ 威胁 × 0.3（先打凶的）− 用掉的 MP 的代价。
//   7. 宠物：同样按技能估值（攻击类、全体类、守护主人、防御），先扣掉主人这回合预计打掉的 HP，
//      避免主人已经打得倒的敌人宠物还去打；自动抓宠时有"手下留情"就对要抓的那只用，其余技能不打它。
// 学习：每场战斗看实际的结果——每种敌人一次打我方多少、自己与宠物每种技能一次打掉多少（没打中算 0）——
// 存在 localStorage（sa.ai.learn.<角色名>），下一场就用学到的数字估，越打越准。

import type { InvItem, PetSkill } from "./proto.gen.ts";
import type { AiCommand, AiSettings, AiTarget } from "./ai.ts";

export interface SmartFighter {
  slot: number; name: string; hp: number; maxHp: number; mp: number; level: number; status: number; gone: boolean;
  rideHp?: number; rideMaxHp?: number; // 骑着的宠物（没骑为 0）：补血时连它一起算（回复会按缺的比例分给骑宠）
}

export interface SmartContext {
  me: number;
  first: boolean;
  fighters: Map<number, SmartFighter>;
  inv: InvItem[];
  myAtk: number;
  pet: { name: string; atk: number; skills: PetSkill[] } | null;
  capturePlace: boolean;
  noEscape: boolean; // 头目 / 事件 / 决斗：原版的逃跑多半没用，不考虑逃跑
  mem: AiMemory;
}

const DISABLED = new Set([2, 3, 4, 9]); // 麻、眠、石、障：不能行动
const STATUS_CHAR: Record<number, string> = { 1: "毒", 2: "麻", 3: "眠", 4: "石", 5: "醉", 6: "乱", 8: "毒" };
const ME_WEIGHT = 1.3, PLAYER_WEIGHT = 1.15, PET_WEIGHT = 1.0;

// 宠物技能的种类（按 petskill2.txt 的函数名）
const PET_GUARD = new Set(["PETSKILL_Guardian"]);
const PET_DEFEND = new Set(["PETSKILL_NormalGuard", "PETSKILL_SetDuck", "PETSKILL_Barrier"]);
const PET_MERCY = "PETSKILL_ShowMercy";
// 不自动用的：离场、偷窃、拐走、变猪、变身、骑乘、地图技能、牺牲自己、挑衅、什么都不做等
const PET_AVOID = new Set(["PETSKILL_Retrace", "PETSKILL_Steal", "PETSKILL_Steal2", "PETSKILL_StealMoney", "PETSKILL_Abduct",
  "PETSKILL_Temptation", "PETSKILL_BecomePig", "PETSKILL_Vary", "PETSKILL_Ride", "PETSKILL_Merge", "PETSKILL_Inslay", "PETSKILL_Upinslay",
  "PETSKILL_Sacrifice", "PETSKILL_ProvokeFight", "PETSKILL_None", "PETSKILL_Regret", "PETSKILL_Take", "PETSKILL_Relifeact", "PETSKILL_Nocast",
  "PETSKILL_SetMagicPet", "PETSKILL_BattleModel", "PETSKILL_Hector", "PETSKILL_Lighttakeed", "PETSKILL_Mucus", "PETSKILL_Sars", PET_MERCY]);

/** 技能 option 里的"攻%±N"（攻击力倍率）与连击次数，当作学到之前的估计 */
function petSkillPrior(sk: PetSkill): number {
  const m = /攻%([+-]\d+)/.exec(sk.option);
  let k = m ? 1 + Number(m[1]) / 100 : 1;
  if (sk.func === "PETSKILL_ContinuationAttack") k *= Math.max(1, Number(sk.option) || 2) * 0.6;
  if (sk.func === "PETSKILL_AttackMagic") k *= 0.8;
  return k;
}

/** 说明里"回复"后面的数字（道具的回复量），没有就当 100 */
function healAmount(it: InvItem): number {
  const m = /回复\D*(\d+)/.exec(it.memo) ?? /(\d+)/.exec(it.memo);
  return m ? Number(m[1]) : 100;
}

// ---------------- 学习 ----------------

type Stat = [number, number]; // [平均, 次数]

export class AiMemory {
  readonly name: string;
  private key: string;
  enemyHit: Record<string, Stat> = {}; // 敌人名 → 一次打我方多少
  act: Record<string, Stat> = {};      // 行动 → 一次打掉多少（me:attack、me:magic:<名>、pet:<宠物名>:<技能编号>）
  constructor(name: string) {
    this.name = name;
    this.key = `sa.ai.learn.${name}`;
    try {
      const j = JSON.parse(localStorage.getItem(this.key) ?? "null");
      if (j) { this.enemyHit = j.enemyHit ?? {}; this.act = j.act ?? {}; }
    } catch { /* 没有或坏掉就从头学 */ }
  }
  private static add(t: Record<string, Stat>, k: string, v: number) {
    const s = t[k] ?? [v, 0];
    s[1] = Math.min(s[1] + 1, 30);
    s[0] += (v - s[0]) / s[1]; // 最近 30 次的移动平均
    t[k] = s;
  }
  get(k: string, prior: number): number { return this.act[k]?.[0] ?? prior; }
  threat(name: string, prior: number): number { return this.enemyHit[name]?.[0] ?? prior; }

  /**
   * 看一回合的结果学习：敌人打我方的伤害记在 enemyHit；自己（actKey）、宠物（petKey）打出的伤害记在 act，没打中记 0。
   * mySide 判断是不是我方的格位。
   */
  observe(events: { actor: number; target: number; damage: number; result: string; kind: string }[],
    names: (slot: number) => string, mySide: (slot: number) => boolean, me: number, actKey: string, petKey: string) {
    for (const e of events) {
      if (e.actor < 0 || e.target < 0 || !["attack", "counter", "magic", "skill"].includes(e.kind)) continue;
      const missed = e.result === "dodge" || e.result === "miss";
      if (!mySide(e.actor) && mySide(e.target)) {
        if (!missed && e.damage > 0) AiMemory.add(this.enemyHit, names(e.actor), e.damage);
      } else if (e.actor === me && actKey && !mySide(e.target)) AiMemory.add(this.act, actKey, missed ? 0 : e.damage);
      else if (e.actor === me + 5 && petKey && !mySide(e.target)) AiMemory.add(this.act, petKey, missed ? 0 : e.damage);
    }
  }
  save() { try { localStorage.setItem(this.key, JSON.stringify({ enemyHit: this.enemyHit, act: this.act })); } catch { /* */ } }
}

// ---------------- 每回合的决定 ----------------

interface Plan { value: number; kind: AiCommand["kind"]; target: number; item: number; label: string; key: string; hits: Map<number, number> }

export function smartChoose(s: AiSettings, c: SmartContext): AiCommand & { key: string; petKey: string } {
  const out: AiCommand & { key: string; petKey: string } = { kind: "none", target: -1, item: -1, label: "", chat: [], pet: [-1, -1], key: "", petKey: "" };
  const me = c.me, pet = me + 5, mine = (i: number) => (i < 10) === (me < 10);
  const side = me < 10 ? 20 : 21, otherSide = me < 10 ? 21 : 20;
  const f = (i: number) => { const x = c.fighters.get(i); return x && !x.gone ? x : undefined; };
  const meF = f(me);
  if (!meF || meF.hp <= 0) { out.kind = "skip"; return out; }
  const allies = [...c.fighters.values()].filter((x) => !x.gone && mine(x.slot));
  const aliveAllies = allies.filter((x) => x.hp > 0);
  const enemies = [...c.fighters.values()].filter((x) => !x.gone && !mine(x.slot) && x.hp > 0);
  if (enemies.length === 0) { out.kind = "attack"; return out; }

  // 威胁：学到的平均伤害，没学到时按等级估；不能动的敌人这回合没有威胁
  const threat = (e: SmartFighter) => DISABLED.has(e.status) ? 0 : c.mem.threat(e.name, e.level * 2.5 + 10);
  const totalThreat = enemies.reduce((a, e) => a + threat(e), 0), maxThreat = Math.max(...enemies.map(threat));
  const incoming = totalThreat / Math.max(1, aliveAllies.length) + maxThreat * 0.5;
  const weight = (x: SmartFighter) => x.slot === me ? ME_WEIGHT : x.slot % 10 < 5 ? PLAYER_WEIGHT : PET_WEIGHT;
  const limit = s.healAt / 100;
  const needs = aliveAllies.map((x) => {
    const riding = (x.rideMaxHp ?? 0) > 0 && (x.rideHp ?? 0) > 0;
    const lethal = x.hp - incoming * 1.2 <= 0, low = x.hp < x.maxHp * limit || riding && x.rideHp! < x.rideMaxHp! * limit;
    const deficit = x.maxHp - x.hp + (riding ? x.rideMaxHp! - x.rideHp! : 0);
    return { x, lethal, deficit, need: lethal || low ? deficit * weight(x) * (lethal ? 2 : 1) : 0 };
  }).filter((n) => n.need > 0).sort((a, b) => b.need - a.need);

  const spells = c.inv.filter((it) => it.magic && it.magicField !== 2 && Math.floor(it.magicMp * 80 / 100) <= meF.mp); // 光精 MP 八折（同原版的 AI）
  const items = c.inv.filter((it) => it.slot >= 9);
  let done = false;
  const send = (kind: AiCommand["kind"], target: number, item: number, label: string, key = "") => {
    out.kind = kind; out.target = target; out.item = item; out.label = label; out.key = key; done = true;
  };

  // 补血
  if (!done && needs.length) {
    const heals = spells.filter((m) => m.magicFunc === "MAGIC_Recovery" || m.magicFunc === "MAGIC_OtherRecovery");
    const group = heals.filter((m) => m.magicTarget === 2 || m.magicTarget === 8).sort((a, b) => Number(b.magicOption) - Number(a.magicOption))[0];
    if (needs.length >= 2 && group) send("magic", side, group.slot, `全体补血：${group.magic}`);
    else {
      const t = needs[0].x, deficit = needs[0].deficit;
      const single = heals.filter((m) => m.magicTarget === 1 || (m.magicTarget === 0 && t.slot === me));
      const meat = items.find((it) => it.name.includes("肉") && it.memo.includes("耐久力"));
      const healItems = items.filter((it) => it.memo.includes("耐久力") && it.memo.includes("回复"));
      const pick = <T,>(list: T[], amount: (x: T) => number) =>
        [...list].sort((a, b) => amount(a) - amount(b)).find((x) => amount(x) >= deficit) ?? [...list].sort((a, b) => amount(b) - amount(a))[0];
      const m = pick(single, (x) => Number(x.magicOption) || 0), it = meat ?? pick(healItems, healAmount);
      if (m) send("magic", t.slot, m.slot, `补血：${t.name}`);
      else if (it) send("item", t.slot, it.slot, `补血：${t.name}（${it.name}）`);
      else if (group) send("magic", side, group.slot, `全体补血：${group.magic}`);
      else out.chat.push("无法回复耐久力了");
    }
  }

  // 复活
  if (!done) {
    const dead = allies.filter((x) => x.hp <= 0 && x.slot !== me).sort((a, b) => weight(b) - weight(a))[0];
    if (dead) {
      const m = spells.find((x) => x.magicFunc === "MAGIC_Ressurect" || x.magicFunc === "MAGIC_ResAndDef");
      const it = items.find((x) => x.memo.includes("气绝回复"));
      if (m) send("magic", dead.slot, m.slot, `复活：${dead.name}`);
      else if (it) send("item", dead.slot, it.slot, `复活：${dead.name}（${it.name}）`);
    }
  }

  // 解状态：队友 / 宠物不能动，或中毒又快没血了
  if (!done) {
    const cures = spells.filter((m) => m.magicFunc === "MAGIC_StatusRecovery");
    for (const x of aliveAllies) {
      const bad = DISABLED.has(x.status) && x.slot !== me || (x.status === 1 || x.status === 8) && x.hp < x.maxHp * 0.3;
      if (!bad) continue;
      const ch = STATUS_CHAR[x.status];
      const m = cures.find((m) => m.magicOption === "全" || (ch && m.magicOption.includes(ch)));
      if (m) { send("magic", m.magicTarget === 2 || m.magicTarget === 8 ? side : x.slot, m.slot, `解除状态：${x.name}`); break; }
    }
  }

  // 补气：MP 连最便宜的补血精灵都放不了
  if (!done) {
    const allHeals = c.inv.filter((it) => it.magicFunc === "MAGIC_Recovery");
    const cheapest = Math.min(...allHeals.map((m) => Math.floor(m.magicMp * 80 / 100)));
    const mpItem = items.find((it) => it.memo.includes("气") && it.memo.includes("回复") && !it.memo.includes("耐久力"));
    if (allHeals.length && meF.mp < cheapest && mpItem) send("item", me, mpItem.slot, "补气");
  }

  // 进攻的候选：普通攻击与攻击精灵
  const hp = new Map(enemies.map((e) => [e.slot, e.hp]));
  const atkPrior = Math.max(1, c.myAtk * 0.5);
  const value = (hits: Map<number, number>, cost: number) => {
    let v = -cost;
    for (const [slot, d] of hits) {
      const e = f(slot)!, left = hp.get(slot) ?? 0;
      v += Math.min(d, left) + (d >= left ? threat(e) * 2 + 5 : 0) + threat(e) * 0.3;
    }
    return v;
  };
  const captureTarget = s.capture && c.capturePlace ? enemies.find((e) => e.level === 1) : undefined;
  const plans: Plan[] = [];
  for (const e of enemies) {
    if (captureTarget && e.slot === captureTarget.slot) continue;
    const d = c.mem.get("me:attack", atkPrior);
    plans.push({ value: value(new Map([[e.slot, d]]), 0), kind: "attack", target: e.slot, item: -1, label: `攻击：${e.name}`, key: "me:attack", hits: new Map([[e.slot, d]]) });
  }
  const mpReserve = Math.min(...c.inv.filter((it) => it.magicFunc === "MAGIC_Recovery").map((m) => m.magicMp), 1e9);
  for (const m of s.magic ? spells.filter((m) => m.magicFunc === "MAGIC_AttMagic") : []) { // "用精灵进攻"关掉：不用攻击精灵
    const key = `me:magic:${m.magic}`, d = c.mem.get(key, atkPrior * 0.8);
    const cost = m.magicMp * (meF.mp - m.magicMp < mpReserve ? 1.5 : 0.3); // 会吃掉补血用的 MP 就贵一点
    const groups: [number, SmartFighter[], string][] = [];
    if (m.magicTarget === 3 || m.magicTarget === 11) groups.push([otherSide, enemies, "全体"]);
    else if (m.magicTarget === 4) groups.push([22, enemies, "全体"]);
    else if (m.magicTarget === 10) {
      const lo = me < 10 ? 10 : 0;
      groups.push([me < 10 ? 23 : 25, enemies.filter((e) => e.slot < lo + 5), "前排"], [me < 10 ? 24 : 26, enemies.filter((e) => e.slot >= lo + 5), "后排"]);
    } else for (const e of enemies) groups.push([e.slot, [e], e.name]);
    for (const [t, list, name] of groups) {
      if (!list.length || list.some((e) => captureTarget && e.slot === captureTarget.slot)) continue;
      const hits = new Map(list.map((e) => [e.slot, d]));
      plans.push({ value: value(hits, cost), kind: "magic", target: t, item: m.slot, label: `${m.magic}：${name}`, key, hits });
    }
  }

  // 逃跑：打不过而且逃得掉（"打不过就逃跑"关掉就不逃）
  if (!done && !c.noEscape && s.escape) {
    const myHp = aliveAllies.reduce((a, x) => a + x.hp, 0), best = Math.max(0, ...plans.map((p) => [...p.hits.values()].reduce((a, b) => a + b, 0)));
    const enemyHp = enemies.reduce((a, e) => a + e.hp, 0);
    const lost = needs.some((n) => n.lethal && n.x.slot === me) || (totalThreat * 2 > myHp && best * 2 * aliveAllies.length < enemyHp);
    if (lost) { send("escape", -1, -1, "打不过，逃跑"); out.pet = [-1, -1]; return out; }
  }

  // 抓宠：只抓 1 级的（原版的规则），没有就逃
  if (!done && captureTarget) send("capture", captureTarget.slot, -1, `抓宠：${captureTarget.name}`);
  else if (!done && s.capture && c.capturePlace && !c.noEscape) { send("escape", -1, -1, "没有 1 级的，逃跑"); return out; }

  // 固定的行动（设定的第一回合 / 之后每回合）：没有紧急的事时照做
  const bestEnemy = () => { const p = plans.filter((p) => p.kind === "attack").sort((a, b) => b.value - a.value)[0]; return p ? p.target : enemies[0]?.slot ?? -1; };
  const act = c.first ? s.first : s.then;
  let onlyAttack = false;
  if (!done && act.kind !== "auto") {
    if (act.kind === "attack") onlyAttack = true;
    else if (act.kind === "guard") send("guard", -1, -1, "防御");
    else if (act.kind === "magic") {
      const m = spells.find((x) => x.magic === act.name);
      const pf = f(pet), t = m ? magicTargetSlot(m.magicTarget, act.target, me, pet, !!pf && pf.hp > 0, side, otherSide, bestEnemy) : -1;
      if (m && t >= 0) send("magic", t, m.slot, `${m.magic}`, `me:magic:${m.magic}`);
      else out.chat.push(`无法使用 ${act.name}，改用智能判断`);
    }
  }

  let mine0: Plan | undefined;
  if (!done) {
    mine0 = plans.filter((p) => !onlyAttack || p.kind === "attack").sort((a, b) => b.value - a.value)[0];
    if (mine0) send(mine0.kind, mine0.target, mine0.item, mine0.label, mine0.key);
    else send("attack", enemies[0].slot, -1, "攻击");
  }
  // 主人这回合预计打掉的，从敌人剩下的 HP 扣掉，宠物就不去补刀已经打得倒的
  if (mine0) for (const [slot, d] of mine0.hits) hp.set(slot, Math.max(0, (hp.get(slot) ?? 0) - d));

  // 宠物
  const petF = f(pet);
  if (c.pet && petF && petF.hp > 0) {
    // 宠物固定的行动（设定）：普通攻击 = 技能 1、防御 = 技能 2，其余按名字找；对象照设定
    const pact = c.first ? s.petFirst : s.petThen;
    if (pact.kind !== "auto") {
      const skills = c.pet.skills.filter((x) => x.field !== 2);
      const sk = pact.kind === "attack" ? skills.find((x) => x.id === 1) : pact.kind === "guard" ? skills.find((x) => x.id === 2) : skills.find((x) => x.name === pact.name);
      const live = enemies.filter((e) => (hp.get(e.slot) ?? 0) > 0 && !(captureTarget && e.slot === captureTarget.slot));
      const petEnemy = () => (live.sort((a, b) => threat(b) - threat(a))[0] ?? enemies[0])?.slot ?? -1;
      const t = sk ? petTargetSlot(sk.target, pact.target, me, pet, side, otherSide, petEnemy) : -1;
      if (sk && t >= 0) { out.pet = [sk.slot, t]; out.petKey = `pet:${c.pet.name}:${sk.id}`; return out; }
      out.chat.push(`宠物无法使用 ${pact.kind === "attack" ? "攻击" : pact.kind === "guard" ? "防御" : pact.name}，改用智能判断`);
    }
    const pplans: { value: number; slot: number; target: number; key: string }[] = [];
    const petPrior = Math.max(1, c.pet.atk * 0.5);
    const meDanger = needs.find((n) => n.x.slot === me);
    for (const sk of c.pet.skills) {
      if (sk.field === 2 || PET_AVOID.has(sk.func) && sk.func !== PET_MERCY) continue;
      const key = `pet:${c.pet.name}:${sk.id}`;
      if (sk.func === PET_MERCY) {
        if (captureTarget && (out.kind === "capture")) pplans.push({ value: 1e6, slot: sk.slot, target: captureTarget.slot, key });
        continue;
      }
      if (PET_GUARD.has(sk.func)) { pplans.push({ value: meDanger ? meDanger.need * 0.8 : 0, slot: sk.slot, target: me, key: "" }); continue; }
      if (PET_DEFEND.has(sk.func)) {
        const pn = needs.find((n) => n.x.slot === pet);
        pplans.push({ value: pn?.lethal ? pn.need * 0.5 : 0, slot: sk.slot, target: pet, key: "" });
        continue;
      }
      const d = c.mem.get(key, petPrior * petSkillPrior(sk));
      const live = enemies.filter((e) => (hp.get(e.slot) ?? 0) > 0 && !(captureTarget && e.slot === captureTarget.slot));
      if (!live.length) continue;
      if (sk.target === 3 || sk.target === 4) {
        let v = 0;
        for (const e of live) v += Math.min(d, hp.get(e.slot)!) + (d >= hp.get(e.slot)! ? threat(e) * 2 : 0);
        pplans.push({ value: v, slot: sk.slot, target: sk.target === 4 ? 22 : otherSide, key });
      } else if (sk.target === 1 || sk.target === 6 || sk.target === 7) {
        for (const e of live) {
          const left = hp.get(e.slot)!;
          pplans.push({ value: Math.min(d, left) + (d >= left ? threat(e) * 2 + 5 : 0) + threat(e) * 0.3, slot: sk.slot, target: e.slot, key });
        }
      }
    }
    const best = pplans.sort((a, b) => b.value - a.value)[0];
    if (best) { out.pet = [best.slot, best.target]; out.petKey = best.key; }
  }
  return out;
}

// ---------------- 固定行动的对象 ----------------

/** 咒术的目标编号：按咒术的范围（magicTarget）与设定的对象算出要送的目标；-1 是没有对象（回到智能判断） */
function magicTargetSlot(type: number, want: AiTarget, me: number, pet: number, petAlive: boolean, side: number, otherSide: number, enemy: () => number): number {
  const rowOf = (s: number) => s < 0 ? -1 : [26, 25, 23, 24][Math.floor(s / 5)];
  const single = (def: number) => want === "self" ? me : want === "pet" ? (petAlive ? pet : -1) : want === "enemy" ? enemy() : def;
  switch (type) {
    case 0: return me;                                    // 只能对自己
    case 1: return single(me);                            // 其他人（含自己）：预设自己
    case 2: return side;                                  // 我方全体
    case 8: return want === "enemies" ? otherSide : side; // 任一边全体
    case 3: case 11: return otherSide;                    // 对方全体 / 全部的排
    case 4: return 22;                                    // 全体
    case 9: return enemy();                               // 单一敌人
    case 10: return rowOf(enemy());                       // 一排
  }
  return me;
}

/** 宠物技能的目标编号（PETSKILL_TARGET：0 自己、1 别人含自己、2 我方全体、3 对方全体、4 全部、5 无、6 别人不含自己、7 自己与宠物以外） */
function petTargetSlot(type: number, want: AiTarget, me: number, pet: number, side: number, otherSide: number, enemy: () => number): number {
  const single = (allowSelf: boolean, allowOwner: boolean) =>
    want === "self" && allowSelf ? pet : want === "owner" && allowOwner ? me : enemy();
  switch (type) {
    case 0: case 5: return pet;
    case 1: return single(true, true);
    case 2: return side;
    case 3: return otherSide;
    case 4: return 22;
    case 6: return single(false, true);
  }
  return enemy();
}
