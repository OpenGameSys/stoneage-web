// 宠物窗口（原版 menu.cpp MenuToggleFlag & JOY_CTRL_P，Ctrl+P / 任务栏），petWndNo 分三页：
//   0 宠物一览：MakeWindowDisp(4, 4, 272, 320)，底图 CG_PET_WND_VIEW（26044）
//   1 详细资料：MakeWindowDisp(4, 4, 272, 332)，底图 CG_PET_WND_DETAIL（26045），宠物动画 MakeAnimDisp(x + 200, y + 144, graNo, ANIM_DISP_PET)；
//     改名窗口 pActMenuWnd3 = MakeWindowDisp(4, 4 + 280 + 56, 272, 88)，底图 CG_NAME_CHANGE_WND（26049）
//   2 技能一览：MakeWindowDisp(4, 4, 272, 348)，底图 CG_PET_WAZA_WND（26130）+ 每个技能栏一条 CG_PET_WAZA_BAR_1 + i
// 这几个窗口原版都在 x ≤ 320，800 模式下不平移；图号按 directdraw.cpp SetAnimTbl 的 8 位图分支与 anim_tbl.h 求值。
// 编译宏（version.h）：_SHOWPETTRN_、_DROPPETWND、_LIZARDPOSITION 有定义，_NEWFONT_、_PET_SKINS 没有。
// 骑乘（_RIDEPET_）：能骑的宠物（K 字串的 rideflg）忠诚度 100 时，状态钮下方 (x − 27, y + 11) 有骑乘钮（图 55257 不在这套资源里，改用文字），
// 按下送 "R|P|格位"，骑着时再按就下来；骑着的那只状态钮是 CG_PET_WND_RIDE_BTN（btnNo 195），点它就下来并改成休息。
// 邮件宠物（pc.mailPetNo）只记在客户端，宠物邮件窗口（mail.ts）用它。没做的：
// _PETCOM_ 成长计算（图 55218 不在这套资源里）；原版的音效（play_se）也还没有。

import type { PetInfo, PetSkill, S2CPets } from "./proto.gen.ts";
import { ANIM, findAction } from "./assets.ts";
import { FONT, FONT_SIZE, SYS, InputBox, boxColor, measure, type Ui } from "./ui.ts";

const CG = {
  PET_WND_VIEW: 26044, PET_WND_DETAIL: 26045, NAME_CHANGE_WND: 26049, PET_WAZA_WND: 26130, PET_WAZA_BAR_1: 26131,
  CLOSE_BTN: 26042, RETURN_BTN: 26043, OK_BTN: 26093, CANCEL_BTN: 26050, PET_WND_WAZA_BTN: 26046, PET_WND_STATUS_BTN: 26056,
  NAME_CHANGE_BTN: 26058, PREV_BTN: 26180, NEXT_BTN: 26182, ALBUM_WND_SNAP_BTN_UP: 26170,
  PET_WND_REST_BTN: 26052, // + btnNo：0 休息、1 出战、2 待命、3 邮件、195 骑乘
  ATR_ICON_EARTH_BIG: 25120, ATR_ICON_EARTH_SML: 25122, ATR_ICON_WATER_BIG: 25123, ATR_ICON_WATER_SML: 25125,
  ATR_ICON_FIRE_BIG: 25126, ATR_ICON_FIRE_SML: 25128, ATR_ICON_WIND_BIG: 25129, ATR_ICON_WIND_SML: 25131,
};
const PET_SLOTS = 5;
const PET_NAME_LEN = 16;       // pc.h PET_NAME_LEN（半角宽）
const MAX_STANDBY = 4;         // BattlePetStMenCnt < 4（出战的那只也算在内）
const PETSKILL_FIELD_BATTLE = 1; // pc.h：PETSKILL_FIELD_ALL / BATTLE / MAP
const PET_WND_ATTR_X = 159, PET_WND_ATTR_Y = 5;
// _LIZARDPOSITION：龙蜥的图偏低，画高一点
const LIZARDS = [101493, 101494, 101495, 101496];

