// 自动战斗（原版 ai_setting.cpp，__AI / _AI_OTHER / _AI_CAPTURE / __ATTACK_MAGIC 开着、_ATTACK_AI 没开）：
//   设定 4 格：0 补血、1 补气、2 首次、3 复活；每格是"精灵（装备位 0..8，9 使用道具，-1 不能使用）+ 顺序（目标）+ 数值（%）"，
//   另有"自动丢肉""自动抓宠"两个开关。设定存在 localStorage（sa.ai.<角色名>，原版存 data\AISetting.dat）。
//   每回合（AI_ChooseAction）：第一回合先用"首次"的精灵 → 有人 HP 低于补血的数值就补血（肉优先，再按设定用精灵或道具）→
//   复活倒下的 → MP 低于补气的数值用回复气力的道具 → 自己或宠物 HP 太低又没法补血时逃跑，否则攻击（开着自动抓宠时抓 1 级的、没有就逃跑）。
//   宠物用"攻击"（技能 1），没有时用最后一个不是待机 / 防御的战斗技能。
// 设定窗口（AI_SettingProc）：系统菜单的"战斗设定"，底图 35225（413×302，画面中央），确定 / 取消、每格上下箭头切换；"其它功能"开自动丢肉 / 抓宠的小窗口。

import type { InvItem } from "./proto.gen.ts";
import { FONT, type Ui } from "./ui.ts";

// 目标代号（ai_setting.cpp 15..28）
export const AT = { MYSELF: 1, MYPET: 2, MYSIDE: 3, OTHERONE: 4, OTHERSIDE: 5, ALL: 6, P1: 7, P5: 11, ONE_ROW: 12 };
const TARGET_TEXT: Record<number, string> = { 1: "自己", 2: "宠物", 3: "我方全体", 4: "敌人", 5: "敌人全体", 6: "全体",
  7: "玩家一", 8: "玩家二", 9: "玩家三", 10: "玩家四", 11: "玩家五", 12: "敌一排" };
// MAGIC_TARGET_*（magic.h）
const MT = { MYSELF: 0, OTHER: 1, ALLMYSIDE: 2, ALLOTHERSIDE: 3, ALL: 4, WHOLEOTHERSIDE: 8, SINGLE: 9, ONE_ROW: 10, ALL_ROWS: 11 };
const MAX_MAGIC = 9, USE_ITEM = 9, NONE = -1;

// other：0 自动丢肉、1 自动抓宠、2 智能战斗（aismart.ts，额外新功能；0 是原版的 AI 模式）
export interface AiSettings { method: number[]; target: number[]; limit: number[]; other: number[] }

export function defaultAi(): AiSettings {
  return { method: [USE_ITEM, USE_ITEM, NONE, USE_ITEM], target: [1, 1, 1, 2], limit: [30, 30, 30, 30], other: [0, 0, 1] };
}

export function loadAi(name: string): AiSettings {
  try {
    const s = JSON.parse(localStorage.getItem(`sa.ai.${name}`) ?? "null");
    if (s && Array.isArray(s.method) && s.method.length === 4) {
      if (!Array.isArray(s.other)) s.other = [0, 0];
      if (s.other.length < 3) s.other.push(1); // 旧的设定没有这一格：预设开着智能战斗
      return s;
    }
  } catch { /* 存不了或坏掉就用预设 */ }
  return defaultAi();
}

export function saveAi(name: string, s: AiSettings) {
  try { localStorage.setItem(`sa.ai.${name}`, JSON.stringify(s)); } catch { /* 无痕模式等存不了就算了 */ }
}

/** 装备位上的咒术（magic[0..8]） */
export function magicAt(inv: InvItem[], i: number): InvItem | undefined {
  return inv.find((x) => x.slot === i && x.magic);
}

/** memo 里先有 a、后面再有 b（原版 strstr 两次） */
function memoHas(memo: string, a: string, b: string): boolean {
  const k = memo.indexOf(a);
  return k >= 0 && memo.indexOf(b, k + a.length) >= 0;
}
const isCure = (m?: InvItem) => !!m && memoHas(m.magicMemo, "耐久力", "回复");
const isRevive = (m?: InvItem) => !!m && memoHas(m.magicMemo, "气绝状态", "回复");

