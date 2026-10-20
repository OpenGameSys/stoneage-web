// 交易窗口（menu.cpp 交易部分，_TRADESYSTEM2 + _TRADELOCKBTN + _MONEYINPUT）：
//   MakeWindowDisp((800 − 620)/2, (600 − 456)/2, 620, 456)，底图 CG_TRADE_WND 26328 画在 (mx, my − 20)
//   上面的清单是对方放的、下面是自己放的（各 5 行，用卷轴看其余的），右上是宠物（◀ ▶ 换、"放"放进去）与石币（+ − 或直接输入，"放"只能放一次），
//   右下是自己道具栏的这一页（双击放一个）；左下"锁定" → 两边都锁定后变成"确定"，"取消"关掉。
//   开交易：任务栏没有，原版是 Ctrl+T 或画面左上的"交易"钮（面向对方）；对方要开着"交易"开关。
// 没做的：查看对方的窗口（CG_TRADE_VIEWWND，这里对方放的直接列在上面）、交易对话气泡（_TRADETALKWND）、交易中头上的记号。

import type { InvItem, PetInfo } from "./proto.gen.ts";
import { FONT, InputBox, boxColor, type Ui } from "./ui.ts";
import { ANIM, findAction } from "./assets.ts";

const CG = {
  WND: 26328, LOCK_BTN: 26300, OK_BTN: 26093, CANCEL_BTN: 26050, LEFT: 26180, RIGHT: 26182,
  UP: 26064, DOWN: 26066, PUT: 26062, SCROLL_UP: 26331, SCROLL_DOWN: 26332,
};
const W = 620, H = 456;
const BAG = 15;

interface Row { kind: "item" | "pet" | "gold"; name: string; col2: string; col3: string; col4: string; color: string; slot?: number }

export interface TradeHost {
  ui: Ui;
  canvas: HTMLCanvasElement;
  items(): InvItem[];      // 自己的道具栏
  pets(): PetInfo[];       // 自己的宠物栏
  ridePet(): number;
  gold(): number;
  send(cmd: string, slot?: number, amount?: number): void; // lssproto_TD_send
}

export class TradeWindow {
  open = false;
  private partner = "";
  // trade_confirm：1 都没锁、2 我锁了、3 对方锁了、4 都锁了；sent：按过"确定"
  private confirm = 1;
  private sent = false;
  private partnerOk = false;
  private mine: Row[] = [];
  private theirs: Row[] = [];
  private myGold = 0;            // 已放的石币（0 还没放）
  private putCount = new Map<number, number>(); // 格位 → 已放几个
  private putPets = new Set<number>();
  private petIdx = -1;
  private gold = 0;              // 要放的石币
  private goldInput: InputBox | null = null;
  private scroll = [0, 0];       // 上（对方）、下（自己）清单的第一行
  private page = 0;
  private lastClick = { slot: -1, at: 0 };
  private host: TradeHost;

  constructor(host: TradeHost) { this.host = host; }

  private get x() { return (800 - W) / 2; }
  private get y() { return (600 - H) / 2; }

  /** TD C|fd|name|1：打开 */
  start(name: string) {
    this.open = true; this.partner = name; this.confirm = 1; this.sent = false; this.partnerOk = false;
    this.mine = []; this.theirs = []; this.myGold = 0; this.putCount.clear(); this.putPets.clear();
    this.gold = 0; this.scroll = [0, 0]; this.page = 0;
    this.petIdx = this.nextPet(-1, 1);
  }

  /** TD W：关掉 */
  close() { this.open = false; this.goldInput?.remove(); this.goldInput = null; }

  partnerItem(count: number, it: InvItem) {
    const name = it.name, row: Row = { kind: "item", name, col2: "", col3: `x${count}`, col4: it.damage || "", color: FONT.WHITE };
    const i = this.theirs.findIndex((r) => r.kind === "item" && r.slot === it.slot);
    row.slot = it.slot;
    if (i >= 0) this.theirs[i] = row; else this.theirs.push(row);
  }
  partnerPet(p: PetInfo) {
    this.theirs.push({ kind: "pet", name: p.name, col2: `[${p.freeName}]`, col3: "", col4: `Lv:${p.level}${p.trans ? ["", "一转", "二转"][Math.min(2, p.trans)] : ""}`, color: FONT.RED });
  }
  partnerGold(n: number) {
    this.theirs = this.theirs.filter((r) => r.kind !== "gold");
    this.theirs.push({ kind: "gold", name: "石币", col2: String(n), col3: "", col4: "Gold", color: FONT.WHITE });
  }
  /** 对方锁定（C）或对方先按了确定（A） */
  partnerState(s: string) {
    if (s === "locked") this.confirm = this.confirm === 2 ? 4 : 3;
    if (s === "ok") this.partnerOk = true;
  }

