// 游戏画面里的菜单窗口，按原客户端 menu.cpp 的 MenuProc 实现：
//   任务栏（鼠标移到画面底部时升起，TASK_BAR_*）
//   状态窗口（Ctrl+S / 任务栏，statusWndNo 0）
//   宠物窗口（Ctrl+P / 任务栏，pets.ts）
//   系统窗口（Esc / 任务栏，systemWndNo 0）及其子窗口：回记录点、原地登出（1/7 确认窗）、
//   聊天设定（2）、音效设定（3）、背景音乐（4）、滑鼠设定（6）
// 坐标都是原版 800×600 模式下的值（这些窗口原版用 lpDraw->xSize 居中或固定在左上角，不再平移）。

import type { PartyMember, S2CAccountInfo, S2CNpcWindow, S2CPets, S2CStatus, S2CVendorList, VendorEntry } from "./proto.gen.ts";
import { FONT, SYS, measure, type Ui, InputBox } from "./ui.ts";
import { ItemWindow } from "./items.ts";
import { TradeWindow } from "./trade.ts";
import { ActionWindow } from "./actions.ts";
import { MailWindow } from "./mail.ts";
import { FamilyWindow } from "./family.ts";
import { PetWindow } from "./pets.ts";
import { MapWindow } from "./worldmap.ts";
import { md5 } from "./md5.ts";
import { VendorWindow } from "./vendor.ts";
import type { Field } from "./field.ts";
import type { Assets } from "./assets.ts";
import { AiWindow } from "./aisetting.ts";
import { GmWindow } from "./gm.ts";
import { QuestWindow } from "./quests.ts";
import { album } from "./album.ts";

// 原版图号（anim_tbl.h，按 VER25_RELEASE 求值）
const CG = {
  WND_TITLE_SYSTEM: 26010, WND_TITLE_LOGOUT: 26011, WND_TITLE_CHAT: 26015, WND_TITLE_BGM: 26016, WND_TITLE_SE: 26017,
  WND_TITLE_MOUSE: 26030, WND_TITLE_RESULT: 26018, CLOSE_BTN: 26042, STATUS_GROUP_BTN: 26080, MAP_WND: 26081,
  STATUS_WND: 26073, STATUS_LV_UP_POINT: 26075, STATUS_UP_BTN: 26076, // directdraw.cpp SetAnimTbl 的 8 位图分支（_NEW_CHARDATA_ 的 55266 不在这套资源里）
  TASK_BAR_BACK: 26012, TASK_BAR_MAP: 26150, TASK_BAR_STATUS: 26152, TASK_BAR_PET: 26154, TASK_BAR_ITEM: 26156,
  TASK_BAR_MAIL: 26158, TASK_BAR_ALBUM: 26160, TASK_BAR_SYSTEM: 26162,
};

/** 原版文字调色 FONT_PAL_0..9 的顺序（聊天文字颜色用下标） */
export const FONT_PAL = [FONT.WHITE, FONT.AQUA, FONT.PURPLE, FONT.BLUE, FONT.YELLOW, FONT.GREEN, FONT.RED, FONT.GRAY, FONT.BLUE2, FONT.GREEN2];

// 原版 menu.cpp TransmigrationStr（_TRANS_6 未定义时的 6 项版本）
const TRANS_STR = ["零", "壹", "贰", "参", "肆", "伍"];

// ---------------------------------------------------------------------------
// 用户设定（原版 saveUserSetting 存在本机文件里，这里存 localStorage）

export interface Settings {
  chatLines: number;   // 显示的聊天行数 0..20（NowMaxChatLine）
  chatColor: number;   // 自己发言的文字颜色 0..9（MyChatBuffer.color）
  voice: number;       // 发言音量 1..10（NowMaxVoice，决定附近频道听得到的范围）
  bgmVolume: number;   // 1..15（t_music_bgm_volume）
  bgmPitches: Record<number, number>; // 每首背景音乐各自的节奏 -8..8（t_music_bgm_pitch[曲子编号]）
  seVolume: number;    // 1..15（t_music_se_volume）
  stereo: boolean;     // stereo_flg
  mute: boolean;       // MuteFlag
  smoothCursor: boolean; // MouseCursorFlag：平滑 = 用系统光标
  hideOthers: boolean; // 人物屏蔽开关
  autoBattle: boolean; // 自动战斗（原版 AI_SELECT）
  quickBattle: boolean; // 快速战斗（额外新功能）：不进战斗画面，照战斗设定自动打完
  minimap: boolean;    // 右上角常显小地图（额外新功能，地图窗口里开关）
}

const DEFAULTS: Settings = {
  chatLines: 6, chatColor: 0, voice: 3, bgmVolume: 15, bgmPitches: {}, seVolume: 15,
  stereo: true, mute: false, smoothCursor: false, hideOthers: false, autoBattle: false, quickBattle: false, minimap: false,
};
const KEY = "sa.settings";

function loadSettings(): Settings {
  try { return { ...DEFAULTS, ...JSON.parse(localStorage.getItem(KEY) ?? "{}") }; } catch { return { ...DEFAULTS }; }
}
export const settings: Settings = loadSettings();
export function saveSettings() {
  try { localStorage.setItem(KEY, JSON.stringify(settings)); } catch { /* 隐私模式等存不了就算了 */ }
}

const clamp = (v: number, lo: number, hi: number) => Math.max(lo, Math.min(hi, v));

// ---------------------------------------------------------------------------