export interface PetHost {
  ui: Ui;
  canvas: HTMLCanvasElement;          // 改名输入框（InputBox）叠在画布上
  pets(): S2CPets | null;
  setState(slot: number, state: string): void; // lssproto_KS（出战）/ 待命
  rename(slot: number, name: string): void;    // lssproto_KN
  drop(slot: number): void;                    // lssproto_DP，由主程序弹确认框
  ride(slot: number): void;                    // lssproto_FM "R|P|格位"
  todo(name: string): void;                    // 没做的功能
  snap(p: PetInfo): void;                      // 照相（相簿）
  mailPet(): number;                           // 邮件宠物的格位（-1 没有）
  setMailPet(slot: number): void;
  fieldSkill(p: PetInfo, sk: PetSkill): void;  // 技能页点了地图技能（加工等，道具窗口 items.ts 的 startMix）
}

/** GBK 字节数（全角 2、半角 1），原版字串长度都按字节算 */
function bytes(s: string): number { let n = 0; for (const ch of s) n += ch.charCodeAt(0) > 0xff ? 2 : 1; return n; }

/** 按字节宽切行（技能说明每行 24 个半角宽，全角字不切开，同原版 GetStrLastByte 的处理） */
function splitBytes(s: string, max: number): string[] {
  const out: string[] = [];
  let line = "", w = 0;
  for (const ch of s) {
    const cw = ch.charCodeAt(0) > 0xff ? 2 : 1;
    if (w + cw > max) { out.push(line); line = ""; w = 0; }
    line += ch; w += cw;
  }
  if (line) out.push(line);
  return out;
}

export class PetWindow {
  open = false;
  private page: 0 | 1 | 2 = 0;  // petWndNo
  private sel = 0;              // petStatusNo：详细资料 / 技能页看的是哪个格位
  private dir = 1;              // pActPet->anim_ang：MakeAnimDisp 起始朝向 1，点宠物转向
  private animAt = 0;
  private renameBox: InputBox | null = null; // pActMenuWnd3 + petNameChange
  private waiting: S2CPets | null = null;    // BattlePetReceiveFlag：送出后等服务端回新的宠物栏
  private waitAt = 0;
  private host: PetHost;
  readonly wx = 4;
  readonly wy = 4;

  constructor(host: PetHost) { this.host = host; }

  /** Ctrl+P / 任务栏：开关窗口，打开时回到一览（petWndNo = 0） */
  toggle() { if (this.open) this.close(); else { this.open = true; this.page = 0; } }

  close() { this.open = false; this.page = 0; this.closeRename(); }

  /** 离开地图画面（进战斗等）时拿掉 DOM 输入框 */
  leave() { this.closeRename(); }

  private list(): PetInfo[] {
    return (this.host.pets()?.pets ?? []).filter((p) => p.slot >= 0 && p.slot < PET_SLOTS).sort((a, b) => a.slot - b.slot);
  }
  private pet(slot: number): PetInfo | undefined { return this.list().find((p) => p.slot === slot); }

  frame(rects: [number, number, number, number][]) {
    if (!this.open) return;
    if (this.waiting && (this.host.pets() !== this.waiting || performance.now() - this.waitAt > 3000)) this.waiting = null;
    if (this.page !== 0 && !this.pet(this.sel)) {
      // 看着的宠物没了（丢出、交给别人）：换成第一只，一只都没有就回一览
      const first = this.list()[0];
      if (first) this.sel = first.slot; else { this.page = 0; this.closeRename(); }
    }
    if (this.page !== 1) this.closeRename();
    if (this.page === 0) this.viewPage(rects);
    else if (this.page === 1) this.detailPage(rects);
    else this.skillPage(rects);
  }

