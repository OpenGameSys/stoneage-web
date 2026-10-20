// 家族窗口（VER25，_FM_MODIFY / _FMVER21 / _NEW_MANOR_LAW）：
//   家族选单 joinChannelWN（field.cpp）：MakeWindowDisp(20, 30, 2, 5)，选项在 (+25, +65 + 36k)，关闭钮 (+65, +213)
//   族长功能 leaderFuncWN：MakeWindowDisp(160, 30, 2, 5)，选项在 (+22, +65 + 36k)
//   成立家族 familyAddWN（login.cpp，WN 14）与家族资料（FM S|D）：571×295 居中，底图 CG_FAMILY_DETAIL_WIN 26239 在 (+286, +148)
//   家族列表 familyListWN（FM S|F）：7×8 格居中
//   成员列表 FMWindowType（WN 18）：9×8 格；职务选单 fmselectdetuyWindow 3×5 格在 (280, 160)
//   留言板 FMWindowType1（WN 19 / 20）：9×9 格；据点列表 FMWindowType2（WN 21）：5×7 格
//   强者表 FMWindowType4（WN 22，10×7 格）、FMWindowType3（WN 23 / 42 / 43，8×7 格）；家族资料 mineFamilyDetailWN（WN 31，7×6 格）
//   族长让位 WN 32（7×8 格）/ 33（6×2 格）/ 34（6×3 格）；家族银行 menu.cpp（270×160 居中，底图 26240 在中心；现金 (180, 40)、存款 (180, 86)、上下钮 (140 / 200, 70)、OK / 取消 (75 / 200, 133)）；家族的帐户 familyTaxWN（5×4 格）
// 回传照原版字串：FM 的 "A|…"、"S|…" 等用 Family 送；服务端的 WN 窗口回传 NpcWindowReply（npc 0）。
// 与原版不同：家族徽章的图不在资源里，没有做；主旨的输入框原版是 46 字一行的多行框，这里用一行。

import type { PetInfo, S2CFamilyStatus, S2CNpcWindow } from "./proto.gen.ts";
import { findAction } from "./assets.ts";
import { FONT, InputBox, measure, type Ui } from "./ui.ts";

const CG = {
  DETAIL_WIN: 26239, BANK_WIN: 26240, OK: 26093, EXIT: 26096, EXIT2: 26241, JOIN: 26243, CLOSE: 26042, CANCEL: 26050,
  PREV: 26180, NEXT: 26182, PREV10: 26184, NEXT10: 26186, UP: 26188, DOWN: 26190,
};
export const DUTY = { NONE: -1, MEMBER: 1, APPLY: 2, LEADER: 3, ELDER: 4 };
const WN_BTN = ["确  定", "取  消", "确  定", "取  消", "上一页", "下一页"];
const TOWNS: Record<number, string> = { 100: "萨姆吉尔", 200: "玛丽娜丝", 300: "加    加", 400: "卡鲁它那" };
const MAX_FMBANKGOLD = 100_000_000, MAX_BANKGOLD = 10_000_000;

// makeEscapeString / makeStringFromEscaped（服务端 fmEscape）：| 与 \ 前面加 \
export function fmEscape(s: string): string { return s.replace(/\\/g, "\\\\").replace(/\|/g, "\\|"); }
function fmUnescape(s: string): string { return s.replace(/\\([\\|])/g, "$1"); }
function fmSplit(s: string): string[] {
  const out: string[] = []; let cur = "";
  for (let i = 0; i < s.length; i++) {
    if (s[i] === "\\" && i + 1 < s.length) { cur += s[i] + s[i + 1]; i++; }
    else if (s[i] === "|") { out.push(cur); cur = ""; }
    else cur += s[i];
  }
  out.push(cur);
  return out.map(fmUnescape);
}
function bytes(s: string): number { let n = 0; for (const ch of s) n += ch.charCodeAt(0) < 0x80 ? 1 : 2; return n; }
/** sprintf 的 %-Ns / %Ns（按 GBK 字节数补空白） */
function ls(s: string | number, n: number): string { s = String(s); return s + " ".repeat(Math.max(0, n - bytes(s))); }
function rs(s: string | number, n: number): string { s = String(s); return " ".repeat(Math.max(0, n - bytes(s))) + s; }

export interface FamilyHost {
  ui: Ui;
  canvas: HTMLCanvasElement;
  send(text: string): void;                                   // lssproto_FM_send
  reply(win: S2CNpcWindow, button: number, data: string): void; // lssproto_WN_send
  chat(text: string, color: string): void;
  name(): string;
  face(): number;
  pets(): PetInfo[];
  gold(): number;
  walking(): boolean;
}

type View = null | "menu" | "leader" | "add" | "list" | "detail" | "members" | "duty" | "kick" | "board" | "points"
  | "top30" | "dp" | "info" | "leaders" | "leaderQ" | "leaderA" | "bank" | "tax";

interface ListRow { index: number; name: string; leader: string; fame: number; members: number; slot: number; setup: boolean; accept: boolean }
interface Detail { slot: number; index: number; name: string; leader: string; face: number; pet: string; petSprite: number; members: number; accept: boolean; rule: string; sprite: number }
interface Member { pid: number; name: string; lv: string; online: boolean; fame: string; elder: boolean; duty: number; mom: string }

