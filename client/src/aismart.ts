// 智能自动战斗（原版没有，额外新功能；原版的 AI 模式在 ai.ts，设定窗口"其它功能"切换）。
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
import type { AiCommand, AiSettings } from "./ai.ts";

export interface SmartFighter {
  slot: number; name: string; hp: number; maxHp: number; mp: number; level: number; status: number; gone: boolean;
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
  const limit = s.limit[0] / 100;
  const needs = aliveAllies.map((x) => {
    const lethal = x.hp - incoming * 1.2 <= 0, low = x.hp < x.maxHp * limit;
    return { x, lethal, need: lethal || low ? (x.maxHp - x.hp) * weight(x) * (lethal ? 2 : 1) : 0 };
  }).filter((n) => n.need > 0).sort((a, b) => b.need - a.need);

  const spells = c.inv.filter((it) => it.magic && it.magicField !== 2 && Math.floor(it.magicMp * 80 / 100) <= meF.mp); // 光精 MP 八折（同原版的 AI）
  const items = c.inv.filter((it) => it.slot >= 9);
  let done = false;
  const send = (kind: AiCommand["kind"], target: number, item: number, label: string, key = "") => {
    out.kind = kind; out.target = target; out.item = item; out.label = label; out.key = key; done = true;
  };

  // 首次：照设定的"首次"精灵（原版的规则，智能模式也保留）
  const first = s.method[2] >= 0 && s.method[2] < 9 ? c.inv.find((x) => x.slot === s.method[2] && x.magic) : undefined;
  if (c.first && first && spells.includes(first) && !needs.some((n) => n.lethal)) {
    const t = first.magicTarget === 0 ? me : first.magicTarget === 2 || first.magicTarget === 8 ? side : first.magicTarget === 3 ? otherSide : me;
    send("magic", t, first.slot, `首次：${first.magic}`);
  }

  // 补血
  if (!done && needs.length) {
    const heals = spells.filter((m) => m.magicFunc === "MAGIC_Recovery" || m.magicFunc === "MAGIC_OtherRecovery");
    const group = heals.filter((m) => m.magicTarget === 2 || m.magicTarget === 8).sort((a, b) => Number(b.magicOption) - Number(a.magicOption))[0];
    if (needs.length >= 2 && group) send("magic", side, group.slot, `全体补血：${group.magic}`);
    else {
      const t = needs[0].x, deficit = t.maxHp - t.hp;
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
  const captureTarget = s.other[1] && c.capturePlace ? enemies.find((e) => e.level === 1) : undefined;
  const plans: Plan[] = [];
  for (const e of enemies) {
    if (captureTarget && e.slot === captureTarget.slot) continue;
    const d = c.mem.get("me:attack", atkPrior);
    plans.push({ value: value(new Map([[e.slot, d]]), 0), kind: "attack", target: e.slot, item: -1, label: `攻击：${e.name}`, key: "me:attack", hits: new Map([[e.slot, d]]) });
  }
  const mpReserve = Math.min(...c.inv.filter((it) => it.magicFunc === "MAGIC_Recovery").map((m) => m.magicMp), 1e9);
  for (const m of spells.filter((m) => m.magicFunc === "MAGIC_AttMagic")) {
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

  // 逃跑：打不过而且逃得掉
  if (!done && !c.noEscape) {
    const myHp = aliveAllies.reduce((a, x) => a + x.hp, 0), best = Math.max(0, ...plans.map((p) => [...p.hits.values()].reduce((a, b) => a + b, 0)));
    const enemyHp = enemies.reduce((a, e) => a + e.hp, 0);
    const lost = needs.some((n) => n.lethal && n.x.slot === me) || (totalThreat * 2 > myHp && best * 2 * aliveAllies.length < enemyHp);
    if (lost) { send("escape", -1, -1, "打不过，逃跑"); out.pet = [-1, -1]; return out; }
  }

  // 抓宠：只抓 1 级的（原版的规则），没有就逃
  if (!done && captureTarget) send("capture", captureTarget.slot, -1, `抓宠：${captureTarget.name}`);
  else if (!done && s.other[1] && c.capturePlace && !c.noEscape) { send("escape", -1, -1, "没有 1 级的，逃跑"); return out; }

  let mine0: Plan | undefined;
  if (!done) {
    mine0 = plans.sort((a, b) => b.value - a.value)[0];
    if (mine0) send(mine0.kind, mine0.target, mine0.item, mine0.label, mine0.key);
    else send("attack", enemies[0].slot, -1, "攻击");
  }
  // 主人这回合预计打掉的，从敌人剩下的 HP 扣掉，宠物就不去补刀已经打得倒的
  if (mine0) for (const [slot, d] of mine0.hits) hp.set(slot, Math.max(0, (hp.get(slot) ?? 0) - d));

  // 宠物
  const petF = f(pet);
  if (c.pet && petF && petF.hp > 0) {
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
