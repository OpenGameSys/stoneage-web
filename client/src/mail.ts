// 名片匣与邮件（menu.cpp mailWndNo，Ctrl+E 或任务栏的邮件）：
//   名片一览 MAIL_WND_VIEW：MakeWindowDisp(4, 4, 272, 440)，底图 CG_MAIL_WND 26082；每页 4 张、20 页，第 k 张在 y + 96k：
//     头像 (44, 68)、名字 (80, 30)（点了看邮件历史）、等级 (104, 60)、星球名 (198, 30)、转生 (152, 59)、DP (103, 88)、
//     在线图 (225, 60)、写信钮 (225, 80)、删除钮 (225, 100)；有没看的信时左边一封信的动画
//   选择 MAIL_WND_SELECT：普通邮件 / 宠物邮件（没有邮件宠物时红字）
//   普通邮件 MAIL_WND_SEND：底图 26200（272×304），收件人可以选好几个；5 秒只能寄一次
//   宠物邮件 MAIL_WND_PET_SEND：底图 26201，收件人只能一个，可以带一件能宠物邮寄的道具（道具窗口里双击）
//   邮件历史 MAIL_WND_HISTORY：底图 26203，每人 20 封（原版存在 data\mail.dat，这里存 localStorage sa.mail.<角色名>）
// 输入框原版是 28 字节一行的多行框，这里用一行（最多 140 字节）。

import type { AddressEntry, InvItem, PetInfo } from "./proto.gen.ts";
import { FONT, InputBox, boxColor, measure, type Ui } from "./ui.ts";

const CG = {
  WND: 26082, SEND_WND: 26200, PET_SEND_WND: 26201, ITEM_BTN: 26202, HISTORY_WND: 26203,
  ON_LINE: 26084, OFF_LINE: 26085, MAIL_BTN: 26086, DELETE_BTN: 26087, CLEAR_BTN: 26172, SEND_BTN: 26099, SEND_BTN_DOWN: 26174,
  PREV: 26180, NEXT: 26182, CLOSE: 26042, RETURN: 26043, ITEM_WND: 26061, ITEM_TITLE: 26072, SPR_MAIL: 100507,
};
const MAX_BOOK = 80, PER_PAGE = 4, HISTORY_MAX = 20;
const TRANS = ["零", "壹", "贰", "参", "肆", "伍"];

interface Mail { date: string; text: string; unread: boolean; pet?: { sprite: number; level: number; name: string }; item?: number }

export interface MailHost {
  ui: Ui;
  canvas: HTMLCanvasElement;
  name(): string;                 // 自己的名字（历史存档的键）
  planet(): string;               // 线路名（名片上的星球名）
  pets(): PetInfo[];
  items(): InvItem[];
  mailPet(): number;
  clearMailPet(): void;
  inBattle(): boolean;
  send(index: number, text: string): void;                       // lssproto_MSG_send
  petSend(index: number, pet: number, item: number, text: string): void; // lssproto_PMSG_send
  remove(index: number): void;    // lssproto_DAB_send（主程序先问）
  lamp(on: boolean): void;
  chat(text: string, color: string): void;
}

export class MailWindow {
  open = false;
  private book: (AddressEntry | null)[] = Array(MAX_BOOK).fill(null);
  private history = new Map<number, Mail[]>();
  private key = "";
  private view: "view" | "select" | "send" | "pet" | "item" | "history" = "view";
  private page = 0;        // 名片一览的页
  private sendPage = 0;    // 收件人清单的页
  private picked = new Set<number>();
  private target = -1;     // 看历史的那一格
  private histIdx = 0;
  private attach = -1;     // 宠物邮件带的道具格位
  private input: InputBox | null = null;
  private sentAt = -1e9;
  private host: MailHost;
  constructor(host: MailHost) { this.host = host; }

  // ---------------- 资料 ----------------