export class FamilyWindow {
  private host: FamilyHost;
  view: View = null;
  status: S2CFamilyStatus = { t: "FamilyStatus", index: 0, name: "", duty: DUTY.NONE, channel: -1, sprite: 0 };
  private win: S2CNpcWindow | null = null; // 服务端开的窗口（回传要用）
  private input: InputBox | null = null;
  private input2: InputBox | null = null;
  // 成立 / 资料窗口
  private spriteSel = -1; private petSel = -1; private animAt = 0;
  private detail: Detail | null = null; private detailPet = -1; private detailRule = "";
  // 家族列表
  private list: ListRow[] = []; private listTotal = 0; private listPage = 1;
  // 成员列表
  private members: (Member | null)[] = []; private recruit = { start: 0, accept: 1 }; private dutyRow = -1;
  // 留言板
  private memos: [string, string][] = []; private memoIdx = 6;
  // 强者表、据点、资料、让位
  private lines: string[] = []; private kind = "";
  private leaders: [number, string][] = []; private leaderPage = 0; private leaderPick: [number, string] = [0, ""];
  private ask: [string, number] = ["", 0];
  // 银行
  private cash = 0; private bank = 0; private hold = { id: "", frames: 0 };

  constructor(host: FamilyHost) { this.host = host; }

  get open() { return this.view !== null; }
  inFamily() { return this.status.index > 0; }

  close() {
    this.view = null; this.win = null;
    this.input?.remove(); this.input = null; this.input2?.remove(); this.input2 = null;
  }

  private show(v: View) { this.close(); this.view = v; }

  /** 任务栏 / 画面上方的家族钮（FIELD_FUNC_CHANNEL）：没有家族时说"请先加入家族。"并送 S|P */
  toggle() {
    if (this.view === "menu" || this.view === "leader") { this.close(); return; }
    if (!this.inFamily()) { this.host.chat("请先加入家族。", FONT.WHITE); this.host.send("S|P"); return; }
    this.show("menu");
  }

  private makeInput(x: number, y: number, w: number, max: number, value = ""): InputBox {
    const box = new InputBox(this.host.canvas, x, y, w, 16, { maxLength: max });
    box.value = value;
    box.el.addEventListener("keydown", (e) => {
      e.stopPropagation();
      if (e.ctrlKey && ["s", "i", "m", "p"].includes(e.key.toLowerCase())) e.preventDefault();
    });
    return box;
  }

  // ---------------- 服务端送来的 ----------------

  /** lssproto_FM_recv：S|F 家族列表、S|D 家族资料、B|G 银行、B|T 家族的帐户、C|J 族长广播 */
  onMsg(text: string) {
    const f = fmSplit(text);
    const kind = `${f[0]}|${f[1]}`;
    if (kind === "S|F") {
      this.listTotal = Number(f[2]); this.listPage = Number(f[3]);
      this.list = f.slice(5, 5 + Number(f[4])).map((r) => {
        const p = r.split(" ");
        return { index: Number(p[0]), name: p[1], leader: p[2], fame: Number(p[3]), members: Number(p[4]), slot: Number(p[5]), setup: p[6] === "1", accept: p[7] === "1" };
      });
      if (this.view !== "list") this.show("list");
    } else if (kind === "S|D") {
      const [img] = (f[8] ?? "").split(" ");
      this.detail = { slot: Number(f[2]), index: Number(f[3]), name: f[4], leader: f[5], face: Number(f[6]), pet: f[7], petSprite: Number(img),
        members: Number(f[9]), accept: f[10] === "1", rule: f[11], sprite: Number(f[12]) };
      this.show("detail");
      this.detailPet = -1; this.detailRule = this.detail.rule;
      if (this.ownLeader()) this.input = this.makeInput(this.x0() + 82, this.y0() + 162, 46 * 7, 120, this.detail.rule);
    } else if (kind === "B|G") {
      this.bank = Number(f[2]); this.show("bank"); this.cash = this.host.gold();
    } else if (kind === "B|T") {
      this.bank = Number(f[2]); this.show("tax"); this.cash = this.host.gold();
    } else if (kind === "C|J") {
      this.status = { ...this.status, channel: Number(f[2]) };
    }
  }

  /** 家族的 WN 窗口：处理了回传 true */
  onWindow(m: S2CNpcWindow): boolean {
    const views: Record<string, View> = { familyadd: "add", fmmembers: "members", fmboard: "board", fmsboard: "board", fmpoints: "points",
      fmtop30: "top30", fmdp: "dp", fmmomentum: "dp", fmownmomentum: "dp", fmdetail: "info", fmleaders: "leaders", fmleaderask: "leaderA" };
    const v = views[m.kind];
    if (!v) return false;
    this.show(v);
    this.win = m; this.kind = m.kind;
    const lines = m.text.split("\n");
    switch (v) {
      case "add":
        this.spriteSel = -1; this.petSel = this.host.pets().length ? this.host.pets()[0].slot : -1;
        this.input = this.makeInput(this.x0() + 100, this.y0() + 40, 16 * 7, 16);
        this.input2 = this.makeInput(this.x0() + 82, this.y0() + 162, 46 * 7, 120);
        setTimeout(() => this.input?.focus(), 0);
        break;
      case "members":
        this.members = lines.slice(0, 10).map((l) => {
          if (!l) return null;
          const p = fmSplit(l);
          return { pid: Number(p[0]), name: p[1], lv: p[2], online: Number(p[3]) > 0, fame: p[4], elder: p[5] === "1", duty: Number(p[6]), mom: p[8] ?? "0" };
        });
        { const r = fmSplit(lines[10] ?? ""); this.recruit = { start: Number(r[1] ?? 0), accept: Number(r[2] ?? 1) }; }
        break;
      case "board":
        this.memos = lines.slice(0, 7).map((l) => { const p = fmSplit(l); return [p[0] ?? "", p[1] ?? ""] as [string, string]; });
        this.memoIdx = Number(lines[7] ?? 6);
        this.input = this.makeInput(this.x0(9) + 27, this.y0(9) + 356, 58 * 7, 58);
        break;
      case "leaders": {
        const p = fmSplit(m.text), n = Number(p[3]);
        this.leaders = [];
        for (let i = 0; i < n; i++) this.leaders.push([Number(p[4 + 2 * i]), p[5 + 2 * i]]);
        this.leaderPage = 0;
        break;
      }
      case "leaderA": { const p = fmSplit(m.text); this.ask = [p[0], Number(p[1])]; break; }
      default:
        this.lines = lines;
    }
    return true;
  }

