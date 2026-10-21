// 战斗画面，按原客户端的语义实现：
//   战斗背景    battlemap.cpp ReadBattleMap：battleNN.sab（"SAB " + 20×20 地块图号，BE u16），斜向铺开
//   站位        oft.cpp oft_test：己方在右下、敌方在左上，每排 5 人，monster_place_no 决定中间优先
//   指令面板    battlemenu.cpp BattleMenuProc：CG_BTL_BUTTON_*，从右侧滑入；选攻击后点目标（BattleTargetSelect）
//   倒数        BattleCntDownDisp：CG_CNT_DOWN_0..9，30 秒
//   血条        HpMeterDisp：己方头顶的 CG_BATTLE_BAR_*，绿线 HP、黄线 MP
//   伤害数字    damage_num：红色数字上浮；Miss / Guard / Escape 等用 CG_ICON_*
//   命中特效    set_hit_mark：CG_HIT_MARK_22（被防御时 _12）三帧递减；会心一击放出 5 个 CG_HIT_MARK_01 火花
//   异常状态    头顶 64 像素处的状态动画（SPR_poison / shock / sleep / stone / drunk / conf）
//   偷袭        SurprisalDisp：" 遭敌偷袭 " / "出其不意的攻击"
//   自动战斗    系统菜单的"自动战斗"（原版 AI_SELECT）：照"战斗设定"补血、复活、补气、攻击或逃跑（ai.ts）
// 每回合服务端给出按速度排好的行动列表（BattleTurn），这里按顺序播放：
// 攻击者走到目标面前 → 攻击动画 → 在动画的命中帧（事件 10000..10099）让目标受伤 / 闪避 → 走回原位；连击时中途不走回去。

import { ANIM, findAction, type AnimAction, type Assets } from "./assets.ts";
import { sfx, setSfx } from "./audio.ts";
import type { BattleEvent, BattleFighter, InvItem, PetInfo, S2CBattleEnd, S2CBattleJoin, S2CBattleStart, S2CBattleTurn, S2CInventory, S2CPets } from "./proto.gen.ts";
import { battleItems } from "./items.ts";
import { album } from "./album.ts";
import { aiChoose, type AiFighter, type AiSettings } from "./ai.ts";
import { smartChoose, type AiMemory, type SmartFighter } from "./aismart.ts";
import { FONT, SYS, boxColor, measure, wrapBytes, type Ui } from "./ui.ts";

const CG = {
  BTN_ATTACK: 25100, BTN_JUJUTU: 25102, BTN_CAPTURE: 25104, BTN_HELP: 25106, BTN_GUARD: 25108,
  BTN_ITEM: 25110, BTN_PET: 25112, BTN_ESCAPE: 25114, BTN_BASE: 25116, BTN_CROSS: 25117,
  PET_BTN_BASE: 25140, PET_BTN_WAZA: 25141, PET_BTN_CANCEL: 25143,
  PET_WAZA_WND: 26130, PET_WAZA_BAR_1: 26131, CLOSE_BTN: 26042, // directdraw.cpp SetAnimTbl 的 8 位图分支
  CNT_DOWN_0: 25900,
  BAR_PLAYER: 26019, BAR_PET: 26014,
  ICON_MISS: 26504, ICON_FAIL: 26505, ICON_GET: 26506, ICON_COUNTER: 26507, ICON_NO: 26509, ICON_GUARD: 26510,
  ICON_ESCAPE: 26511, ICON_CAPTURE: 26512, ICON_LEAVE: 26513, ICON_GUARD_BREAK: 26514, ICON_COME_ON: 26501, ICON_GO: 26502,
  HIT_MARK_00: 25500, HIT_MARK_01: 25501, HIT_MARK_10: 25502, HIT_MARK_12: 25504, HIT_MARK_20: 25505, HIT_MARK_22: 25507,
};
// 异常状态动画（oft.cpp set_single_jujutsu），下标为状态编号 1 毒 2 麻 3 眠 4 石 5 醉 6 乱 7 虚 8 剧 9 障 10 默
const STATUS_SPR = [0, 100555, 100551, 100553, 100550, 100552, 100554, 101420, 101417, 101421, 101419];
const ST_NOCAST = 10;
// 咒术（battlemenu.cpp BattleButtonJujutsu）：窗口 MakeWindowDisp(520, 140, 271, 281)（_NEW_WIN_POS_，不平移），底图 CG_JUJUTU_WND
const JUJUTU = { WND: 26068, X: 520, Y: 140, W: 271, H: 281, EFFECT_USER: 100600 };
// MAGIC_TARGET_*（magic.h）与 MAGIC_FIELD_MAP
const M_MYSELF = 0, M_OTHER = 1, M_ALLMYSIDE = 2, M_ALLOTHERSIDE = 3, M_ALL = 4, M_WHOLEOTHERSIDE = 8, M_SINGLE = 9, M_ONE_ROW = 10, M_ALL_ROWS = 11;
const MAGIC_FIELD_MAP = 2;
const ITEM_EFFECT_USER = 100612; // SPR_item3
const ELEM = ["", "地", "水", "火", "风"];
// 换宠物（BattleButtonPet）：MakeWindowDisp(520, 160, 271, 281)（不平移），底图 CG_BTL_PET_CHANGE_WND，收回钮 CG_BTL_PET_RETURN_BTN
const PETCHG = { WND: 26040, RETURN_BTN: 26041, X: 520, Y: 160, W: 271, H: 281 };
const ATR_ICONS: [number, number][] = [[25120, 25122], [25123, 25125], [25126, 25128], [25129, 25131]]; // CG_ATR_ICON_*_BIG / _SML

// oft.cpp 的站位：四排的起点与间距（800×600 模式，己方加了 DISPLACEMENT）
const ROWS: [number, number, number, number][] = [
  [613, 552, 32, -24],  // 0..4  己方人物
  [453, 528, 64, -48],  // 5..9  己方宠物
  [37, 198, 32, -24],   // 10..14 敌方后排
  [69, 298, 64, -48],   // 15..19 敌方前排
];
const PLACE = [2, 1, 3, 0, 4, 7, 6, 8, 5, 9, 12, 11, 13, 10, 14, 17, 16, 18, 15, 19]; // monster_place_no
function slotPos(slot: number): [number, number] {
  const place = PLACE[slot], row = Math.floor(place / 5), i = place % 5;
  const [x, y, dx, dy] = ROWS[row];
  return [x + dx * i, y + dy * i];
}

const HIT_EVENT = (e: number) => e >= 10000 && e < 10100;

/**
 * play_damage（oft.cpp 1097）：挨打的声音按攻击动画的命中帧记号（武器种类）：10000 空手 250、10001 爪 254、10002 斧 / 10003 棍 251、
 * 10004 枪 252、10005 弓 / 10006 投掷斧 没有、10007 咒术 254，其余 250；10100 以上（连击）先减 100。
 */
function damageSe(marker: number): number {
  const m = marker >= 10100 ? marker - 100 : marker;
  return ({ 10000: 250, 10001: 254, 10002: 251, 10003: 251, 10004: 252, 10005: 0, 10006: 0, 10007: 254 } as Record<number, number>)[m] ?? 250;
}
const FRAME = 1000 / 60;
const ANGRY = 8; // loadsprbin.h ANIM_ANGRY

// PETSKILL_TARGET_*（pet_skill.h）与 PETSKILL_FIELD_MAP
const T_MYSELF = 0, T_OTHER = 1, T_ALLMYSIDE = 2, T_ALLOTHERSIDE = 3, T_ALL = 4, T_NONE = 5, T_WITHOUTME = 6, T_WITHOUTMEANDPET = 7;
const FIELD_MAP = 2;

/** 屏幕上的方向 → 角色方向 0..7（等距投影下，屏幕向量换回格子向量再取方向） */
function dirToward(sx: number, sy: number): number {
  const gx = sx / 64 - sy / 48, gy = sx / 64 + sy / 48; // 反解 sx=(gx+gy)*32, sy=(gy-gx)*24
  const deg = (Math.atan2(gy, gx) * 180) / Math.PI - 135 + 22.5;
  return Math.floor((((deg % 360) + 360) % 360) / 45) & 7;
}

/** 战斗中的一个角色。动作播一次就停在最后一帧（死亡）或回到站立。 */
class Fighter {
  f: BattleFighter;
  x: number; y: number;      // 当前绘制位置
  homeX: number; homeY: number;
  dir: number;
  hp: number; mp: number;
  rideHp: number;           // 骑着的宠物的 HP（没骑为 0；落马后为 0）
  status = 0;
  dead = false; gone = false; hidden = false;
  private action = ANIM.STAND; private loop = true; private actAt = 0;
  private onHit: (() => void) | null = null; private hitDone = false;
  private lastFrame = -1;
  hitMarker = 10000; // 这次攻击动画的命中帧记号（ATR_HIT，挨打的声音按它选）
  rect: [number, number, number, number] | null = null; // 上一帧画出的范围（点选目标用）

  constructor(f: BattleFighter) {
    this.f = f;
    [this.homeX, this.homeY] = slotPos(f.slot);
    this.x = this.homeX; this.y = this.homeY;
    this.dir = this.homeDir();
    this.hp = f.hp; this.mp = f.mp; this.rideHp = f.rideMaxHp > 0 ? f.rideHp : 0;
  }

  homeDir() { return this.f.slot < 10 ? 3 : 7; } // 己方朝左上，敌方朝右下

  play(action: number, now: number, loop = false, onHit: (() => void) | null = null) {
    this.action = action; this.loop = loop; this.actAt = now; this.onHit = onHit; this.hitDone = false; this.lastFrame = -1;
  }