  // ---------------- 一览（petWndNo 0） ----------------
  // 每只一组两行：名字 (x + 3, y)（CenteringStr 到 16 个半角宽，可点 → 详细资料，右键 → 丢出），
  // "%d转" (x + 122, y)；y += 24 后属性图标 (窗口 x + 228 / 244, y − 16)，状态钮 (x − 27, y − 14)，
  // "%3d     %4d   %4d"（等级、HP、最大 HP）(x + 53, y)；y += 27。x = 窗口 x + 66，y 从窗口 y + 31 起。
  // 文字颜色：休息白、待命水蓝、出战黄。
  private viewPage(rects: [number, number, number, number][]) {
    const ui = this.host.ui, wx = this.wx, wy = this.wy, mx = wx + 136;
    ui.hitBox(wx, wy, wx + 272, wy + 320, "pet-wnd", false);
    rects.push([wx, wy, wx + 272, wy + 320]);
    ui.image(CG.PET_WND_VIEW, mx, wy + 160);
    const pets = this.list();
    const selected = pets.filter((p) => p.state === "standby" || p.state === "battle").length; // BattlePetStMenCnt
    const x = wx + 16 + 50;
    let y = wy + 31;
    for (const p of pets) {
      let color = FONT.WHITE, btnNo = 0;
      if (p.state === "standby") { color = FONT.AQUA; btnNo = 2; }
      if (p.state === "battle") { color = FONT.YELLOW; btnNo = 1; }
      if (p.state === "ride") { color = FONT.YELLOW; btnNo = 195; }
      if (p.state === "mail" || p.slot === this.host.mailPet()) { color = FONT.GREEN; btnNo = 3; } // 邮件宠物（飞出去的也是）
      const nameId = `pet-name${p.slot}`;
      if (this.centerText(x + 3, y, p.freeName || p.name, color, nameId)) { this.sel = p.slot; this.page = 1; this.dir = 1; }
      if (ui.hovered(nameId)) {
        ui.bottomLine = "点名字看详细资料，按右键丢出宠物。";
        // MOUSE_RIGHT_CRICK：丢出（_DROPPETWND 先问"确定要丢出你的宠物吗？"）
        if (ui.mouse.rightClicked && p.state !== "ride") this.host.drop(p.slot); // 骑着的不能丢
      }
      ui.text(x + 122, y, `${p.trans}转`, color); // _SHOWPETTRN_
      y += 24;
      const icons = this.attrIcons(p);
      if (icons[0]) ui.image(icons[0], wx + 228, y - 16);
      if (icons[1]) ui.image(icons[1], wx + 228 + 16, y - 16);
      const btnId = `pet-st${p.slot}`;
      if (ui.image(CG.PET_WND_REST_BTN + btnNo, x - 27, y - 14, btnId) && ui.clicked(btnId)) this.cycle(p, selected);
      const ride = this.host.pets()?.ride ?? -1;
      if (p.rideable && p.loyalty === 100 && (p.slot === ride || ride < 0)) {
        const rideId = `pet-ride${p.slot}`, label = p.slot === ride ? "下骑" : "骑乘";
        if (ui.text(x - 27 - measure(label) / 2, y + 11 - FONT_SIZE / 2, label, FONT.YELLOW, rideId) && !this.waiting) {
          this.waiting = this.host.pets(); this.waitAt = performance.now();
          this.host.ride(p.slot);
        }
      }
      ui.text(x + 26 + 27, y, `${String(p.level).padStart(3)}     ${String(p.hp).padStart(4)}   ${String(p.maxHp).padStart(4)}`, color);
      y += 27;
    }
    if (pets.length === 0) {
      if (ui.image(CG.CLOSE_BTN, mx, wy + 299, "pet-close") && ui.clicked("pet-close")) this.close();
      ui.text(x + 10, y, "你没有宠物", FONT.WHITE);
    } else {
      if (ui.image(CG.CLOSE_BTN, mx + 56, wy + 299, "pet-close") && ui.clicked("pet-close")) this.close();
      // 详细资料钮：看第一只
      if (ui.image(CG.PET_WND_STATUS_BTN, mx - 56, wy + 299, "pet-detail") && ui.clicked("pet-detail")) { this.sel = pets[0].slot; this.page = 1; this.dir = 1; }
    }
  }

  /**
   * 点状态钮（原版的判断顺序，去掉邮件与骑乘）：
   *   出战 → 休息（lssproto_KS −1）
   *   待命 → 没有出战的宠物时出战（HP 为 0 的不行，原版播错误音、不变）；已经有出战的 → 休息
   *   休息 → 待命（BattlePetStMenCnt < 4，出战的也算；满了原版是改成邮件宠物，这里不变）
   */
  private cycle(p: PetInfo, selected: number) {
    if (this.waiting || p.state === "mail") return; // BattlePetReceiveFlag：上一次还没回来；飞出去的邮件宠物不能改
    // 邮件宠物（pc.mailPetNo，只在客户端）：邮件 → 休息；休息而待命满了 → 邮件（只能一只）
    if (p.slot === this.host.mailPet()) { this.host.setMailPet(-1); return; }
    if (p.state === "rest" && selected >= MAX_STANDBY) { this.host.setMailPet(p.slot); return; }
    const hasBattle = this.list().some((q) => q.state === "battle");
    let next = "";
    if (p.state === "battle" || p.state === "ride") next = "rest"; // 骑着的：先下来（"R|P|-1"）再休息，服务端一起做
    else if (p.state === "standby") {
      if (!hasBattle) { if (p.hp > 0) next = "battle"; /* else play_se(220) 错误音 */ }
      else next = "rest";
    } else if (selected < MAX_STANDBY) next = "standby";
    if (!next) return;
    this.waiting = this.host.pets();
    this.waitAt = performance.now();
    this.host.setState(p.slot, next);
  }