  // ---------------- 画 ----------------

  private x0(w = 571 / 64) { return Math.floor((800 - w * 64) / 2); }
  private y0(h = 295 / 48) { return Math.floor((600 - h * 48) / 2); }
  private ownLeader() { return !!this.detail && this.detail.index === this.status.index && this.status.duty === DUTY.LEADER; }

  frame(rects: [number, number, number, number][], esc: boolean) {
    if (!this.view) return;
    if (esc && this.view !== "leaderA") { this.close(); return; }
    switch (this.view) {
      case "menu": this.drawMenu(rects); break;
      case "leader": this.drawLeader(rects); break;
      case "add": this.drawAdd(rects); break;
      case "detail": this.drawDetail(rects); break;
      case "list": this.drawList(rects); break;
      case "members": this.drawMembers(rects); break;
      case "duty": case "kick": this.drawMembers(rects); this.drawDuty(rects); break;
      case "board": this.drawBoard(rects); break;
      case "points": this.drawPoints(rects); break;
      case "top30": this.drawTop30(rects); break;
      case "dp": this.drawDP(rects); break;
      case "info": this.drawInfo(rects); break;
      case "leaders": this.drawLeaders(rects); break;
      case "leaderQ": this.drawLeaderQ(rects); break;
      case "leaderA": this.drawLeaderA(rects); break;
      case "bank": this.drawBank(rects); break;
      case "tax": this.drawTax(rects); break;
    }
  }

  /** 原版 WN 窗口的按钮：位元由左到右，x = 宽 × (k + 1) ÷ (个数 + 1) − 27 */
  private wnButtons(x: number, y: number, w: number, by: number, data: () => string) {
    const ui = this.host.ui, win = this.win;
    if (!win) return;
    const bits = [0, 1, 2, 3, 4, 5].filter((i) => win.buttons & (1 << i)).slice(0, 4);
    const bw = w * 64 / (bits.length + 1);
    bits.forEach((bit, k) => {
      if (ui.text(x + bw * (k + 1) - 27, y + by, WN_BTN[bit], FONT.YELLOW, `fm-wn${bit}`)) {
        const d = data();
        this.close();
        this.host.reply(win, 1 << bit, d);
      }
    });
  }

  private drawMenu(rects: [number, number, number, number][]) {
    const ui = this.host.ui, x = 20, y = 30, st = this.status;
    ui.window(x, y, 2, 5, "fm-menu");
    rects.push([x, y, x + 128, y + 240]);
    ui.text(x + (128 - measure(st.name)) / 2, y + 30, st.name, FONT.GREEN);
    const boss = st.duty === DUTY.LEADER || st.duty === DUTY.ELDER;
    const items: [string, boolean, string][] = [[" 家族资料 ", true, "查看家族详细资料。"], [" 族长功能 ", boss, "族长专用功能。"],
      ["家族布告栏", true, "使用家族布告栏功能。"], [" 关闭广播 ", boss, "关闭族长广播功能。"]];
    items.forEach(([label, ok, tip], k) => {
      const id = `fm-menu${k}`;
      if (ui.text(x + 25, y + 65 + 36 * k, label, ok ? FONT.WHITE : FONT.GRAY, ok ? id : undefined)) {
        if (k === 0) { this.close(); this.host.send("S|D2"); }
        else if (k === 1) this.show("leader");
        else if (k === 2) { this.close(); this.host.send("D"); }
        else { this.host.send("C|J|-1"); this.host.send("S|SELF"); }
      }
      if (ui.hovered(id)) ui.bottomLine = tip;
    });
    if (ui.button(CG.CLOSE, x + 65, y + 213, "fm-menu-close")) this.close();
    if (ui.hovered("fm-menu-close")) ui.bottomLine = "离开聊天频道选单。";
  }

  private drawLeader(rects: [number, number, number, number][]) {
    const ui = this.host.ui, x = 160, y = 30, st = this.status, leader = st.duty === DUTY.LEADER;
    ui.window(x, y, 2, 5, "fm-leader");
    rects.push([x, y, x + 128, y + 240]);
    const items: [string, string | null, string][] = leader
      ? [["  族长广播  ", "C|J|5", "族长对全星系的族员广播。"], ["家族成员管理", "L|F|1", "家族成员列表。"],
        [" 家族邀请函 ", "L|L|1", "制作家族参观邀请函。"], ["  族长让位  ", "L|CHANGE|L", "族长让位。"]]
      : [["  族长广播  ", null, "族长对全星系的族员广播。"], ["家族成员管理", "L|F|1", "家族成员列表。"],
        [" 家族邀请函 ", "L|L|1", "制作家族参观邀请函。"], [" 学习同意书 ", "L|L|2", "制作学习骑乘同意书。"], ["  族长让位  ", null, "族长让位。"]];
    items.forEach(([label, cmd, tip], k) => {
      const id = `fm-lead${k}`;
      const color = !cmd ? FONT.GRAY : k === 0 && st.channel === 5 ? FONT.YELLOW : FONT.WHITE;
      if (ui.text(x + 22, y + 65 + 36 * k, label, color, cmd ? id : undefined) && cmd) {
        this.close();
        this.host.send(cmd);
      }
      if (ui.hovered(id)) ui.bottomLine = tip;
    });
    if (ui.button(CG.CLOSE, x + 65, y + 213, "fm-lead-close")) this.show("menu");
  }