  frame(assets: Assets, now: number): [number, number, number] | null {
    const def = assets.anim(this.f.sprite);
    if (!def) return null;
    let act: AnimAction | undefined = findAction(def, this.dir, this.action);
    if (!act || act.frames.length === 0) return null;
    const ms = Math.max(1, act.ticks) * FRAME;
    let i = Math.max(0, Math.floor((now - this.actAt) / ms)); // 动作可能在本帧稍后才开始（actAt 略大于 now）
    if (i >= act.frames.length) {
      if (this.onHit && !this.hitDone) { this.hitDone = true; this.onHit(); } // 没有命中帧的动作在结束时触发
      if (this.loop) i %= act.frames.length;
      else if (this.dead || this.action === ANIM.DEAD) i = act.frames.length - 1;
      else { this.play(ANIM.STAND, now, true); act = findAction(def, this.dir, ANIM.STAND)!; i = 0; }
    } else if (this.onHit && !this.hitDone) {
      for (let k = 0; k <= i; k++) if (HIT_EVENT(act.frames[k][3])) { this.hitMarker = act.frames[k][3]; this.hitDone = true; this.onHit(); break; }
    }
    // 帧事件 1..9999 是音效（pattern.cpp），这一帧刚变成当前帧时放
    const idx = Math.min(i, act.frames.length - 1);
    if (idx !== this.lastFrame) {
      this.lastFrame = idx;
      const ev = act.frames[idx][3];
      if (ev > 0 && ev < 10000) sfx(ev, this.x);
    }
    const [img, fx, fy] = act.frames[idx];
    return img < 0 ? null : [img, fx, fy];
  }
}

/** 一个会随时间变化的小特效：浮起的数字 / 图示、命中火花 */
interface Fx { at: number; ms: number; draw(t: number, age: number): void; }

type Step = (now: number) => boolean; // 返回 true 表示这一步播完

export interface BattleHost {
  ui: Ui;
  assets: Assets;
  root: string; // 资源根目录（读 battlemap）
  sendHelp(on: boolean): void; // lssproto_HL_send
  captured(): void;            // 自己抓到宠物（放抓宠的音乐）
  send(kind: string, target: number, item?: number): void;
  sendPet(skill: number, target: number): void; // 宠物的指令（lssproto_B "W|技能|目标"，-1 为什么也不做）
  pets(): S2CPets | null;
  inventory(): S2CInventory;
  chat(text: string, color: string): void;
  auto(): boolean; // 是否开着自动战斗
  aiSettings(): AiSettings; // 战斗设定（ai.ts）
  aiMemory(): AiMemory;     // 智能战斗学到的伤害（aismart.ts）
  myAtk(): number;          // 自己的攻击力（状态窗口的数字）
  place(): [number, number, number]; // 楼层、x、y（自动抓宠在楼层 500 的部分区域不动作）
  finish(end: S2CBattleEnd | null): void; // 战斗画面结束，回到地图（结果窗口画在地图上）
  quick(): boolean;         // 快速战斗：不画战斗画面（quickFrame），开战时决定
}

export class BattleScreen {
  private host: BattleHost;
  private fighters = new Map<number, Fighter>();
  private me: number;
  private turnMs: number;
  private surprise: number;
  private tiles: Uint16Array | null = null;
  private startAt = performance.now();
  private queue: Step[] = [];
  private fx: Fx[] = [];
  private banner = { text: "", until: 0 }; // 技能名（原版 p_kanji 在画面上方显示技能名）
  private menuAt = 0;            // 指令面板出现的时刻；0 表示不显示
  private deadline = 0;          // 本回合倒数结束的时刻
  private selecting = false;     // 选目标中
  private itemMenu = false;      // 道具选单（BattleButtonItem）
  // 上回合按的按钮（battleButtonBak2：攻击 / 捕捉下回合直接选目标、咒术直接开窗口）与宠物选好的技能格（battleWazaTargetBak）。
  // 原版只记在这一场里；这里存进 localStorage（sa.battle.<角色名>），下一场战斗也沿用
  private mem = { lastButton: "attack", petSkill: -1 };
  private memKey = "";
  private get lastButton() { return this.mem.lastButton; }
  private set lastButton(v: string) { this.mem.lastButton = v; this.saveMem(); }
  private get petSkill() { return this.mem.petSkill; }
  private set petSkill(v: number) { this.mem.petSkill = v; this.saveMem(); }
  private saveMem() {
    try { if (this.memKey) localStorage.setItem(this.memKey, JSON.stringify(this.mem)); } catch { /* 无痕模式等存不了就算了 */ }
  }
  private capturing = false;     // 选目标是为了捕捉（BATTLE_CAPTURE）
  private magicMenu = false;     // 咒术窗口（BattleButtonJujutsu）
  private itemPick: InvItem | null = null; // 选好的道具，选目标中
  private petChange = false;     // 换宠物的窗口（BattleButtonPet）
  private magicItem: InvItem | null = null; // 选好的咒术（BattleJujutuNo），选目标中
  private fieldAttr = 0;         // 战场属性（BV）：0 无 1 地 2 水 3 火 4 风
  private sent = false;
  // 宠物的指令面板（BattleMenuProc 的 BATTLE_MENU_PET）：人下完指令后滑入
  private petMenuAt = 0;
  private petSent = true;
  private petWnd = false;        // 技能窗口（BattleButtonWaza）
  private petSelecting = false;
  private turns = 0;
  // 自动战斗：这场的第一回合（AI_First）、决定的事（AI_Choosen，播放时显示）、宠物的指令
  private aiFirst = true;
  private aiLabel = "";
  private aiPet: [number, number] = [-1, -1];
  private end: S2CBattleEnd | null = null;
  private helpFlag = false;
  pvp = false; // DuelFlag：决斗
  private event = false; // eventEnemyFlag：NPC 开的战斗（智能战斗不考虑逃跑）
  private aiKey = ""; private aiPetKey = ""; // 智能战斗这回合选的行动（学习用）
  private endIdleAt = 0;
  // 快速战斗（额外新功能）：不切到战斗画面，排队的动画用快进的时钟一口气播完（只为了照样更新 HP、倒下、捕获等状态），
  // 马上照战斗设定下指令；服务端收齐指令就结算，所以整场几乎是一瞬间打完
  readonly quick: boolean;
  private clock = 0;

  /** lssproto_HL_recv：服务端的求救旗标 */
  setHelp(on: boolean) { this.helpFlag = on; }

  /** 有人中途加入（BATTLE_RescueEntry 的 BP|…|JOIN）：从画面外滑进来 */
  join(m: S2CBattleJoin) {
    const added: Fighter[] = [];
    for (const f of m.fighters) {
      const a = new Fighter(f);
      const off = f.slot < 10 ? 300 : -300;
      a.x += off; a.y += off;
      this.fighters.set(f.slot, a);
      added.push(a);
    }
    let at = -1;
    this.queue.push((now) => {
      if (at < 0) at = now;
      const t = Math.min(1, (now - at) / 600);
      for (const a of added) {
        const off = (a.f.slot < 10 ? 300 : -300) * (1 - t);
        a.x = a.homeX + off; a.y = a.homeY + off;
      }
      return t >= 1;
    });
  }

  constructor(host: BattleHost, m: S2CBattleStart) {
    this.host = host;
    this.quick = host.quick();
    this.me = m.mySlot;
    this.pvp = m.pvp;
    this.event = m.event;
    this.turnMs = m.turnTime;
    this.surprise = m.surprise;
    for (const f of m.fighters) {
      album.seen(f.sprite); // CheckBattleNewPet
      const a = new Fighter(f);
      // 入场：从画面外滑进来（oft.cpp：起点在定位点 ±300）
      const off = f.slot < 10 ? 300 : -300;
      a.x += off; a.y += off;
      this.fighters.set(f.slot, a);
    }
    const no = String(Math.max(0, m.field)).padStart(2, "0");
    if (this.quick) this.tiles = new Uint16Array(0); // 不画背景
    else fetch(`${host.root}battlemap/battle${no}.sab`).then((r) => r.ok ? r.arrayBuffer() : Promise.reject(r.status)).then((b) => {
      const v = new DataView(b), t = new Uint16Array(400);
      for (let i = 0; i < 400 && 4 + i * 2 + 1 < b.byteLength; i++) t[i] = v.getUint16(4 + i * 2);
      this.tiles = t;
    }).catch(() => { this.tiles = new Uint16Array(0); });
    if (!this.quick) this.queue.push(this.enterStep());
    const self = m.fighters.find((f) => f.slot === m.mySlot);
    this.memKey = self ? `sa.battle.${self.name}` : "";
    try {
      const saved = this.memKey ? JSON.parse(localStorage.getItem(this.memKey) ?? "null") : null;
      if (saved && typeof saved.lastButton === "string" && typeof saved.petSkill === "number") this.mem = saved;
    } catch { /* 坏掉的资料不用 */ }
  }

  onTurn(m: S2CBattleTurn) {
    this.menuAt = 0; this.selecting = false; this.turns++;
    const ev = m.events;
    // 智能战斗：看这回合的结果学习（敌人一次打我方多少、自己与宠物这回合的行动打掉多少）
    {
      const mem = this.host.aiMemory(), mySide = (s: number) => (s < 10) === (this.me < 10);
      mem.observe(ev, (s) => this.fighters.get(s)?.f.name ?? "", mySide, this.me, this.aiKey, this.aiPetKey);
      mem.save();
      this.aiKey = ""; this.aiPetKey = "";
    }
    for (let i = 0; i < ev.length; i++) {
      if (ev[i].actor < 0) {
        // 战场属性的回合数到了，回到无属性
        if (ev[i].kind === "field") this.queue.push(this.waitStep(1, () => { this.fieldAttr = 0; }));
        continue;
      }
      // 同一个人的连击合成一组：只在第一下前走过去、最后一下后走回来
      let j = i;
      while (j < ev.length - 1 && ev[j].combo && ev[j + 1].actor === ev[i].actor) j++;
      this.queue.push(...this.eventSteps(ev.slice(i, j + 1)));
      i = j;
    }
  }

  onEnd(m: S2CBattleEnd) { this.end = m; }

  // ---------------- 播放 ----------------

  private enterStep(): Step {
    let started = false;
    return (now) => {
      if (!started) { started = true; this.startAt = now; }
      const t = Math.min(1, (now - this.startAt) / 600);
      for (const a of this.fighters.values()) {
        const off = (a.f.slot < 10 ? 300 : -300) * (1 - t);
        a.x = a.homeX + off; a.y = a.homeY + off;
      }
      return t >= 1;
    };
  }

