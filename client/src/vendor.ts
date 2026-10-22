// 摆摊（_STREET_VENDOR：原客户端 field.cpp 的摆摊钮 35227、StreetVendorWndfunc 设定窗口与 StreetVendorBuyWndfunc 购买窗口）。
// 原版的底图（35240 一带）不在这套资源里，窗口自己画：
//   设定窗口：标题输入框、道具栏与宠物栏的清单（每页 10 行，点一行就加进贩卖清单并输入价钱，再点一次取消），"开张" / "取消"。
//     送 Vendor cmd set（kinds / slots / prices），最多 20 项（MAX_SELL_ITEM）。
//   购买窗口：店家的摊位名、清单（点一行勾选，"详情"要那一项的资料显示在下方），合计、身上的石币，"购买" / "离开"。
//   摆着摊、看着摊位时都不能走（服务端挡，这里 Field.locked 也不送 Move）。

import type { InvItem, PetInfo, S2CVendorDetail, S2CVendorList, VendorEntry } from "./proto.gen.ts";
import { FONT, InputBox, measure, type Ui } from "./ui.ts";

export interface VendorHost {
  ui: Ui;
  canvas: HTMLCanvasElement;
  items(): InvItem[];
  pets(): PetInfo[];
  ridePet(): number;
  gold(): number;
  send(cmd: string, title?: string, kinds?: number[], slots?: number[], prices?: number[]): void;
  chat(text: string, color: string): void;
}

const W = 9, H = 8;
const X = (800 - W * 64) / 2, Y = 56;
const PAGE = 10, ROW = 22, LIST_Y = 96;
const MAX_SELL = 20, MAX_PRICE = 10000000;
const EQUIP_SLOTS = 9;

interface SetupRow { kind: number; slot: number; name: string; info: string }

export class VendorWindow {
  open = false;
  mode: "setup" | "buy" = "setup";
  private host: VendorHost;
  // 设定
  private picks = new Map<string, number>(); // "kind:slot" → 价钱
  private sel: string | null = null;
  private titleBox: InputBox | null = null;
  private priceBox: InputBox | null = null;
  private page = 0;
  // 购买
  seller = ""; title = "";
  entries: VendorEntry[] = [];
  private chosen = new Set<number>();
  private detail: S2CVendorDetail | null = null;

  constructor(host: VendorHost) { this.host = host; }

  /** 服务端送来 VendorSetup：打开设定窗口（改内容时带着原来的项目） */
  openSetup(entries: VendorEntry[], title = "") {
    this.close(false);
    this.open = true; this.mode = "setup"; this.page = 0; this.sel = null;
    this.picks = new Map();
    for (const e of entries) this.picks.set(`${e.kind}:${e.slot}`, e.price);
    this.titleBox = this.makeInput(X + 110, Y + 44, 200, 16, title.replace(/^\[石\]/, ""));
  }

  /** 服务端送来 VendorList：打开（或更新）购买窗口 */
  openBuy(m: S2CVendorList) {
    if (!this.open || this.mode !== "buy") { this.close(false); this.open = true; this.mode = "buy"; this.page = 0; this.chosen = new Set(); this.detail = null; }
    this.seller = m.seller; this.title = m.title; this.entries = m.entries;
    for (const i of [...this.chosen]) if (!m.entries.some((e) => e.index === i)) this.chosen.delete(i);
  }

  setDetail(d: S2CVendorDetail) { this.detail = d; }

  /** 关掉窗口；tell 时通知服务端（设定中取消 = 收摊 close，购买中 = 离开 leave） */
  close(tell: boolean) {
    if (this.open && tell) this.host.send(this.mode === "setup" ? "close" : "leave");
    this.open = false;
    this.titleBox?.remove(); this.titleBox = null;
    this.priceBox?.remove(); this.priceBox = null;
  }