  /** AB：整个名片匣；名片被删掉的那一格的历史也清掉 */
  setBook(entries: AddressEntry[]) {
    for (const e of entries) {
      if (!e.used && this.book[e.slot]) this.history.delete(e.slot);
      this.book[e.slot] = e.used ? e : null;
    }
    this.save();
  }
  /** ABI：一格 */
  update(entries: AddressEntry[]) { this.setBook(entries); }

  /** 上线时读这个角色的邮件历史 */
  load(name: string) {
    this.key = `sa.mail.${name}`;
    this.history.clear();
    try {
      const saved = JSON.parse(localStorage.getItem(this.key) ?? "{}");
      for (const k in saved) this.history.set(Number(k), saved[k]);
    } catch { /* 坏掉就重来 */ }
    this.host.lamp(this.anyUnread());
  }
  private save() {
    if (!this.key) return;
    const out: Record<number, Mail[]> = {};
    for (const [k, v] of this.history) out[k] = v;
    try { localStorage.setItem(this.key, JSON.stringify(out)); } catch { /* 存不了就算了 */ }
  }
  private anyUnread() { for (const v of this.history.values()) if (v.some((m) => m.unread)) return true; return false; }

  /** lssproto_MSG_recv：放进那个人的历史（20 封的环），说"收到%s送来的邮件！" */
  receive(index: number, date: string, text: string, pet: { sprite: number; level: number; name: string } | null, item: number) {
    const list = this.history.get(index) ?? [];
    list.push({ date, text, unread: true, pet: pet ?? undefined, item: item >= 0 ? item : undefined });
    while (list.length > HISTORY_MAX) list.shift();
    this.history.set(index, list);
    this.save();
    const who = this.book[index]?.name ?? "";
    this.host.chat(pet ? `收到${who}送来的宠物邮件！` : `收到${who}送来的邮件！`, FONT.WHITE);
    this.host.lamp(true);
  }

  // ---------------- 窗口 ----------------

  toggle() {
    this.open = !this.open;
    this.view = "view";
    this.closeInput();
    if (this.open) { // 跳到第一封没看的信那一页
      for (let i = 0; i < MAX_BOOK; i++) if (this.history.get(i)?.some((m) => m.unread)) { this.page = Math.floor(i / PER_PAGE); break; }
    }
  }

  private closeInput() { this.input?.remove(); this.input = null; }
  private openInput(x: number, y: number) {
    if (this.input) return;
    const box = new InputBox(this.host.canvas, x, y, 248, 18, { maxLength: 70 });
    box.el.addEventListener("keydown", (ev) => ev.stopPropagation());
    this.input = box;
    setTimeout(() => box.focus(), 0);
  }

  frame(rects: [number, number, number, number][]) {
    if (!this.open) return;
    switch (this.view) {
      case "view": this.listView(rects); break;
      case "select": this.selectView(rects); break;
      case "send": this.sendView(rects, false); break;
      case "pet": this.sendView(rects, true); break;
      case "item": this.itemView(rects); break;
      case "history": this.historyView(rects); break;
    }
  }