  private moveStep(a: Fighter, tx: () => [number, number], ms: number, face = true): Step {
    let from: [number, number] | null = null, to: [number, number] = [0, 0], at = 0;
    return (now) => {
      if (!from) {
        from = [a.x, a.y]; to = tx(); at = now; a.play(ANIM.WALK, now, true);
        if (face && Math.hypot(to[0] - a.x, to[1] - a.y) > 1) a.dir = dirToward(to[0] - a.x, to[1] - a.y);
      }
      const t = Math.min(1, (now - at) / ms);
      a.x = from[0] + (to[0] - from[0]) * t; a.y = from[1] + (to[1] - from[1]) * t;
      if (t >= 1) { a.play(ANIM.STAND, now, true); return true; }
      return false;
    };
  }

  private waitStep(ms: number, fn?: (now: number) => void): Step {
    let at = -1;
    return (now) => {
      if (at < 0) { at = now; fn?.(now); }
      return now - at >= ms;
    };
  }

  private showSkill(name: string, now: number) {
    if (name) this.banner = { text: name, until: now + 1200 };
  }

  /** damage_num：先快后慢地上浮约 24 像素后停住，约 1 秒后消失 */
  private popup(a: Fighter, now: number, p: { text?: string; icon?: number; color?: string; dy?: number }) {
    const x = a.x, y = a.y - 80 + (p.dy ?? 0), ui = this.host.ui;
    this.fx.push({ at: now, ms: 1600, draw: (_t, age) => {
      const k = Math.min(1, age / 400), yy = y - 24 * (1 - (1 - k) * (1 - k));
      if (p.icon) ui.image(p.icon, x, yy);
      if (p.text) ui.texts.push({ x, y: yy - 8, text: p.text, color: p.color ?? FONT.RED, align: "center" });
    } });
  }

  /** set_hit_mark：普通命中一个三帧递减的火花（8 帧）；会心一击放出 5 个向外飞的火花 */
  private hitMark(t: Fighter, now: number, guarded: boolean, critical: boolean) {
    const ui = this.host.ui, x = t.x, y = t.y - 32;
    const base = guarded ? CG.HIT_MARK_12 : CG.HIT_MARK_22;
    const life = 8 * FRAME * (critical ? 2 : 1);
    this.fx.push({ at: now, ms: life, draw: (k) => ui.image(base - Math.min(2, Math.floor(k * 3)), x, y) });
    if (!critical) return;
    for (let d = 5; d > 0; d--) {
      const ang = (d * 6) / 32 * Math.PI * 2, spd = 4 + Math.random() * 3;
      this.fx.push({ at: now, ms: 16 * FRAME, draw: (k, age) => {
        const dist = spd * age / FRAME;
        ui.image(k < 0.5 ? CG.HIT_MARK_01 : CG.HIT_MARK_00, x + Math.cos(ang) * dist, y + Math.sin(ang) * dist * 0.75);
      } });
    }
  }

  private eventSteps(group: BattleEvent[]): Step[] {
    const e = group[0];
    const a = this.fighters.get(e.actor);
    if (!a || a.gone) return [];
    const steps: Step[] = [];
    switch (e.kind) {
      case "attack":
      case "counter":
        return this.attackSteps(a, group);
      case "guard":
        steps.push(this.waitStep(400, (now) => { this.showSkill(e.skill, now); a.play(ANIM.GUARD, now); this.popup(a, now, { icon: CG.ICON_GUARD }); }));
        break;
      case "escape":
        if (e.ok) {
          // BATTLE_Exit：玩家逃走时出场的宠物一起离开
          const pet = a.f.slot % 10 < 5 ? this.fighters.get(a.f.slot + 5) : undefined;
          const runners = [a, ...(pet && !pet.dead && !pet.gone ? [pet] : [])];
          steps.push(this.waitStep(1, (now) => { for (const r of runners) r.dir = (r.dir + 4) & 7; this.popup(a, now, { icon: CG.ICON_ESCAPE }); }));
          const off = a.f.slot < 10 ? 400 : -400;
          const moves = runners.map((r) => this.moveStep(r, () => [r.homeX + off, r.homeY + off], 900, false));
          steps.push((now) => moves.map((m) => m(now)).every(Boolean));
          steps.push(this.waitStep(1, () => { for (const r of runners) r.gone = true; }));
        } else {
          steps.push(this.waitStep(1, () => { a.dir = (a.dir + 4) & 7; }));
          const off = a.f.slot < 10 ? 40 : -40;
          steps.push(this.moveStep(a, () => [a.homeX + off, a.homeY + off], 300, false));
          steps.push(this.waitStep(1, (now) => { this.popup(a, now, { icon: CG.ICON_FAIL }); a.dir = (a.dir + 4) & 7; }));
          steps.push(this.moveStep(a, () => [a.homeX, a.homeY], 300, false));
          steps.push(this.waitStep(1, () => { a.dir = a.homeDir(); }));
        }
        break;
      case "capture":
        return this.captureSteps(a, e);
      case "magic":
        return this.magicSteps(a, group);
      case "disobey":
        // 宠物不听话（BX）："No!" 并摇头
        steps.push(this.waitStep(700, (now) => {
          this.popup(a, now, { icon: CG.ICON_NO });
          const d = a.dir;
          [150, 300, 450].forEach((ms, i) => setTimeout(() => { a.dir = i === 2 ? d : (d + (i === 0 ? 1 : 7)) & 7; }, ms));
        }));
        break;
      case "leave": {
        // 宠物逃走（BATTLE_LostEscape）："Leave!" 后跑出画面
        steps.push(this.waitStep(400, (now) => { this.popup(a, now, { icon: CG.ICON_LEAVE }); a.dir = (a.dir + 4) & 7; }));
        const off = a.f.slot < 10 ? 400 : -400;
        steps.push(this.moveStep(a, () => [a.homeX + off, a.homeY + off], 900, false));
        steps.push(this.waitStep(1, () => { a.gone = true; }));
        break;
      }
      case "flee": {
        // 被赶出战场 / 自己离开（怯战、旅程伙伴、偷窃：BATTLE_Exit）：转身跑出画面
        steps.push(this.waitStep(1, () => { a.dir = (a.homeDir() + 4) & 7; }));
        const off = a.f.slot < 10 ? 400 : -400;
        steps.push(this.moveStep(a, () => [a.homeX + off, a.homeY + off], 700, false));
        steps.push(this.waitStep(1, () => { a.gone = true; }));
        break;
      }
      case "abduct":
      case "steal": {
        // 旅程伙伴（B!）/ 偷窃（B#）：走到对方面前，成功时冒"Get!!"，失败冒"Fail..."
        const t = this.fighters.get(e.target);
        steps.push(this.waitStep(1, (now) => this.showSkill(e.skill, now)));
        if (!t) break;
        const front = (): [number, number] => {
          const dx = a.homeX - t.homeX, dy = a.homeY - t.homeY, len = Math.hypot(dx, dy) || 1;
          return [t.homeX + dx / len * 50, t.homeY + dy / len * 50];
        };
        steps.push(this.moveStep(a, front, 350));
        steps.push(this.waitStep(600, (now) => { a.play(ANIM.ATTACK, now); this.popup(t, now, { icon: e.ok ? CG.ICON_GET : CG.ICON_FAIL }); }));
        if (!e.ok || e.kind === "abduct") steps.push(this.moveStep(a, () => [a.homeX, a.homeY], 350, false));
        steps.push(this.waitStep(1, () => { a.dir = a.homeDir(); }));
        break;
      }
      case "vary":
        // 暗月变身：放出效果后换图（变回来时也一样）
        steps.push(this.waitStep(600, (now) => {
          this.showSkill(e.skill, now);
          this.animOnce(101120, a.x, a.y, now);
          a.f.sprite = e.sprite;
        }));
        break;
      case "petin": {
        // BATTLE_PetIn（BS f0）：主人头上冒"Come On!"、SE 216（oft.cpp case 60），宠物转身跑回去、离开战场
        const t = this.fighters.get(e.target);
        if (!t || t.gone) break;
        steps.push(this.waitStep(400, (now) => { this.popup(a, now, { icon: CG.ICON_COME_ON }); sfx(216, a.x); }));
        steps.push(this.waitStep(1, () => { t.dir = (t.homeDir() + 4) & 7; }));
        const off = t.f.slot < 10 ? 400 : -400;
        steps.push(this.moveStep(t, () => [t.homeX + off, t.homeY + off], 700, false));
        steps.push(this.waitStep(1, () => { t.gone = true; this.fighters.delete(e.target); }));
        break;
      }
      case "petout": {
        // BATTLE_PetOut（BS f1）：主人头上冒"Go!"（oft.cpp case 62），新的宠物从画面外跑进来
        const f: BattleFighter = { slot: e.target, name: e.name, sprite: e.sprite, level: e.level, hp: e.hp, maxHp: e.maxHp, mp: 0, maxMp: 0,
          rideName: "", rideLevel: 0, rideHp: 0, rideMaxHp: 0, fallSprite: 0 };
        const n = new Fighter(f);
        const off = f.slot < 10 ? 400 : -400;
        steps.push(this.waitStep(400, (now) => { this.popup(a, now, { icon: CG.ICON_GO }); }));
        steps.push(this.waitStep(1, () => { n.x = n.homeX + off; n.y = n.homeY + off; this.fighters.set(f.slot, n); }));

        steps.push(this.moveStep(n, () => [n.homeX, n.homeY], 700, true));
        steps.push(this.waitStep(1, () => { n.dir = n.homeDir(); }));
        break;
      }
      case "field":
        // BV：战场属性改变；回合数到了（actor -1，不会走到这里）在 onTurn 处理
        steps.push(this.waitStep(600, (now) => { this.showSkill(e.skill, now); a.play(ANIM.ATTACK, now); this.fieldAttr = e.status; }));
        break;
      case "poison":
        // BD|r|0|0|人|p骑宠|：骑宠的那一份另外显示
        steps.push(this.waitStep(500, (now) => {
          a.hp = e.hp; a.status = e.status;
          if (a.f.rideMaxHp > 0) a.rideHp = e.rideHp;
          if (e.damage > 0) this.popup(a, now, { text: String(e.damage), color: FONT.PURPLE });
          if (e.petDamage > 0) this.popup(a, now, { text: String(e.petDamage), color: FONT.PURPLE, dy: 25 });
          a.play(ANIM.DAMAGE, now);
        }));
        break;
      case "cure":
        steps.push(this.waitStep(200, () => { a.status = e.status; }));
        break;
      case "stun":
        steps.push(this.waitStep(400, () => { a.status = e.status; }));
        break;
      case "charge":
      case "noguard":
        steps.push(this.waitStep(500, (now) => { this.showSkill(e.skill || "蓄力", now); a.play(ANIM.GUARD, now); }));
        break;
      case "hide":
        // 地球一周：钻进地底，下回合从对手身后出来攻击（原版 BF / BI）
        steps.push(this.waitStep(1, (now) => this.showSkill(e.skill, now)));
        steps.push(this.moveStep(a, () => [a.x, a.y + 30], 300, false));
        steps.push(this.waitStep(1, () => { a.hidden = true; a.x = a.homeX; a.y = a.homeY; }));
        break;
      case "item":
        // 道具（BJ，使用者身上放出 SPR_item3）：播放同咒术
        return this.magicSteps(a, group, ITEM_EFFECT_USER);
      case "regen":
        // 咒 / 恩 / 祝每回合的增减（BD）：HP 绿字、MP 黄字（减少时红字）
        steps.push(this.waitStep(500, (now) => {
          a.hp = e.hp; if (a.f.slot === this.me) a.mp = e.actorMp;
          if (e.damage !== 0) this.popup(a, now, { text: String(Math.abs(e.damage)), color: e.damage > 0 ? FONT.GREEN : FONT.RED });
          if (e.mpDamage !== 0) this.popup(a, now, { text: String(Math.abs(e.mpDamage)), color: e.mpDamage > 0 ? FONT.YELLOW : FONT.RED, dy: 14 });
        }));
        break;
      case "heal":
      case "revive": {
        const t = this.fighters.get(e.target);
        if (!t) break;
        steps.push(this.waitStep(1, (now) => { this.showSkill(e.skill, now); a.play(ANIM.ATTACK, now); }));
        steps.push(this.waitStep(600, (now) => {
          t.hp = e.hp; t.status = e.status;
          if (e.kind === "revive") { t.dead = false; t.play(ANIM.STAND, now, true); }
          this.popup(t, now, { text: String(e.damage), color: FONT.GREEN });
        }));
        break;
      }
    }
    return steps;
  }