  private attrIcons(p: PetInfo): number[] {
    const out: number[] = [];
    const tbl: [number, number][] = [[CG.ATR_ICON_EARTH_BIG, CG.ATR_ICON_EARTH_SML], [CG.ATR_ICON_WATER_BIG, CG.ATR_ICON_WATER_SML],
      [CG.ATR_ICON_FIRE_BIG, CG.ATR_ICON_FIRE_SML], [CG.ATR_ICON_WIND_BIG, CG.ATR_ICON_WIND_SML]];
    tbl.forEach(([big, sml], i) => { const v = p.attrs[i] ?? 0; if (v > 0) out.push(v > 50 ? big : sml); });
    return out;
  }

  /** CenteringStr(name, moji, PET_NAME_LEN)：名字在 16 个半角宽里居中；给了 id 时整个 16 格宽都可点（StockFontBuffer hitFlag 2） */
  private centerText(x: number, y: number, text: string, color: string, id?: string): boolean {
    const ui = this.host.ui, w = PET_NAME_LEN * 7;
    let s = text;
    while (bytes(s) > PET_NAME_LEN) s = s.slice(0, -1);
    const tx = x + Math.max(0, (w - measure(s)) / 2);
    if (id) {
      ui.hitBox(x - 2, y - 2, x + w + 2, y + FONT_SIZE + 2, id, false);
      if (ui.hovered(id)) ui.box(x - 3, y - 4, x + w + 2, y + FONT_SIZE + 4, boxColor(), false);
    }
    ui.text(tx, y, s, color);
    return id ? ui.clicked(id) : false;
  }

  /** 上一只 / 下一只（while 循环找下一个有宠物的格位） */
  private step(d: number) {
    for (let i = 1; i <= PET_SLOTS; i++) {
      const s = (this.sel + d * i + PET_SLOTS * 2) % PET_SLOTS;
      if (this.pet(s)) { this.sel = s; return; }
    }
  }