  /** 守护兽的动画（ANIM_DISP_PET 的站立动画） */
  private drawPetAnim(sprite: number, x: number, y: number) {
    const ui = this.host.ui;
    if (sprite <= 0) return;
    const def = ui.assets.anim(sprite), act = def && findAction(def, 0, 0);
    if (!act || !act.frames.length) return;
    const [img, fx, fy] = act.frames[Math.floor((performance.now() - this.animAt) / (Math.max(1, act.ticks) * (1000 / 60))) % act.frames.length];
    if (img >= 0) ui.image(img, x + fx, y + fy, undefined, true);
  }

  private cyclePet(cur: number, step: number): number {
    const pets = this.host.pets().map((p) => p.slot).sort((a, b) => a - b);
    if (!pets.length) return -1;
    const i = pets.indexOf(cur);
    return pets[((i < 0 ? 0 : i + step) % pets.length + pets.length) % pets.length];
  }

  private drawAdd(rects: [number, number, number, number][]) {
    const ui = this.host.ui, x = this.x0(), y = this.y0();
    ui.image(CG.DETAIL_WIN, x + 286, y + 148, "fm-add-bg", false, false);
    rects.push([x, y, x + 571, y + 295]);
    ui.text(x + 100, y + 70, this.host.name(), FONT.WHITE);
    ui.text(x + 100, y + 100, "无", FONT.WHITE);
    ui.image(this.host.face(), x + 312, y + 79);
    const spr = this.spriteSel < 0 ? "        " : `  ${["光明精灵", "黑暗精灵"][this.spriteSel]}  `;
    if (ui.text(x + 436, y + 36, spr, FONT.WHITE, "fm-add-sprite")) this.spriteSel = (this.spriteSel + 1) % 2;
    if (ui.hovered("fm-add-sprite")) ui.bottomLine = "选择守护精灵。";
    if (ui.hitBox(x + 430, y + 32, x + 540, y + 52, "fm-add-sprite2") && ui.clicked("fm-add-sprite2")) this.spriteSel = (this.spriteSel + 1) % 2;
    const pet = this.host.pets().find((p) => p.slot === this.petSel);
    if (pet) { ui.text(x + 436, y + 80, pet.freeName || pet.name, FONT.WHITE); this.drawPetAnim(pet.sprite, x + 460, y + 240); }
    if (ui.button(CG.PREV, x + 435, y + 270, "fm-add-prev", CG.PREV + 1)) this.petSel = this.cyclePet(this.petSel, -1);
    if (ui.button(CG.NEXT, x + 495, y + 270, "fm-add-next", CG.NEXT + 1)) this.petSel = this.cyclePet(this.petSel, 1);
    if (ui.button(CG.OK, x + 90, y + 274, "fm-add-ok")) {
      const name = this.input?.value.trim() ?? "", rule = this.input2?.value.trim() ?? "";
      const err = !name ? "请输入家族名称！" : !rule ? "请输入家族主旨！" : this.spriteSel < 0 ? "请选择守护精灵！" : this.petSel < 0 ? "请选择家族守护兽！" : "";
      if (err) { this.host.chat(err, FONT.RED); return; }
      this.close();
      this.host.send(`A|${fmEscape(name)}|${this.petSel}|${this.spriteSel}|${fmEscape(rule)}|0`);
    }
    if (ui.button(CG.EXIT, x + 270, y + 274, "fm-add-exit")) this.close();
  }

  private drawDetail(rects: [number, number, number, number][]) {
    const ui = this.host.ui, x = this.x0(), y = this.y0(), d = this.detail!;
    ui.image(CG.DETAIL_WIN, x + 280, y + 145, "fm-det-bg", false, false);
    rects.push([x, y, x + 571, y + 295]);
    ui.text(x + 100, y + 40, d.name, FONT.WHITE);
    ui.text(x + 100, y + 70, d.leader, FONT.WHITE);
    ui.text(x + 100, y + 100, `${d.members} 人`, FONT.WHITE);
    if (!this.input) ui.text(x + 82, y + 162, d.rule, FONT.WHITE);
    ui.text(x + 436, y + 36, `  ${["光明精灵", "黑暗精灵"][d.sprite] ?? ""}  `, FONT.WHITE);
    ui.image(d.face, x + 306, y + 76);
    const own = this.ownLeader();
    const pet = this.detailPet >= 0 ? this.host.pets().find((p) => p.slot === this.detailPet) : null;
    ui.text(x + 436, y + 80, pet ? pet.freeName || pet.name : d.pet, FONT.WHITE);
    this.drawPetAnim(pet ? pet.sprite : d.petSprite, x + 460, y + 240);
    if (own) {
      if (ui.button(CG.PREV, x + 435, y + 270, "fm-det-prev", CG.PREV + 1)) this.detailPet = this.cyclePet(this.detailPet, -1);
      if (ui.button(CG.NEXT, x + 495, y + 270, "fm-det-next", CG.NEXT + 1)) this.detailPet = this.cyclePet(this.detailPet, 1);
    }
    const changed = own && (this.detailPet >= 0 || (this.input?.value.trim() ?? d.rule) !== d.rule);
    if (!this.inFamily() && d.accept) {
      if (ui.button(CG.JOIN, x + 90, y + 274, "fm-det-join")) {
        this.close();
        this.host.send(`J|${d.slot}|${d.index}|${fmEscape(d.name)}|${d.sprite}`);
      }
      if (ui.button(CG.EXIT2, x + 270, y + 274, "fm-det-exit")) this.close();
    } else if (changed) {
      if (ui.button(CG.OK, x + 90, y + 274, "fm-det-ok")) {
        const rule = this.input?.value.trim() ?? "";
        if (rule !== d.rule) this.host.send(`X|R|${fmEscape(rule)}`);
        if (this.detailPet >= 0) this.host.send(`X|P|${this.detailPet}`);
        this.close();
      }
      if (ui.button(CG.EXIT2, x + 270, y + 274, "fm-det-exit")) this.close();
    } else if (ui.button(CG.EXIT2, x + 180, y + 274, "fm-det-exit")) this.close();
  }