  /**
   * 咒术（BJ）：施法者转回原来的方向站着、身上放出 MAGIC_EFFECT_USER，接着每个对象身上放出效果动画（回复 / 状态 / 攻击魔法的图），
   * 播完时一起结算：回复与复活冒绿字，攻击魔法冒红字，附加状态失败冒"Fail..."，闪开冒"Miss"。没有对象（MP 不够、沉默）时只冒"Fail..."。
   */
  private magicSteps(a: Fighter, group: BattleEvent[], userEffect = JUJUTU.EFFECT_USER): Step[] {
    const first = group[0];
    const steps: Step[] = [];
    steps.push(this.waitStep(700, (now) => {
      this.showSkill(first.skill, now);
      // oft.cpp ATT_JUJUTSU → case 75：施法的人（人或宠物）不做攻击动作，只转回自己那一边的方向（我方 3、敌方 7）站着，
      // 效果动画放在他身上（以前这里播攻击动作，宠物施法时会对着空气扑一下）
      a.dir = a.homeDir();
      a.play(ANIM.STAND, now, true);
      a.mp = first.actorMp;

      this.animOnce(userEffect, a.x, a.y, now);
      if (first.target < 0 && first.result === "fail") this.popup(a, now, { icon: CG.ICON_FAIL });
    }));
    const hits = group.filter((e) => e.target >= 0 && this.fighters.has(e.target));
    if (hits.length === 0) return steps;
    let wait = 600;
    steps.push(this.waitStep(1, (now) => {
      // 攻击魔法照 attmagic.bin 放动画（BuildAttackMagicData / RunTimeMagic）：显示方式 1 是画面上的固定位置（640 座标）；
      // 整排同时 / 全体同时是所有目标的中心 + (x, y) 放一次；其余每个目标的位置 + (x, y)，轮流的按时间差依次放
      const att = first.att ? first.att.split("|").map(Number) : null;
      if (att && first.effect) {
        const [type, slice, show, sx, sy] = att;
        if (show === 1 || type === 2 || type === 4) {
          let x = sx + 80, y = sy + 60;
          if (show !== 1) {
            const ts = hits.map((e) => this.fighters.get(e.target)!);
            x = sx + ts.reduce((s, t) => s + t.homeX, 0) / ts.length;
            y = sy + ts.reduce((s, t) => s + t.homeY, 0) / ts.length;
          }
          wait = Math.max(wait, Math.min(2500, this.animOnce(first.effect, x, y, now)));
        } else {
          hits.forEach((e, i) => {
            const t = this.fighters.get(e.target)!, delay = type === 0 ? 0 : i * slice;
            wait = Math.max(wait, Math.min(2500, delay + this.animOnce(first.effect, t.homeX + sx, t.homeY + sy, now + delay)));
          });
        }
        return;
      }
      for (const e of hits) {
        const t = this.fighters.get(e.target)!;
        if (e.effect) wait = Math.max(wait, Math.min(1500, this.animOnce(e.effect, t.x, t.y, now)));
      }
    }));
    let at = -1;
    steps.push((now) => {
      // 等最长的效果动画播完再结算
      if (at < 0) at = now;
      if (now - at < wait) return false;
      for (const e of hits) this.applyMagic(e, this.fighters.get(e.target)!, a, now);
      return true;
    });
    steps.push(this.waitStep(300));
    return steps;
  }

  private applyMagic(e: BattleEvent, t: Fighter, a: Fighter, now: number) {
    switch (e.result) {
      case "heal":
      case "revive":
        if (e.result === "revive" && e.hp > 0) { t.dead = false; t.play(ANIM.STAND, now, true); }
        if (e.damage > 0 || e.result === "revive") this.popup(t, now, { text: String(e.damage), color: FONT.GREEN });
        if (e.mpDamage > 0) { if (t !== a) t.mp += e.mpDamage; this.popup(t, now, { text: String(e.mpDamage), color: FONT.YELLOW, dy: 14 }); } // 对自己用时 actorMp 已经是回复后的
        if (e.petDamage > 0) this.popup(t, now, { text: String(e.petDamage), color: FONT.GREEN, dy: 25 });
        break;
      case "fail":
        this.popup(t, now, { icon: CG.ICON_FAIL });
        break;
      case "dodge":
        this.popup(t, now, { icon: CG.ICON_MISS });
        break;
      case "damage":
        this.applyHit(e, t, a, now);
        return;
    }
    t.hp = e.hp; t.status = e.status;
    if (t.f.rideMaxHp > 0) t.rideHp = e.rideHp;
  }

  /** 在 (x, y) 放一次动画（不循环）；回传播放时间（动画还没载入时按 600 毫秒） */
  private animOnce(id: number, x: number, y: number, now: number): number {
    const def = this.host.assets.anim(id);
    const act = def && findAction(def, 0, ANIM.STAND);
    const ms = act && act.frames.length > 0 ? act.frames.length * Math.max(1, act.ticks) * FRAME : 600;
    this.fx.push({ at: now, ms, draw: (_k, age) => {
      if (age < 0) return; // 还没轮到（轮流攻击的时间差）
      const d = this.host.assets.anim(id), ac = d && findAction(d, 0, ANIM.STAND);
      if (!ac || ac.frames.length === 0) return;
      const i = Math.min(ac.frames.length - 1, Math.floor(age / (Math.max(1, ac.ticks) * FRAME)));
      const [img, fx, fy] = ac.frames[i];
      if (img >= 0) this.host.ui.image(img, x + fx, y + fy, undefined, true);
    } });
    return ms;
  }

  /**
   * 捕捉（oft.cpp 35..45）：走到敌人面前做攻击动作，命中帧冒"Capture!"，然后拖着敌人往回走；
   * 成功时回到原位冒"Get!!"、敌人消失；失败时敌人挣脱，冒"Fail..."、生气一下再走回去。
   */
  private captureSteps(a: Fighter, e: BattleEvent): Step[] {
    const t = this.fighters.get(e.target);
    if (!t) return [];
    const front = (): [number, number] => {
      const dx = a.homeX - t.homeX, dy = a.homeY - t.homeY, len = Math.hypot(dx, dy) || 1;
      return [t.homeX + dx / len * 50, t.homeY + dy / len * 50];
    };
    const steps: Step[] = [this.moveStep(a, front, 350)];
    let hit = false, at = 0;
    steps.push((now) => {
      if (at === 0) {
        at = now;
        a.dir = dirToward(t.x - a.x, t.y - a.y);
        a.play(ANIM.ATTACK, now, false, () => { hit = true; this.popup(a, performance.now(), { icon: CG.ICON_CAPTURE }); });
      }
      if (!hit && now - at > 2500) hit = true;
      return hit && now - at > 500;
    });
    // 拖着敌人往回走：敌人跟在身后
    const pull = e.ok ? 1 : 0.4;
    const ax = a.x, ay = a.y;
    let from = 0;
    steps.push((now) => {
      if (from === 0) { from = now; a.play(ANIM.WALK, now, true); t.play(ANIM.WALK, now, true); a.dir = (a.dir + 4) & 7; }
      const k = Math.min(1, (now - from) / 900) * pull;
      a.x = ax + (a.homeX - ax) * k; a.y = ay + (a.homeY - ay) * k;
      t.x = t.homeX + (a.x - ax); t.y = t.homeY + (a.y - ay);
      return now - from >= 900 * pull;
    });
    if (e.ok) {
      steps.push(this.waitStep(900, (now) => {
        a.play(ANIM.STAND, now, true); a.dir = a.homeDir();
        t.gone = true;
        this.popup(a, now, { icon: CG.ICON_GET });
        if (a.f.slot === this.me && !this.quick) this.host.captured(); // 抓到宠物的音乐（oft.cpp 8675：play_bgm(0)）
      }));
    } else {
      steps.push(this.waitStep(700, (now) => { this.popup(a, now, { icon: CG.ICON_FAIL }); a.play(ANGRY, now, true); }));
      steps.push((now) => { t.play(ANIM.STAND, now, true); return true; });
      steps.push(this.moveStep(t, () => [t.homeX, t.homeY], 300));
      steps.push(this.moveStep(a, () => [a.homeX, a.homeY], 300, false));
      steps.push(this.waitStep(80, () => { a.dir = a.homeDir(); t.dir = t.homeDir(); }));
    }
    return steps;
  }