/** 菜单需要的外部操作（由 main.ts 的 FieldScreen 提供） */
export interface MenuHost {
  status: S2CStatus | null;
  party(): (PartyMember | null)[];             // 队伍（0 是队长）
  selfId(): number;
  trade(cmd: string, slot: number, amount: number): void; // lssproto_TD_send
  action(no: number): void;                   // lssproto_AC_send（并换自己的姿势）
  emote(spr: number): void;                   // 打呼 / 冷场（只在自己画面上）
  planet(): string;
  mailPet(): number;
  setMailPet(slot: number): void;
  mailSend(index: number, text: string): void;
  petMail(index: number, pet: number, item: number, text: string): void;
  cardDelete(index: number): void;         // 由主程序先问
  mailLamp(on: boolean): void;
  chat(text: string, color: string): void;   // 原版 StockChatBufferLine
  returnHome(): void;                         // 回记录点
  statUp(stat: number): void;                 // 分配升级点数（0 体力、1 腕力、2 耐力、3 敏捷）
  itemMove(from: number, to: number): void;
  itemUse(slot: number, target: number): void;
  magicUse(slot: number, target: number): void; // 地图上用咒术（lssproto_MU_send）
  dropGold(amount: number): void;               // 丢石币（lssproto_DG_send）
  itemDrop(slot: number, name: string): void;
  petItem(pet: number, from: number, to: number): void; // 宠物身上的道具（lssproto_PetItemEquip_send）
  petSkill(pet: number, skill: number, data: string): void; // 地图上用宠物技能（lssproto_PS_send）
  petState(slot: number, state: string): void;  // 宠物窗口的状态钮（rest / standby / battle）
  petRename(slot: number, name: string): void;
  petDrop(slot: number): void;                  // 由主程序弹确认框
  petRide(slot: number): void;                  // 骑乘钮（lssproto_FM "R|P|格位"，骑着时再按就下来）
  canvas: HTMLCanvasElement;                    // 宠物改名的输入框叠在画布上
  logout(): void;                             // 原地登出 → 角色选择
  settingsChanged(): void;
  /** 地图窗口：楼层名、自己的格子座标、画好的自动地图（还没进地图时为 null） */
  field(): Field | null;                        // 地图窗口（worldmap.ts）要看的格子资料
  assets: Assets;
  root: string;                                 // 资源根目录（地图取平均色要读图集）
  family(text: string): void;                   // lssproto_FM_send
  fmReply(win: S2CNpcWindow, button: number, data: string): void; // 家族窗口的 WN 回传
  walking(): boolean;
  esc(): boolean;                               // 本帧按了 Esc
  currentBgm(): number;                         // 正在播的背景音乐编号（节奏设定用）
  uiSe(no: number): void;                       // 界面音效（这一帧就不再放点击声 217）
  gmSend(text: string): void;                   // GM 指令（GmCommand，只有 GM 有选单）
  gmList(kind: string): void;                   // GM 窗口的清单（GmListReq）
  gmDetail(kind: string, id: number): void;     // 清单里一项的详情（GmDetailReq）
  questReq(): void;                             // 任务日志（QuestReq，打开任务窗口时要一次）
  vendor(cmd: string, title: string, kinds: number[], slots: number[], prices: number[]): void; // 摆摊（Vendor）
  partyKick(slot: number): void;                // 状态窗口队伍页的踢人（lssproto_KTEAM_send）
  accountReq(): void;                           // 系统窗口"个人信息"：要账号资料（AccountInfoReq）
  changePassword(old: string, password: string): void; // 都是 MD5
  changeSecret(password: string, secret: string): void;


  autoEncount(): boolean;                       // 自动遇敌开着没有
  setAutoEncount(on: boolean): void;
  place(): [number, number, number];            // 楼层、x、y
}

type SystemPage = "main" | "home" | "logout" | "chat" | "se" | "bgm" | "mouse" | "account";

/** 战斗结果窗口里的一只宠物 */
export interface ResultPet { name: string; exp: number; lvUp: boolean; }

export class Menus {
  statusOpen = false;  // MenuToggleFlag & JOY_CTRL_S
  mapOpen = false;     // MenuToggleFlag & JOY_CTRL_M
  systemOpen = false;  // MenuToggleFlag & JOY_ESC
  private page: SystemPage = "main";
  private upHold = { id: "", at: 0, last: 0, sent: 0, max: 0 }; // 状态窗口的加点钮按住不放

  private result: { name: string; exp: number; lvUp: boolean; items: string[]; pets: ResultPet[]; at: number } | null = null;
  pets: S2CPets | null = null; // 宠物栏（宠物窗口用）
  /** 本帧画出的窗口范围。头顶名字画在 2D 覆盖层上，会盖住窗口图，main.ts 用它把窗口下的名字略去 */
  rects: [number, number, number, number][] = [];
  private ui: Ui;
  private host: MenuHost;

  readonly items: ItemWindow;
  readonly trade: TradeWindow;
  readonly actions: ActionWindow;
  readonly mail: MailWindow;
  readonly family: FamilyWindow;
  readonly ai: AiWindow;
  readonly petWnd: PetWindow;
  readonly gm: GmWindow;
  readonly quests: QuestWindow;
  readonly mapWnd: MapWindow;
  readonly vendor: VendorWindow;