/** AI_SetMethod：选了精灵之后按咒术的目标种类定顺序 */
function targetForMagic(type: number): number {
  switch (type) {
    case MT.MYSELF: case MT.OTHER: return AT.MYSELF;
    case MT.ALLMYSIDE: case MT.WHOLEOTHERSIDE: return AT.MYSIDE;
    case MT.ALLOTHERSIDE: case MT.ALL_ROWS: return AT.OTHERSIDE;
    case MT.ALL: return AT.ALL;
    case MT.SINGLE: return AT.OTHERONE;
    case MT.ONE_ROW: return AT.ONE_ROW;
  }
  return AT.MYSELF;
}

const isPlayerTarget = (t: number) => t >= AT.P1 && t <= AT.P5;

/** AI_CheckSetting：打开窗口时与上线时把不合规的设定改回来 */
export function checkAi(s: AiSettings, inv: InvItem[]) {
  for (let i = 0; i < 4; i++) if (!(s.limit[i] >= 0 && s.limit[i] <= 100 && s.limit[i] % 5 === 0)) s.limit[i] = 30;
  const m0 = magicAt(inv, s.method[0]);
  if (s.method[0] >= 0 && s.method[0] < MAX_MAGIC) {
    if (isCure(m0)) {
      const t = m0!.magicTarget;
      if (t === MT.MYSELF) s.target[0] = AT.MYSELF;
      else if (t === MT.ALLMYSIDE || t === MT.WHOLEOTHERSIDE) s.target[0] = AT.MYSIDE;
      else if (t !== MT.OTHER) { s.method[0] = USE_ITEM; s.target[0] = AT.MYSELF; }
    } else { s.method[0] = USE_ITEM; s.target[0] = AT.MYSELF; }
  } else s.method[0] = USE_ITEM;
  if (s.method[0] === USE_ITEM && !(s.target[0] === 1 || s.target[0] === 2 || isPlayerTarget(s.target[0]))) s.target[0] = AT.MYSELF;
  s.method[1] = USE_ITEM; s.target[1] = AT.MYSELF;
  const m2 = magicAt(inv, s.method[2]);
  const legal: Record<number, number[]> = {
    [MT.MYSELF]: [1], [MT.ALLMYSIDE]: [3], [MT.WHOLEOTHERSIDE]: [3, 5], [MT.OTHER]: [1, 2, 4, 7, 8, 9, 10, 11],
    [MT.ALLOTHERSIDE]: [5], [MT.ALL]: [6], [MT.SINGLE]: [4], [MT.ONE_ROW]: [12], [MT.ALL_ROWS]: [5] };
  if (!m2 || !(legal[m2.magicTarget] ?? []).includes(s.target[2])) s.method[2] = NONE;
  if (!(s.method[3] >= 0 && s.method[3] < MAX_MAGIC && isRevive(magicAt(inv, s.method[3])))) s.method[3] = USE_ITEM;
  if (!(s.target[3] === 3 || s.target[3] === 2 || isPlayerTarget(s.target[3]))) s.target[3] = AT.MYPET;
}

// ---------------- 每回合的决定 ----------------

export interface AiFighter { slot: number; hp: number; maxHp: number; mp: number; level: number; dead: boolean; gone: boolean }
export interface AiPetSkill { slot: number; id: number; field: number; target: number }

/** AI_ChooseAction 需要的战场资料 */
export interface AiContext {
  me: number;
  first: boolean;                 // AI_First：这场的第一回合
  fighters: Map<number, AiFighter>;
  inv: InvItem[];
  petSkills: AiPetSkill[] | null; // 出战宠物的技能（没有出战宠物为 null）
  capturePlace: boolean;          // 自动抓宠在这个地方有效（原版楼层 500 的部分区域不行）
}

/** 送出的指令（battle.ts 换成 BattleCommand / BattlePetCommand） */
export interface AiCommand {
  kind: "magic" | "item" | "attack" | "capture" | "escape" | "none" | "skip";
  target: number;
  item: number;
  label: string;         // AI_Choosen：播放时右下角显示的字
  chat: string[];        // 原版 StockChatBufferLine 的提示
  pet: [number, number]; // "W|技能|目标"，-1 是 FF
}

const ORDINAL = [4, 2, 0, 1, 3]; // 玩家一..玩家五对应的位置（原版的表，对右边的阵营不对，照原版）
const BATTLE_ORDER = [19, 17, 15, 16, 18, 14, 12, 10, 11, 13, 9, 7, 5, 6, 8, 4, 2, 0, 1, 3]; // GetBattelTarget 的 Ordinal