  /** 攻击（含连击、反击、守护）：走到目标面前 → 每一下攻击动画在命中帧结算 → 走回原位 */
  private attackSteps(a: Fighter, group: BattleEvent[]): Step[] {
    const steps: Step[] = [];
    const first = group[0];
    const t0 = this.fighters.get(first.target);
    if (!t0) return [];
    const front = (t: Fighter): [number, number] => {
      const dx = a.homeX - t.homeX, dy = a.homeY - t.homeY, len = Math.hypot(dx, dy) || 1;
      return [t.homeX + dx / len * 50, t.homeY + dy / len * 50];
    };
    if (a.hidden) {
      // 从地底钻出来，出现在目标身后
      steps.push(this.waitStep(1, () => {
        a.hidden = false;
        const [fx, fy] = front(t0);
        a.x = t0.homeX * 2 - fx; a.y = t0.homeY * 2 - fy;
      }));
    } else {
      steps.push(this.waitStep(1, (now) => {
        this.showSkill(first.skill, now);
        if (first.kind === "counter") this.popup(a, now, { icon: CG.ICON_COUNTER });
      }));
      steps.push(this.moveStep(a, () => front(t0), first.kind === "counter" ? 200 : 350));
    }
    for (const e of group) {
      const t = this.fighters.get(e.target);
      if (!t) continue;
      const g = e.guardian >= 0 ? this.fighters.get(e.guardian) : undefined;
      if (g) {
        // 守护者冲到被守护者身前替他挨打
        steps.push(this.moveStep(g, () => {
          const [fx, fy] = front(t);
          return [(fx + t.homeX) / 2, (fy + t.homeY) / 2];
        }, 200));
      }
      let hit = false, done = 0, at = 0;
      const strike = (now: number) => { if (!hit) { hit = true; done = now; this.applyHit(e, g ?? t, a, now); } };
      steps.push((now) => {
        if (!hit && done === 0) {
          done = -1; at = now;
          a.dir = dirToward(t.x - a.x, t.y - a.y);
          a.play(ANIM.ATTACK, now, false, () => strike(performance.now())); // 命中帧的时刻（不是这一步开始的时刻），否则挨打的动作一开始就已经播完
        }
        if (!hit && now - at > 2500) strike(now); // 动画数据没到时也不能卡住
        return hit && now - done > (e.combo ? 250 : 450);
      });
      if (g) steps.push(this.moveStep(g, () => [g.homeX, g.homeY], 200));
    }
    steps.push(this.moveStep(a, () => [a.homeX, a.homeY], first.kind === "counter" ? 200 : 350));
    steps.push(this.waitStep(80, () => { a.dir = a.homeDir(); }));
    return steps;
  }

  private applyHit(e: BattleEvent, t: Fighter, a: Fighter, now: number) {
    switch (e.result) {
      case "dodge": {
        // 闪避：往旁边一闪再回来（SE 8）
        sfx(8, t.x);
        const hx = t.x, hy = t.y;
        this.popup(t, now, { icon: CG.ICON_MISS });
        t.x = hx - 24; t.y = hy - 18;
        setTimeout(() => { t.x = hx; t.y = hy; }, 250);
        return;
      }
      case "miss":
        sfx(8, t.x);
        this.popup(t, now, { icon: CG.ICON_MISS });
        break;
      case "guard":
        sfx(damageSe(a.hitMarker), t.x);
        this.popup(t, now, { icon: CG.ICON_GUARD });
        this.hitMark(t, now, true, false);
        t.play(ANIM.GUARD, now);
        break;
      default:
        sfx(damageSe(a.hitMarker), t.x);
        this.hitMark(t, now, e.guarded, e.result === "critical");
        this.popup(t, now, { text: String(e.damage), color: e.result === "critical" ? FONT.YELLOW : FONT.RED });
        if (e.mpDamage > 0) this.popup(t, now, { text: String(e.mpDamage), color: FONT.YELLOW, dy: 14 });
        t.play(e.guarded ? ANIM.GUARD : ANIM.DAMAGE, now);
    }
    t.hp = e.hp; t.status = e.status;
    if (t.f.rideMaxHp > 0 && t.rideHp > 0) {
      // BATTLE_DamageSub：骑宠分到的伤害（BH 的 p），骑宠 HP 到 0 就落马，换回人的形象
      if (e.petDamage > 0) this.popup(t, now, { text: String(e.petDamage), color: FONT.RED, dy: 25 });
      t.rideHp = e.rideHp;
      if (e.fall) {
        t.rideHp = 0;
        t.f.sprite = t.f.fallSprite;
        if (t.f.slot === this.me) this.host.chat("你的座骑受伤过重，已退出战斗。", FONT.YELLOW);
      }
    }
    if (e.actorHp > a.hp) this.popup(a, now, { text: String(e.actorHp - a.hp), color: FONT.GREEN });
    a.hp = e.actorHp;
    if (e.dead) { t.dead = true; t.status = 0; t.play(ANIM.DEAD, now); }
  }

  // ---------------- 每帧 ----------------

  frame(now: number) {
    const ui = this.host.ui;
    while (this.queue.length > 0 && this.queue[0](now)) this.queue.shift();
    const idle = this.queue.length === 0;
    if (idle && !this.end && this.menuAt === 0 && this.tiles !== null && !(this.surprise === 2 && this.turns === 0)) this.openMenu(now);

    this.drawBackground();
    this.drawFighters(now);
    this.drawFx(now);
    // AI_Choosen：自动战斗决定的事，播放中靠右画在 (640 − 宽 − 4, 440) + (80, 60)
    if (this.aiLabel && this.menuAt === 0 && this.host.auto()) {
      ui.text(720 - 4 - measure(this.aiLabel), 500, this.aiLabel, FONT.YELLOW);
    }
    if (this.banner.text && now < this.banner.until) {
      const w = measure(this.banner.text) + 24;
      ui.box(400 - w / 2, 12, 400 + w / 2, 34, "rgba(0,0,0,0.6)", true);
      ui.texts.push({ x: 400, y: 16, text: this.banner.text, color: FONT.YELLOW, align: "center" });
    }
    // SurprisalDisp：第一回合播完之前显示
    // 战场属性（BV）：画在左上角
    if (this.fieldAttr > 0 && this.fieldAttr < ELEM.length) ui.text(16, 16, `战场属性：${ELEM[this.fieldAttr]}`, FONT.YELLOW);
    if (this.surprise && this.turns === 0) {
      ui.window(304, 252, 3, 2);
      ui.text(304 + 38, 252 + 40, this.surprise === 2 ? " 遭敌偷袭 " : "出其不意的攻击", FONT.WHITE);
    }
    if (this.menuAt > 0 && !this.end) this.menu(now);
    if (this.petMenuAt > 0 && !this.petSent && !this.end) this.petMenu(now);
    if (idle && this.end) {
      if (!this.endIdleAt) this.endIdleAt = now;
      if (now - this.endIdleAt > 600) this.host.finish(this.end);
    }
    const fade = 1 - Math.min(1, (now - this.startAt) / 400);
    if (fade > 0) ui.box(0, 0, 800, 600, `rgba(0,0,0,${fade.toFixed(2)})`, true);
  }

  /** 快速战斗的一帧：播完排队的动画（静音、快进），轮到下指令就马上照战斗设定下；打完回到地图。只在画面上方写一行字 */
  quickFrame(now: number) {
    if (this.queue.length > 0) {
      const mute = sfx;
      setSfx(() => {});
      let t = Math.max(this.clock, now), n = 0;
      try {
        while (this.queue.length > 0 && n++ < 200000) { if (this.queue[0](t)) this.queue.shift(); else t += 50; }
      } finally { setSfx(mute); }
      this.queue.length = 0;
      this.clock = t;
      this.fx = [];
    }
    if (this.end) { this.host.finish(this.end); return; }
    if (this.menuAt === 0 && !(this.surprise === 2 && this.turns === 0)) this.openMenu(now);
    if (this.menuAt > 0 && !this.sent) this.autoCommand();
    const label = `快速战斗中…　第 ${this.turns + 1} 回合`, ui = this.host.ui;
    ui.box(400 - 90, 40, 400 + 90, 62, "rgba(0,0,0,0.6)", true);
    ui.texts.push({ x: 400, y: 44, text: label, color: FONT.YELLOW, align: "center" });
  }

  private openMenu(now: number) {

    const me = this.fighters.get(this.me);
    if (!me || me.gone) return;
    this.menuAt = now; this.deadline = now + this.turnMs;
    this.petMenuAt = 0; this.petSent = this.myPet() === null;
    if (me.dead) {
      // 自己倒下：没有人的指令，宠物也不下指令（BattleMenuProc 送 "W|FF|FF"）
      this.sent = true;
      if (!this.petSent) this.sendPet(-1, -1);
      return;
    }
    this.sent = false;
    this.offButtons();
    this.capturing = this.lastButton === "capture" && this.petCount() < 5;
    this.selecting = this.lastButton === "attack" || this.capturing;
    // 上回合用了咒术：直接打开咒术窗口（battleButtonBak2 = 1），沉默中不开
    this.magicMenu = this.lastButton === "magic" && me.status !== ST_NOCAST;
  }

  /** 出场中还站着的自己的宠物（p_party[BattleMyNo + 5]）与它在宠物栏的资料 */
  private myPet(): { a: Fighter; info: PetInfo } | null {
    const a = this.fighters.get(this.me + 5);
    const p = this.host.pets();
    const info = p?.pets.find((x) => x.slot === p.battle);
    if (!a || a.dead || a.gone || !info) return null;
    return { a, info };
  }