  private makeInput(x: number, y: number, w: number, max: number, value = ""): InputBox {
    const box = new InputBox(this.host.canvas, x, y, w, 16, { maxLength: max });
    box.value = value;
    box.el.addEventListener("keydown", (ev) => { ev.stopPropagation(); if (ev.key === "Enter" || ev.key === "Escape") box.el.blur(); });
    return box;
  }

  private setupRows(): SetupRow[] {
    const rows: SetupRow[] = [];
    for (const it of this.host.items()) {
      if (it.slot < EQUIP_SLOTS) continue;
      rows.push({ kind: 0, slot: it.slot, name: it.name, info: it.pile > 1 ? `×${it.pile}` : "" });
    }
    for (const p of this.host.pets()) {
      if (p.slot < 0 || p.slot >= 5) continue;
      rows.push({ kind: 1, slot: p.slot, name: p.freeName ? `${p.freeName}（${p.name}）` : p.name, info: `Lv${p.level}${p.slot === this.host.ridePet() ? " 骑乘中" : ""}` });
    }
    return rows;
  }

  frame(rects: [number, number, number, number][], esc: boolean) {
    if (!this.open) return;
    if (esc) { this.close(true); return; }
    const ui = this.host.ui, x = X, y = Y;
    ui.window(x, y, W, H, "vendor-wnd");
    rects.push([x, y, x + W * 64, y + H * 48]);
    ui.box(x + 22, y + 10, x + W * 64 - 22, y + H * 48 - 10, "rgba(0,0,0,0.6)", true);
    if (this.mode === "setup") this.setupFrame(ui, x, y); else this.buyFrame(ui, x, y);
  }

  // ---------------- 设定 ----------------
  private setupFrame(ui: Ui, x: number, y: number) {
    ui.text(x + W * 32 - 32, y + 16, "摆摊设定", FONT.YELLOW);
    const n = `已选 ${this.picks.size} / ${MAX_SELL} 项`;
    ui.text(x + W * 64 - 28 - measure(n), y + 16, n, FONT.GRAY);
    ui.text(x + 28, y + 44, "摊位名称：", FONT.AQUA);
    ui.box(x + 106, y + 42, x + 314, y + 62, "rgba(255,255,255,0.35)", false);
    ui.text(x + 330, y + 44, "（最多 8 个中文字，空白就是\"欢迎光临!!\"）", FONT.GRAY);
    ui.text(x + 28, y + 72, "卖", FONT.AQUA); ui.text(x + 60, y + 72, "道具 / 宠物", FONT.AQUA);
    ui.text(x + 330, y + 72, "数量 / 等级", FONT.AQUA); ui.text(x + 450, y + 72, "价钱（石币）", FONT.AQUA);
    ui.box(x + 24, y + 90, x + W * 64 - 24, y + 91, "rgba(255,255,255,0.25)", true);
    const rows = this.setupRows(), pages = Math.max(1, Math.ceil(rows.length / PAGE));
    this.page = Math.min(this.page, pages - 1);
    if (rows.length === 0) ui.text(x + 28, y + LIST_Y + 4, "身上没有可以卖的东西。", FONT.GRAY);
    rows.slice(this.page * PAGE, this.page * PAGE + PAGE).forEach((r, i) => {
      const ry = y + LIST_Y + i * ROW, key = `${r.kind}:${r.slot}`, on = this.picks.has(key), id = `vendor-r${i}`;
      if (ui.hitBox(x + 24, ry - 3, x + 440, ry + ROW - 5, id, false)) ui.box(x + 24, ry - 3, x + W * 64 - 24, ry + ROW - 5, "rgba(255,255,255,0.08)", true);
      if (this.sel === key) ui.box(x + 24, ry - 3, x + W * 64 - 24, ry + ROW - 5, "rgba(255,216,74,0.18)", true);
      ui.text(x + 30, ry, on ? "☑" : "☐", on ? FONT.YELLOW : FONT.GRAY);
      ui.text(x + 60, ry, r.name, on ? FONT.YELLOW : FONT.WHITE);
      ui.text(x + 330, ry, r.info, FONT.WHITE);
      if (on && this.sel !== key) ui.text(x + 450, ry, String(this.picks.get(key) ?? 0), FONT.WHITE);
      if (ui.clicked(id)) {
        if (on && this.sel === key) { this.picks.delete(key); this.select(null, 0); }
        else {
          if (!on) {
            if (this.picks.size >= MAX_SELL) { this.host.chat(`最多只能卖 ${MAX_SELL} 项。`, FONT.RED); return; }
            this.picks.set(key, 0);
          }
          this.select(key, ry);
        }
      }
    });
    if (this.priceBox && this.sel) {
      const v = Math.max(0, Math.min(MAX_PRICE, parseInt(this.priceBox.value, 10) || 0));
      if (this.priceBox.value !== "" && String(v) !== this.priceBox.value) this.priceBox.value = String(v);
      this.picks.set(this.sel, v);
    }
    const by = y + H * 48 - 40;
    if (pages > 1) {
      ui.text(x + 28, by, `第 ${this.page + 1} / ${pages} 页`, FONT.GRAY);
      if (ui.text(x + 120, by, "◀", FONT.AQUA, "vendor-pp") && this.page > 0) { this.page--; this.select(null, 0); }
      if (ui.text(x + 144, by, "▶", FONT.AQUA, "vendor-np") && this.page < pages - 1) { this.page++; this.select(null, 0); }
    }
    ui.text(x + 28, by - 22, "点一行加进清单并填价钱，再点一次取消；登出会消失的道具、守护兽、骑着的宠物不能卖。", FONT.GRAY);
    if (ui.text(x + W * 64 - 28 - measure("开　张　　取　消"), by, "开　张", FONT.YELLOW, "vendor-ok")) this.submit();
    if (ui.text(x + W * 64 - 28 - measure("取　消"), by, "取　消", FONT.AQUA, "vendor-cancel")) this.close(true);
  }