export function aiChoose(s: AiSettings, c: AiContext): AiCommand {
  const out: AiCommand = { kind: "none", target: -1, item: -1, label: "", chat: [], pet: [-1, -1] };
  const me = c.me, pet = me + 5;
  const f = (i: number) => { const x = c.fighters.get(i); return x && !x.gone ? x : undefined; };
  const meF = f(me)!, petF = f(pet);
  const low = (x: AiFighter) => Math.floor(x.maxHp * s.limit[0] / 100) > x.hp;
  const mpOK = (m: InvItem) => Math.floor(m.magicMp * 80 / 100) <= meF.mp; // pc.familySprite == 0（光精）
  const side = me < 10 ? 20 : 21, otherSide = me < 10 ? 21 : 20;
  const enemy = (): number => {
    const list = me < 10 ? BATTLE_ORDER.slice(0, 10) : BATTLE_ORDER.slice(10);
    const ok = list.filter((i) => { const x = f(i); return x && x.hp > 0 && i !== me && i !== pet; });
    if (s.other[1]) { const lv1 = ok.find((i) => f(i)!.level === 1); if (lv1 !== undefined) return lv1; }
    return ok[0] ?? -1;
  };
  const byOrdinal = (t: number, test: (x: AiFighter) => boolean) => {
    const k = ORDINAL[t - AT.P1];
    for (const i of [k, k + 5]) { const x = f(i); if (x && test(x)) return i; }
    return -1;
  };
  let done = false, esc = false;
  const send = (kind: AiCommand["kind"], target: number, item: number, label: string) => {
    out.kind = kind; out.target = target; out.item = item; out.label = label; done = true;
  };
  if (!meF || meF.hp <= 0) {
    out.kind = "skip"; // 倒下了：人不下指令（宠物照样 W|FF|FF）
  } else {
    // A. 第一回合的"首次"精灵
    const m2 = s.method[2] >= 0 && s.method[2] < MAX_MAGIC ? magicAt(c.inv, s.method[2]) : undefined;
    if (c.first && m2) {
      if (!mpOK(m2)) out.chat.push("MP不足无法使用首次的精灵");
      else {
        let t = -1;
        const type = m2.magicTarget;
        switch (s.target[2]) {
          case AT.MYSELF: t = me; break;
          case AT.MYPET: t = petF ? pet : -1; break;
          case AT.MYSIDE: t = type === MT.ALLMYSIDE || type === MT.WHOLEOTHERSIDE ? side : -1; break;
          case AT.OTHERONE: t = enemy(); break;
          case AT.OTHERSIDE: t = type === MT.ALL_ROWS || type === MT.WHOLEOTHERSIDE || type === MT.ALLOTHERSIDE ? otherSide : -1; break;
          case AT.ALL: t = 22; break;
          case AT.ONE_ROW: { const e = enemy(); t = e < 5 ? 25 : e < 10 ? 26 : e < 15 ? 23 : 24; break; }
          default: if (isPlayerTarget(s.target[2])) t = byOrdinal(s.target[2], () => true);
        }
        if (t > -1) send("magic", t, m2.slot, "使用首次精灵");
      }
    }
    // B. 补血
    if (!done) {
      let item = -1, meat = -1;
      for (const it of c.inv) {
        if (!memoHas(it.memo, "耐久力", "回复")) continue;
        item = it.slot;
        if (it.name.includes("肉")) { meat = it.slot; break; }
      }
      const m0 = s.method[0] >= 0 && s.method[0] < MAX_MAGIC ? magicAt(c.inv, s.method[0]) : undefined;
      const magicOK = !!m0 && isCure(m0) && mpOK(m0);
      let t = isPlayerTarget(s.target[0]) ? byOrdinal(s.target[0], (x) => x.hp > 0 && low(x)) : -1;
      if (t === -1) {
        const meLow = low(meF), petLow = !!petF && petF.hp > 0 && low(petF);
        if (s.target[0] === AT.MYSELF) t = meLow ? me : petLow ? pet : -1;
        else t = petLow ? pet : meLow ? me : -1;
      }
      if (t > -1) {
        if (item < 0 && !magicOK) out.chat.push("无法回复耐久力了");
        else if (meat > -1) send("item", t, meat, "使用肉回复耐久力");
        else {
          if (m0 && (m0.magicTarget !== MT.MYSELF || t === me)) {
            if (magicOK) send("magic", m0.magicTarget === MT.WHOLEOTHERSIDE ? side : t, m0.slot, "使用耐久力回复精灵");
            else out.chat.push("无法使用耐久力回复的精灵");
          }
          if (!done) {
            if (item > -1) send("item", t, item, "使用耐久力回复道具");
            else out.chat.push("没有耐久力回复的道具");
          }
        }
      }
    }
    // C. 复活
    if (!done) {
      let t = -1;
      if (s.target[3] === AT.MYSIDE) {
        if (petF && petF.hp <= 0) t = pet;
        else for (let i = side === 20 ? 0 : 10; i < (side === 20 ? 10 : 20) && t < 0; i++) {
          const x = f(i);
          if (i !== me && i !== pet && x && x.hp <= 0) t = i;
        }
      } else if (s.target[3] === AT.MYPET) t = petF && petF.hp <= 0 ? pet : -1;
      else if (isPlayerTarget(s.target[3])) t = byOrdinal(s.target[3], (x) => x.hp <= 0);
      if (t > -1) {
        if (s.method[3] === USE_ITEM) {
          const it = c.inv.find((x) => x.memo.includes("气绝回复"));
          if (it) send("item", t, it.slot, "使用气绝回复道具");
          else out.chat.push("没有气绝回复的道具");
        } else {
          const m3 = magicAt(c.inv, s.method[3]);
          if (m3 && isRevive(m3)) {
            if (mpOK(m3)) send("magic", t, m3.slot, "使用气绝回复精灵");
            else out.chat.push("MP不足无法使用气绝回复的精灵");
          } else out.chat.push("没有设定气绝回复的精灵");
        }
      }
    }
    // D. 补气：原版拿补气的数值直接和 MP 比（当作最大 MP 100）
    if (!done && s.limit[1] > meF.mp) {
      const it = c.inv.find((x) => memoHas(x.memo, "气", "回复"));
      if (it) send("item", me, it.slot, "使用回复气力道具");
    }
    // E. 逃跑或攻击
    if (!done) {
      const m0 = s.method[0] >= 0 && s.method[0] < MAX_MAGIC ? magicAt(c.inv, s.method[0]) : undefined;
      const hurt = low(meF) || (!!petF && ((petF.hp > 0 && low(petF)) || petF.hp <= 0));
      if (hurt && !(m0 && mpOK(m0))) { esc = true; send("escape", -1, -1, ""); }
      else if (s.other[1] && c.capturePlace) {
        const e = enemy();
        if (e > -1 && f(e)!.level === 1) send("capture", e, -1, "自动抓宠");
        else { esc = true; send("escape", -1, -1, "抓宠逃跑"); }
      } else send("attack", enemy(), -1, "自动攻击");
    }
  }
  // F. 宠物
  if (c.petSkills && petF && petF.hp > 0 && meF && meF.hp > 0 && !esc) {
    let sk: AiPetSkill | undefined;
    for (const x of c.petSkills) {
      if (x.field === 2) continue; // PETSKILL_FIELD_MAP
      if (!sk) sk = x;
      if (x.id !== 0 && x.id !== 2) sk = x;
      if (x.id === 1) { sk = x; break; }
    }
    if (sk) {
      let t = -1;
      switch (sk.target) {
        case 0: t = pet; break;
        case 1: case 6: t = enemy(); break;
        case 2: t = side; break;
        case 3: t = otherSide; break;
        case 4: t = 22; break;
      }
      out.pet = [sk.slot, t];
    }
  }
  return out;
}

