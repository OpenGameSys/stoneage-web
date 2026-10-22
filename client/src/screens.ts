// 登录前的各个画面，按原客户端 login.cpp 的画面与流程实现，图都是原版资源：
//   输入账号密码（idPasswordProc / inputIdPassword）
//   选择大区与线路（titleProc / selectGroup / selectServer）
//   角色选择（selectCharacterProc）
//   创建角色：选形象与颜色（selCharGraNo / selCharGraColorWin）→ 设定（editCharParam）→ 出生地（selHomeTown）
// 原版 640×480 坐标的画面统一加 OFFSET_640 放到 800×600 中央。

import { md5 } from "./md5.ts";
import { Actor } from "./field.ts";
import type { Connection } from "./net.ts";
import type { CharSummary, ServerGroup } from "./proto.gen.ts";
import { FONT, InputBox, OFFSET_640, SYS, measure, type Ui } from "./ui.ts";

const [OX, OY] = OFFSET_640;

// 原版图号（anim_tbl.h，按 VER25_RELEASE 的宏求值，见 h5/tools/macros.mjs）
export const CG = {
  MOUSE_CURSOR: 25000, GRID_CURSOR: 25001,
  TITLE: 29025, ID_PASS: 26301, ID_PASS_OK: 26302, ID_PASS_QUIT: 26303,
  CHR_SEL_BG: 29032, CHR_SEL_LOGIN_BTN: 29033, CHR_SEL_NEW_BTN: 29034, CHR_SEL_DEL_BTN: 29035, CHR_SEL_BACK_BTN: 29036,
  CHR_MAKE_SEL_BG: 29030, CHR_MAKE_BG: 29031, CHR_MAKE_SEL2_BG: 29039, CHR_MAKE_OK_BTN: 29037, CHR_MAKE_BACK_BTN: 29038,
  CHR_MAKE_HOME_NAME0: 29040, CHR_MAKE_EYE_SEL: 29044, CHR_MAKE_NOSE_SEL: 29045, CHR_MAKE_FACE: 30000,
  PREV_BTN: 26180, PREV_BTN_DOWN: 26181, NEXT_BTN: 26182, NEXT_BTN_DOWN: 26183,
  COMMON_WIN_YORO: 26090, COMMON_YES_BTN: 26091, COMMON_NO_BTN: 26092,
};

export const HOMETOWNS = ["萨姆吉尔村", "玛丽娜丝村", "加加村", "卡鲁它那村"];

/** 各画面共享的应用状态与操作（由 main.ts 实现） */
export interface App {
  ui: Ui;
  canvas: HTMLCanvasElement;
  net: Connection | null;
  connect(): Promise<Connection>;
  disconnect(): void;
  go(screen: Screen): void;
  message(text: string, then?: () => void): void;      // 原版 commonMsgWin
  confirm(text: string, then: (yes: boolean) => void): void; // 原版 commonYesNoWindow
  groups: ServerGroup[];
  lineName: string;
  chars: CharSummary[];
  maxChars: number;
  enterGame(charId: number): void;
}

export interface Screen {
  enter?(): void;
  leave?(): void;
  frame(now: number): void;
}

// ---------------------------------------------------------------------------
// 输入账号密码。这一版的面板图（26301「失落的世界」）与代码里 2.5 版的坐标不同，热区按面板图本身的位置。

export class LoginScreen implements Screen {
  private app: App;
  private id!: InputBox;
  private pass!: InputBox;
  private busy = false;
  // H5 新增的两个对话框（原版账号由官网注册、找回密码也在官网）：注册（账号 / 密码 / 确认密码 / 安全码）、找回密码（账号 / 安全码 / 新密码 / 确认）
  private dialog: "register" | "reset" | null = null;
  private fields: InputBox[] = [];
  constructor(app: App) { this.app = app; }

  enter() {
    this.id = new InputBox(this.app.canvas, 456, 154, 92, 16, { maxLength: 16 });
    this.pass = new InputBox(this.app.canvas, 456, 194, 92, 16, { password: true, maxLength: 64 });
    for (const box of [this.id, this.pass]) box.el.addEventListener("keydown", (e) => {
      if (e.key === "Enter") { e.preventDefault(); if (box === this.id) this.pass.focus(); else this.submit("Login"); }
      if (e.key === "Tab") { e.preventDefault(); (box === this.id ? this.pass : this.id).focus(); }
    });
    try { this.id.value = localStorage.getItem("sa-account") ?? ""; } catch {}
    setTimeout(() => (this.id.value ? this.pass : this.id).focus(), 0);
  }