  private drawList(rects: [number, number, number, number][]) {
    const ui = this.host.ui, x = this.x0(7), y = this.y0(8);
    ui.window(x, y, 7, 8, "fm-list");
    rects.push([x, y, x + 448, y + 384]);
    ui.text(x + 190, y + 20, "家族列表", FONT.WHITE);
    const pages = Math.max(1, Math.ceil(this.listTotal / 10));
    const go = (p: number) => this.host.send(`S|F|${((p - 1) % pages + pages) % pages + 1}|0`);
    ui.text(x + 100, y + 60, `${rs(this.listPage, 3)} / ${rs(pages, 3)} 页`, FONT.WHITE);
    if (ui.button(CG.PREV, x + 76, y + 68, "fm-list-prev", CG.PREV + 1)) go(this.listPage - 1);
    if (ui.button(CG.NEXT, x + 226, y + 68, "fm-list-next", CG.NEXT + 1)) go(this.listPage + 1);
    if (ui.button(CG.PREV10, x + 36, y + 68, "fm-list-prev10", CG.PREV10 + 1)) go(this.listPage - 10);
    if (ui.button(CG.NEXT10, x + 266, y + 68, "fm-list-next10", CG.NEXT10 + 1)) go(this.listPage + 10);
    ui.text(x + 25, y + 90, `${ls("", 4)} ${ls("[家 族 名 称]", 16)} ${ls("[族  长]", 16)} ${rs("[人数]", 6)} ${rs("[家族声望]", 10)}`, FONT.WHITE);
    this.list.forEach((r, i) => {
      const color = !r.accept ? FONT.AQUA : !r.setup ? FONT.GREEN : FONT.WHITE;
      const line = `${rs(r.index, 4)}: ${ls(r.name, 16)} ${ls(r.leader, 16)} ${rs(r.members, 6)} ${rs(Math.floor(r.fame / 100), 10)}`;
      if (ui.text(x + 15, y + 110 + 20 * i, line, color, `fm-list${i}`)) this.host.send(`S|D|${fmEscape(r.name)}|${r.index}|${r.slot}`);
    });
    if (ui.button(CG.EXIT, x + 220, y + 350, "fm-list-exit")) this.close();
  }

  private drawMembers(rects: [number, number, number, number][]) {
    const ui = this.host.ui, W = 9, x = this.x0(W), y = this.y0(8), st = this.status;
    ui.window(x, y, W, 8, "fm-mem");
    rects.push([x, y, x + W * 64, y + 384]);
    ui.text(x + 250, y + 21, "‘家族成员列表’", FONT.YELLOW);
    ui.text(x + 24, y + 21 + 2 * 21, "‘状态’‘ 成 员 姓 名 ’            ‘等级’‘个人声望’‘个人气势’‘加入’", FONT.YELLOW);
    const dutyName = (d: number) => d === DUTY.MEMBER ? "一般族员" : d === DUTY.LEADER ? "族    长" : d === DUTY.ELDER ? "长    老" : d === DUTY.APPLY ? "申请加入" : "退    出";
    const popup = this.view !== "members";
    this.members.forEach((m, i) => {
      if (!m) return;
      const color = m.duty === DUTY.APPLY ? FONT.BLUE : m.duty === DUTY.NONE ? FONT.RED : FONT.WHITE;
      const line = `${ls(m.online ? "OnLine" : "OffLine", 9)}${ls(m.name, 18)}${rs(m.lv, 3)}${rs(m.fame, 12)}${rs(m.mom, 12)}${rs(dutyName(m.duty), 10)}${rs("无", 7)}`;
      const ly = y + 21 + (i + 3) * 21;
      ui.text(x + 34, ly, line, color);
      if (popup) return;
      const canEdit = st.duty === DUTY.LEADER && m.duty !== DUTY.LEADER && m.duty > 0 || st.duty === DUTY.ELDER && (m.duty === DUTY.MEMBER || m.duty === DUTY.APPLY);
      const y1 = y + 18 + (i + 3) * 21 + 2;
      if (canEdit && ui.hitBox(x + 420, y1, x + 490, y1 + 20, `fm-mem-duty${i}`) && ui.clicked(`fm-mem-duty${i}`)) { this.dutyRow = i; this.view = "duty"; }
    });
    const ry = y + 31 + 13 * 21, boss = st.duty === DUTY.LEADER || st.duty === DUTY.ELDER;
    ui.text(x + 34, ry, ` ${ls("是否继续召募家族人员", 20)}                         ${this.recruit.accept ? "继续召募" : "中止召募"}`, this.recruit.accept ? FONT.GREEN : FONT.RED);
    if (!popup && boss && ui.hitBox(x + 25, ry - 3, x + 430, ry + 17, "fm-mem-recruit") && ui.clicked("fm-mem-recruit")) {
      this.recruit.accept = this.recruit.accept ? 0 : 1;
      this.host.send(`T|${this.recruit.accept}`);
    }
    if (!popup) this.wnButtons(x, y, W, 335, () => `${this.recruit.start}|0|0`);
  }