  constructor(ui: Ui, host: MenuHost) {
    this.ui = ui; this.host = host;
    try { album.load(localStorage.getItem("sa-account") ?? ""); } catch { album.load(""); }
    this.items = new ItemWindow({
      ui, level: () => host.status?.level ?? 1, mp: () => host.status?.mp ?? 0, name: () => host.status?.name ?? "",
      pets: () => (this.pets?.pets ?? []).map((p) => ({ slot: p.slot, name: p.freeName || p.name })),
      partyMembers: () => host.party().map((p, slot) => p && p.id !== host.selfId() ? { slot, name: p.name } : null).filter((x): x is { slot: number; name: string } => x !== null),
      move: (f, t) => host.itemMove(f, t), use: (s, t) => host.itemUse(s, t), drop: (s, n) => host.itemDrop(s, n),
      magicUse: (s, t) => host.magicUse(s, t), dropGold: (n) => host.dropGold(n), canvas: host.canvas,
      petList: () => this.pets?.pets ?? [], ridePet: () => this.pets?.ride ?? -1,
      petItem: (p, f, t) => host.petItem(p, f, t), notice: (text) => host.chat(text, FONT.YELLOW),
      petSkill: (p, s, d) => host.petSkill(p, s, d),
    });
    this.ai = new AiWindow({
      ui, inv: () => this.items.inv.items,
      petSkills: () => (this.pets?.pets ?? []).find((p) => p.state === "battle")?.skills ?? null,
      autoBattle: () => settings.autoBattle, toggleAuto: () => this.toggleAuto(),
      quickBattle: () => settings.quickBattle, toggleQuick: () => this.toggleQuick(),
      autoEncount: () => host.autoEncount(), setAutoEncount: (on) => host.setAutoEncount(on),
    });
    this.gm = new GmWindow({
      ui, canvas: host.canvas, send: (t) => host.gmSend(t), list: (k) => host.gmList(k), detail: (k, id) => host.gmDetail(k, id), place: () => host.place(),
      status: () => host.status, pets: () => (this.pets?.pets ?? []).filter((p) => p.slot >= 0 && p.slot < 5).sort((a, b) => a.slot - b.slot),
    });
    this.mapWnd = new MapWindow({ ui, assets: host.assets, root: host.root, field: () => host.field(), walkTo: (x, y) => { const f = host.field(); if (f) f.target = [x, y]; } });
    this.vendor = new VendorWindow({
      ui, canvas: host.canvas, items: () => this.items.inv.items, pets: () => this.pets?.pets ?? [], ridePet: () => this.pets?.ride ?? -1,
      gold: () => this.items.inv.gold, send: (cmd, title = "", kinds = [], slots = [], prices = []) => host.vendor(cmd, title, kinds, slots, prices),
      chat: (t, c) => host.chat(t, c),
    });
    this.quests = new QuestWindow({ ui, chat: (t, c) => host.chat(t, c), se: (no) => host.uiSe(no), refresh: () => host.questReq() });
    this.actions = new ActionWindow({ ui, act: (no) => host.action(no), emote: (spr) => host.emote(spr) });
    this.family = new FamilyWindow({
      ui, canvas: host.canvas, send: (t) => host.family(t), reply: (w, b, d) => host.fmReply(w, b, d), chat: (t, c) => host.chat(t, c),
      name: () => host.status?.name ?? "", face: () => host.status?.face ?? 0, pets: () => this.pets?.pets ?? [],
      gold: () => this.items.inv.gold, walking: () => host.walking(),
    });
    this.mail = new MailWindow({
      ui, canvas: host.canvas, name: () => host.status?.name ?? "", planet: () => host.planet(),
      pets: () => this.pets?.pets ?? [], items: () => this.items.inv.items, mailPet: () => host.mailPet(), clearMailPet: () => host.setMailPet(-1),
      inBattle: () => false, send: (i, text) => host.mailSend(i, text), petSend: (i, p, it, text) => host.petMail(i, p, it, text),
      remove: (i) => host.cardDelete(i), lamp: (on) => host.mailLamp(on), chat: (t, c) => host.chat(t, c),
    });
    this.trade = new TradeWindow({
      ui, canvas: host.canvas, items: () => this.items.inv.items, pets: () => this.pets?.pets ?? [], ridePet: () => this.pets?.ride ?? -1,
      gold: () => this.items.inv.gold, send: (cmd, slot = 0, amount = 0) => host.trade(cmd, slot, amount),
    });
    this.petWnd = new PetWindow({
      ui, canvas: host.canvas, pets: () => this.pets,
      setState: (s, st) => host.petState(s, st), rename: (s, n) => host.petRename(s, n), drop: (s) => host.petDrop(s),
      ride: (s) => host.petRide(s),
      todo: (name) => host.chat(`${name}功能尚未开放。`, FONT.RED),
      snap: (p) => { const msg = album.snap(p); if (msg) host.chat(msg, FONT.WHITE); },
      mailPet: () => host.mailPet(), setMailPet: (s) => host.setMailPet(s),
      // 标记不够就打开道具窗口（原版 MenuToggleFlag |= JOY_CTRL_I、itemWndNo = 0）
      fieldSkill: (p, sk) => { if (!this.items.startMix(p, sk) && !this.items.open) this.toggleItems(); },
    });
  }

  /** 有窗口开着（CheckMenuFlag 等：这时画面上方的按钮不画） */
  windowOpen(): boolean {
    return this.statusOpen || this.mapOpen || this.systemOpen || this.petWnd.open || this.items.open || album.open
      || this.actions.open || this.trade.open || this.ai.open || this.mail.open || this.family.open || this.gm.open || this.quests.open || this.vendor.open;
  }

  /** 开着几个窗口（主程序按它的增减放开窗口 202 / 关窗口 203 的声音） */
  openCount(): number {
    return [this.statusOpen, this.mapOpen, this.systemOpen, this.petWnd.open, this.items.open, album.open, this.actions.open,
      this.trade.open, this.ai.open, this.mail.open, this.family.open, this.gm.open, this.quests.open, this.vendor.open].filter(Boolean).length;
  }

  // 原版 menu.cpp：打开道具窗口时关掉地图（MenuToggleFlag &= ~JOY_CTRL_M），打开地图时关掉道具窗口（&= ~JOY_CTRL_I）
  toggleItems() { this.items.toggle(); if (this.items.open) this.mapOpen = false; }

  // 原版：打开状态（Ctrl+S）、宠物（Ctrl+P）、系统（Esc）窗口时 MenuToggleFlag &= JOY_CTRL_I | JOY_CTRL_M，
  // 只留下道具与地图窗口，其他窗口和战斗结果窗口（BattleResultWndFlag）都关掉，所以这三个窗口互斥
  private closeOthers() { this.statusOpen = false; this.systemOpen = false; this.petWnd.close(); this.result = null; album.open = false; this.quests.open = false; }
  /** 画面上方的"任务查询"钮：任务窗口，和状态 / 宠物 / 系统窗口一样互斥 */
  toggleQuests() { const open = !this.quests.open; if (open) this.closeOthers(); this.quests.toggle(); }
  // 相簿（Ctrl+A / 任务栏）：打开时关掉道具与地图以外的窗口（MenuToggleFlag &= JOY_CTRL_I | JOY_CTRL_M）
  toggleAlbum() { const open = !album.open; if (open) this.closeOthers(); album.open = open; }
  toggleStatus() { const open = !this.statusOpen; if (open) this.closeOthers(); this.statusOpen = open; this.statusPage = 0; }
  togglePets() { const open = !this.petWnd.open; if (open) this.closeOthers(); this.petWnd.toggle(); }
  toggleMap() { this.mapOpen = !this.mapOpen; if (this.mapOpen) { this.mapWnd.reset(); if (this.items.open) this.items.toggle(); } }
  toggleSystem() { const open = !this.systemOpen; if (open) this.closeOthers(); this.systemOpen = open; this.go("main"); }

  /** 离开地图画面（进战斗、登出）：拿掉宠物改名的 DOM 输入框 */
  leave() { this.petWnd.leave(); this.gm.leave(); this.vendor.close(false); this.removeAccountBoxes(); }

  /** 战斗胜利后的结果窗口（menu.cpp BattleResultWndFlag），RESULT_WND_CLOSE_TIME 300 帧后自动关闭 */
  showResult(name: string, exp: number, lvUp: boolean, items: string[], pets: ResultPet[] = []) {
    this.result = { name, exp, lvUp, items, pets, at: performance.now() };
  }

  setPets(m: S2CPets) { this.pets = m; }

  /** 摆摊的设定窗口（VendorSetup）与购买窗口（VendorList）：和交易一样，其他窗口都关掉 */
  openVendorSetup(entries: VendorEntry[]) {
    this.closeOthers(); this.mapOpen = false; if (this.items.open) this.items.toggle();
    this.vendor.openSetup(entries);
  }
  openVendorList(m: S2CVendorList) {
    if (!this.vendor.open) { this.closeOthers(); this.mapOpen = false; if (this.items.open) this.items.toggle(); }
    this.vendor.openBuy(m);
  }

  /** TD C：打开交易窗口，其他窗口都关掉（DeathMenuAction） */
  openTrade(name: string) {
    this.closeOthers(); this.mapOpen = false; if (this.items.open) this.items.toggle();
    this.trade.start(name);
  }

