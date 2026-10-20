// 道具店窗口（原客户端 login.cpp serverWindowType5：shopWindow1..8、shopWindow10）。
// 服务端发 ShopWindow（menu / buy / sell），这里按原版的流程一步步问：
//   买：清单 →（等级不够先问 level_msg）→ 个数 → realy_msg 确认 → 回传"第几项|个数"；道具栏满了显示 itemfull_msg
//   卖：清单 →（会超过金钱上限显示 stone_msg）→ 个数（_ITEM_PILENUMS）→ 回传"格位|个数"
//   寄放 / 取回（serverWindowType7：poolShopWindow2..4）：清单 → realy_msg 确认 → 回传"格位 + 1" / "第几件"
// 清单上的"回"回传 "0"（服务端回选单），Esc 直接关掉。窗口座标照原版 800×600（_NEW_RESOMODE）。
// 声望照 _NEW_MANOR_LAW 的客户端：有声望价的道具显示"声望Ｆ 价钱Ｓ"，声望不够标灰。

import { FONT, Ui, measure } from "./ui.ts";
import type { S2CShopWindow, ShopItem } from "./proto.gen.ts";

const CG = {
  ITEMSHOP_WIN: 26138, ITEMSHOP_KOSU_WIN: 26140,
  BUY_BTN: 26098, SEAL_BTN: 26097, EXIT_BTN: 26096, RETURN_BTN: 26043,
  PREV_BTN: 26180, PREV_BTN_DOWN: 26181, NEXT_BTN: 26182, NEXT_BTN_DOWN: 26183,
  UP_BTN: 26188, UP_BTN_DOWN: 26189, DOWN_BTN: 26190, DOWN_BTN_DOWN: 26191,
  OK_BTN: 26093, CANCEL_BTN: 26050, YES_BTN: 26094, NO_BTN: 26095,
};
const PAGE_ITEMS = 8; // MAX_SHOP_ITEM

export interface ShopHost {
  reply(win: S2CShopWindow, data: string): void;
  gold(): number;
  level(): number;
  freeSlots(): number; // 道具栏空格数（nowUserItemCnt）
}

/** getStrSplit：先按换行切，再按 GBK 字节宽（全角 2）折行，最多 lines 行 */
function split(text: string, maxBytes: number, lines: number): string[] {
  const out: string[] = [];
  for (const para of text.split("\n")) {
    let line = "", w = 0;
    for (const ch of para) {
      const cw = ch.charCodeAt(0) > 0xff ? 2 : 1;
      if (w + cw > maxBytes) { out.push(line); line = ""; w = 0; }
      line += ch; w += cw;
    }
    out.push(line);
  }
  return out.slice(0, lines);
}
const pad = (n: number, width: number) => String(n).padStart(width);

type Step = "menu" | "list" | "level" | "count" | "confirm" | "full" | "stone";

export class Shop {
  win: S2CShopWindow | null = null;
  private step: Step = "menu";
  private page = 0;
  private sel = -1;
  private count = 1;
  private readonly host: ShopHost;

  constructor(host: ShopHost) { this.host = host; }

  get open() { return this.win !== null; }

  /** 收到 ShopWindow：选单从头开始；同一种清单（买完、卖完后服务端再发）留在原来那页 */
  show(m: S2CShopWindow) {
    const same = this.win?.mode === m.mode;
    this.win = m;
    this.step = m.mode === "menu" ? "menu" : "list";
    if (!same) this.page = 0;
    this.page = Math.min(this.page, this.maxPage() - 1);
  }

  close() { this.win = null; }

  private maxPage() { return this.win && this.win.items.length > 0 ? Math.floor((this.win.items.length + 7) / PAGE_ITEMS) : 1; }

  private send(data: string) { const w = this.win!; this.win = null; this.host.reply(w, data); }

  draw(ui: Ui, esc: boolean) {
    const m = this.win;
    if (!m) return;
    ui.hitBox(0, 0, 800, 600, "modal-block", false);
    if (esc) { this.close(); return; }
    switch (this.step) {
      case "menu": this.drawMenu(ui, m); break;
      case "list": this.drawList(ui, m); break;
      case "count": this.drawCount(ui, m); break;
      case "level": this.drawAsk(ui, m.level, () => (this.step = "count"), () => (this.step = "list")); break;
      case "confirm": {
        const pool = m.mode === "pool" || m.mode === "draw";
        const data = pool ? `${m.items[this.sel].slot}` : `${this.sel + 1}|${this.count}`;
        this.drawAsk(ui, m.realy, () => this.send(data), () => (this.step = pool ? "list" : "count"));
        break;
      }
      case "full": this.drawOk(ui, m.full); break;
      case "stone": this.drawOk(ui, m.stone); break;
    }
  }