  private select(key: string | null, ry: number) {
    this.priceBox?.remove(); this.priceBox = null;
    this.sel = key;
    if (!key) return;
    const box = this.makeInput(X + 450, ry - 1, 110, 8, String(this.picks.get(key) ?? 0));
    this.priceBox = box;
    setTimeout(() => { box.focus(); box.el.select(); }, 0);
  }

  private submit() {
    const kinds: number[] = [], slots: number[] = [], prices: number[] = [];
    for (const [key, price] of this.picks) {
      const [k, s] = key.split(":").map(Number);
      kinds.push(k); slots.push(s); prices.push(price);
    }
    if (kinds.length === 0) { this.host.chat("还没有选要卖的东西。", FONT.RED); return; }
    if (prices.some((p) => p <= 0)) { this.host.chat("有东西还没有填价钱。", FONT.RED); return; }
    this.host.send("set", (this.titleBox?.value ?? "").trim(), kinds, slots, prices);
    this.close(false);
  }

  // ---------------- 购买 ----------------
  private buyFrame(ui: Ui, x: number, y: number) {
    ui.text(x + 28, y + 16, this.title, FONT.YELLOW);
    const s = `店家：${this.seller}`;
    ui.text(x + W * 64 - 28 - measure(s), y + 16, s, FONT.GRAY);
    ui.text(x + 28, y + 44, "买", FONT.AQUA); ui.text(x + 60, y + 44, "道具 / 宠物", FONT.AQUA);
    ui.text(x + 330, y + 44, "数量 / 等级", FONT.AQUA); ui.text(x + 420, y + 44, "价钱", FONT.AQUA);
    ui.box(x + 24, y + 62, x + W * 64 - 24, y + 63, "rgba(255,255,255,0.25)", true);
    const top = y + 70, pages = Math.max(1, Math.ceil(this.entries.length / PAGE));
    this.page = Math.min(this.page, pages - 1);
    if (this.entries.length === 0) ui.text(x + 28, top + 4, "店家的东西卖完了。", FONT.GRAY);
    this.entries.slice(this.page * PAGE, this.page * PAGE + PAGE).forEach((e, i) => {
      const ry = top + i * ROW, on = this.chosen.has(e.index), id = `vendor-b${i}`;
      if (ui.hitBox(x + 24, ry - 3, x + 500, ry + ROW - 5, id, false)) ui.box(x + 24, ry - 3, x + W * 64 - 24, ry + ROW - 5, "rgba(255,255,255,0.08)", true);
      ui.text(x + 30, ry, on ? "☑" : "☐", on ? FONT.YELLOW : FONT.GRAY);
      ui.text(x + 60, ry, e.name, e.kind === 1 ? FONT.RED : on ? FONT.YELLOW : FONT.WHITE);
      ui.text(x + 330, ry, e.kind === 0 ? (e.pile > 1 ? `×${e.pile}` : "") : `Lv${e.level}`, FONT.WHITE);
      ui.text(x + 420, ry, String(e.price), FONT.WHITE);
      if (ui.text(x + W * 64 - 28 - measure("详情"), ry, "详情", FONT.AQUA, `vendor-d${i}`)) this.host.send("detail", "", [], [e.index], []);
      if (ui.clicked(id)) { if (on) this.chosen.delete(e.index); else this.chosen.add(e.index); }
    });
    // 详情（VendorDetail）
    const dy = y + 300;
    ui.box(x + 24, dy - 4, x + W * 64 - 24, dy - 3, "rgba(255,255,255,0.25)", true);
    const d = this.detail;
    if (d && this.entries.some((e) => e.index === d.index)) {
      if (d.kind === 0) {
        const it = d.item;
        ui.text(x + 28, dy, `${it.name}　${it.damage ? "耐久 " + it.damage : ""}　${it.level > 1 ? "Lv" + it.level : ""}`, FONT.YELLOW);
        ui.text(x + 28, dy + 18, (it.memo + (it.params ? "　" + it.params : "")).slice(0, 48), FONT.WHITE);
      } else {
        const p = d.pet;
        ui.text(x + 28, dy, `${p.freeName || p.name}　Lv${p.level}${p.trans ? " 转生" + p.trans : ""}　忠诚 ${p.loyalty}`, FONT.YELLOW);
        ui.text(x + 28, dy + 18, `HP ${p.maxHp}　攻 ${p.atk}　防 ${p.def}　敏 ${p.agi}　属性 地${p.attrs[0]} 水${p.attrs[1]} 火${p.attrs[2]} 风${p.attrs[3]}`, FONT.WHITE);
      }
    } else ui.text(x + 28, dy, "按\"详情\"看一项的资料。", FONT.GRAY);
    const total = this.entries.filter((e) => this.chosen.has(e.index)).reduce((a, e) => a + e.price, 0);
    const by = y + H * 48 - 40;
    ui.text(x + 28, by, `合计 ${total} 石币　　身上 ${this.host.gold()} 石币`, total > this.host.gold() ? FONT.RED : FONT.WHITE);
    if (pages > 1) {
      if (ui.text(x + 300, by, "◀", FONT.AQUA, "vendor-bpp") && this.page > 0) this.page--;
      if (ui.text(x + 324, by, "▶", FONT.AQUA, "vendor-bnp") && this.page < pages - 1) this.page++;
    }
    if (ui.text(x + W * 64 - 28 - measure("购　买　　离　开"), by, "购　买", FONT.YELLOW, "vendor-buy")) {
      if (this.chosen.size === 0) this.host.chat("还没有勾选要买的东西。", FONT.RED);
      else { this.host.send("buy", "", [], [...this.chosen], []); this.chosen.clear(); }
    }
    if (ui.text(x + W * 64 - 28 - measure("离　开"), by, "离　开", FONT.AQUA, "vendor-leave")) this.close(true);
  }
}