  frame() {
    this.rects = [];
    if (this.trade.open) { this.trade.frame(this.rects); return; } // 交易中只有交易窗口（MenuToggleFlag = JOY_CTRL_T）
    this.taskBar();
    // GM 窗口开着时只画它：别的窗口的文字画在 2D 覆盖层，会叠到 GM 窗口上（关掉后原来开着的窗口照样回来）
    if (this.gm.open) {
      this.gm.frame(this.rects, this.host.esc());

      return;
    }

    if (this.result) this.resultWindow();
    if (this.duel) this.duelWindow();
    if (this.statusOpen) this.statusWindow();
    if (this.mapOpen) this.mapWindow();
    this.petWnd.frame(this.rects);
    album.frame(this.ui, this.rects);
    this.items.frame(this.rects);
    if (this.systemOpen) this.systemWindow();
    this.ai.frame(this.rects, false);
    this.actions.frame(this.rects);
    this.mail.frame(this.rects);
    this.family.frame(this.rects, this.host.esc());
    this.quests.frame(this.rects, this.host.esc());
    this.vendor.frame(this.rects, this.host.esc());
    this.gm.frame(this.rects, this.host.esc());
  }

  /** Ctrl+E / 任务栏的邮件：只留下道具与地图窗口 */
  toggleMail() { const open = !this.mail.open; if (open) this.closeOthers(); if (open !== this.mail.open) this.mail.toggle(); }

  /** 家族功能钮（FIELD_FUNC_CHANNEL）：还没加入家族时"请先加入家族。"，否则开关家族选单 */
  openFamily() { this.family.toggle(); }

  /** 自动战斗开关（系统菜单的"自动战斗"、PageUp / PageDown）：开启时说"开启ＡＩ模式！" */
  toggleAuto(say = true) {
    settings.autoBattle = !settings.autoBattle;
    if (say) this.host.chat(settings.autoBattle ? "开启ＡＩ模式！" : "关闭ＡＩ模式！", FONT.RED);
    this.changed();
  }

  /** 快速战斗开关（系统菜单、战斗设定窗口） */
  toggleQuick() {
    settings.quickBattle = !settings.quickBattle;
    this.host.chat(settings.quickBattle ? "开启快速战斗！" : "关闭快速战斗！", FONT.RED);
    this.changed();
  }

  // ---------------- 任务栏 ----------------
  // 原版：底图 CG_TASK_BAR_BACK 常驻 (TASK_BAR_X, TASK_BAR_Y) = (320, 468 + 120)，鼠标进到 y ≥ 576 时按钮才升起。
  // 这里按需求改为按钮常驻显示（不再藏在画面下方）。按钮图的锚点偏移本身就按 800 宽排好了，底图却只有 640 宽
  // （原版 800 模式下贴在左边，右侧露空），这里再补画一次铺满 800。
  private taskBar() {
    const ui = this.ui, TASK_BAR_X = 320, TASK_BAR_Y = 588;
    ui.image(CG.TASK_BAR_BACK, TASK_BAR_X, TASK_BAR_Y);
    ui.image(CG.TASK_BAR_BACK, TASK_BAR_X + 160, TASK_BAR_Y);
    const x = TASK_BAR_X, y = TASK_BAR_Y;
    const btn = (num: number, dx: number, on: boolean, id: string) => ui.image(num + (on ? 1 : 0), x + dx, y, id) && ui.clicked(id);
    const todo = (name: string) => this.host.chat(`${name}功能尚未开放。`, FONT.RED);
    if (btn(CG.TASK_BAR_MAP, 0, this.mapOpen, "tb-map")) this.toggleMap();
    if (btn(CG.TASK_BAR_STATUS, 24, this.statusOpen, "tb-status")) this.toggleStatus();
    if (btn(CG.TASK_BAR_PET, 48, this.petWnd.open, "tb-pet")) this.togglePets();
    if (btn(CG.TASK_BAR_ITEM, 72, this.items.open, "tb-item")) this.toggleItems();
    if (btn(CG.TASK_BAR_MAIL, 95, this.mail.open, "tb-mail")) this.toggleMail();
    if (btn(CG.TASK_BAR_ALBUM, 118, album.open, "tb-album")) this.toggleAlbum();
    if (btn(CG.TASK_BAR_SYSTEM, 140, this.systemOpen, "tb-system")) this.toggleSystem();
  }

  // ---------------- 地图窗口 ----------------
  // 地图窗口：世界地图（worldmap.ts），原版的小自动地图窗口（CG_MAP_WND）不再用
  private mapWindow() { this.mapWnd.frame(this.rects, () => { this.mapOpen = false; }); }