  private listView(rects: [number, number, number, number][]) {
    const ui = this.host.ui, x = 4, y = 4;
    ui.hitBox(x, y, x + 272, y + 440, "mail-wnd", false);
    rects.push([x, y, x + 272, y + 440]);
    ui.image(CG.WND, x + 136, y + 220);
    for (let k = 0; k < PER_PAGE; k++) {
      const i = this.page * PER_PAGE + k, e = this.book[i], by = y + 96 * k;
      if (!e) continue;
      if (e.face) ui.image(e.face, x + 44, by + 68);
      const nx = x + 80 + Math.max(0, (16 * 7 - measure(e.name)) / 2);
      if (ui.text(nx, by + 30, e.name, FONT.WHITE, `mail-name${i}`)) { this.target = i; this.openHistory(); }
      ui.text(x + 104, by + 60, String(e.level).padStart(3, " "), FONT.WHITE);
      if (e.online) ui.text(x + 198, by + 30, this.host.planet(), FONT.GREEN);
      ui.text(x + 152, by + 59, TRANS[Math.min(5, e.trans)] ?? "", FONT.AQUA);
      ui.text(x + 103, by + 88, String(e.dp).padStart(8, " "), FONT.WHITE);
      ui.image(e.online ? CG.ON_LINE : CG.OFF_LINE, x + 225, by + 60);
      if (ui.image(CG.MAIL_BTN, x + 225, by + 80, `mail-write${i}`) && ui.clicked(`mail-write${i}`)) {
        this.picked = new Set([i]);
        this.view = this.host.inBattle() || this.host.mailPet() < 0 ? "send" : "select";
      }
      if (ui.image(CG.DELETE_BTN, x + 225, by + 100, `mail-del${i}`) && ui.clicked(`mail-del${i}`)) this.host.remove(i);
      if (this.history.get(i)?.some((m) => m.unread)) { // 没看的信：一封信的动画
        const def = ui.assets.anim(CG.SPR_MAIL), act = def?.actions[0];
        if (act && act.frames.length) {
          const [img, fx, fy] = act.frames[Math.floor(performance.now() / (Math.max(1, act.ticks) * (1000 / 60))) % act.frames.length];
          if (img >= 0) ui.image(img, x - 20 + fx, by + 38 + fy, undefined, true);
        }
      }
    }
    const pages = MAX_BOOK / PER_PAGE;
    if (ui.button(CG.PREV, x + 45, y + 421, "mail-prev", CG.PREV + 1)) this.page = (this.page + pages - 1) % pages;
    ui.text(x + 66, y + 413, String(this.page + 1).padStart(2, " "), FONT.WHITE);
    if (ui.button(CG.NEXT, x + 103, y + 421, "mail-next", CG.NEXT + 1)) this.page = (this.page + 1) % pages;
    if (ui.image(CG.CLOSE, x + 192, y + 421, "mail-close") && ui.clicked("mail-close")) this.toggle();
  }

  /** 普通邮件 / 宠物邮件 */
  private selectView(rects: [number, number, number, number][]) {
    const ui = this.host.ui, x = 40, y = 40;
    ui.window(x, y, 3, 3, "mail-sel");
    rects.push([x, y, x + 192, y + 144]);
    if (ui.text(x + 28, y + 32, "  普 通 邮 件   ", FONT.WHITE, "mail-normal")) this.view = "send";
    const hasPet = this.host.mailPet() >= 0;
    if (ui.text(x + 28, y + 64, "  宠 物 邮 件   ", hasPet ? FONT.WHITE : FONT.RED, "mail-petsel") && hasPet) { this.view = "pet"; this.attach = -1; }
    if (ui.image(CG.RETURN, x + 96, y + 112, "mail-selback") && ui.clicked("mail-selback")) this.view = "view";
  }

  /** 收件人清单（4 个一页）：离线灰、在线白、选中水蓝；普通邮件可以选好几个，宠物邮件只能一个 */
  private recipients(x: number, top: number, single: boolean) {
    const ui = this.host.ui;
    const list = this.book.map((e, i) => e ? i : -1).filter((i) => i >= 0);
    const pages = Math.max(1, Math.ceil(list.length / PER_PAGE));
    this.sendPage = Math.min(this.sendPage, pages - 1);
    list.slice(this.sendPage * PER_PAGE, this.sendPage * PER_PAGE + PER_PAGE).forEach((i, k) => {
      const e = this.book[i]!;
      const color = this.picked.has(i) ? FONT.AQUA : e.online ? FONT.WHITE : FONT.GRAY;
      if (!ui.text(x + 111, top + 23 * k, e.name, color, `mail-to${i}`)) return;
      if (this.picked.has(i)) this.picked.delete(i);
      else if (!single || this.picked.size === 0) this.picked.add(i);
    });
    return pages;
  }