// ---------------- 设定窗口 ----------------

const WND = { BG: 35225, OK: 26262, CANCEL: 26264, UP: 26064, DOWN: 26066, CLOSE: 26042, W: 413, H: 302 };
const UP_OFF: [number, number] = [94, -106], DOWN_OFF: [number, number] = [113, -106]; // 箭头图的位移
const COL_X = [210, 297, 380]; // 精灵 / 顺序 / 数值 三栏箭头的左上角
const NOTICE: Record<string, string> = {
  ok: "储存设定", cancel: "取消设定", m0: "选择回复精灵", t0: "选择优先顺序", lu: "增加耐久力忍受度", ld: "减少耐久力忍受度",
  l1: "建议设定高于使用精灵所需MP", m2: "选择首次精灵", t2: "选择使用目标", m3: "选择气绝回复方式", t3: "选择使用目标",
};

export class AiWindow {
  open = false;
  private other = false;
  private s: AiSettings = defaultAi();
  private name = "";
  private inv: () => InvItem[];
  private ui: Ui;

  constructor(ui: Ui, inv: () => InvItem[]) { this.ui = ui; this.inv = inv; }

  /** AI_State = 1：重读、检查后打开 */
  show(name: string) {
    this.name = name;
    this.s = loadAi(name);
    checkAi(this.s, this.inv());
    saveAi(name, this.s);
    this.open = true; this.other = false;
  }