  leave() { this.id.remove(); this.pass.remove(); this.closeDialog(); }

  frame() {
    const ui = this.app.ui;
    ui.image(CG.ID_PASS, 400, 300);
    if (this.dialog) { this.dialogFrame(); return; }
    // 输入框所在的木框，点击时把焦点给对应输入框
    if (ui.hitBox(452, 150, 552, 172, "id", false) && ui.clicked("id")) this.id.focus();
    if (ui.hitBox(452, 190, 552, 212, "pass", false) && ui.clicked("pass")) this.pass.focus();
    // OK / Quit：悬停时显示原版的高亮图（两张图共用同一个锚点）
    const ok = ui.hitBox(400, 296, 480, 395, "ok", false), quit = ui.hitBox(495, 310, 575, 390, "quit", false);
    if (ok) { ui.image(CG.ID_PASS_OK, 401, 306); ui.bottomLine = "进入游戏。"; }
    if (quit) { ui.image(CG.ID_PASS_QUIT, 401, 306); ui.bottomLine = "结束游戏。"; }
    if (ui.clicked("ok")) this.submit("Login");
    if (ui.clicked("quit")) { this.id.value = ""; this.pass.value = ""; this.id.focus(); }
    // H5 新增：原版账号由官网注册，这里提供注册与找回密码的入口
    // 两个连结在账号 / 密码木框（x 452..552，中心 502）正下方居中，各占一行
    const cx = 502;
    if (ui.textButton(cx - measure("注册新账号") / 2, 224, "注册新账号", FONT.YELLOW, "register")) this.openDialog("register");
    if (ui.hovered("register")) ui.bottomLine = "填写账号、密码与安全码注册新账号。";
    if (ui.textButton(cx - measure("找回密码") / 2, 246, "找回密码", FONT.AQUA, "reset")) this.openDialog("reset");
    if (ui.hovered("reset")) ui.bottomLine = "用注册时填的安全码重设密码。";
  }

  // ---------------- 注册 / 找回密码对话框 ----------------
  private static readonly DLG_X = 208; private static readonly DLG_Y = 150; // 6×5 格的窗口（384×240）
  private static readonly ROWS: Record<"register" | "reset", { label: string; password: boolean; max: number }[]> = {
    register: [{ label: "账　　号", password: false, max: 16 }, { label: "密　　码", password: true, max: 64 },
      { label: "确认密码", password: true, max: 64 }, { label: "安 全 码", password: true, max: 32 }],
    reset: [{ label: "账　　号", password: false, max: 16 }, { label: "安 全 码", password: true, max: 32 },
      { label: "新 密 码", password: true, max: 64 }, { label: "确认密码", password: true, max: 64 }],
  };

  private openDialog(kind: "register" | "reset") {
    this.closeDialog();
    this.dialog = kind;
    this.id.el.style.display = "none"; this.pass.el.style.display = "none"; // 登录页的输入框先藏起来
    const x = LoginScreen.DLG_X, y = LoginScreen.DLG_Y;
    this.fields = LoginScreen.ROWS[kind].map((r, i) => new InputBox(this.app.canvas, x + 120, y + 66 + i * 32, 200, 16, { password: r.password, maxLength: r.max }));
    this.fields[0].value = this.id.value.trim();
    this.fields.forEach((box, i) => box.el.addEventListener("keydown", (e) => {
      if (e.key === "Enter") { e.preventDefault(); if (i + 1 < this.fields.length) this.fields[i + 1].focus(); else this.submitDialog(); }
      if (e.key === "Tab") { e.preventDefault(); this.fields[(i + 1) % this.fields.length].focus(); }
      if (e.key === "Escape") { e.preventDefault(); this.closeDialog(); }
    }));
    setTimeout(() => (this.fields[0].value ? this.fields[1] : this.fields[0]).focus(), 0);
  }