  private petCount(): number { return this.host.pets()?.pets.length ?? 0; }

  private drawBackground() {
    const ui = this.host.ui, t = this.tiles;
    if (!t || t.length < 400) return;
    // 20×20 的图样斜向铺开；800×600 下 20×20 盖不满四角（原版 800 模式改用 33×33 的 .sabex），这里按 20 取模铺 30×30
    const R = 30, c = (R - 1) / 2;
    for (let i = 0; i < R; i++) for (let j = 0; j < R; j++) {
      const n = t[((i + 25) % 20) * 20 + ((j + 25) % 20)];
      if (!n) continue;
      ui.image(n, 400 + (i - c) * 32 + (j - c) * 32, 300 + (i - c) * 24 - (j - c) * 24);
    }
  }

  private drawFighters(now: number) {
    const ui = this.host.ui, assets = this.host.assets;
    const list = [...this.fighters.values()].filter((a) => !a.gone && !a.hidden).sort((p, q) => p.y - q.y);
    for (const a of list) {
      const fr = a.frame(assets, now);
      a.rect = null;
      if (!fr) continue;
      const e = assets.image(fr[0]);
      ui.image(fr[0], a.x + fr[1], a.y + fr[2], undefined, true);
      if (e) a.rect = [a.x + fr[1] + e.ox, a.y + fr[2] + e.oy, e.w, e.h];
    }
    for (const a of list) {
      // 异常状态动画（katino）
      if (a.status > 0 && !a.dead) this.animAt(STATUS_SPR[a.status], a.x, a.y - 64, now);
      // 己方头顶的血条（HpMeterDisp）
      if (a.f.slot >= 10 || a.dead || a.hp <= 0) continue;
      const mx = Math.round(a.x), my = Math.round(a.y - 80);
      ui.image(a.f.slot === this.me ? CG.BAR_PLAYER : CG.BAR_PET, mx, my);
      const w = Math.round(a.hp / Math.max(1, a.f.maxHp) * 40);
      ui.box(mx - 21, my - 3, mx - 21 + w, my - 1, SYS.GREEN, true);
      if (a.f.slot === this.me) {
        const m = Math.round(a.mp / Math.max(1, a.f.maxMp) * 40);
        ui.box(mx - 21, my + 1, mx - 21 + m, my + 3, SYS.YELLOW, true);
      }
      // 骑着宠物时下方再画骑宠的血条（HpMeterDisp：onRide > 0 时 meterX + 20、meterY + 25）
      if (a.f.rideMaxHp > 0 && a.rideHp > 0) {
        const rx = mx + 20, ry = my + 25;
        ui.image(CG.BAR_PET, rx, ry);
        ui.box(rx - 21, ry - 1, rx - 21 + Math.round(a.rideHp / a.f.rideMaxHp * 40), ry + 1, SYS.GREEN, true);
      }
    }
  }

  /** 画一个循环播放的动画（状态标记） */
  private animAt(id: number, x: number, y: number, now: number) {
    const def = this.host.assets.anim(id);
    const act = def && findAction(def, 0, ANIM.STAND);
    if (!act || act.frames.length === 0) return;
    const ms = Math.max(1, act.ticks) * FRAME;
    const [img, fx, fy] = act.frames[Math.floor(now / ms) % act.frames.length];
    if (img >= 0) this.host.ui.image(img, x + fx, y + fy, undefined, true);
  }

  private drawFx(now: number) {
    this.fx = this.fx.filter((f) => now - f.at < f.ms);
    for (const f of this.fx) f.draw(Math.min(1, (now - f.at) / f.ms), now - f.at);
  }

  private menu(now: number) {
    const ui = this.host.ui;
    if (this.sent) return;
    // 面板从右边滑入：每帧移动量 24、23 … 0，共 300 像素（buttonX 820+160 → 680）
    const f = Math.min(25, Math.floor((now - this.menuAt) / FRAME));
    const moved = 25 * f - f * (f + 1) / 2;
    const bx = 680 + (300 - moved), by = 65;
    if (!this.sent) {
      const rest = Math.max(0, this.deadline - now);
      this.countdown(rest);
      if (rest <= 0) { this.submit("none", -1); return; }
      if (this.host.auto() && this.turnMs - rest >= 2000) { this.autoCommand(); return; }
    }
    ui.image(CG.BTN_BASE, bx, by); // 底板在下（DISP_PRIO_MENU），按钮在上（DISP_PRIO_IME3）
    const btn = (num: number, id: string, on: boolean) => {
      ui.image(num + (on ? 1 : 0), bx, by, id);
      return ui.clicked(id);
    };
    if (btn(CG.BTN_ATTACK, "b-attack", this.selecting && !this.capturing && !this.magicItem)) {
      const on = !(this.selecting && !this.capturing && !this.magicItem);
      this.offButtons(); this.selecting = on;
    }
    const todo = (name: string) => { this.host.chat(`${name}尚未开放。`, FONT.RED); };
    // BattleButtonJujutsu：沉默中按不下去（NoCastFlag）
    const nocast = this.fighters.get(this.me)?.status === ST_NOCAST;
    if (nocast) this.magicMenu = false;
    if (btn(CG.BTN_JUJUTU, "b-jujutu", this.magicMenu || this.magicItem !== null) && !nocast) {
      const open = !(this.magicMenu || this.magicItem);
      this.offButtons(); this.magicMenu = open;
    }
    const full = this.petCount() >= 5; // BattleButtonCapture：宠物栏满了不能捕捉，按钮上打叉
    if (btn(CG.BTN_CAPTURE, "b-capture", this.capturing) && !full) {
      const on = !this.capturing;
      this.offButtons(); this.capturing = on; this.selecting = on;
    }
    if (full) ui.image(CG.BTN_CROSS, bx + 28, by - 18);
    // BattleButtonHelp：切换求救（lssproto_HL_send），服务端回 HL 盖过去；不能求救的地方按钮打叉（NoHelpFlag）
    const [floor] = this.host.place();
    const noHelp = this.pvp || floor >= 8200 && floor <= 8213 || floor >= 30017 && floor <= 30021; // PvP 不能求救（NoHelpFlag）
    if (btn(CG.BTN_HELP, "b-help", this.helpFlag) && !noHelp) { this.helpFlag = !this.helpFlag; this.host.sendHelp(this.helpFlag); }
    if (noHelp) ui.image(CG.BTN_CROSS, bx + 82, by - 18);
    if (btn(CG.BTN_GUARD, "b-guard", false)) { this.lastButton = "guard"; this.submit("guard", -1); }
    if (btn(CG.BTN_ITEM, "b-item", this.itemMenu)) { const on = !this.itemMenu; this.offButtons(); this.itemMenu = on; }
    if (btn(CG.BTN_PET, "b-pet", this.petChange)) { const on = !this.petChange; this.offButtons(); this.petChange = on; }
    if (btn(CG.BTN_ESCAPE, "b-escape", false)) { this.lastButton = "escape"; this.submit("escape", -1); }
    const tips: Record<string, string> = { "b-attack": "攻击。", "b-guard": "防御。", "b-escape": "逃脱。", "b-help": "求救。",
      "b-capture": full ? "不能再捕获更多的宠物了。" : "捕获。", "b-jujutu": nocast ? "沉默中无法吟唱咒术。" : "吟唱咒术。", "b-pet": "更换宠物。" };
    for (const id in tips) if (ui.hovered(id)) ui.bottomLine = tips[id];
    if (this.magicItem) this.magicTarget(this.magicItem);
    else if (this.itemPick) this.itemTarget(this.itemPick);
    else if (this.selecting) this.selectTarget();
    if (this.itemMenu) this.itemList();
    if (this.magicMenu) this.magicWindow();
    if (this.petChange) this.petChangeWindow();
  }

  /**
   * 换宠物的窗口：列出待命 / 出战的宠物（pc.selectPetNo），名字水蓝（倒下的红、出战中的黄）居中画在 (x + 17, y + 10)，
   * 属性图在 (x + 214, 行 + 8)；下一行等级 "%3d"、HP "%4d" / 最大 HP "%4d"，再下一行忠诚度 "%3d"，每只占 87。
   * 点了还站着、不是出战中的宠物就送 "S|格位"；有出战的宠物时左下"收回"送 "S|-1"、右下关闭，否则关闭在中间。
   */
  private petChangeWindow() {
    const ui = this.host.ui, wx = PETCHG.X, wy = PETCHG.Y, mx = wx + Math.floor(PETCHG.W / 2), my = wy + Math.floor(PETCHG.H / 2);
    if (ui.mouse.rightClicked) { this.petChange = false; return; }
    ui.hitBox(wx, wy, wx + PETCHG.W, wy + 380, "bpet-wnd", false);
    ui.image(PETCHG.WND, mx, my);
    const p = this.host.pets();
    const cur = p?.battle ?? -1;
    const list = (p?.pets ?? []).filter((x) => x.state === "standby" || x.state === "battle");
    const x = wx + 17;
    let y = wy + 10;
    for (const pet of list) {
      const a = pet.slot === cur ? this.fighters.get(this.me + 5) : undefined;
      const hp = a && !a.gone ? a.hp : pet.hp;
      const color = pet.slot === cur ? FONT.YELLOW : hp <= 0 ? FONT.RED : FONT.AQUA;
      const name = pet.freeName || pet.name, id = `bpet-${pet.slot}`;
      const clicked = ui.text(x + Math.max(0, (16 * 7 - measure(name)) / 2), y, name, color, id);
      const icons: number[] = [];
      ATR_ICONS.forEach(([big, sml], i) => { const v = pet.attrs[i] ?? 0; if (v > 0) icons.push(v > 50 ? big : sml); });
      icons.slice(0, 2).forEach((g, i) => ui.image(g, wx + 214 + i * 16, y + 8));
      y += 26;
      ui.text(x + 27, y, String(pet.level).padStart(3, " "), color);
      ui.text(x + 110, y, String(hp).padStart(4, " "), color);
      ui.text(x + 160, y, String(pet.maxHp).padStart(4, " "), color);
      y += 25;
      ui.text(x + 60, y, String(pet.loyalty).padStart(3, " "), color);
      y += 36;
      if (clicked && hp > 0 && pet.slot !== cur) { this.submitPet(pet.slot); return; }
    }
    if (list.length === 0) ui.text(x + 11, y, "没有参战的宠物", FONT.WHITE);
    if (list.length > 0 && cur >= 0) {
      if (ui.image(PETCHG.RETURN_BTN, wx + 70, wy + 367, "bpet-return") && ui.clicked("bpet-return")) { this.submitPet(-1); return; }
      if (ui.image(CG.CLOSE_BTN, wx + 187, wy + 367, "bpet-close") && ui.clicked("bpet-close")) this.petChange = false;
    } else if (ui.image(CG.CLOSE_BTN, mx, wy + 367, "bpet-close") && ui.clicked("bpet-close")) this.petChange = false;
  }