  /** shopWindow1：4×3 格，店名、main_msg，买 / 卖 / 离开（回传 1 / 2 / 3） */
  private drawMenu(ui: Ui, m: S2CShopWindow) {
    const w = 4, h = 3, x = (800 - w * 64) / 2, y = (600 - h * 48) / 2;
    ui.window(x, y, w, h);
    ui.text(x + (w * 64 - measure(m.title)) / 2, y + 12, m.title, FONT.WHITE);
    ui.text(x + (w * 64 - measure(m.msg)) / 2, y + 32, m.msg, FONT.WHITE);
    if (ui.button(CG.BUY_BTN, x + w * 32, y + 8 + 66, "shop-buy")) this.send("1");
    else if (ui.button(CG.SEAL_BTN, x + w * 32, y + 8 + 90, "shop-sell")) this.send("2");
    else if (ui.button(CG.EXIT_BTN, x + w * 32, y + 8 + 114, "shop-exit")) this.send("3");
  }

  /** shopWindow2（买）/ shopWindow7（卖）：432×428，每页 8 项，滑鼠停在某项上显示图与说明 */
  private drawList(ui: Ui, m: S2CShopWindow) {
    const w = 432, h = 428, x = (800 - w) / 2, y = (600 - h) / 2;
    const gold = this.host.gold(), buy = m.mode === "buy", pool = m.mode === "pool", draw = m.mode === "draw";
    // 寄放：寄放格满了改显示 poolfull_msg；取回：道具栏满了改显示 itemfull_msg
    const blocked = pool ? m.rest <= 0 : draw ? this.host.freeSlots() <= 0 : false;
    ui.image(CG.ITEMSHOP_WIN, x + w / 2, y + h / 2);
    ui.text(x + (w - measure(m.title)) / 2, y + 18, m.title, FONT.WHITE);
    split(blocked ? m.full : m.main, buy || pool || draw ? 44 : 38, 2).forEach((l, i) => ui.text(x + 18, y + 48 + i * 18, l, FONT.WHITE));
    if (buy) ui.text(x + 244, y + 64, `声望 ${pad(m.fame, 8)}Ｆ`, FONT.WHITE); // _NEW_MANOR_LAW（只有买的清单有）
    ui.text(x + 244, y + 84, `金钱 ${pad(gold, 8)}Ｓ`, FONT.WHITE);
    if (ui.button(CG.PREV_BTN, x + 32, y + 100, "shop-prev", CG.PREV_BTN_DOWN) && this.page > 0) this.page--;
    if (ui.button(CG.NEXT_BTN, x + 200, y + 100, "shop-next", CG.NEXT_BTN_DOWN) && this.page + 1 < this.maxPage()) this.page++;
    ui.text(x + 66, y + 92, `${pad(this.page + 1, 2)}/${pad(this.maxPage(), 2)} 页`, FONT.WHITE);
    let focus: ShopItem | null = null;
    for (let i = 0; i < PAGE_ITEMS; i++) {
      const k = this.page * PAGE_ITEMS + i, it = m.items[k];
      if (!it) continue;
      const y1 = y + i * 21 + 116;
      if (ui.hitBox(x + 32, y1, x + 32 + 372, y1 + 20, `shop-item${k}`)) focus = it;
      let color = FONT.WHITE;
      if (buy) {
        if (it.level > this.host.level()) color = FONT.RED;
        if (it.sealed || it.price > gold || it.fame > m.fame) color = FONT.GRAY;
      } else if (pool) {
        if (it.sealed || it.price > gold || blocked) color = FONT.GRAY;
      } else if (draw) {
        if (it.sealed || blocked) color = FONT.GRAY;
      } else {
        if (it.sealed) color = FONT.GRAY;
        if (gold + it.price > m.maxGold) color = FONT.GRAY; // 原版用 7 号色（灰）
      }
      ui.text(x + 34, y + 118 + i * 21, it.name, color);
      if (!buy) ui.text(x + 34 + 250, y + 118 + i * 21, `x${it.pile}`, color);
      if (buy && it.fame > -1) ui.text(x + 242, y + 118 + i * 21, `${pad(it.fame, 8)}Ｆ${pad(it.price, 8)}Ｓ`, color);
      else if (!draw) ui.text(x + 312, y + 118 + i * 21, `${pad(it.price, 8)}Ｓ`, color);
      if (ui.clicked(`shop-item${k}`)) this.pick(k, it);
    }
    if (focus) {
      ui.image(focus.image, x + 64, y + 350);
      split(focus.info.trimEnd(), 28, 3).forEach((l, i) => ui.text(x + 120, y + 308 + i * 20, l, FONT.WHITE));
    }
    const back = m.sellOnly ? CG.EXIT_BTN : CG.RETURN_BTN;
    if (ui.button(back, x + 216, y + 402, "shop-back")) this.send("0");
  }