  /** 对话框里弹提示：提示窗画在画布上、输入框是 DOM，会盖在提示上面，所以先藏起来，按掉提示再显示 */
  private msgOpen = false; // 提示窗开着：对话框整个不画（只留暗幕），免得把提示窗盖住
  private say(text: string) {
    for (const f of this.fields) f.el.style.display = "none";
    this.msgOpen = true;
    this.app.message(text, () => { this.msgOpen = false; for (const f of this.fields) f.el.style.display = ""; });
  }

  private closeDialog() {
    for (const f of this.fields) f.remove();
    this.fields = []; this.dialog = null;
    if (this.id) { this.id.el.style.display = ""; this.pass.el.style.display = ""; }
  }

  private dialogFrame() {
    const ui = this.app.ui, kind = this.dialog!, x = LoginScreen.DLG_X, y = LoginScreen.DLG_Y;
    ui.hitBox(0, 0, 800, 600, "dlg-block", false); // 挡住后面登录页的点击
    ui.box(0, 0, 800, 600, "rgba(0,0,0,0.45)", true);
    if (this.msgOpen) return; // 提示窗（main.ts 的 modal）画在最上面，这时对话框不画
    ui.window(x, y, 6, 5, "login-dlg");
    ui.box(x + 22, y + 10, x + 362, y + 230, "rgba(0,0,0,0.55)", true);
    const title = kind === "register" ? "注册新账号" : "找回密码";
    ui.text(x + (384 - measure(title)) / 2, y + 22, title, FONT.YELLOW);
    LoginScreen.ROWS[kind].forEach((r, i) => {
      const ry = y + 66 + i * 32;
      ui.text(x + 40, ry, r.label, FONT.WHITE);
      ui.box(x + 116, ry - 2, x + 324, ry + 18, "rgba(255,255,255,0.35)", false);
      if (ui.hitBox(x + 116, ry - 2, x + 324, ry + 18, `dlg-f${i}`, false) && ui.clicked(`dlg-f${i}`)) this.fields[i].focus();
    });
    const hint = kind === "register" ? "密码 8–64 位；安全码 4–32 位，找回密码时要用，请记好。" : "安全码连错几次会暂时锁住。";
    ui.text(x + 24, y + 196, hint, FONT.GRAY);
    const okLabel = kind === "register" ? "注　册" : "重　设";
    if (ui.textButton(x + 110, y + 214, okLabel, FONT.YELLOW, "dlg-ok")) this.submitDialog();
    if (ui.textButton(x + 230, y + 214, "取　消", FONT.AQUA, "dlg-cancel")) this.closeDialog();
  }

  private async submitDialog() {
    if (this.busy || !this.dialog) return;
    const kind = this.dialog, v = this.fields.map((f) => f.value);
    const account = v[0].trim();
    const check = (password: string, confirm: string, secret: string): string | null => {
      if (!account) return "请输入账号！";
      if (!/^[A-Za-z0-9_]{3,16}$/.test(account)) return "账号为 3–16 位字母、数字或下划线。";
      if (password.length < 8 || password.length > 64) return "密码要 8–64 位。";
      if (password !== confirm) return "两次输入的密码不一样。";
      if (secret.length < 4 || secret.length > 32) return "安全码要 4–32 位。";
      return null;
    };
    const err = kind === "register" ? check(v[1], v[2], v[3]) : check(v[2], v[3], v[1]);
    if (err) { this.say(err); return; }
    this.busy = true;
    try {
      const net = await this.app.connect();
      if (kind === "register") {
        net.send({ t: "Register", account, password: md5(v[1]), secret: md5(v[3]) }); // 密码与安全码都只送 MD5
        const r = await net.once("AuthResult");
        if (!r.ok) { this.say(r.message); return; }
        this.closeDialog();
        this.id.value = account; this.pass.value = "";
        try { localStorage.setItem("sa-account", account); } catch {}
        const list = await net.once("ServerList");
        this.app.groups = list.groups;
        this.app.go(new LineScreen(this.app));
      } else {
        net.send({ t: "ResetPassword", account, secret: md5(v[1]), password: md5(v[2]) });
        const r = await net.once("AuthResult");
        if (r.message.includes("已重设")) {
          this.closeDialog(); this.id.value = account; this.pass.value = "";
          this.app.message(r.message, () => this.pass.focus());
        } else this.say(r.message);
      }
    } catch (e) {
      this.say((e as Error).message);
    } finally {
      this.busy = false;
    }
  }