  private submitPet(slot: number) {
    if (this.sent) return;
    this.sent = true; this.offButtons(); this.lastButton = "pet";
    this.host.send("pet", -1, slot);
    this.afterPlayer("pet");
  }

  /** BattleButtonOff：按另一个按钮时先把其他按钮的状态（选目标、道具选单、咒术窗口）关掉 */
  private offButtons() {
    this.selecting = false; this.capturing = false; this.itemMenu = false; this.magicMenu = false; this.magicItem = null; this.petChange = false;
    this.itemPick = null;
  }

  /** 装备着的道具带的咒术（magic[0..4]，来自 InvItem.magic） */
  private magics(): InvItem[] {
    return this.host.inventory().items.filter((i) => i.slot < 5 && i.magic).sort((p, q) => p.slot - q.slot);
  }

  /**
   * 咒术窗口：名字在 (x + 30, y + 35 + 28n)（MP 不够红字、地图专用灰字）；鼠标停在名字上时 "%2d/%2d"（要的 MP / 现在的 MP）画在 (x + 155, y + 183)，
   * 说明从 (x + 74, y + 208) 起每 22 字节一行，道具图画在 (x + 37, y + 220)；关闭钮在 (mx, y + 272)。点了能用的咒术就进入选目标；右键关闭。
   */
  private magicWindow() {
    const ui = this.host.ui, x = JUJUTU.X, y = JUJUTU.Y, mx = x + Math.floor(JUJUTU.W / 2), my = y + Math.floor(JUJUTU.H / 2);
    if (ui.mouse.rightClicked) { this.magicMenu = false; return; }
    ui.hitBox(x, y, x + JUJUTU.W, y + JUJUTU.H, "bmagic-wnd", false);
    ui.image(JUJUTU.WND, mx, my + 4);
    const me = this.fighters.get(this.me);
    const mp = me?.mp ?? 0;
    const list = this.magics();
    if (list.length === 0) ui.text(x + 30, y + 35, "没有咒术", FONT.WHITE);
    list.forEach((m, i) => {
      const id = `bmagic-${m.slot}`;
      const color = m.magicField === MAGIC_FIELD_MAP ? FONT.GRAY : m.magicMp > mp ? FONT.RED : FONT.WHITE;
      const name = m.magic, pad = Math.max(0, (16 * 8 - measure(name)) / 2); // CenteringStr(MAGIC_NAME_LEN)
      const clicked = ui.text(x + 30 + pad, y + 35 + i * 28, name, color, id);
      if (ui.hovered(id)) {
        ui.text(x + 155, y + 183, `${String(m.magicMp).padStart(2, " ")}/${String(mp).padStart(2, " ")}`, FONT.WHITE);
        wrapBytes(m.magicMemo, 22).forEach((l, k) => ui.text(x + 74, y + 208 + k * 24, l, FONT.WHITE));
        ui.image(m.image, x + 37, y + 220);
      }
      if (clicked && m.magicField !== MAGIC_FIELD_MAP && m.magicMp <= mp) {
        this.magicMenu = false; this.magicItem = m;
        if (m.magicTarget === M_MYSELF) this.submitMagic(m, this.me); // 只能对自己：直接送出
      }
    });
    if (ui.image(CG.CLOSE_BTN, mx, y + 272, "bmagic-close") && ui.clicked("bmagic-close")) this.magicMenu = false;
  }

  /**
   * 咒术的选目标（BattleTargetSelect 的 BATTLE_JUJUTU）：按咒术的目标种类决定哪些人能点、送出的目标编号：
   * 单人送位置；我方全体 20、敌方全体 21、全体 22；"点哪边就是哪边"按点的那边送 20 / 21；一排按点的人在哪排送 26 / 25 / 23 / 24；
   * 不能对倒下的人用的咒术（deadTargetFlag 为假）倒下的人不能点。鼠标停着时同一组的人一起框起来。右键回到咒术窗口。
   */
  private magicTarget(m: InvItem) {
    this.chooseTarget(m.magicTarget, m.magicDead, (to) => this.submitMagic(m, to), () => { this.magicItem = null; this.magicMenu = true; });
  }

  /**
   * 战斗中道具的选目标（BattleTargetSelect 的 BATTLE_ITEM）：目标种类同咒术的 0..4（自己 / 其他人 / 我方全体 / 对方全体 / 全体），
   * 目标栏 100 以上可以点倒下的人。右键回到道具选单。
   */
  private itemTarget(it: InvItem) {
    this.chooseTarget(it.target % 100, it.target >= 100, (to) => this.submitItem(it.slot, to), () => { this.itemPick = null; this.itemMenu = true; });
  }

  /** 按目标种类决定能点的人与送出的目标编号（咒术与道具共用），onBack 是右键 */
  private chooseTarget(type: number, dead: boolean, onPick: (to: number) => void, onBack: () => void) {
    const ui = this.host.ui, me = this.me, mine = me < 10;
    const m = { magicTarget: type, magicDead: dead };
    if (ui.mouse.rightClicked) { onBack(); return; }
    let wy = 416;
    if (ui.mouse.y > 400) wy = 4;
    ui.window(290, wy, 3, 2);
    ui.text(290 + 38, wy + 28, "请选择", FONT.YELLOW);
    ui.text(290 + 38, wy + 52, "您的目标", FONT.YELLOW);
    const side = (s: number) => s < 10;
    const row = (s: number) => Math.floor(s / 5);
    const ok = (s: number): boolean => {
      switch (m.magicTarget) {
        case M_MYSELF: return s === me;
        case M_ALLMYSIDE: return side(s) === mine;
        case M_ALLOTHERSIDE: case M_SINGLE: case M_ALL_ROWS: return side(s) !== mine;
      }
      return true;
    };
    const send = (s: number): number => {
      switch (m.magicTarget) {
        case M_ALLMYSIDE: return mine ? 20 : 21;
        case M_ALLOTHERSIDE: return mine ? 21 : 20;
        case M_ALL: return 22;
        case M_WHOLEOTHERSIDE: return s < 10 ? 20 : 21;
        case M_ALL_ROWS: return mine ? 21 : 20;
        case M_ONE_ROW: return [26, 25, 23, 24][row(s)];
      }
      return s;
    };
    // 同一组：单人只有自己；整边 / 全体 / 一排按目标编号相同
    let hot = -1;
    const cand = [...this.fighters.values()].filter((a) => ok(a.f.slot) && !a.gone && !a.hidden && a.rect && (m.magicDead || !a.dead));
    for (const a of cand) {
      const [x, y, w, h] = a.rect!;
      if (ui.hitBox(x, y, x + w, y + h, `mt-${a.f.slot}`)) hot = a.f.slot;
      if (ui.clicked(`mt-${a.f.slot}`)) { onPick(send(a.f.slot)); return; }
    }
    if (hot < 0) return;
    const group = send(hot) >= 20 ? cand.filter((a) => send(a.f.slot) === send(hot)) : cand.filter((a) => a.f.slot === hot);
    for (const a of group) {
      const [x, y, w, h] = a.rect!;
      if (a.f.slot !== hot) ui.box(x, y, x + w, y + h, boxColor(), false);
      ui.texts.push({ x: a.x, y: y - 16, text: `${a.f.name} LV${a.f.level}`, color: FONT.WHITE, align: "center" });
    }
  }

  private submitMagic(m: InvItem, target: number) {
    if (this.sent) return;
    this.sent = true; this.offButtons(); this.lastButton = "magic";
    this.host.send("magic", target, m.slot);
    this.afterPlayer("magic");
  }

  /** 战斗中的道具选单：列出能在战斗中用的回复道具，点一下对自己使用（单人战斗没有其他队友）；右键关闭 */
  private itemList() {
    const ui = this.host.ui;
    if (ui.mouse.rightClicked) { this.itemMenu = false; return; }
    const list = battleItems(this.host.inventory());
    const w = 4, h = Math.max(2, Math.ceil((list.length * 22 + 50) / 48)), x = 520, y = 120;
    ui.window(x, y, w, h, "bitem-wnd");
    ui.text(x + 20, y + 16, "使用道具：", FONT.YELLOW);
    if (list.length === 0) ui.text(x + 20, y + 44, "没有可以使用的道具。", FONT.WHITE);
    list.slice(0, 10).forEach((it, i) => {
      const label = it.pile > 1 ? `${it.name} ×${it.pile}` : it.name;
      if (ui.text(x + 20, y + 44 + i * 22, label, FONT.WHITE, `bitem-${it.slot}`)) {
        this.itemMenu = false; this.lastButton = "item";
        if (it.target % 100 === 0) this.submitItem(it.slot, this.me); // 只能对自己：直接送出
        else this.itemPick = it;
      }
      if (ui.hovered(`bitem-${it.slot}`)) ui.bottomLine = it.memo.trim();
    });
  }

  private submitItem(slot: number, target: number) {
    if (this.sent) return;
    this.sent = true; this.offButtons();
    this.host.send("item", target, slot);
    this.afterPlayer("item");
  }