  /** 职务选单（fmselectdetuyWindow）与退出的确认 */
  private drawDuty(rects: [number, number, number, number][]) {
    const ui = this.host.ui, m = this.members[this.dutyRow], st = this.status;
    if (!m) { this.view = "members"; return; }
    if (this.view === "kick") {
      const x = 240, y = 250;
      ui.window(x, y, 5, 2, "fm-kick");
      rects.push([x, y, x + 320, y + 96]);
      const t = `确定要让 ${m.name} 退出家族吗？`;
      ui.text(x + (320 - measure(t)) / 2, y + 22, t, FONT.WHITE);
      if (ui.text(x + 90, y + 60, "确  定", FONT.YELLOW, "fm-kick-ok")) { this.close(); this.host.send(`M|${m.name}|${m.pid}|-1`); }
      if (ui.text(x + 190, y + 60, "取  消", FONT.YELLOW, "fm-kick-no")) this.view = "members";
      return;
    }
    const x = 280, y = 160;
    ui.window(x, y, 3, 5, "fm-duty");
    rects.push([x, y, x + 192, y + 240]);
    ui.text(x + 20, y + 14, "请选择", FONT.WHITE);
    ui.text(x + 20, y + 33, ` ${m.name} `, FONT.YELLOW);
    ui.text(x + 20, y + 52, "所担任的家族职务：", FONT.WHITE);
    const elderOk = st.duty !== DUTY.ELDER && m.duty !== DUTY.APPLY && m.duty !== DUTY.ELDER;
    const opts: [string, number | null][] = [["一般族员", DUTY.MEMBER], ["退    出", DUTY.NONE], ["长    老", elderOk ? DUTY.ELDER : null], ["取    消", 0]];
    opts.forEach(([label, d], k) => {
      if (!ui.text(345, 235 + 30 * k, label, d === null ? FONT.GRAY : FONT.WHITE, d === null ? undefined : `fm-duty${k}`) || d === null) return;
      if (d === 0) this.view = "members";
      else if (d === DUTY.NONE) this.view = "kick";
      else { this.close(); this.host.send(`M|${m.name}|${m.pid}|${d}`); }
    });
  }

  private drawBoard(rects: [number, number, number, number][]) {
    const ui = this.host.ui, W = 9, x = this.x0(W), y = this.y0(9);
    ui.window(x, y, W, 9, "fm-board");
    rects.push([x, y, x + W * 64, y + 432]);
    ui.text(x + 200, y + 25, this.kind === "fmsboard" ? "‘家族之间留言板’" : "‘家 族 留 言 板’", FONT.GREEN);
    this.memos.forEach(([text, sign], i) => {
      if (text) ui.text(x + 27, y + 66 + 40 * i, text, FONT.WHITE);
      if (sign) ui.text(x + 327, y + 86 + 40 * i, sign, FONT.YELLOW);
    });
    this.wnButtons(x, y, W, 384, () => `${this.memoIdx}|${fmEscape(this.input?.value.trim() ?? "")}`);
  }

  private drawPoints(rects: [number, number, number, number][]) {
    const ui = this.host.ui, W = 5, H = 7, x = this.x0(W), y = this.y0(H);
    ui.window(x, y, W, H, "fm-points");
    rects.push([x, y, x + W * 64, y + H * 48]);
    ui.text(x + 34, y + 28, "        ‘家族据点申请列表’", FONT.YELLOW);
    ui.text(x + 30, y + 18 + 2 * 21, "‘地 点’‘东’‘南’        ‘状  态’", FONT.GREEN);
    this.lines.slice(0, 4).forEach((l, i) => {
      if (!l) return;
      const p = fmSplit(l), owner = Number(p[4]);
      const line = `${rs(TOWNS[Number(p[1])] ?? p[1], 9)}${rs(p[2], 5)} ${rs(p[3], 5)}${rs(owner > 0 ? p[5] : "未使用", 18)}`;
      const ly = y + 21 + (i + 3) * 21;
      ui.text(x + 30, ly, line, owner <= 0 ? FONT.GRAY : FONT.WHITE);
      if (this.status.duty === DUTY.LEADER && owner <= 0 && ui.hitBox(x + 30, ly - 3, x + 290, ly + 17, `fm-pt${i}`) && ui.clicked(`fm-pt${i}`)) {
        this.close();
        this.host.send(`P|${p[0]}|${p[1]}|${p[2]}|${p[3]}`);
      }
    });
    this.wnButtons(x, y, W, H * 48 - 40, () => "0");
  }