  private pick(k: number, it: ShopItem) {
    if (!this.win || this.step !== "list") return;
    const gold = this.host.gold();
    this.sel = k; this.count = 1;
    const m = this.win;
    if (m.mode === "pool") {
      if (!it.sealed && it.price <= gold && m.rest > 0) this.step = "confirm";
    } else if (m.mode === "draw") {
      if (!it.sealed && this.host.freeSlots() > 0) this.step = "confirm";
    } else if (m.mode === "buy") {
      if (it.sealed || it.price > gold || it.fame > m.fame) return; // 原版播放不能选的音效
      if (this.host.freeSlots() <= 0) this.step = "full";
      else this.step = it.level > this.host.level() ? "level" : "count";
    } else {
      if (it.sealed) return;
      this.step = it.price + gold > this.win.maxGold ? "stone" : "count";
    }
  }

  /** shopWindow3（买）/ shopWindow10（卖）：440×160 的个数窗口，▼▲ 调个数，ＯＫ / 取消 */
  private drawCount(ui: Ui, m: S2CShopWindow) {
    const w = 440, h = 160, x = (800 - w) / 2, y = (600 - h) / 2;
    const it = m.items[this.sel], gold = this.host.gold(), buy = m.mode === "buy";
    ui.image(CG.ITEMSHOP_KOSU_WIN, x + w / 2, y + h / 2);
    if (buy && m.what) ui.text(x + (w - measure(m.what)) / 2, y + 20, m.what, FONT.WHITE);
    ui.text(x + 16, y + 52, it.name, FONT.WHITE);
    ui.text(x + 300, y + 52, `${pad(this.count, 2)}个`, FONT.WHITE);
    ui.text(x + (buy ? 16 : 146), y + 82, `${pad(it.price, 8)}Ｓ x ${pad(this.count, 2)} = ${pad(it.price * this.count, 8)}Ｓ`, FONT.WHITE);
    if (buy && it.fame > -1) ui.text(x + 16, y + 102, `${pad(it.fame, 8)}Ｆ x ${pad(this.count, 2)} = ${pad(it.fame * this.count, 8)}Ｆ`, FONT.WHITE);
    if (buy) ui.text(x + 248, y + 100, `声望 ${pad(m.fame, 8)}Ｆ`, FONT.WHITE);
    ui.text(x + 248, y + 120, `金钱 ${pad(gold, 8)}Ｓ`, FONT.WHITE);
    if (ui.button(CG.DOWN_BTN, x + 364, y + 60, "shop-down", CG.DOWN_BTN_DOWN) && this.count > 1) this.count--;
    if (ui.button(CG.UP_BTN, x + 404, y + 60, "shop-up", CG.UP_BTN_DOWN)) {
      const n = this.count + 1;
      const ok = buy ? n <= this.host.freeSlots() && it.price * n <= gold && Math.max(0, it.fame) * n <= m.fame
        : n <= it.pile && it.price * n + gold <= m.maxGold;
      if (ok) this.count = n;
    }
    if (ui.button(CG.OK_BTN, x + 64, y + 140, "shop-ok")) {
      if (buy) this.step = "confirm";
      else this.send(`${it.slot}|${this.count}`);
    } else if (ui.button(CG.CANCEL_BTN, x + 164, y + 140, "shop-cancel")) this.step = "list";
  }

  /** shopWindow4 / shopWindow5：6×2 格，两行文字，是 / 否 */
  private drawAsk(ui: Ui, text: string, yes: () => void, no: () => void) {
    const w = 6, h = 2, x = (800 - w * 64) / 2, y = (600 - h * 48) / 2;
    ui.window(x, y, w, h);
    split(text, 38, 2).forEach((l, i) => ui.text(x + 20, y + 20 + i * 20, l, FONT.WHITE));
    if (ui.button(CG.YES_BTN, x + 128, y + 72, "shop-yes")) yes();
    else if (ui.button(CG.NO_BTN, x + 256, y + 72, "shop-no")) no();
  }

  /** shopWindow6 / shopWindow8：只有"是"，回到清单 */
  private drawOk(ui: Ui, text: string) {
    const w = 6, h = 2, x = (800 - w * 64) / 2, y = (600 - h * 48) / 2;
    ui.window(x, y, w, h);
    split(text, 38, 2).forEach((l, i) => ui.text(x + 20, y + 20 + i * 20, l, FONT.WHITE));
    if (ui.button(CG.YES_BTN, x + 192, y + 72, "shop-yes")) this.step = "list";
  }
}