  private async submit(kind: "Login") {
    if (this.busy) return;
    const account = this.id.value.trim(), password = this.pass.value;
    if (!account || !password) { this.app.message("请输入账号和密码！"); return; }
    if (password.length < 8 || password.length > 64) { this.app.message("密码要 8–64 位。"); return; }
    this.busy = true;
    try {
      const net = await this.app.connect();
      net.send({ t: kind, account, password: md5(password) }); // 只送 MD5，服务端只存、只比对这个值
      const r = await net.once("AuthResult");
      if (!r.ok) { this.app.message(r.message); return; }
      try { localStorage.setItem("sa-account", account); } catch {}
      const list = await net.once("ServerList");
      this.app.groups = list.groups;
      this.app.go(new LineScreen(this.app));
    } catch (e) {
      this.app.message((e as Error).message);
    } finally {
      this.busy = false;
    }
  }
}

// ---------------------------------------------------------------------------
// 选择大区与线路：标题背景上的九宫格窗口，每列 4 项，黄色文字，最后一项是「回上一页」。

export class LineScreen implements Screen {
  private app: App;
  private group = -1;
  private busy = false;
  constructor(app: App) { this.app = app; }

  frame() {
    const ui = this.app.ui, groups = this.app.groups;
    ui.image(CG.TITLE, 400, 300);
    const items = this.group < 0 ? groups.map((g) => g.name) : groups[this.group].lines.map((l) => l.name);
    const counts = this.group < 0 ? [] : groups[this.group].lines.map((l) => l.online);
    const rows = (items.length >> 2) + 1;
    // MakeWindowDisp 的 800×600 修正：x > 40 时右移 DISPLACEMENT_X/2，y > 40 时下移 DISPLACEMENT_Y/2
    const x = [256, 192, 128, 64][Math.min(rows, 4) - 1] + 80, y = 270 + 60;
    ui.window(x, y, rows * 2, 3);
    const tx = x + 22, ty = y + 28;
    items.forEach((name, i) => {
      const ix = tx + (i >> 2) * 128, iy = ty + (i % 4) * 25;
      if (ui.textButton(ix, iy, name, FONT.YELLOW, `item${i}`)) this.pick(i);
      if (this.group >= 0) ui.text(ix + 70, iy, `${counts[i]}人`, FONT.GREEN);
    });
    if (ui.textButton(tx + (items.length >> 2) * 128, ty + 75, "  回上一页  ", FONT.YELLOW, "back")) {
      if (this.group < 0) { this.app.disconnect(); this.app.go(new LoginScreen(this.app)); }
      else this.group = -1;
    }
    ui.bottomLine = this.group < 0 ? "请选择大区。" : "请选择线路。";
  }

  private async pick(i: number) {
    if (this.group < 0) { this.group = i; return; }
    if (this.busy || !this.app.net) return;
    this.busy = true;
    const net = this.app.net, line = this.app.groups[this.group].lines[i];
    net.send({ t: "SelectLine", id: line.id });
    const sel = await net.once("LineSelected");
    this.app.lineName = sel.name;
    net.send({ t: "CharList" });
    await net.once("CharListResult");
    this.busy = false;
    this.app.go(new CharSelectScreen(this.app));
  }
}

// ---------------------------------------------------------------------------
// 角色选择：两个栏位，显示头像、名字、等级、耐久/攻击/防御/敏捷/魅力、DP、四属性条、登入次数。

const ATTR_COLORS: [string, string][] = [[SYS.GREEN, SYS.GREEN2], [SYS.AQUA, SYS.AQUA2], [SYS.RED, SYS.RED2], [SYS.YELLOW, SYS.YELLOW2]];

function attrBar(ui: Ui, x1: number, y1: number, value: number, color: [string, string]) {
  if (value <= 0) return;
  const x2 = x1 + value * 8, y2 = y1 + 8;
  ui.box(x1 + 2, y1 + 2, x2 + 2, y2 + 2, color[1], true);
  ui.box(x1 + 1, y1 + 1, x2 + 1, y2 + 1, color[1], true);
  ui.box(x1, y1, x2, y2, color[0], true);
}

/** 原版 sprintf("%3d") 右对齐：每个半角字符约 7 像素 */
function num(ui: Ui, x: number, y: number, v: number, width: number) {
  const s = String(v);
  ui.text(x + (width - s.length) * 7, y, s, FONT.WHITE);
}