  private nextPet(from: number, step: number): number {
    const have = this.host.pets().map((p) => p.slot).filter((s) => s !== this.host.ridePet() && !this.putPets.has(s));
    if (have.length === 0) return -1;
    for (let n = 1; n <= 5; n++) { const s = (((from + step * n) % 5) + 5) % 5; if (have.includes(s)) return s; }
    return have[0];
  }

  private canPut() { return this.confirm === 1 || this.confirm === 3; }

  frame(rects: [number, number, number, number][]) {
    if (!this.open) return;
    const ui = this.host.ui, x = this.x, y = this.y, mx = x + W / 2, my = y + H / 2;
    ui.hitBox(x, y, x + W, y + H, "trade-wnd", false);
    rects.push([x, y, x + W, y + H]);
    ui.image(CG.WND, mx, my - 20);
    ui.text(x + 5, y + 6, this.partner, FONT.PURPLE);
    ui.text(x + 5, y + 193, "我", FONT.PURPLE);
    this.list(this.theirs, 0, y + 40);
    this.list(this.mine, 1, y + 228);
    // 状态文字
    const st = this.confirm === 2 ? ["锁定交易", 220] : this.confirm === 3 ? ["对方锁定交易", 210]
      : this.confirm === 4 ? (this.sent ? ["交易确认", 220] : ["最后交易确认", 210]) : null;
    if (st) ui.text(x + (st[1] as number), y + 194, st[0] as string, FONT.RED);

    this.petPanel(x, y);
    this.goldPanel(x, y);
    this.bag(x, y);

    // 锁定 / 确定、取消
    if (this.confirm !== 2 && !this.sent) {
      const lock = this.confirm !== 4;
      if (ui.image(lock ? CG.LOCK_BTN : CG.OK_BTN, x + 59, y + 390, "trade-lock") && ui.clicked("trade-lock")) {
        if (lock) { this.host.send("lock"); this.confirm = this.confirm === 3 ? 4 : 2; }
        else { this.host.send("ok"); this.sent = true; }
      }
    }
    if (ui.image(CG.CANCEL_BTN, x + 240, y + 390, "trade-cancel") && ui.clicked("trade-cancel")) this.host.send("cancel");
  }

  /** 一个清单：5 行，名字 / 第二栏 / 个数 / 耐久或等级 */
  private list(rows: Row[], which: number, top: number) {
    const ui = this.host.ui, x = this.x;
    const first = Math.min(this.scroll[which], Math.max(0, rows.length - 5));
    rows.slice(first, first + 5).forEach((r, i) => {
      const ry = top + 29 * i;
      ui.text(x + 12, ry, r.name, FONT.WHITE);
      if (r.col2) ui.text(x + 102, ry, r.col2, r.color);
      if (r.col3) ui.text(x + 180, ry, r.col3, FONT.WHITE);
      if (r.col4) ui.text(x + 220, ry, r.col4, FONT.WHITE);
    });
    const sy = which === 0 ? this.y + 42 : this.y + 232;
    if (ui.button(CG.SCROLL_UP, x + 302, sy, `trade-up${which}`)) this.scroll[which] = Math.max(0, first - 1);
    if (ui.button(CG.SCROLL_DOWN, x + 302, sy + 134, `trade-down${which}`)) this.scroll[which] = Math.min(Math.max(0, rows.length - 5), first + 1);
  }

  private petPanel(x: number, y: number) {
    const ui = this.host.ui;
    const p = this.host.pets().find((q) => q.slot === this.petIdx);
    if (ui.button(CG.LEFT, x + 472, y + 71, "trade-pl", CG.LEFT + 1)) this.petIdx = this.nextPet(this.petIdx, -1);
    if (ui.button(CG.RIGHT, x + 506, y + 71, "trade-pr", CG.RIGHT + 1)) this.petIdx = this.nextPet(this.petIdx, 1);
    if (!p || this.putPets.has(p.slot)) return;
    ui.text(x + 330, y + 33, p.freeName || p.name, p.trans === 1 ? FONT.AQUA : FONT.WHITE);
    [p.level, p.atk, p.def, p.agi, p.maxHp].forEach((v, i) => ui.text(x + 485, y + 105 + i * 24, String(v).padStart(3, " "), FONT.WHITE));
    const def = ui.assets.anim(p.sprite), act = def && findAction(def, 0, ANIM.STAND);
    if (act && act.frames.length) {
      const [img, fx, fy] = act.frames[Math.floor(performance.now() / (Math.max(1, act.ticks) * (1000 / 60))) % act.frames.length];
      if (img >= 0) ui.image(img, x + 390 + fx, y + 158 + fy, undefined, true);
    }
    if (this.canPut() && ui.button(CG.PUT, x + 328, y + 306, "trade-putpet", CG.PUT + 1)) {
      this.putPets.add(p.slot);
      this.mine.push({ kind: "pet", name: p.name, col2: p.freeName ? `[${p.freeName}]` : "", col3: "", col4: `Lv:${p.level}`, color: FONT.RED });
      this.host.send("pet", p.slot);
      this.petIdx = this.nextPet(p.slot, 1);
    }
  }