  private sendView(rects: [number, number, number, number][], pet: boolean) {
    const ui = this.host.ui, x = 4, y = 4, h = pet ? 440 : 304, mx = x + 136;
    ui.hitBox(x, y, x + 272, y + h, "mail-send", false);
    rects.push([x, y, x + 272, y + h]);
    ui.image(pet ? CG.PET_SEND_WND : CG.SEND_WND, mx, y + h / 2);
    if (pet && this.picked.size > 1) this.picked = new Set([...this.picked].slice(0, 1));
    const top = pet ? y + 177 : y + 41;
    const pages = this.recipients(x, top, pet);
    const py = pet ? y + 241 : y + 109;
    if (ui.button(CG.PREV, x + (pet ? 21 : 22), py, "mail-sprev", CG.PREV + 1)) this.sendPage = (this.sendPage + pages - 1) % pages;
    ui.text(x + (pet ? 42 : 43), py - 8, String(this.sendPage + 1).padStart(2, " "), FONT.WHITE);
    if (ui.button(CG.NEXT, x + (pet ? 79 : 80), py, "mail-snext", CG.NEXT + 1)) this.sendPage = (this.sendPage + 1) % pages;
    if (!pet && ui.button(CG.CLEAR_BTN, x + 51, y + 75, "mail-clear", CG.CLEAR_BTN + 1)) this.picked.clear();
    let mp: PetInfo | undefined;
    if (pet) {
      mp = this.host.pets().find((p) => p.slot === this.host.mailPet());
      if (mp) {
        ui.text(x + 120, y + 14, mp.freeName || mp.name, FONT.WHITE);
        const def = ui.assets.anim(mp.sprite), act = def?.actions.find((a) => a.dir === 0) ?? def?.actions[0];
        if (act && act.frames.length) {
          const [img, fx, fy] = act.frames[Math.floor(performance.now() / (Math.max(1, act.ticks) * (1000 / 60))) % act.frames.length];
          if (img >= 0) ui.image(img, x + 190 + fx, y + 120 + fy, undefined, true);
        }
      }
      const it = this.host.items().find((i) => i.slot === this.attach);
      if (it) ui.image(it.image, x + 56, y + 96);
      if (ui.hitBox(x + 26, y + 66, x + 86, y + 126, "mail-item") && ui.clicked("mail-item")) { this.view = "item"; this.closeInput(); return; }
      if (ui.hovered("mail-item")) ui.bottomLine = "选择要附带的道具。";
    }
    const ty = pet ? y + 280 : y + 144;
    this.openInput(x + 12, ty);
    const by = pet ? y + 421 : y + 285;
    if (ui.button(CG.CLEAR_BTN, mx + 60, pet ? y + 390 : y + 254, "mail-cleartext", CG.CLEAR_BTN + 1) && this.input) this.input.value = "";
    if (ui.button(CG.SEND_BTN, mx - 60, by, "mail-dosend", CG.SEND_BTN_DOWN)) this.doSend(pet, mp);
    if (ui.image(CG.RETURN, mx + 60, by, "mail-sendback") && ui.clicked("mail-sendback")) { this.view = "view"; this.closeInput(); }
  }

  private doSend(pet: boolean, mp: PetInfo | undefined) {
    const text = this.input?.value.trim() ?? "";
    const now = performance.now();
    if (!text || this.picked.size === 0 || now - this.sentAt < 5000) return; // ItemUseTime：5 秒一次
    if (pet) {
      const i = [...this.picked][0], e = this.book[i];
      if (!mp || !e) return;
      if (!e.online) { this.host.chat("该玩家不在线上！", FONT.RED); return; }
      this.host.petSend(i, mp.slot, this.attach, text);
      this.host.clearMailPet();
    } else {
      for (const i of this.picked) this.host.send(i, text);
    }
    this.sentAt = now;
    if (this.input) this.input.value = "";
    this.view = "view";
    this.closeInput();
  }