// 一页两个角色位（原版的画面）；服务端的 maxChars 超过 2 时可以翻页（额外新功能，原版固定 2 个），翻到哪一页记着，建完角色回来还在那页。
let charPage = 0;

export class CharSelectScreen implements Screen {
  private app: App;
  constructor(app: App) { this.app = app; }

  frame() {
    const ui = this.app.ui, ix = OX, iy = OY;
    ui.image(CG.CHR_SEL_BG, 400, 300);
    const pages = Math.max(1, Math.ceil(Math.max(this.app.maxChars, this.app.chars.length) / 2));
    charPage = Math.min(charPage, pages - 1);
    if (pages > 1) {
      // 翻页：箭头放在背景图两侧的黑边上（借用宠物窗口的上一只 / 下一只钮），页数写在背景图上方
      if (charPage > 0 && ui.button(CG.PREV_BTN, ix - 30, 300, "chr-prev", CG.PREV_BTN + 1)) charPage--;
      if (charPage < pages - 1 && ui.button(CG.NEXT_BTN, 800 - ix + 30, 300, "chr-next", CG.NEXT_BTN + 1)) charPage++;
      if (ui.hovered("chr-prev") || ui.hovered("chr-next")) ui.bottomLine = "翻到别的角色位。";
      const label = `角色位 第 ${charPage + 1} / ${pages} 页`;
      ui.text(400 - measure(label) / 2, iy - 30, label, FONT.YELLOW);

    }
    for (let i = 0; i < 2; i++) {
      const k = charPage * 2 + i;
      const c = this.app.chars[k], px = i * 304 + ix;
      if (c) {
        ui.image(c.face, 169 + px, 84 + iy);
        const w = measure(c.name);
        ui.text(93 + px + (144 - w) / 2, 127 + iy, c.name, FONT.WHITE);
        num(ui, 104 + px, 152 + iy, c.level, 3);
        num(ui, 104 + px, 174 + iy, c.hp, 3);
        num(ui, 104 + px, 196 + iy, c.atk, 3);
        num(ui, 104 + px, 218 + iy, c.def, 3);
        num(ui, 104 + px, 240 + iy, c.agi, 3);
        num(ui, 104 + px, 262 + iy, c.charm, 3);
        num(ui, 188 + px, 262 + iy, c.dp, 8);
        c.attrs.forEach((a, j) => attrBar(ui, 208 + px, 177 + j * 22 + iy, a, ATTR_COLORS[j]));
        num(ui, 156 + px, 284 + iy, Math.min(c.logins, 9999), 4);
        if (ui.button(CG.CHR_SEL_LOGIN_BTN, 100 + px, 342 + iy, `login${i}`)) this.app.enterGame(c.id);
        if (ui.button(CG.CHR_SEL_DEL_BTN, 230 + px, 342 + iy, `del${i}`))
          this.app.confirm(`确定要删除「${c.name}」吗？`, (yes) => { if (yes) this.app.net?.send({ t: "CharDelete", id: c.id }); });
        if (ui.hovered(`login${i}`)) ui.bottomLine = "用这个人物进行游戏。";
        if (ui.hovered(`del${i}`)) ui.bottomLine = "删除这个人物。";
      } else if (k < this.app.maxChars) {

        if (ui.button(CG.CHR_SEL_NEW_BTN, 160 + px, 342 + iy, `new${i}`)) this.app.go(new MakeSelectScreen(this.app));
        if (ui.hovered(`new${i}`)) ui.bottomLine = "制作新的人物。";
      }
    }
    if (ui.button(CG.CHR_SEL_BACK_BTN, 320 + ix, 432 + iy, "back")) this.app.go(new LineScreen(this.app));
    if (ui.hovered("back")) ui.bottomLine = "回到线路选择。";
    ui.text(8, 8, this.app.lineName, FONT.YELLOW);
  }
}

// ---------------------------------------------------------------------------
// 创建角色 1：在村子背景上选 12 种形象之一，再用小窗口改颜色、转方向。