  // ---------------- 状态窗口 ----------------
  // MakeWindowDisp(4, 4, 272, 360, 0, -1)：无边框窗口，中心 (mx, my) = (140, 184)，底图 CG_STATUS_WND 画在中心。
  // 字段位置按 _CHARTITLE_STR_ 定义、_NEW_CHARDATA_ 不用时的分支。
  private statusPage = 0; // statusWndNo：0 状态、1 队伍
  private statusWindow() {
    if (this.statusPage === 1) { this.statusPartyWindow(); return; }
    const ui = this.ui, st = this.host.status;
    const wx = 4, wy = 4, mx = wx + 136, my = wy + 180;
    ui.hitBox(wx, wy, wx + 272, wy + 360, "status-wnd", false);
    this.rects.push([wx, wy, wx + 272, wy + 360]);
    ui.image(CG.STATUS_WND, mx, my);
    if (!st) return;
    const num = (v: number, w: number) => String(v).padStart(w, " "); // 原版 sprintf "%8d" 等（宋体半角等宽）
    let x = wx + 20, y = wy + 28;
    // CenteringStr(pc.name, CHAR_NAME_LEN)：名字在 16 个半角宽度里居中
    ui.text(x - 6 + Math.max(0, (16 * 7 - measure(st.name)) / 2), y - 2, st.name, FONT.WHITE);
    ui.text(x + 170, y - 2, num(st.dp, 8), FONT.WHITE);
    y += 21;
    if (st.trans >= 1 && st.trans <= 5) ui.text(x + 190, y + 2, `转生${TRANS_STR[st.trans]}`, FONT.AQUA);
    y += 19; // _CHARTITLE_STR_：称号栏占 19
    ui.text(x + 50, y, num(st.level, 3), FONT.WHITE); y += 21;
    ui.text(x + 48, y, num(st.exp, 8), FONT.WHITE); y += 21;
    ui.text(x + 48, y, num(st.nextExp, 8), FONT.WHITE); y += 21;
    ui.text(x + 48, y, num(st.hp, 4), FONT.WHITE);
    ui.text(x + 98, y, num(st.maxHp, 4), FONT.WHITE); y += 21;
    for (const v of [st.mp, st.atk, st.def, st.agi, st.charm]) { ui.text(x + 50, y, num(v, 3), FONT.WHITE); y += 21; }
    y += 29;
    ui.text(x + 61, y, num(st.vital, 3), FONT.WHITE);
    ui.text(x + 61 + 120, y, num(st.str, 3), FONT.WHITE); y += 20;
    ui.text(x + 61, y, num(st.tgh, 3), FONT.WHITE);
    ui.text(x + 61 + 120, y, num(st.dex, 3), FONT.WHITE); y += 20;
    y += 17;
    if (ui.image(CG.STATUS_GROUP_BTN, x + 59, y, "st-group") && ui.clicked("st-group")) { this.statusPage = 1; this.host.uiSe(202); } // statusWndNo = 1
    if (ui.image(CG.CLOSE_BTN, x + 173, y, "st-close") && ui.clicked("st-close")) this.statusOpen = false;
    // 头像：_CHARTITLE_STR_ 的 (209+1, 118+13) 是配 _NEW_CHARDATA_ 底图 55266 的，导出资源里没有那张图，
    // 用的是 CG_STATUS_WND，所以取配它的 (209, 118)（量过底图头像框中心在 (210, 119)）
    ui.image(st.face, wx + 209, wy + 118);
    // 升级点数（StatusUpPoint）：有点数时显示剩余点数和四个 ▲ 按钮，点一下加一点（lssproto_SKUP）
    // 位置用的是配 CG_STATUS_WND 的那一支（不加称号栏的位移）：_CHARTITLE_STR_ 的 +31 是配 55266 底图的，用在这张底图上会低一截
    if (st.points > 0) {
      const py = my;
      ui.image(CG.STATUS_LV_UP_POINT, mx, py);
      ui.text(mx + 50, my + 88, String(st.points).padStart(2, " "), FONT.YELLOW);
      const pos: [number, number][] = [[mx, py], [mx + 120, py], [mx, py + 20], [mx + 120, py + 20]];
      // 按一下加一点；按住不放（额外新功能）0.4 秒后连续加，每 0.12 秒一点，按住超过 1.2 秒加快到每 0.05 秒一点，
      // 加到按下时的剩余点数为止（服务端回来的点数会慢一点，不能用它判断）
      const now = performance.now(), h = this.upHold;
      pos.forEach(([bx, by], i) => {
        const id = `st-up${i}`;
        const down = ui.hovered(id) && ui.mouse.down;
        ui.image(CG.STATUS_UP_BTN + (down ? 1 : 0), bx, by, id);
        if (ui.hovered(id)) ui.bottomLine = "按一下加 1 点，按住不放连续加点。";
        if (ui.clicked(id)) {
          this.host.statUp(i);
          this.upHold = { id, at: now, last: now, sent: 1, max: st.points };
        } else if (down && h.id === id) {
          const t = now - h.at;
          if (t >= 400 && now - h.last >= (t >= 1200 ? 50 : 120) && h.sent < h.max) {
            this.host.statUp(i);
            h.last = now; h.sent++;
          }
        }
      });
      if (!ui.mouse.down || !pos.some((_, i) => ui.hovered(`st-up${i}`))) this.upHold.id = "";
    }
    // 四属性条（PET_WND_ATTR_X/Y = 159/5），长度 = 属性值 × 0.8，三层方框做出立体感
    x = wx + 19; y = wy + 174;
    const bars: [number, string, string][] = [[st.attrs[0], SYS.GREEN, SYS.GREEN2], [st.attrs[1], SYS.AQUA, SYS.AQUA2],
      [st.attrs[2], SYS.RED, SYS.RED2], [st.attrs[3], SYS.YELLOW, SYS.YELLOW2]];
    for (const [v, c1, c2] of bars) {
      if (v > 0) {
        const bx = x + 159, by = y + 5, w = Math.floor(v * 0.8);
        ui.box(bx + 2, by + 2, bx + 2 + w + 1, by + 11, c2, false);
        ui.box(bx + 1, by + 1, bx + 1 + w + 1, by + 10, c2, false);
        ui.box(bx, by, bx + w, by + 8, c1, true);
      }
      y += 20;
    }
  }

  // statusWndNo 1：队伍页（menu.cpp：MakeWindowDisp(4, 0, 256, 456) 配底图 CG_STATUS_WND_GROUP_WND，那张图不在这套资源里，自己画）：
  // 自己与宠物的 ＭＰ / ＨＰ，下面是队员的 ＭＰ / ＨＰ；队长每个队员旁有踢人钮（_TEAM_KICKPARTY：CG_MAIL_WND_DELETE_BTN → lssproto_KTEAM_send）
  private statusPartyWindow() {
    const ui = this.ui, st = this.host.status, wx = 4, wy = 4;
    ui.window(wx, wy, 4, 7, "status-party");
    this.rects.push([wx, wy, wx + 256, wy + 336]);
    ui.box(wx + 22, wy + 10, wx + 234, wy + 326, "rgba(0,0,0,0.45)", true);
    if (!st) return;
    const x = wx + 26;
    let y = wy + 18;
    ui.text(x, y, "队伍", FONT.YELLOW); ui.text(x + 104, y, "ＭＰ", FONT.AQUA); ui.text(x + 146, y, "ＨＰ", FONT.AQUA); y += 20;
    const row = (name: string, mp: number | null, hp: number, maxHp: number, color: string) => {
      ui.text(x, y, name.length > 7 ? name.slice(0, 7) : name, color);
      if (mp !== null) ui.text(x + 104, y, String(mp), FONT.WHITE);
      ui.text(x + 146, y, `${hp}/${maxHp}`, FONT.WHITE);
      y += 18;
    };
    row(st.name, st.mp, st.hp, st.maxHp, FONT.WHITE);
    for (const p of (this.pets?.pets ?? []).filter((p) => p.slot >= 0 && p.slot < 5).sort((a, b) => a.slot - b.slot)) row("　" + (p.freeName || p.name), null, p.hp, p.maxHp, FONT.GRAY);
    y += 6; ui.box(x, y, x + 204, y + 1, "rgba(255,255,255,0.25)", true); y += 10;
    const party = this.host.party(), me = this.host.selfId(), leader = party[0]?.id === me;
    const members = party.map((m, slot) => ({ m, slot })).filter((v): v is { m: PartyMember; slot: number } => !!v.m && v.m.id !== me);
    if (members.length === 0) ui.text(x, y, "没有组队。", FONT.GRAY);
    for (const { m, slot } of members) {
      const ry = y;
      row(m.name, m.mp, m.hp, m.maxHp, FONT.WHITE);
      if (leader && ui.text(x + 190, ry, "踢", FONT.RED, `st-kick${slot}`)) this.host.partyKick(slot);
      if (ui.hovered(`st-kick${slot}`)) ui.bottomLine = `把 ${m.name} 踢出队伍。`;
    }
    if (ui.image(CG.CLOSE_BTN, wx + 128, wy + 300, "st-pclose") && ui.clicked("st-pclose")) { this.statusPage = 0; this.host.uiSe(203); }
  }