  /** 选附带的道具（MAIL_WND_ITEM）：双击能宠物邮寄的（flags 第 0 位） */
  private itemView(rects: [number, number, number, number][]) {
    const ui = this.host.ui, x = 4, y = 30, mx = x + 135, my = y + 140;
    ui.hitBox(x, y, x + 271, y + 281, "mail-itemwnd", false);
    rects.push([x, y, x + 271, y + 281]);
    ui.image(CG.ITEM_WND, mx, my);
    ui.image(CG.ITEM_TITLE, mx, my - 80);
    const items = this.host.items();
    for (let k = 0; k < 15; k++) {
      const slot = 9 + k, cx = x + 32 + 51 * (k % 5), cy = y + 56 + 48 * Math.floor(k / 5) + 40;
      const it = items.find((i) => i.slot === slot);
      const id = `mail-bag${k}`;
      const on = ui.hitBox(cx - 24, cy - 24, cx + 26, cy + 23, id, false);
      if (!it) continue;
      ui.image(it.image, cx, cy);
      if (on) { ui.box(cx - 24, cy - 24, cx + 26, cy + 23, boxColor(), false); ui.bottomLine = it.name; }
      if (ui.clicked(id) && it.flags & 1) { this.attach = slot; this.view = "pet"; }
    }
    if (ui.image(CG.CLOSE, mx, y + 262, "mail-itemclose") && ui.clicked("mail-itemclose")) this.view = "pet";
  }

  private openHistory() {
    const list = this.history.get(this.target) ?? [];
    const first = list.findIndex((m) => m.unread);
    this.histIdx = first >= 0 ? first : Math.max(0, list.length - 1);
    this.view = "history";
  }

  private historyView(rects: [number, number, number, number][]) {
    const ui = this.host.ui, x = 4, y = 4;
    ui.hitBox(x, y, x + 272, y + 424, "mail-hist", false);
    rects.push([x, y, x + 272, y + 424]);
    ui.image(CG.HISTORY_WND, x + 136, y + 212);
    const e = this.book[this.target];
    ui.text(x + 64, y + 34, e?.name ?? "", FONT.WHITE);
    const list = this.history.get(this.target) ?? [];
    const m = list[this.histIdx];
    if (m) {
      if (m.unread) { m.unread = false; this.save(); this.host.lamp(this.anyUnread()); }
      ui.text(x + 148, y + 236, m.date, FONT.WHITE);
      let line = "", w = 0, ly = y + 260;
      for (const ch of m.text) {
        const cw = ch.charCodeAt(0) > 0xff ? 2 : 1;
        if (w + cw > 28) { ui.text(x + 12, ly, line, FONT.WHITE); ly += 20; line = ""; w = 0; }
        line += ch; w += cw;
      }
      if (line) ui.text(x + 12, ly, line, FONT.WHITE);
      if (m.pet) ui.text(x + 12, y + 60, `宠物邮件：${m.pet.name}  Lv.${m.pet.level}`, FONT.YELLOW);
      if (m.item) ui.image(m.item, x + 40, y + 120);
    }
    const n = Math.max(1, list.length);
    if (ui.button(CG.PREV, x + 49, y + 405, "mail-hprev", CG.PREV + 1)) this.histIdx = (this.histIdx + n - 1) % n;
    ui.text(x + 71, y + 397, String(this.histIdx + 1).padStart(2, " "), FONT.WHITE);
    if (ui.button(CG.NEXT, x + 108, y + 405, "mail-hnext", CG.NEXT + 1)) this.histIdx = (this.histIdx + 1) % n;
    if (ui.image(CG.MAIL_BTN, x + 196, y + 371, "mail-reply") && ui.clicked("mail-reply")) {
      this.picked = new Set([this.target]);
      this.view = this.host.inBattle() || this.host.mailPet() < 0 ? "send" : "select";
    }
    if (ui.image(CG.RETURN, x + 196, y + 405, "mail-hback") && ui.clicked("mail-hback")) this.view = "view";
  }
}