  /** 自动战斗（AI_ChooseAction，ai.ts）：按"战斗设定"决定人和宠物这回合的指令；决定的事显示在右下角（AI_Choosen） */
  private autoCommand() {
    const fighters = new Map<number, AiFighter>();
    for (const [slot, a] of this.fighters) {
      fighters.set(slot, { slot, hp: a.hp, maxHp: a.f.maxHp, mp: a.mp, level: a.f.level, dead: a.dead, gone: a.gone || a.hidden });
    }
    const pet = this.myPet();
    const [floor, gx, gy] = this.host.place();
    const s = this.host.aiSettings(), capturePlace = floor !== 500 || (gx >= 250 && gy >= 360);
    let cmd;
    if (s.other[2]) {
      const sf = new Map<number, SmartFighter>();
      for (const [slot, a] of this.fighters) {
        sf.set(slot, { slot, name: a.f.name, hp: a.dead ? 0 : a.hp, maxHp: a.f.maxHp, mp: a.mp, level: a.f.level, status: a.status, gone: a.gone || a.hidden });
      }
      const sc = smartChoose(s, {
        me: this.me, first: this.aiFirst, fighters: sf, inv: this.host.inventory().items, myAtk: this.host.myAtk(),
        pet: pet ? { name: pet.info.freeName || pet.info.name, atk: pet.info.atk, skills: pet.info.skills } : null,
        capturePlace, noEscape: this.pvp || this.event, mem: this.host.aiMemory(),
      });
      this.aiKey = sc.key; this.aiPetKey = sc.petKey;
      cmd = sc;
    } else {
      cmd = aiChoose(s, {
        me: this.me, first: this.aiFirst, fighters, inv: this.host.inventory().items,
        petSkills: pet ? pet.info.skills : null, capturePlace,
      });
    }
    this.aiFirst = false;
    for (const t of cmd.chat) this.host.chat(t, FONT.WHITE);
    this.aiLabel = cmd.label;
    this.aiPet = cmd.pet;
    switch (cmd.kind) {
      case "magic": this.submitMagic({ slot: cmd.item } as InvItem, cmd.target); break;
      case "item": this.submitItem(cmd.item, cmd.target); break;
      case "skip": this.sent = true; if (!this.petSent) this.sendPet(...cmd.pet); break;
      default: this.submit(cmd.kind, cmd.target);
    }
  }

  /** BattleTargetSelect：提示窗口 + 点选敌人；右键取消 */
  private selectTarget() {
    const ui = this.host.ui;
    if (ui.mouse.rightClicked) { this.selecting = false; return; }
    // MakeWindowDisp(210, 356, 3, 2) 在 800 模式下平移到 (290, 416)；鼠标靠下时移到上方（_NEW_WIN_POS_）
    let wy = 416;
    if (ui.mouse.y > 400) wy = 4;
    ui.window(290, wy, 3, 2);
    ui.text(290 + 38, wy + 28, "请选择", FONT.YELLOW);
    ui.text(290 + 38, wy + 52, "您的目标", FONT.YELLOW);
    for (const a of this.fighters.values()) {
      if (a.f.slot < 10 || a.dead || a.gone || a.hidden || !a.rect) continue;
      const [x, y, w, h] = a.rect, id = `t-${a.f.slot}`;
      if (ui.hitBox(x, y, x + w, y + h, id)) {
        const label = `${a.f.name} LV${a.f.level}`;
        ui.texts.push({ x: a.x, y: y - 16, text: label, color: FONT.WHITE, align: "center" });
      }
      if (ui.clicked(id)) {
        const kind = this.capturing ? "capture" : "attack";
        this.lastButton = kind; this.capturing = false;
        this.submit(kind, a.f.slot);
        return;
      }
    }
  }

  private submit(kind: string, target: number) {
    if (this.sent) return;
    this.sent = true; this.offButtons();
    this.host.send(kind, target);
    this.afterPlayer(kind);
  }

  // ---------------- 宠物的指令 ----------------

  /**
   * 人下完指令后轮到宠物（BattleMenuProc：battlePetMenuFlag）：人要逃跑时宠物不下指令（"W|FF|FF"）；
   * 否则面板滑入，上回合选过技能就直接选目标，没有就打开技能窗口。
   */
  private afterPlayer(kind: string) {
    const pet = this.myPet();
    if (!pet || this.petSent) return;
    if (kind === "escape") { this.sendPet(-1, -1); return; }
    if (this.host.auto()) { this.sendPet(...this.aiPet); return; }
    this.petMenuAt = performance.now();
    const s = pet.info.skills.find((x) => x.slot === this.petSkill);
    this.petSelecting = !!s && s.field !== FIELD_MAP;
    if (!this.petSelecting) this.petSkill = -1;
    this.petWnd = !this.petSelecting;
  }

  private sendPet(skill: number, target: number) {
    if (this.petSent) return;
    this.petSent = true; this.petWnd = false; this.petSelecting = false;
    this.host.sendPet(skill, target);
  }

  // 宠物的自动指令由 autoCommand 一起决定（aiPet）
  /** 宠物的指令面板：CG_PET_BTL_BUTTON_BASE 从 (815+160, 48) 滑入；选了技能时显示技能名与"取消"按钮 */
  private petMenu(now: number) {
    const ui = this.host.ui, pet = this.myPet();
    if (!pet) { this.sendPet(-1, -1); return; }
    const rest = Math.max(0, this.deadline - now);
    this.countdown(rest);
    if (rest <= 0) { this.sendPet(-1, -1); return; }
    const f = Math.min(25, Math.floor((now - this.petMenuAt) / FRAME));
    const px = 675 + (300 - (25 * f - f * (f + 1) / 2)), py = 48;
    ui.image(CG.PET_BTN_BASE, px, py);
    const skill = pet.info.skills.find((x) => x.slot === this.petSkill);
    if (this.petSelecting && skill) {
      ui.texts.push({ x: px - 100, y: py - 7, text: skill.name, color: FONT.WHITE });
      ui.image(CG.PET_BTN_CANCEL, px, py, "pb-cancel");
      if (ui.clicked("pb-cancel")) { this.petSkill = -1; this.petSelecting = false; this.petWnd = true; }
    } else {
      ui.image(CG.PET_BTN_WAZA + (this.petWnd ? 1 : 0), px, py, "pb-waza");
      if (ui.clicked("pb-waza")) this.petWnd = !this.petWnd;
      if (ui.hovered("pb-waza")) ui.bottomLine = "使用技能。";
    }
    // 右键：回到技能窗口
    if (ui.mouse.rightClicked) { this.petSkill = -1; this.petSelecting = false; this.petWnd = true; return; }
    if (this.petWnd) this.petWindow(pet.info);
    else if (this.petSelecting && skill) this.petTarget(skill.target);
  }

  /** BattleButtonWaza 的技能窗口：MakeWindowDisp(364, 41, 272, 348) 在 800 模式下平移到 (444, 101) */
  private petWindow(info: PetInfo) {
    const ui = this.host.ui, wx = 444, wy = 101, mx = wx + 136;
    ui.image(CG.PET_WAZA_WND, mx, wy + 174);
    for (let i = 0; i < info.maxSkill && i < 7; i++) ui.image(CG.PET_WAZA_BAR_1 + i, mx, wy + 174);
    const name = info.freeName || info.name;
    ui.text(wx + 12 + Math.max(0, (16 * 7 - measure(name)) / 2), wy + 32, name, FONT.WHITE);
    for (let i = 0; i < info.maxSkill && i < 7; i++) {
      const s = info.skills.find((x) => x.slot === i);
      if (!s) continue;
      const y = wy + 58 + i * 25, id = `pw-${i}`;
      const usable = s.field !== FIELD_MAP;
      if (ui.text(wx + 15 + 44, y, s.name, usable ? FONT.WHITE : FONT.GRAY, id)) {
        if (usable) this.chooseSkill(s.slot, s.target);
      }
      if (ui.hovered(id)) wrapBytes(s.memo, 24).forEach((l, k) => ui.text(wx + 32, wy + 252 + k * 20, l, FONT.WHITE));
    }
    if (ui.image(CG.CLOSE_BTN, mx, wy + 330, "pw-close") && ui.clicked("pw-close")) this.petWnd = false;
  }

  /** BattleSetWazaHitBox：没有目标的技能直接送出（目标是宠物自己），其余进入选目标 */
  private chooseSkill(slot: number, target: number) {
    this.petWnd = false;
    if (target === T_NONE) { this.petSkill = -1; this.sendPet(slot, this.me + 5); return; }
    this.petSkill = slot; this.petSelecting = true;
  }

  /** 宠物技能的选目标：按技能的目标种类决定哪些人可以点（全体类的点任何一个都行）；右键取消在 petMenu 处理 */
  private petTarget(type: number) {
    const ui = this.host.ui, me = this.me, pet = me + 5;
    let wy = 416;
    if (ui.mouse.y > 400) wy = 4;
    ui.window(290, wy, 3, 2);
    ui.text(290 + 38, wy + 28, "请选择", FONT.YELLOW);
    ui.text(290 + 38, wy + 52, "您的目标", FONT.YELLOW);
    const ok = (slot: number) => {
      switch (type) {
        case T_MYSELF: return slot === pet;
        case T_ALLMYSIDE: return slot < 10;
        case T_ALLOTHERSIDE: return slot >= 10;
        case T_OTHER: case T_ALL: return true;
        case T_WITHOUTME: return slot !== pet;
        case T_WITHOUTMEANDPET: return slot !== pet && slot !== me;
      }
      return slot >= 10;
    };
    for (const a of this.fighters.values()) {
      if (!ok(a.f.slot) || a.dead || a.gone || a.hidden || !a.rect) continue;
      const [x, y, w, h] = a.rect, id = `pt-${a.f.slot}`;
      if (ui.hitBox(x, y, x + w, y + h, id)) ui.texts.push({ x: a.x, y: y - 16, text: `${a.f.name} LV${a.f.level}`, color: FONT.WHITE, align: "center" });
      if (ui.clicked(id)) { this.sendPet(this.petSkill, a.f.slot); return; }
    }
  }

  private countdown(ms: number) {
    if (this.pvp) this.host.ui.text(370, 245, `第 ${String(this.turns + 1).padStart(2, "0")} 回合`, FONT.YELLOW); // DuelFlag 时的回合数
    const s = String(Math.ceil(ms / 1000)).padStart(2, " ");
    let x = 384;
    for (const ch of s) {
      if (ch !== " ") this.host.ui.image(CG.CNT_DOWN_0 + Number(ch), x, 300);
      x += 32;
    }
  }
}