  // ---------------- 详细资料（petWndNo 1） ----------------
  // x = 窗口 x + 20、y = 窗口 y + 35：名字 (x − 7, y)、"[%d转]" (x + 108, y) 绿字；改名钮 (x + 66, y + 31)；
  // 之后每行 24：等级 "%3d" (x + 50)、经验 / 下一级 "%8d" (x + 48)、HP "%4d" (x + 46) / (x + 93)、
  // 攻击 / 防御 / 敏捷 / 忠诚度 "%3d" (x + 50)，这四行右边是地水火风的属性条；再空 12：照相钮 (x + 52)、技能钮 (x + 178)；
  // 再 31：上一只 (x + 40)、下一只 (x + 84)、返回 (x + 170)。
  private detailPage(rects: [number, number, number, number][]) {
    const ui = this.host.ui, wx = this.wx, wy = this.wy, p = this.pet(this.sel);
    if (!p) return;
    ui.hitBox(wx, wy, wx + 272, wy + 332, "pet-wnd", false);
    rects.push([wx, wy, wx + 272, wy + 332]);
    ui.image(CG.PET_WND_DETAIL, wx + 136, wy + 166);
    this.drawPet(p);
    const x = wx + 20;
    let y = wy + 35;
    const num = (v: number, w: number) => String(v).padStart(w, " ");
    this.centerText(x - 7, y, p.freeName || p.name, FONT.WHITE);
    y += 24;
    ui.text(x + 108, y - 24, `[${p.trans}转]`, FONT.GREEN); // _SHOWPETTRN_，FONT_PAL 5
    // 改名钮：按着时换成按下的图（CG_NAME_CHANGE_BTN + petWndBtnFlag[3]）；不是自己捕获的不能改名（原版播错误音）
    if (ui.button(CG.NAME_CHANGE_BTN, x + 66, y + 7, "pet-rename", CG.NAME_CHANGE_BTN + 1)) {
      if (p.canRename) { if (this.renameBox) this.closeRename(); else this.openRename(); }
    }
    y += 23;
    ui.text(x + 50, y, num(p.level, 3), FONT.WHITE); y += 24;
    ui.text(x + 48, y, num(p.exp, 8), FONT.WHITE); y += 24;
    ui.text(x + 48, y, num(p.nextExp, 8), FONT.WHITE); y += 24;
    ui.text(x + 46, y, num(p.hp, 4), FONT.WHITE);
    ui.text(x + 48 + 45, y, num(p.maxHp, 4), FONT.WHITE); y += 24;
    const bars: [string, string][] = [[SYS.GREEN, SYS.GREEN2], [SYS.AQUA, SYS.AQUA2], [SYS.RED, SYS.RED2], [SYS.YELLOW, SYS.YELLOW2]];
    [p.atk, p.def, p.agi, p.loyalty].forEach((v, i) => {
      const a = p.attrs[i] ?? 0;
      if (a > 0) {
        // 属性条长度 = 属性值 × 0.8，三层方框做出立体感（StockBoxDispBuffer）
        const bx = x + PET_WND_ATTR_X, by = y + PET_WND_ATTR_Y, w = Math.floor(a * 0.8);
        ui.box(bx + 2, by + 2, bx + 2 + w + 1, by + 11, bars[i][1], false);
        ui.box(bx + 1, by + 1, bx + 1 + w + 1, by + 10, bars[i][1], false);
        ui.box(bx, by, bx + w, by + 8, bars[i][0], true);
      }
      ui.text(x + 50, y, num(v, 3), FONT.WHITE); y += 24;
    });
    y += 12;
    if (ui.button(CG.ALBUM_WND_SNAP_BTN_UP, x + 52, y, "pet-snap", CG.ALBUM_WND_SNAP_BTN_UP + 1)) this.host.snap(p);
    if (ui.image(CG.PET_WND_WAZA_BTN, x + 178, y, "pet-waza") && ui.clicked("pet-waza")) { this.page = 2; this.closeRename(); }
    y += 31;
    if (ui.image(CG.RETURN_BTN, x + 100 + 70, y, "pet-return") && ui.clicked("pet-return")) { this.page = 0; this.closeRename(); }
    if (ui.button(CG.PREV_BTN, x + 16 + 16 + 8, y, "pet-prev", CG.PREV_BTN + 1)) { this.closeRename(); this.step(-1); }
    if (ui.button(CG.NEXT_BTN, x + 50 + 16 + 18, y, "pet-next", CG.NEXT_BTN + 1)) { this.closeRename(); this.step(1); }
    if (this.renameBox) this.renameWindow(rects);
  }

  /** ANIM_DISP_PET：站立动画循环播放（pattern ANM_NOMAL_SPD），点一下宠物转一个方向（anim_ang++） */
  private drawPet(p: PetInfo) {
    const ui = this.host.ui, now = performance.now();
    const lizard = LIZARDS.includes(p.sprite);
    const px = this.wx + (lizard ? 220 : 200), py = this.wy + (lizard ? 124 : 144);
    if (p.sprite < 100000) { const idx = ui.assets.tile(p.sprite); if (idx !== undefined) ui.image(idx, px, py, undefined, true); return; }
    const def = ui.assets.anim(p.sprite);
    const act = def && findAction(def, this.dir, ANIM.STAND);
    if (!act || act.frames.length === 0) return;
    const ms = Math.max(1, act.ticks) * (1000 / 60);
    const [img, fx, fy] = act.frames[Math.floor((now - this.animAt) / ms) % act.frames.length];
    if (img < 0) return;
    if (ui.image(img, px + fx, py + fy, "pet-anim", true, false) && ui.clicked("pet-anim")) { this.dir = (this.dir + 1) % 8; this.animAt = now; }
  }

  // ---------------- 改名窗口（pActMenuWnd3） ----------------
  // 输入行 petNameChange (窗口 x + 22, y + 25)，长度 16 个字节；OK (x + 80, y + 67)、取消 (x + 190, y + 67)。
  // 按 OK 或 Enter 等于 KeyboardReturn：关掉窗口、lssproto_KN_send(petStatusNo, 输入的字)；空字串由服务端改回原名。
  private openRename() {
    const x = this.wx + 22, y = 4 + 280 + 56 + 25;
    const box = new InputBox(this.host.canvas, x, y, PET_NAME_LEN * 7 + 14, 16, { maxLength: PET_NAME_LEN });
    // 输入框的按键不往下传（Enter 会打开聊天、Esc 会开系统窗口）
    box.el.addEventListener("keydown", (e) => {
      e.stopPropagation();
      if (e.ctrlKey && ["s", "i", "m", "p"].includes(e.key.toLowerCase())) e.preventDefault(); // 不让浏览器"保存网页""打印"
      if (e.key === "Enter") this.submitRename();
      else if (e.key === "Escape") this.closeRename();
    });
    this.renameBox = box;
    setTimeout(() => box.focus(), 0);
  }