  close() { this.open = false; this.other = false; }

  /** 画一帧；回传画出的范围 */
  frame(rects: [number, number, number, number][], esc: boolean) {
    if (!this.open) return;
    const ui = this.ui, x = Math.floor((800 - WND.W) / 2), y = Math.floor((600 - WND.H) / 2), s = this.s, inv = this.inv();
    if (esc) { this.close(); return; }
    ui.hitBox(x, y, x + WND.W, y + WND.H, "ai-wnd", false);
    rects.push([x, y, x + WND.W, y + WND.H]);
    ui.image(WND.BG, x + 206, y + 151);
    const arrow = (row: number, col: number, id: string, up: () => void, down: () => void) => {
      const rx = x + COL_X[col], uy = y + 91 + 36 * row, dy = uy + 16;
      if (ui.button(WND.UP, rx - UP_OFF[0], uy - UP_OFF[1], `${id}u`, WND.UP + 1)) up();
      if (ui.button(WND.DOWN, rx - DOWN_OFF[0], dy - DOWN_OFF[1], `${id}d`, WND.DOWN + 1)) down();
      for (const k of [`${id}u`, `${id}d`]) if (ui.hovered(k)) ui.bottomLine = NOTICE[id] ?? NOTICE[k] ?? "";
    };
    // 补血：精灵（道具 ↔ 回复耐久力的精灵）、顺序、数值
    arrow(0, 0, "m0", () => this.stepMethod(0, 1, isCure), () => this.stepMethod(0, -1, isCure));
    arrow(0, 1, "t0", () => this.stepTarget(0, 1), () => this.stepTarget(0, -1));
    arrow(0, 2, "l0", () => { s.limit[0] = Math.min(100, s.limit[0] + 5); }, () => { s.limit[0] = Math.max(0, s.limit[0] - 5); });
    if (ui.hovered("l0u")) ui.bottomLine = NOTICE.lu;
    if (ui.hovered("l0d")) ui.bottomLine = NOTICE.ld;
    // 补气：只有数值
    arrow(1, 2, "l1", () => { s.limit[1] = Math.min(100, s.limit[1] + 5); }, () => { s.limit[1] = Math.max(0, s.limit[1] - 5); });
    // 首次：任何精灵；复活：道具 ↔ 回复气绝状态的精灵
    arrow(2, 0, "m2", () => this.stepFirst(1), () => this.stepFirst(-1));
    arrow(2, 1, "t2", () => this.stepTarget(2, 1), () => this.stepTarget(2, -1));
    arrow(3, 0, "m3", () => { this.stepMethod(3, 1, isRevive); s.target[3] = AT.MYPET; }, () => { this.stepMethod(3, -1, isRevive); s.target[3] = AT.MYPET; });
    arrow(3, 1, "t3", () => this.stepRevive(1), () => this.stepRevive(-1));
    for (let i = 0; i < 4; i++) {
      const ty = y + 102 + 36 * i;
      const m = s.method[i];
      const name = m === USE_ITEM ? "使用道具" : m === NONE ? "不能使用" : magicAt(inv, m)?.magic ?? "不能使用";
      ui.text(x + 85, ty, name, FONT.YELLOW);
      ui.text(x + 236, ty, name === "不能使用" ? "不能使用" : TARGET_TEXT[s.target[i]] ?? "错误请重设", FONT.YELLOW);
      if (i < 2 && name !== "不能使用") ui.text(x + 336, ty, `${s.limit[i]}％`, FONT.YELLOW);
    }
    if (ui.button(WND.OK, x + 207, y + 152, "ai-ok", WND.OK + 1)) { saveAi(this.name, s); this.close(); return; }
    if (ui.button(WND.CANCEL, x + 207, y + 152, "ai-cancel", WND.CANCEL + 1)) { this.close(); return; }
    if (ui.hovered("ai-ok")) ui.bottomLine = NOTICE.ok;
    if (ui.hovered("ai-cancel")) ui.bottomLine = NOTICE.cancel;
    if (ui.hitBox(x + 317, y + 168, x + 386, y + 193, "ai-other") && ui.clicked("ai-other")) this.other = !this.other;
    ui.text(x + 322, y + 173, "其它功能", FONT.WHITE);
    if (this.other) this.otherFrame(rects);
  }