  private drawTop30(rects: [number, number, number, number][]) {
    const ui = this.host.ui, W = 10, H = 7, x = this.x0(W), y = this.y0(H);
    ui.window(x, y, W, H, "fm-top");
    rects.push([x, y, x + W * 64, y + H * 48]);
    ui.text(x + 235, y + 21, "‘史上最强之家族强者排行榜’", FONT.YELLOW);
    ui.text(x + 25, y + 58, "‘名次’ ‘家  族  名  称’ ‘族  长  名  称’ ‘综合声望’‘成员声望’‘家族总声望’", FONT.YELLOW);
    const rows = this.lines.slice(0, 10).map((l) => l ? fmSplit(l) : null);
    const marked = rows.some((r) => r?.[6] === "1");
    rows.forEach((r, i) => {
      if (!r) return;
      const sum = Number(r[4]), total = Number(r[5]);
      const line = ` ${ls(r[1], 7)}${ls(r[2], 18)}${ls(r[3], 16)}${rs(sum, 13)}${rs(total - sum, 12)}${rs(total, 14)}`;
      ui.text(x + 40, y + 14 + (i + 3) * 21, line, marked && r[6] !== "1" ? FONT.GRAY : FONT.WHITE);
    });
    this.wnButtons(x, y, W, H * 48 - 40, () => `${this.lines[10] ?? "0"}|0`);
  }

  private drawDP(rects: [number, number, number, number][]) {
    const ui = this.host.ui, W = 8, H = 7, x = this.x0(W), y = this.y0(H);
    ui.window(x, y, W, H, "fm-dp");
    rects.push([x, y, x + W * 64, y + H * 48]);
    ui.text(x + 20, y + 21, "                  ‘史上最强之家族强者排行榜’", FONT.YELLOW);
    if (this.kind === "fmownmomentum") {
      ui.text(x + 25, y + 58, "‘名次’‘家 族 名 称’     ‘族 长 名 称’    ‘家族总气势’  ‘个人气势’", FONT.YELLOW);
      const r = fmSplit(this.lines[0] ?? "");
      if (r.length >= 6) ui.text(x + 40, y + 14 + 3 * 21, `${ls(r[1], 7)}${ls(r[2], 16)}${ls(r[3], 16)}${rs(r[4], 8)}${rs(r[5], 12)}`, FONT.WHITE);
    } else {
      ui.text(x + 25, y + 58, `‘名 次’ ‘家  族  名  称’ ‘族  长  名  称’  ${this.kind === "fmmomentum" ? "‘家族气势值’" : "‘家族声望值’"}`, FONT.YELLOW);
      this.lines.slice(0, 10).forEach((l, i) => {
        if (!l) return;
        const r = fmSplit(l);
        ui.text(x + 58, y + 14 + (i + 3) * 21, ` ${ls(r[1], 9)}${ls(r[2], 19)}${ls(r[3], 17)}${rs(r[4], 12)}`, FONT.WHITE);
      });
    }
    this.wnButtons(x, y, W, H * 48 - 40, () => "0");
  }

  private drawInfo(rects: [number, number, number, number][]) {
    const ui = this.host.ui, W = 7, H = 6, x = this.x0(W), y = this.y0(H);
    ui.window(x, y, W, H, "fm-info");
    rects.push([x, y, x + W * 64, y + H * 48]);
    const p = fmSplit(this.lines.join("\n"));
    const duty = ({ 1: "一般成员", 2: "待 审 核", 3: " 族  长 ", 4: " 长  老 " } as Record<string, string>)[p[6]] ?? "未加入家族";
    ui.text(x + 190, y + 20, "家族资料", FONT.WHITE);
    const left = [`家族名称：${p[0]}`, `家族人数：${p[1]}`, `声望排名：${p[3]}`, `家族职位：${duty}`, "家族踢馆：", "家族徽章："];
    const right = [`族长名称：${p[2]}`, `守护精灵：${["光明精灵", "黑暗精灵"][Number(p[7])] ?? ""}`, `家族声望：${p[4]}`, `个人声望：${p[5]}`, `家族气势：${p[9]}`, `个人气势：${p[10]}`];
    left.forEach((t, i) => ui.text(x + 30, y + 60 + 30 * i, t, FONT.WHITE));
    right.forEach((t, i) => ui.text(x + 240, y + 60 + 30 * i, t, FONT.WHITE));
    ui.text(x + 120, y + 180, p[8] ?? "", FONT.WHITE);
    if (ui.text(x + 180, y + 250, "  离  开  ", FONT.YELLOW, "fm-info-close")) this.close();
  }

  private drawLeaders(rects: [number, number, number, number][]) {
    const ui = this.host.ui, W = 7, H = 8, x = this.x0(W), y = this.y0(H);
    ui.window(x, y, W, H, "fm-leaders");
    rects.push([x, y, x + W * 64, y + H * 48]);
    ui.text(x + 50, y + 30, "请选择家族成员：", FONT.WHITE);
    this.leaders.slice(this.leaderPage * 30, this.leaderPage * 30 + 30).forEach(([j, name], i) => {
      if (ui.text(x + 30 + Math.floor(i / 10) * 130, y + 60 + (i % 10) * 20, name, FONT.WHITE, `fm-ld${i}`)) { this.leaderPick = [j, name]; this.view = "leaderQ"; }
    });
    if (this.leaders.length > (this.leaderPage + 1) * 30 && ui.text(x + 100, y + 350, "下一页", FONT.YELLOW, "fm-ld-next")) this.leaderPage++;
    if (this.leaderPage > 0 && ui.text(x + 30, y + 350, "上一页", FONT.YELLOW, "fm-ld-prev")) this.leaderPage--;
    if (ui.text(x + 300, y + 350, "  离  开  ", FONT.YELLOW, "fm-ld-close")) this.close();
  }