  // MakeWindowDisp(320-160, 240-120, 5, 6, CG_WND_TITLE_RESULT) 在 800 模式下平移到 (240, 180)
  /** 决斗的 DP 结果（RD）：MakeWindowDisp(224, 168, 3, 3, CG_WND_TITLE_RESULT)，300 帧后自动关 */
  showDuelResult(delta: number, dp: number) { this.duel = { delta, dp, at: performance.now() }; }
  private duel: { delta: number; dp: number; at: number } | null = null;
  private duelWindow() {
    const ui = this.ui, d = this.duel!;
    if (performance.now() - d.at > 300 * 1000 / 60) { this.duel = null; return; }
    const wx = 224, wy = 168;
    ui.window(wx, wy, 3, 3, "duel-wnd");
    ui.image(CG.WND_TITLE_RESULT, wx + 96, wy + 27);
    this.rects.push([wx, wy, wx + 192, wy + 144]);
    const sign = d.delta >= 0 ? "+" : "-";
    ui.text(wx + 17, wy + 48, ` Ｄ  Ｐ  ${(sign + Math.abs(d.delta)).padStart(8, " ")}`, FONT.WHITE);
    ui.text(wx + 17, wy + 76, ` 合  计  ${String(d.dp).padStart(8, " ")}`, FONT.WHITE);
    if (ui.image(CG.CLOSE_BTN, wx + 96, wy + 120, "duel-close") && ui.clicked("duel-close")) this.duel = null;
  }

  private resultWindow() {
    const ui = this.ui, r = this.result!;
    if (performance.now() - r.at > 300 * 1000 / 60) { this.result = null; return; }
    const wx = 240, wy = 180, mx = wx + 160;
    ui.window(wx, wy, 5, 6, "result-wnd");
    ui.image(CG.WND_TITLE_RESULT, wx + 5 * 32, wy + 27);
    this.rects.push([wx, wy, wx + 320, wy + 288]);
    const x = wx + 12, y = wy + 44;
    ui.text(x + Math.max(0, (16 * 7 - measure(r.name)) / 2), y, r.name, FONT.WHITE);
    if (r.lvUp) ui.text(x + 152, y, "LvUp!", FONT.YELLOW);
    ui.text(x + 148 + 58, y, `Exp ${(r.exp >= 0 ? "+" : "") + r.exp}`.padEnd(4), FONT.WHITE);
    // 出战与待命的宠物，每只一行（y += 20）
    r.pets.slice(0, 5).forEach((p, i) => {
      const py = y + 20 * (i + 1);
      ui.text(x + Math.max(0, (16 * 7 - measure(p.name)) / 2), py, p.name, FONT.WHITE);
      if (p.lvUp) ui.text(x + 152, py, "LvUp!", FONT.YELLOW);
      ui.text(x + 148 + 58, py, `Exp +${p.exp}`, FONT.WHITE);
    });
    ui.text(x, wy + 134 + 30, "  道具 ：", FONT.WHITE);
    if (r.items.length === 0) ui.text(x + 85, wy + 134 + 30, "没有得到任何道具。", FONT.WHITE);
    r.items.slice(0, 3).forEach((n, i) => ui.text(x + 85, wy + 134 + 30 + i * 22, n, FONT.WHITE));
    if (ui.image(CG.CLOSE_BTN, mx, wy + 216 + 30, "result-close") && ui.clicked("result-close")) this.result = null;
  }

  // ---------------- 系统窗口 ----------------

  /** 原版 StockFontBuffer(..., 2)：可点的文字，返回本帧是否被点 */
  private item(x: number, y: number, text: string, id: string, color: string = FONT.WHITE): boolean {
    return this.ui.text(x, y, text, color, id);
  }

  /** MakeWindowDisp(x, y, w, h, title, 1)：九宫格窗口，标题图画在 (x + w*32, y + 27) */
  private frameWnd(x: number, y: number, w: number, h: number, title: number) {
    this.ui.window(x, y, w, h, "sys-wnd");
    this.rects.push([x, y, x + w * 64, y + h * 48]);
    this.ui.image(title, x + w * 32, y + 27);
  }

  private go(page: SystemPage) {
    this.page = page;
    if (page === "account") this.openAccount(); else this.removeAccountBoxes();
  }

  private systemWindow() {
    switch (this.page) {
      case "main": return this.systemMain();
      case "home": return this.confirmWnd(() => { this.host.returnHome(); this.go("main"); });
      case "logout": return this.confirmWnd(() => { this.systemOpen = false; this.go("main"); this.host.logout(); });
      case "chat": return this.chatWnd();
      case "se": return this.seWnd();
      case "bgm": return this.bgmWnd();
      case "mouse": return this.mouseWnd();
      case "account": return this.accountWnd();
    }
  }

  // ---------------- 个人信息（H5 新增：原版这一项是私服的充值 / 卡密资料）----------------
  // 账号名、注册日期、角色数、有没有安全码（AccountInfo），下面两组表单：旧密码 / 新密码 / 确认 → 修改密码；
  // 目前密码 / 新安全码 → 修改安全码。密码与安全码都只送 MD5；结果（AccountResult）写在聊天栏。
  private account: S2CAccountInfo | null = null;
  private acctBoxes: InputBox[] = [];

  setAccountInfo(m: S2CAccountInfo) { this.account = m; }
  /** 服务端的结果：成功就清空表单 */
  accountResult(ok: boolean) { if (ok) for (const b of this.acctBoxes) b.value = ""; }

  private openAccount() {
    this.removeAccountBoxes();
    this.account = null;
    this.host.accountReq();
    const W = 6, wx = (800 - W * 64) / 2, wy = (600 - 9 * 48) / 2;
    const make = (y: number, max: number) => {
      const b = new InputBox(this.host.canvas, wx + 150, y, 180, 16, { password: true, maxLength: max });
      b.el.addEventListener("keydown", (e) => { e.stopPropagation(); if (e.key === "Escape") this.go("main"); });
      return b;
    };
    this.acctBoxes = [make(wy + 166, 64), make(wy + 192, 64), make(wy + 218, 64), make(wy + 290, 64), make(wy + 316, 32)];
  }