  private closeRename() { this.renameBox?.remove(); this.renameBox = null; }

  private submitRename() {
    const box = this.renameBox;
    if (!box) return;
    let name = box.value.trim();
    while (bytes(name) > PET_NAME_LEN) name = name.slice(0, -1);
    const slot = this.sel;
    this.closeRename();
    this.host.rename(slot, name);
  }

  private renameWindow(rects: [number, number, number, number][]) {
    const ui = this.host.ui, wx = this.wx, wy = 4 + 280 + 56;
    ui.hitBox(wx, wy, wx + 272, wy + 88, "pet-rename-wnd", false);
    rects.push([wx, wy, wx + 272, wy + 88]);
    ui.image(CG.NAME_CHANGE_WND, wx + 136, wy + 44);
    const x = wx + 20, y = wy + 60;
    if (ui.image(CG.OK_BTN, x + 60, y + 7, "pet-rename-ok") && ui.clicked("pet-rename-ok")) this.submitRename();
    if (ui.image(CG.CANCEL_BTN, x + 170, y + 7, "pet-rename-cancel") && ui.clicked("pet-rename-cancel")) this.closeRename();
  }

  // ---------------- 技能一览（petWndNo 2） ----------------
  // 名字 (窗口 x + 12, y + 32)；技能每格 25，从 (窗口 x + 15, y + 58) 起，"       %-22s"（可点）：只能在战斗用的灰字、其他白字；
  // 鼠标停在技能上时说明从 (窗口 x + 32, y + 252) 起每行 24 个半角宽、行距 20；上一只 (x + 40)、下一只 (x + 84)、返回 (x + 170)，在 y + 330。
  private skillPage(rects: [number, number, number, number][]) {
    const ui = this.host.ui, wx = this.wx, wy = this.wy, p = this.pet(this.sel);
    if (!p) return;
    const mx = wx + 136, my = wy + 174;
    ui.hitBox(wx, wy, wx + 272, wy + 348, "pet-wnd", false);
    rects.push([wx, wy, wx + 272, wy + 348]);
    ui.image(CG.PET_WAZA_WND, mx, my);
    for (let i = 0; i < Math.min(p.maxSkill, 7); i++) ui.image(CG.PET_WAZA_BAR_1 + i, mx, my);
    let x = wx + 40, y = wy + 32;
    this.centerText(x - 28, y, p.freeName || p.name, FONT.WHITE);
    y += 26; x += 18;
    let hover: PetSkill | undefined;
    for (let i = 0; i < p.maxSkill; i++) {
      const sk = p.skills.find((s) => s.slot === i);
      if (sk) {
        const id = `pet-skill${i}`;
        // 地图技能（加工、修复等）点了配合道具窗口里右键标记的道具使用
        const field = sk.field !== PETSKILL_FIELD_BATTLE;
        if (ui.text(x - 43, y, `       ${sk.name}${" ".repeat(Math.max(0, 22 - bytes(sk.name)))}`, field ? FONT.WHITE : FONT.GRAY, id) && field) this.host.fieldSkill(p, sk);
        if (ui.hovered(id)) hover = sk;
      }
      y += 25;
    }
    if (hover) splitBytes(hover.memo.trim(), 24).forEach((l, i) => ui.text(wx + 32, wy + 252 + i * 20, l, FONT.WHITE));
    x = wx + 20; y = wy + 330;
    if (ui.image(CG.RETURN_BTN, x + 100 + 70, y, "pet-return") && ui.clicked("pet-return")) this.page = 1;
    if (ui.button(CG.PREV_BTN, x + 16 + 16 + 8, y, "pet-prev", CG.PREV_BTN + 1)) this.step(-1);
    if (ui.button(CG.NEXT_BTN, x + 50 + 16 + 18, y, "pet-next", CG.NEXT_BTN + 1)) this.step(1);
  }
}