const SEL_SPRITES = [100000, 100020, 100040, 100060, 100080, 100100, 100120, 100140, 100160, 100180, 100200, 100220];
// selectGraLocate（_NEW_WIN_POS_，已是 800×600 坐标）与 selectGraHitArea
const SEL_POS = [[305, 290], [240, 360], [410, 500], [330, 400], [200, 240], [480, 430], [560, 410], [600, 340], [500, 310], [380, 240], [460, 200], [280, 200]];
const SEL_HIT = [[-32, -56, 63, 65], [-21, -72, 42, 88], [-19, -78, 38, 94], [-20, -67, 41, 80], [-30, -80, 60, 100], [-36, -84, 72, 100],
  [-30, -51, 59, 64], [-19, -77, 40, 88], [-18, -72, 34, 83], [-22, -72, 43, 89], [-18, -78, 35, 90], [-20, -76, 40, 90]];

interface MakeState { body: number; color: number[]; dir: number[]; }

export class MakeSelectScreen implements Screen {
  private app: App;
  private state: MakeState;
  private actors: Actor[];
  private selected = -1;
  constructor(app: App, state?: MakeState) {
    this.app = app;
    this.state = state ?? { body: -1, color: Array(12).fill(0), dir: Array(12).fill(0) };
    this.actors = SEL_SPRITES.map((s, i) => new Actor({ id: i, kind: "player", name: "", sprite: s, x: 0, y: 0, dir: 0 }));
  }

  frame(now: number) {
    const ui = this.app.ui, st = this.state;
    ui.image(CG.CHR_MAKE_SEL_BG, 400, 300);
    for (let i = 0; i < 12; i++) {
      const [x, y] = SEL_POS[i], [hx, hy, hw, hh] = SEL_HIT[i], a = this.actors[i];
      a.sprite = SEL_SPRITES[i] + st.color[i] * 5;
      a.dir = st.dir[i];
      const hot = this.selected < 0 && ui.hitBox(x + hx, y + hy, x + hx + hw, y + hy + hh, `chr${i}`, false);
      if (this.selected === i) ui.box(x + hx, y + hy, x + hx + hw, y + hy + hh, SYS.AQUA, false);
      ui.actor(a, x, y, now, hot || this.selected === i);
      if (hot && ui.clicked(`chr${i}`)) this.selected = i;
    }
    if (this.selected < 0) {
      ui.bottomLine = "请选择一个人物。";
      if (ui.button(CG.CHR_MAKE_BACK_BTN, 64, 520, "back")) this.app.go(new CharSelectScreen(this.app));
      if (ui.hovered("back")) ui.bottomLine = "回到前一个画面。";
      return;
    }
    // selCharGraColorWin：(496, 41) 的 2×3 窗口，800×600 下修正为 (656, 101)
    const wx = 656, wy = 101, i = this.selected;
    ui.window(wx, wy, 2, 3, "colorwin");
    const items = ["   决定   ", " 改变颜色 ", " 转动方向 ", "   取消   "];
    const hints = ["决定目前所选择的人物。", "改变人物的颜色。", "转动人物的方向。", "回到选择人物的画面。"];
    items.forEach((t, k) => {
      if (ui.textButton(wx + 22, wy + k * 22 + 32, t, FONT.YELLOW, `menu${k}`)) {
        if (k === 0) { st.body = i; this.app.go(new MakeEditScreen(this.app, st)); }
        if (k === 1) st.color[i] = (st.color[i] + 1) % 4;
        if (k === 2) st.dir[i] = (st.dir[i] + 1) % 8;
        if (k === 3) this.selected = -1;
      }
      if (ui.hovered(`menu${k}`)) ui.bottomLine = hints[k];
    });
  }
}

// ---------------------------------------------------------------------------
// 创建角色 2：姓名、长相（眼睛/嘴巴）、基本状态 20 点、属性 10 点（editCharParam，640×480 坐标）。

class EditState {
  name = "";
  eye = 0; mouth = 0;
  stat = [10, 10, 0, 0];   // 体力 腕力 耐力 速度：原版初始 10/10/0/0，剩余 0
  statPoint = 0;
  attr = [0, 0, 0, 0];     // 地 水 火 风
  attrPoint = 10;
}

const STAT_POS = [[111, 250], [111, 278], [111, 305], [111, 334]];
const ATTR_POS = [[297, 255], [297, 279], [297, 303], [297, 327]];
const STAT_HINTS = ["这项主要与耐久力有关。", "这项主要与攻击力有关。", "这项主要与防御力有关。", "这项主要与战斗时的行动顺序有关。"];
const ATTR_HINTS = ["地属性。与水属性的对手战斗时较为有利。", "水属性。与火属性的对手战斗时较为有利。",
  "火属性。与风属性的对手战斗时较为有利。", "风属性。与地属性的对手战斗时较为有利。"];