  private removeAccountBoxes() { for (const b of this.acctBoxes) b.remove(); this.acctBoxes = []; }

  private accountWnd() {
    const ui = this.ui, W = 6, H = 9, wx = (800 - W * 64) / 2, wy = (600 - H * 48) / 2;
    this.frameWnd(wx, wy, W, H, CG.WND_TITLE_SYSTEM);
    ui.box(wx + 22, wy + 44, wx + W * 64 - 22, wy + H * 48 - 10, "rgba(0,0,0,0.45)", true);
    const x = wx + 36, a = this.account;
    let y = wy + 56;
    ui.text(x, y, "个人信息", FONT.YELLOW); y += 24;
    if (!a) ui.text(x, y, "读取中…", FONT.GRAY);
    else {
      ui.text(x, y, `账号：${a.account}　　角色：${this.host.status?.name ?? ""}`, FONT.WHITE); y += 20;
      ui.text(x, y, `注册日期：${a.created}　　角色数：${a.chars}　　安全码：${a.hasSecret ? "已设定" : "未设定"}`, FONT.WHITE); y += 20;
    }
    const row = (ry: number, label: string, i: number) => {
      ui.text(x, ry, label, FONT.WHITE);
      ui.box(wx + 146, ry - 3, wx + 334, ry + 17, "rgba(255,255,255,0.35)", false);
      if (ui.hitBox(wx + 146, ry - 3, wx + 334, ry + 17, `acct-f${i}`, false) && ui.clicked(`acct-f${i}`)) this.acctBoxes[i]?.focus();
    };
    const v = (i: number) => this.acctBoxes[i]?.value ?? "";
    ui.text(x, wy + 140, "修改密码", FONT.AQUA);
    row(wy + 166, "目前密码", 0); row(wy + 192, "新 密 码", 1); row(wy + 218, "确认密码", 2);
    if (ui.text(wx + 146, wy + 244, "【 修改密码 】", FONT.GREEN, "acct-pw")) {
      if (v(0).length < 8 || v(1).length < 8 || v(1).length > 64) this.host.chat("密码要 8–64 位。", FONT.RED);
      else if (v(1) !== v(2)) this.host.chat("两次输入的新密码不一样。", FONT.RED);
      else this.host.changePassword(md5(v(0)), md5(v(1)));
    }
    ui.text(x, wy + 266, "修改安全码（找回密码用）", FONT.AQUA);
    row(wy + 290, "目前密码", 3); row(wy + 316, "新安全码", 4);
    if (ui.text(wx + 146, wy + 342, "【 修改安全码 】", FONT.GREEN, "acct-secret")) {
      if (v(3).length < 8) this.host.chat("请输入目前的密码。", FONT.RED);
      else if (v(4).length < 4 || v(4).length > 32) this.host.chat("安全码要 4–32 位。", FONT.RED);
      else this.host.changeSecret(md5(v(3)), md5(v(4)));
    }
    ui.text(x, wy + 372, "改好之后结果显示在聊天栏；密码与安全码不会以明文送出。", FONT.GRAY);
    if (this.item(wx + W * 32 - 28, wy + H * 48 - 36, "返　回", "acct-back", FONT.AQUA)) this.go("main");
  }

  // 原版 _NEW_SYSTEM_MENU 是 7×8 的三栏窗口，大半是私服功能（充值、卡密等）。这里只放实现了的项目，
  // 用同样的行距（34）与栏距（140），窗口缩成 5×7。
  private systemMain() {
    const W = 5, H = 7, wx = (800 - W * 64) / 2, wy = (600 - H * 48) / 2;
    this.frameWnd(wx, wy, W, H, CG.WND_TITLE_SYSTEM);
    const x = wx + 28;
    let y = wy + 54;
    if (this.item(x, y, "    回记录点    ", "sys-home")) this.go("home"); y += 34;
    if (this.item(x, y, "    原地登出    ", "sys-logout")) this.go("logout"); y += 34;
    if (this.item(x, y, "    聊天设定    ", "sys-chat")) this.go("chat"); y += 34;
    if (this.item(x, y, "    背景音乐  ", "sys-bgm")) this.go("bgm"); y += 34;
    if (this.item(x, y, "    音效设定    ", "sys-se")) this.go("se"); y += 34;
    if (this.item(x, y, "    滑鼠设定    ", "sys-mouse")) this.go("mouse"); y += 34;
    if (this.item(x, y, settings.mute ? "    开启声音  " : "    关闭声音  ", "sys-mute")) {
      settings.mute = !settings.mute;
      this.host.chat(settings.mute ? "关闭声音！" : "开启声音！", FONT.RED);
      this.changed();
    }
    y += 34;
    let y2 = wy + 54;
    if (this.item(x + 140, y2, "    个人信息    ", "sys-account")) this.go("account"); y2 += 34;
    if (this.ui.hovered("sys-account")) this.ui.bottomLine = "账号资料，修改密码与安全码。";
    if (this.item(x + 140, y2, settings.hideOthers ? "    人物显示    " : "    人物屏蔽    ", "sys-hide")) {
      settings.hideOthers = !settings.hideOthers;
      this.host.chat(settings.hideOthers ? "屏蔽周边人物！" : "显示周边人物！", FONT.RED);
      this.changed();
    }
    y2 += 34;
    // 战斗设定：自动战斗、快速战斗、自动遇敌的开关与行动计划都在这个窗口里（aisetting.ts）
    if (this.item(x + 140, y2, "    战斗设定  ", "sys-ai")) { this.systemOpen = false; this.ai.show(this.host.status?.name ?? ""); }
    if (this.ui.hovered("sys-ai")) this.ui.bottomLine = "自动战斗、快速战斗、自动遇敌，以及人物与宠物每回合的行动。";
    y2 += 34;

    // GM 指令：只有服务端发了 GM 选单（config.yaml 的 gms）才有
    if (this.gm.enabled && this.item(x + 140, y2, "    GM 指令    ", "sys-gm", FONT.YELLOW)) { this.systemOpen = false; this.gm.show(); }

    if (this.item(x + 70, y, "    关    闭    ", "sys-close", FONT.AQUA)) this.systemOpen = false;
  }

  // systemWndNo 1 / 7：3×3 确认窗，确定 / 不要
  private confirmWnd(yes: () => void) {
    const wx = (800 - 3 * 64) / 2, wy = (600 - 3 * 48) / 2;
    this.frameWnd(wx, wy, 3, 3, CG.WND_TITLE_LOGOUT);
    const x = wx + 38, y = wy + 56;
    if (this.item(x, y, "     确  定     ", "sys-yes")) yes();
    else if (this.item(x, y + 40, "     不  要     ", "sys-no")) this.go("main");
  }