  /**
   * AI_OtherProc：自动丢肉 / 自动抓宠（原版 3×2 格的小窗口，左上角 (413, 450)）；
   * 这里多一行"智能战斗"（额外新功能），窗口加高一格、往上移 48。
   */
  private otherFrame(rects: [number, number, number, number][]) {
    const ui = this.ui, x = 413, y = 402, s = this.s;
    ui.window(x, y, 3, 3, "ai-other-wnd");
    rects.push([x, y, x + 192, y + 144]);
    ["自动丢肉", "自动抓宠", "智能战斗"].forEach((label, i) => {
      const ry = y + 20 * (i + 1);
      ui.text(x + 25, ry, label, FONT.WHITE);
      const id = `ai-o${i}`;
      if (ui.text(x + 100, ry, s.other[i] ? " ＹＥＳ " : " Ｎ  Ｏ ", FONT.WHITE, id)) s.other[i] = s.other[i] ? 0 : 1;
      if (i === 2 && ui.hovered(id)) ui.bottomLine = "智能战斗：按预计的伤害决定补血、复活、集火与宠物的技能，并记住每种敌人与技能的实际伤害。";
    });
    if (ui.image(WND.CLOSE, x + 100, y + 128, "ai-o-close") && ui.clicked("ai-o-close")) { saveAi(this.name, s); this.other = false; }
  }

  /** AI_SetUp/DownCureMethod、ResurrectMethod：在"使用道具"与合条件的精灵之间切换 */
  private stepMethod(row: number, dir: number, ok: (m?: InvItem) => boolean) {
    const s = this.s, inv = this.inv();
    let m = s.method[row];
    if (m < 0) m = USE_ITEM;
    for (let k = 0; k < MAX_MAGIC + 1; k++) {
      m = (m + dir + MAX_MAGIC + 1) % (MAX_MAGIC + 1);
      if (m === USE_ITEM) break;
      const mg = magicAt(inv, m);
      if (mg && ok(mg)) break;
    }
    s.method[row] = m;
    if (m === USE_ITEM) { if (row === 0) s.target[0] = AT.MYSELF; }
    else if (row === 0) s.target[0] = targetForMagic(magicAt(inv, m)!.magicTarget);
  }

  /** 首次：任何装备着的精灵，两头之外是"不能使用" */
  private stepFirst(dir: number) {
    const s = this.s, inv = this.inv();
    let m = s.method[2];
    for (let k = 0; k < MAX_MAGIC + 1; k++) {
      m += dir;
      if (m >= MAX_MAGIC) m = NONE; else if (m < NONE) m = MAX_MAGIC - 1;
      if (m === NONE || magicAt(inv, m)) break;
    }
    s.method[2] = m;
    if (m >= 0) s.target[2] = targetForMagic(magicAt(inv, m)!.magicTarget);
  }

  /** AI_SetUp/DownTarget：补血与首次的顺序 */
  private stepTarget(row: number, dir: number) {
    const s = this.s, m = s.method[row];
    if (m === NONE) return;
    const cycle = (list: number[]) => {
      const k = list.indexOf(s.target[row]);
      s.target[row] = list[((k < 0 ? 0 : k) + (dir > 0 ? 1 : list.length - 1)) % list.length];
    };
    const P_UP = [11, 10, 9, 8, 7], P_DOWN = [7, 8, 9, 10, 11];
    if (m === USE_ITEM) { cycle(dir > 0 ? [1, 2, ...P_UP] : [1, ...P_DOWN, 2]); return; }
    const mg = magicAt(this.inv(), m);
    if (!mg) return;
    switch (mg.magicTarget) {
      case MT.WHOLEOTHERSIDE: if (row === 2) s.target[2] = s.target[2] === AT.MYSIDE ? AT.OTHERSIDE : AT.MYSIDE; break;
      case MT.OTHER:
        if (row === 0) cycle(dir > 0 ? [1, 2, 3, ...P_UP] : [1, 3, ...P_DOWN, 2]);
        else cycle(dir > 0 ? [1, 2, 4, ...P_UP] : [1, 3, ...P_DOWN, 4, 2]);
        break;
      default: s.target[row] = targetForMagic(mg.magicTarget);
    }
  }

  /** 复活的顺序：宠物 → 玩家五..玩家一 → 我方全体 */
  private stepRevive(dir: number) {
    const s = this.s, list = dir > 0 ? [2, 11, 10, 9, 8, 7, 3] : [2, 3, 7, 8, 9, 10, 11];
    const k = list.indexOf(s.target[3]);
    s.target[3] = list[((k < 0 ? 0 : k) + 1) % list.length];
  }
}