export class MakeEditScreen implements Screen {
  private app: App;
  private make: MakeState;
  private st: EditState;
  private nameBox!: InputBox;
  constructor(app: App, make: MakeState, st = new EditState()) {
    this.app = app;
    this.make = make;
    this.st = st;
  }

  enter() {
    this.nameBox = new InputBox(this.app.canvas, 104 + OX, 102 + OY, 150, 16, { maxLength: 8 });
    this.nameBox.value = this.st.name;
    setTimeout(() => this.nameBox.focus(), 0);
  }
  leave() { this.st.name = this.nameBox.value; this.nameBox.remove(); }

  get face() {
    return CG.CHR_MAKE_FACE + this.make.body * 100 + this.make.color[this.make.body] * 25 + this.st.eye * 5 + this.st.mouth;
  }

  frame() {
    const ui = this.app.ui, st = this.st, body = this.make.body;
    const P = (x: number, y: number): [number, number] => [x + OX, y + OY];
    ui.image(CG.CHR_MAKE_BG, 400, 300);
    ui.image(body === 0 || body === 6 ? CG.CHR_MAKE_NOSE_SEL : CG.CHR_MAKE_EYE_SEL, ...P(320, 240));
    ui.image(this.face, ...P(93, 172));
    // 长相：第 0 行眼睛（豆丁两种为鼻子），第 1 行嘴巴，各 0..4 循环
    for (let i = 0; i < 2; i++) {
      const key = i === 0 ? "eye" : "mouth";
      if (ui.button(CG.PREV_BTN, ...P(156, 172 + i * 24), `${key}-`, CG.PREV_BTN_DOWN)) st[key] = (st[key] + 4) % 5;
      if (ui.button(CG.NEXT_BTN, ...P(260, 172 + i * 24), `${key}+`, CG.NEXT_BTN_DOWN)) st[key] = (st[key] + 1) % 5;
      if (ui.hovered(`${key}-`) || ui.hovered(`${key}+`))
        ui.bottomLine = i === 0 ? (body === 0 || body === 6 ? "改变鼻子的形状。" : "改变眼睛的形状。") : "改变嘴巴的形状。";
    }
    // 基本状态
    num(ui, ...P(161, 224), st.statPoint, 3);
    STAT_POS.forEach(([x, y], i) => {
      num(ui, ...P(x, y), st.stat[i], 3);
      if (ui.button(CG.PREV_BTN, ...P(x + 51, y + 8), `s${i}-`, CG.PREV_BTN_DOWN) && st.stat[i] > 0) { st.stat[i]--; st.statPoint++; }
      if (ui.button(CG.NEXT_BTN, ...P(x + 87, y + 8), `s${i}+`, CG.NEXT_BTN_DOWN) && st.statPoint > 0) { st.stat[i]++; st.statPoint--; }
      if (ui.hovered(`s${i}-`) || ui.hovered(`s${i}+`)) ui.bottomLine = STAT_HINTS[i];
    });
    // 属性：相克（地-火、水-风）的一方有点数时，另一方的按钮不显示
    num(ui, ...P(350, 224), st.attrPoint, 3);
    ATTR_POS.forEach(([x, y], i) => {
      attrBar(ui, ...P(x, y), st.attr[i], ATTR_COLORS[i]);
      const opp = i < 2 ? i + 2 : i - 2;
      if (st.attr[opp] !== 0) return;
      if (ui.button(CG.PREV_BTN, ...P(x + 101, y + 5), `a${i}-`, CG.PREV_BTN_DOWN) && st.attr[i] > 0) { st.attr[i]--; st.attrPoint++; }
      if (ui.button(CG.NEXT_BTN, ...P(x + 137, y + 5), `a${i}+`, CG.NEXT_BTN_DOWN)) {
        if (st.attrPoint > 0) { st.attr[i]++; st.attrPoint--; }
        else {
          // 点数用完时从另一种属性挪一点过来（原版行为）
          const from = st.attr.findIndex((v, k) => k !== i && v > 0);
          if (from >= 0) { st.attr[from]--; st.attr[i]++; }
        }
      }
      if (ui.hovered(`a${i}-`) || ui.hovered(`a${i}+`)) ui.bottomLine = ATTR_HINTS[i];
    });
    if (ui.button(CG.CHR_MAKE_OK_BTN, ...P(320, 410), "ok")) this.ok();
    if (ui.button(CG.CHR_MAKE_BACK_BTN, ...P(420, 410), "back")) this.app.go(new MakeSelectScreen(this.app, this.make));
    if (ui.hovered("ok")) ui.bottomLine = "决定人物的设定。";
    if (ui.hovered("back")) ui.bottomLine = "回到前一个画面。";
    if (!ui.bottomLine) ui.bottomLine = "设定姓名、长相、基本状态与属性。";
  }