  private goldPanel(x: number, y: number) {
    const ui = this.host.ui, have = this.host.gold();
    ui.text(x + 550, y + 65, String(have).padStart(7, " "), FONT.WHITE);
    if (this.goldInput) {
      const v = Math.max(0, Math.min(have, parseInt(this.goldInput.value, 10) || 0));
      if (this.goldInput.value !== "" && String(v) !== this.goldInput.value) this.goldInput.value = String(v);
      this.gold = v;
    } else ui.text(x + 550, y + 120, String(this.gold).padStart(7, " "), FONT.WHITE);
    if (this.myGold > 0 || !this.canPut()) return;
    if (ui.hitBox(x + 530, y + 115, x + 616, y + 140, "trade-goldin") && ui.clicked("trade-goldin") && !this.goldInput) {
      const box = new InputBox(this.host.canvas, x + 534, y + 118, 78, 18, { maxLength: 8 });
      this.goldInput = box;
      setTimeout(() => box.focus(), 0);
      box.el.addEventListener("keydown", (ev) => { ev.stopPropagation(); if (ev.key === "Enter" || ev.key === "Escape") box.el.blur(); });
    }
    if (ui.button(CG.UP, x + 460, y + 199, "trade-gup", CG.UP + 1)) this.gold = Math.min(have, this.gold + 1);
    if (ui.button(CG.DOWN, x + 466, y + 199, "trade-gdown", CG.DOWN + 1)) this.gold = Math.max(0, this.gold - 1);
    if (ui.button(CG.PUT, x + 525, y + 264, "trade-putgold", CG.PUT + 1) && this.gold > 0) {
      this.myGold = this.gold;
      this.mine.push({ kind: "gold", name: "石币", col2: String(this.gold), col3: "", col4: "Gold", color: FONT.WHITE });
      this.host.send("gold", 0, this.gold);
      this.goldInput?.remove(); this.goldInput = null;
    }
  }

  /** 自己道具栏的一页（InitItem3(325, 230)：defX = 357 + 51c、defY = 270 + 48r，画在 (x + defX − 8, y + defY)），双击放一个；剩下的个数红字 */
  private bag(x: number, y: number) {
    const ui = this.host.ui, items = this.host.items();
    let hover: InvItem | undefined;
    for (let k = 0; k < BAG; k++) {
      const slot = 9 + this.page * BAG + k, cx = x + 357 + 51 * (k % 5) - 8, cy = y + 270 + 48 * Math.floor(k / 5);
      const it = items.find((i) => i.slot === slot);
      const left = it ? it.pile - (this.putCount.get(slot) ?? 0) : 0;
      const id = `trade-bag${k}`;
      const on = ui.hitBox(cx - 27, cy - 22, cx + 23, cy + 28, id, false);
      if (it && left > 0) {
        ui.image(it.image, cx, cy);
        if (it.pile > 1) ui.text(cx + 10, cy, String(left), FONT.RED);
        if (on) { hover = it; ui.box(cx - 27, cy - 22, cx + 23, cy + 28, boxColor(), false); }
        if (ui.clicked(id)) this.clickBag(slot, it);
      }
    }
    // 道具栏的页（原版是窗口右边的书签 (716, 335 + 56i)）
    for (let i = 0; i < 3; i++) if (ui.textButton(712, 335 + 56 * i, ["壹", "贰", "叁"][i], i === this.page ? FONT.YELLOW : FONT.WHITE, `trade-page${i}`)) this.page = i;
    if (hover) ui.bottomLine = `${hover.name}　${hover.memo.trim()}`.slice(0, 60);
  }

  private clickBag(slot: number, it: InvItem) {
    const now = performance.now(), dbl = this.lastClick.slot === slot && now - this.lastClick.at < 400;
    this.lastClick = { slot, at: now };
    if (!dbl || !this.canPut()) return;
    const n = (this.putCount.get(slot) ?? 0) + 1;
    if (n > it.pile) return;
    this.putCount.set(slot, n);
    const row = this.mine.find((r) => r.kind === "item" && r.slot === slot);
    if (row) row.col3 = `x${n}`;
    else { const r: Row = { kind: "item", name: it.name, col2: "", col3: `x${n}`, col4: it.damage || "", color: FONT.WHITE }; r.slot = slot; this.mine.push(r); }
    this.host.send("item", slot);
  }
}