  // systemWndNo 2：聊天设定（原版的"记录文字"即聊天快捷语，还没做，不显示）
  private chatWnd() {
    const wx = (800 - 4 * 64) / 2, wy = (600 - 8 * 48) / 2;
    this.frameWnd(wx, wy, 4, 8, CG.WND_TITLE_CHAT);
    const x = wx + 56;
    let y = wy + 56;
    this.ui.text(x - 16, y, `◆目前显示的行数${String(settings.chatLines).padStart(3)} 行◆`, FONT.YELLOW); y += 32;
    if (this.item(x, y, "      增    加      ", "chat-more")) { settings.chatLines = clamp(settings.chatLines + 1, 0, 20); this.changed(); } y += 32;
    if (this.item(x, y, "      减    少      ", "chat-less")) { settings.chatLines = clamp(settings.chatLines - 1, 0, 20); this.changed(); } y += 32;
    // 这一项用当前颜色显示，悬停时只加框、不变白，否则看不到选的是什么颜色
    const label = "    改变文字颜色    ";
    this.ui.text(x, y, label, FONT_PAL[settings.chatColor]);
    this.ui.hitBox(x - 2, y - 1, x + measure(label) + 2, y + 16, "chat-color");
    if (this.ui.clicked("chat-color")) { settings.chatColor = (settings.chatColor + 1) % FONT_PAL.length; this.changed(); }
    y += 44;
    this.ui.text(x - 16, y, `◆目前的音量${String(settings.voice).padStart(3)} ◆`, FONT.YELLOW); y += 32;
    if (this.item(x, y, "     增    加     ", "voice-more")) { settings.voice = clamp(settings.voice + 1, 1, 10); this.changed(); } y += 32;
    if (this.item(x, y, "     减    少     ", "voice-less")) { settings.voice = clamp(settings.voice - 1, 1, 10); this.changed(); } y += 44;
    if (this.item(x, y, "     回上一页     ", "chat-back", FONT.AQUA)) this.go("main");
    if (this.ui.hovered("voice-more") || this.ui.hovered("voice-less")) this.ui.bottomLine = "音量越大，附近频道的话传得越远；句末加！可临时加大，以...结尾是悄悄话。";
  }

  // systemWndNo 3：音效设定
  private seWnd() {
    const wx = (800 - 4 * 64) / 2, wy = (600 - 6 * 48) / 2;
    this.frameWnd(wx, wy, 4, 6, CG.WND_TITLE_SE);
    const x = wx + 56;
    let y = wy + 64;
    this.ui.text(x - 8, y, `◆  目前的音量${String(settings.seVolume).padStart(3)}   ◆`, FONT.YELLOW); y += 40;
    if (this.item(x, y, "     增     加     ", "se-more")) { this.step("seVolume", 1, 1, 15); } y += 40;
    if (this.item(x, y, "     减     少     ", "se-less")) { this.step("seVolume", -1, 1, 15); } y += 40;
    if (this.item(x, y, settings.stereo ? "       立体声       " : "       单声道       ", "se-stereo")) { settings.stereo = !settings.stereo; this.changed(); }
    y += 52;
    if (this.item(x, y, "      回上一页      ", "se-back", FONT.AQUA)) this.go("main");
  }

  /** 音量加减：到上下限时是 SE 220（原版），否则照常 */
  private step(key: "seVolume" | "bgmVolume", d: number, lo: number, hi: number) {
    const v = clamp(settings[key] + d, lo, hi);
    if (v === settings[key]) { this.host.uiSe(220); return; }
    settings[key] = v;
    this.changed();
  }

  // systemWndNo 4：背景音乐
  private bgmWnd() {
    const wx = (800 - 4 * 64) / 2, wy = (600 - 8 * 48) / 2;
    this.frameWnd(wx, wy, 4, 8, CG.WND_TITLE_BGM);
    const x = wx + 56;
    let y = wy + 64;
    this.ui.text(x - 8, y, `◆  目前的音量 ${String(settings.bgmVolume).padStart(3)}  ◆`, FONT.YELLOW); y += 40;
    if (this.item(x, y, "     增      加     ", "bgm-more")) { this.step("bgmVolume", 1, 1, 15); } y += 40;
    if (this.item(x, y, "     减      少     ", "bgm-less")) { this.step("bgmVolume", -1, 1, 15); } y += 48;
    // 节奏是正在播的那一首的（t_music_bgm_pitch[t_music_bgm_no]）
    const no = this.host.currentBgm(), p = settings.bgmPitches[no] ?? 0;
    const setPitch = (d: number) => {
      const v = clamp(p + d, -8, 8);
      if (v === p || no < 0) { this.host.uiSe(220); return; } // 到底了（原版 SE 220）
      settings.bgmPitches = { ...settings.bgmPitches, [no]: v };
      this.changed();
    };
    this.ui.text(x - 8, y, p === 0 ? "◆ 目前的节奏   0 ◆" : `◆ 目前的节奏 ${(p > 0 ? "+" : "") + p} ◆`, FONT.YELLOW); y += 40;
    if (this.item(x, y, "      加    快      ", "bgm-fast")) setPitch(1); y += 40;
    if (this.item(x, y, "      减    慢      ", "bgm-slow")) setPitch(-1); y += 52;
    if (this.item(x, y, "      回上一页      ", "bgm-back", FONT.AQUA)) this.go("main");
  }

  // systemWndNo 6：滑鼠设定。正常 = 原版游戏光标；平滑 = 系统光标（原版说明"两色的滑鼠游标，反应较快"）
  private mouseWnd() {
    const wx = (800 - 3 * 64) / 2, wy = (600 - 4 * 48) / 2;
    this.frameWnd(wx, wy, 3, 4, CG.WND_TITLE_MOUSE);
    const x = wx + 38;
    let y = wy + 56;
    this.ui.text(x, y, "◆  游标设定  ◆", FONT.YELLOW); y += 40;
    if (this.item(x, y, settings.smoothCursor ? "     平  滑     " : "     正  常     ", "mouse-mode")) { settings.smoothCursor = !settings.smoothCursor; this.changed(); }
    if (this.ui.hovered("mouse-mode")) this.ui.bottomLine = settings.smoothCursor ? "普通的滑鼠游标。" : "两色的滑鼠游标，反应较快。";
    y += 40;
    if (this.item(x, y, "    回上一页    ", "mouse-back", FONT.AQUA)) this.go("main");
  }

  private changed() { saveSettings(); this.host.settingsChanged(); }
}