  private ok() {
    const name = this.nameBox.value.trim(), st = this.st;
    // 原版的检查顺序与提示（editCharParamProcNo 100..108）
    if (!name) return this.app.message("没有输入名字！");
    if (st.statPoint > 0) return this.app.message("请将状态的点数分配完毕！");
    if (st.attrPoint > 0) return this.app.message("请将属性的点数分配完毕！");
    if (this.app.chars.some((c) => c.name === name)) return this.app.message("已经有同名的人物存在了！");
    if (/\s/.test(this.nameBox.value) || /\s/.test(name)) return this.app.message("名字里面不可以有空白！");
    st.name = name;
    this.app.go(new HomeTownScreen(this.app, this.make, st, this.face));
  }
}

// ---------------------------------------------------------------------------
// 创建角色 3：在地图上选出身地（selHomeTown），然后创建并直接进入游戏。

const TOWN_TEXT = [
  ["位在萨伊那斯的东边，", "是四个村子中最大", "也是最整齐的村子。"],
  ["位在萨伊那斯的西边", "面向美丽海滩的村子。"],
  ["位在加鲁卡的东边，", "四周被森林围绕着", "绿意盎然的村子。"],
  ["位在加鲁卡的西边。", "村子是在好几个小小", "的岛上建立起来的。"],
];

export class HomeTownScreen implements Screen {
  private app: App;
  private make: MakeState;
  private st: EditState;
  private face: number;
  private busy = false;
  constructor(app: App, make: MakeState, st: EditState, face: number) {
    this.app = app; this.make = make; this.st = st; this.face = face;
  }

  frame() {
    const ui = this.app.ui;
    ui.image(CG.CHR_MAKE_SEL2_BG, 400, 300);
    let hot = -1;
    for (let i = 0; i < 4; i++) {
      // 四个村名图都以画面中心为锚点，本身就是热区
      if (ui.image(CG.CHR_MAKE_HOME_NAME0 + i, 400, 300, `town${i}`)) hot = i;
      if (ui.clicked(`town${i}`)) this.create(i);
    }
    const back = ui.button(CG.CHR_MAKE_BACK_BTN, 675, 450, "back");
    if (back) this.app.go(new MakeEditScreen(this.app, this.make, this.st));
    const tx = 390 + OX, ty = OY;
    if (hot >= 0) {
      ui.text(454 + OX, 135 + ty, HOMETOWNS[hot], FONT.BLUE);
      TOWN_TEXT[hot].forEach((line, k) => ui.text(tx - 4, 158 + k * 20 + ty, line, FONT.BLUE));
    } else {
      ui.text(tx, 176 + ty, ui.hovered("back") ? "回到前一个画面。" : "请选择出身地。", FONT.BLUE);
    }
  }

  private async create(town: number) {
    const net = this.app.net;
    if (this.busy || !net) return;
    this.busy = true;
    const st = this.st, body = this.make.body;
    net.send({
      t: "CharCreate", name: st.name, sprite: SEL_SPRITES[body] + this.make.color[body] * 5, face: this.face,
      vital: st.stat[0], str: st.stat[1], tgh: st.stat[2], dex: st.stat[3],
      earth: st.attr[0], water: st.attr[1], fire: st.attr[2], wind: st.attr[3], hometown: town,
    });
    const r = await net.once("CharCreateResult");
    this.busy = false;
    if (!r.ok) { this.app.message(r.message, () => this.app.go(new CharSelectScreen(this.app))); return; }
    await net.once("CharListResult");
    this.app.enterGame(r.id); // 原版创建后直接以新角色登入（PROC_CHAR_LOGIN_START）
  }
}