  private drawLeaderQ(rects: [number, number, number, number][]) {
    const ui = this.host.ui, W = 6, H = 2, x = this.x0(W), y = this.y0(H);
    ui.window(x, y, W, H, "fm-ldq");
    rects.push([x, y, x + W * 64, y + H * 48]);
    ui.text(x + 20, y + 25, `你确定要将族长的位子交给${this.leaderPick[1]}吗？`, FONT.WHITE);
    if (ui.text(x + 200, y + 60, "  离  开  ", FONT.YELLOW, "fm-ldq-no")) this.close();
    if (ui.text(x + 280, y + 60, "  确  定  ", FONT.YELLOW, "fm-ldq-yes")) { this.close(); this.host.send(`L|CHANGE|Q|${this.leaderPick[0]}|${fmEscape(this.leaderPick[1])}`); }
  }

  private drawLeaderA(rects: [number, number, number, number][]) {
    const ui = this.host.ui, W = 6, H = 3, x = this.x0(W), y = this.y0(H);
    ui.window(x, y, W, H, "fm-lda");
    rects.push([x, y, x + W * 64, y + H * 48]);
    ui.text(x + 20, y + 30, `${this.ask[0]} 希望将族长的位子交给你，`, FONT.WHITE);
    ui.text(x + 20, y + 60, "你要接受吗？", FONT.WHITE);
    const answer = (yes: number) => { this.close(); this.host.send(`L|CHANGE|A|${yes}|${fmEscape(this.ask[0])}|${this.ask[1]}`); };
    if (ui.text(x + 200, y + 105, "  不愿意  ", FONT.YELLOW, "fm-lda-no")) answer(0);
    if (ui.text(x + 280, y + 105, "  愿  意  ", FONT.YELLOW, "fm-lda-yes")) answer(1);
  }

  /** 按住上下钮：每 30 帧步进 ×5，最多 10000（familyTaxWN / 银行） */
  private stepButton(img: number, x: number, y: number, id: string): number {
    const ui = this.host.ui;
    const clicked = ui.button(img, x, y, id, img + 1);
    const down = ui.mouse.down && ui.hovered(id);
    if (!down) {
      const held = this.hold.id === id;
      if (held) this.hold = { id: "", frames: 0 };
      return clicked && !held ? 1 : 0; // 很快的一下（没看到按住的那一帧）也算一格
    }
    if (this.hold.id !== id) { this.hold = { id, frames: 0 }; return 1; }
    this.hold.frames++;
    return this.hold.frames % 3 === 0 ? Math.min(10000, 5 ** Math.floor(this.hold.frames / 30)) : 0;
  }

  private drawBank(rects: [number, number, number, number][]) {
    const ui = this.host.ui, x = Math.floor((800 - 270) / 2), y = Math.floor((600 - 160) / 2);
    if (this.host.walking()) { this.close(); return; }
    ui.image(CG.BANK_WIN, x + 135, y + 80, "fm-bank-bg", false, false);
    rects.push([x, y, x + 270, y + 160]);
    const gold = this.host.gold();
    ui.text(x + 180, y + 40, rs(this.cash, 7), FONT.WHITE);
    ui.text(x + 180, y + 86, rs(this.bank, 7), FONT.WHITE);
    const up = this.stepButton(CG.UP, x + 140, y + 70, "fm-bank-up"); // 领出
    if (up) { const v = Math.min(up, this.bank); this.bank -= v; this.cash += v; }
    const down = this.stepButton(CG.DOWN, x + 200, y + 70, "fm-bank-down"); // 存入
    if (down) { const v = Math.min(down, this.cash, MAX_BANKGOLD - this.bank); this.cash -= v; this.bank += v; }
    if (ui.button(CG.OK, x + 75, y + 133, "fm-bank-ok")) { const to = gold - this.cash; this.close(); if (to) this.host.send(`B|G|${to}`); }
    if (ui.button(CG.CANCEL, x + 200, y + 133, "fm-bank-cancel")) this.close();
  }

  private drawTax(rects: [number, number, number, number][]) {
    const ui = this.host.ui, W = 5, H = 4, x = this.x0(W), y = this.y0(H), st = this.status;
    ui.window(x, y, W, H, "fm-tax");
    rects.push([x, y, x + W * 64, y + H * 48]);
    ui.text(x + 80, y + 30, `${st.name}  家族的帐户`, FONT.WHITE);
    ui.text(x + 60, y + 70, "存  款：", FONT.WHITE);
    ui.text(x + 200, y + 70, rs(this.bank, 10), FONT.WHITE);
    ui.text(x + 60, y + 120, "现  金：", FONT.WHITE);
    ui.text(x + 200, y + 120, rs(this.cash, 10), FONT.WHITE);
    const up = this.stepButton(CG.UP, x + 230, y + 105, "fm-tax-up"); // 存入
    if (up) { const v = Math.min(up, this.cash, MAX_FMBANKGOLD - this.bank); this.cash -= v; this.bank += v; }
    if (st.duty === DUTY.LEADER || st.duty === DUTY.ELDER) {
      const down = this.stepButton(CG.DOWN, x + 190, y + 105, "fm-tax-down"); // 领出
      if (down) { const v = Math.min(down, this.bank); this.bank -= v; this.cash += v; }
    }
    if (ui.button(CG.OK, x + 100, y + 160, "fm-tax-ok")) { const delta = this.host.gold() - this.cash; this.close(); if (delta) this.host.send(`B|T|${delta}`); }
    if (ui.button(CG.CANCEL, x + 220, y + 160, "fm-tax-cancel")) this.close();
  }
}
