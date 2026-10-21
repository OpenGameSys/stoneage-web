// GM 指令窗口（额外新功能，原版没有）：服务端只给 config.yaml 的 gms 里的账号发 GmMenu，
// 收到了系统窗口才出现"GM 指令"；别的玩家的客户端没有这份清单，送 GmCommand 服务端也不执行。
// 上面一排是分页（GmItem.group），左边是这一页的指令，右边填参数、按"执行"（或在输入框按 Enter）。
// 参数的输入方式按 GmItem.kinds（server/internal/world/gm.go 的 gmMenu）：直接打字、从清单挑（道具 / 敌人 / 玩家，
// 清单向服务端要一次就记着，玩家每次重要）、选自己的宠物、几个固定的选项。服务端的回应（ChatMsg，from "GM"）列在下方。

import type { GmEntry, GmItem, PetInfo, S2CGmDetail, S2CStatus } from "./proto.gen.ts";
import { ANIM, findAction, type Assets } from "./assets.ts";
import { FONT, InputBox, measure, wrapBytes, type Ui } from "./ui.ts";

export interface GmHost {
  ui: Ui;
  canvas: HTMLCanvasElement;
  send(text: string): void;           // GmCommand
  list(kind: string): void;           // GmListReq
  detail(kind: string, id: number): void; // GmDetailReq（清单里滑鼠停着的那一项）
  place(): [number, number, number];  // 楼层、x、y（预设值 $floor / $x / $y）
  status(): S2CStatus | null;         // 预设值 $level、$vit 等
  pets(): PetInfo[];                  // 参数种类 pet
}

const W = 9, H = 9; // 576×432
const ROW = 22;
const PICK_ROWS = 10;
const PICK_TITLE: Record<string, string> = { item: "选择道具", enemy: "选择敌人（给宠物）", player: "选择玩家", skill: "选择宠物技能" };
const HIST_TAB = "记录";
const HIST_KEY = "sa.gm.history";
const HIST_MAX = 300;
const HIST_ROWS = 14;

/** 一个参数怎么输入：打字（含可从清单挑的）、选项、宠物 */
function argMode(kind: string): "text" | "opt" | "pet" {
  if (kind.startsWith("opt:")) return "opt";
  return kind === "pet" ? "pet" : "text";
}

export class GmWindow {
  open = false;
  items: GmItem[] = [];
  private group = "";
  private sel = 0;                         // items 的下标
  private boxes: (InputBox | null)[] = []; // 每个参数一个；选项与宠物没有输入框
  private vals: string[] = [];             // 选项与宠物的值
  private lists = new Map<string, GmEntry[]>();
  private picker: { arg: number; kind: string; page: number; search: InputBox; filter: string } | null = null;
  // 清单的详情（GmDetail）：kind:id → 图与文字；hover 是最后停着的那一项
  private details = new Map<string, S2CGmDetail | null>();
  private hover: { kind: string; id: number } | null = null;
  // 执行记录（"记录"分页）：时间、种类（cmd 送出的指令 / reply 服务端的回应 / note 客户端的提示）、文字；存 localStorage
  private history: { at: string; kind: "cmd" | "reply" | "note"; text: string }[] = [];
  private histPage = -1;           // -1 是最后一页
  private host: GmHost;

  constructor(host: GmHost) {
    this.host = host;
    try { this.history = JSON.parse(localStorage.getItem(HIST_KEY) ?? "[]"); } catch { this.history = []; }
    if (!Array.isArray(this.history)) this.history = [];
  }


  /** 有选单才是 GM */
  get enabled() { return this.items.length > 0; }

  setMenu(items: GmItem[]) {
    this.items = items;
    if (this.sel >= items.length) this.sel = 0;
    this.group = items[this.sel]?.group ?? "";
  }

  /** 服务端回来的详情（GmDetail） */
  setDetail(m: S2CGmDetail) { this.details.set(`${m.kind}:${m.id}`, m); }

  /** 服务端回来的清单（GmList） */
  setList(kind: string, entries: GmEntry[]) { this.lists.set(kind, entries); }

  show() { if (this.enabled) { this.open = true; this.pick(this.sel); } }

  close() { this.open = false; this.closePicker(); this.removeBoxes(); this.showOthers(); }

  // 别的窗口的 DOM 输入框（道具窗口的石币、邮件、家族……）开着 GM 窗口时藏起来，关掉再放回来
  private hidden = new Set<InputBox>();
  private hideOthers() {
    const mine = new Set<InputBox>([...this.boxes.filter((b): b is InputBox => b !== null), ...(this.picker ? [this.picker.search] : [])]);
    for (const b of InputBox.live) if (!mine.has(b) && !this.hidden.has(b)) { b.el.style.visibility = "hidden"; this.hidden.add(b); }
  }
  private showOthers() { for (const b of this.hidden) b.el.style.visibility = ""; this.hidden.clear(); }

  /** 离开地图画面：拿掉 DOM 输入框（回来时 frame 重建） */
  leave() { this.closePicker(); this.removeBoxes(); this.showOthers(); }


  /** 服务端的回应 */
  reply(text: string) { this.addLog(text, "reply"); }

  private addLog(text: string, kind: "cmd" | "reply" | "note") {
    // 结果不再列在指令页（在聊天栏与"记录"分页）
    const d = new Date(), p = (n: number) => String(n).padStart(2, "0");
    this.history.push({ at: `${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`, kind, text });
    while (this.history.length > HIST_MAX) this.history.shift();
    this.histPage = -1;
    try { localStorage.setItem(HIST_KEY, JSON.stringify(this.history)); } catch { /* 存不了就算了 */ }
  }

  private removeBoxes() { for (const b of this.boxes) b?.remove(); this.boxes = []; }

  private origin(): [number, number] { return [(800 - W * 64) / 2, (600 - H * 48) / 2]; }

  private groups(): string[] { return [...new Set(this.items.map((i) => i.group)), HIST_TAB]; }

  private input(x: number, y: number, w: number, onEnter: () => void): InputBox {
    const b = new InputBox(this.host.canvas, x, y, w, 16, { maxLength: 100 });
    // 打字时不要触发游戏的快捷键（回车开聊天、Ctrl+字母开窗口）
    b.el.addEventListener("keydown", (e) => {
      e.stopPropagation();
      if (e.key === "Enter") onEnter();
      if (e.key === "Escape") { if (this.picker) this.closePicker(); else this.close(); }
    });
    return b;
  }

  /** 选一条指令：照参数建输入框，填上预设值 */
  private pick(i: number) {
    this.closePicker();
    this.removeBoxes();
    this.sel = i;
    const it = this.items[i];
    if (!it) return;
    this.group = it.group;
    const [x, y] = this.origin(), [floor, gx, gy] = this.host.place(), st = this.host.status();
    const attr = (k: number) => String(Math.round((st?.attrs[k] ?? 0) / 10));
    const subst: Record<string, string> = {
      $floor: String(floor), $x: String(gx), $y: String(gy), $level: String(st?.level ?? 1),
      $vit: String(st?.vital ?? 0), $str: String(st?.str ?? 0), $tgh: String(st?.tgh ?? 0), $dex: String(st?.dex ?? 0),
      $earth: attr(0), $water: attr(1), $fire: attr(2), $wind: attr(3),
    };
    this.vals = [];
    (it.args ?? []).forEach((_, k) => {
      const kind = it.kinds?.[k] ?? "", d = it.defaults?.[k] ?? "", v = subst[d] ?? d;
      if (argMode(kind) === "text") {
        const b = this.input(x + 250, this.argY(k) , kind ? 110 : 280, () => this.run());
        b.value = v;
        this.boxes.push(b);
        this.vals.push("");
      } else {
        this.boxes.push(null);
        this.vals.push(argMode(kind) === "pet" ? String(this.host.pets()[0]?.slot ?? "") : v);
      }
    });
    setTimeout(() => this.boxes.find((b) => b)?.focus(), 0);
  }

  /** 第 k 个参数那一行的 y（宠物、选项可能折成两行，按上面的参数推下来） */
  private argY(k: number): number {
    const [, y] = this.origin(), it = this.items[this.sel];
    let ay = y + 150;
    for (let j = 0; j < k; j++) ay += this.argRows(it?.kinds?.[j] ?? "") * 30;
    return ay;
  }

  private argRows(kind: string): number {
    const opts = this.options(kind);
    let w = 0, rows = 1;
    for (const [, label] of opts) { const lw = measure(label) + 14; if (w + lw > 290) { rows++; w = 0; } w += lw; }
    return rows;
  }

  /** 选项（[值, 显示]）：opt: 的固定选项，或自己的宠物 */
  private options(kind: string): [string, string][] {
    if (kind.startsWith("opt:")) return kind.slice(4).split("|").map((o) => [o, o]);
    if (kind === "pet") return this.host.pets().map((p) => [String(p.slot), `${p.slot + 1}.${p.freeName || p.name}(${p.level})`]);
    return [];
  }

  private run() {
    const it = this.items[this.sel];
    if (!it) return;
    const args = (it.args ?? []).map((_, k) => (this.boxes[k]?.value ?? this.vals[k] ?? "").trim());
    while (args.length && args[args.length - 1] === "") args.pop();
    if (args.includes("")) { this.addLog(`${it.name}：参数还没填完`, "note"); return; }
    const text = [it.cmd, ...args].join(" ");
    this.addLog(text, "cmd");
    this.host.send(text);
  }

  // ---------------- 清单（道具 / 敌人 / 玩家） ----------------

  private openPicker(arg: number, kind: string) {
    this.closePicker();
    if (!this.lists.has(kind) || kind === "player") { this.lists.delete(kind); this.host.list(kind); }
    for (const b of this.boxes) if (b) b.el.style.display = "none"; // DOM 输入框会盖在清单上
    const [x, y] = this.origin();
    const search = this.input(x + 8 + 90, y + 66 + 44, 196, () => {});
    search.el.addEventListener("input", () => { if (this.picker) { this.picker.filter = search.value.trim(); this.picker.page = 0; } });
    this.picker = { arg, kind, page: 0, search, filter: "" };
    this.hover = null;
    setTimeout(() => search.focus(), 0);
  }

  private closePicker() {
    if (!this.picker) return;
    this.picker.search.remove();
    this.picker = null;
    for (const b of this.boxes) if (b) b.el.style.display = "";
  }

  private pickerFrame(x: number, y: number) {
    const ui = this.host.ui, p = this.picker!;
    // 在分页那一排下面（y + 66）靠左开一个 5×7 的清单窗口（盖住指令列表），右边 4×7 是滑鼠停着那一项的详情
    const px = x + 8, py = y + 66, pw = 5 * 64;
    ui.window(px, py, 5, 7, "gm-pick");
    ui.text(px + 24, py + 20, PICK_TITLE[p.kind] ?? "选择", FONT.YELLOW);
    ui.text(px + 24, py + 46, "搜寻", FONT.WHITE);
    ui.box(px + 86, py + 41, px + 294, py + 63, "rgba(0,0,0,0.45)", true);
    ui.box(px + 86, py + 41, px + 294, py + 63, "rgba(255,255,255,0.35)", false);
    const all = this.lists.get(p.kind);
    if (!all) { ui.text(px + 24, py + 80, "读取中…", FONT.GRAY); }
    else {
      const f = p.filter.toLowerCase();
      const hit = f ? all.filter((e) => e.name.toLowerCase().includes(f) || String(e.id).startsWith(f)) : all;
      const pages = Math.max(1, Math.ceil(hit.length / PICK_ROWS));
      p.page = Math.min(p.page, pages - 1);
      hit.slice(p.page * PICK_ROWS, (p.page + 1) * PICK_ROWS).forEach((e, i) => {
        const ry = py + 78 + i * ROW, id = `gm-pk${i}`;
        const label = p.kind === "player" ? e.name : `${e.id}  ${e.name}`;
        if (ui.text(px + 24, ry, label, FONT.WHITE, id)) {
          const b = this.boxes[p.arg];
          if (b) b.value = p.kind === "player" ? e.name : String(e.id);
          this.closePicker();
          return;
        }
        // 说明放不下就截短，不超出窗口
        const ix = px + 24 + Math.max(150, measure(label) + 12);
        let info = e.info;
        while (info && measure(info) > px + pw - 24 - ix) info = info.length > 2 ? info.slice(0, -2) + "…" : "";
        ui.text(ix, ry, info, FONT.GRAY);
        if (ui.hovered(id)) {
          ui.bottomLine = `${e.name}　${e.info}`;
          const key = `${p.kind}:${e.id}`;
          this.hover = { kind: p.kind, id: e.id };
          if (!this.details.has(key)) { this.details.set(key, null); this.host.detail(p.kind, e.id); }
        }
      });
      if (!this.picker) return;
      const by = py + 78 + PICK_ROWS * ROW + 6;
      ui.text(px + 24, by, `${hit.length} 项 ${p.page + 1}/${pages} 页`, FONT.AQUA);
      if (p.page > 0 && ui.text(px + 170, by, "◀上一页", FONT.YELLOW, "gm-pk-prev")) p.page--;
      if (p.page < pages - 1 && ui.text(px + 238, by, "下一页▶", FONT.YELLOW, "gm-pk-next")) p.page++;
    }
    if (ui.text(px + pw - 64, py + 20, "取　消", FONT.AQUA, "gm-pk-close")) { this.closePicker(); return; }
    this.detailFrame(px + pw, py);
  }

  /** 详情窗口（4×7）：上面是图（道具的图、宠物与玩家的站立动画），下面是服务端给的几行字 */
  private detailFrame(dx: number, dy: number) {
    const ui = this.host.ui, h = this.hover, cx = dx + 128;
    ui.window(dx, dy, 4, 7, "gm-detail");
    const d = h ? this.details.get(`${h.kind}:${h.id}`) : undefined;
    if (!h) { wrapBytes("滑鼠移到清单上的一项，这里显示它的图与详情。", 28).forEach((l, i) => ui.text(dx + 18, dy + 24 + i * 18, l, FONT.GRAY)); return; }
    if (!d) { ui.text(dx + 18, dy + 24, "读取中…", FONT.GRAY); return; }
    let ty = dy + 22;
    if (d.image > 0) {
      if (d.image >= 100000 && d.image < 140000) {
        // 动画（宠物、玩家）：站立动作循环播放，脚底对齐
        const id = d.image;
        ui.actor({ frameOf: (assets: Assets, now: number) => {
          const def = assets.anim(id), act = def && findAction(def, 1, ANIM.STAND);
          if (!act || act.frames.length === 0) return null;
          const fr = act.frames[Math.floor(now / (Math.max(1, act.ticks) * 1000 / 60)) % act.frames.length];
          return fr[0] < 0 ? null : [fr[0], fr[1], fr[2]];
        } }, cx, dy + 100, performance.now(), false);
        ty = dy + 108;
      } else {
        ui.image(d.image, cx, dy + 50); // 道具的图（地图图号，画在中心）
        ty = dy + 84;
      }
    }
    const lines: [string, string][] = [];
    d.lines.forEach((l, i) => wrapBytes(l, 30).forEach((w) => lines.push([w, i === 0 ? FONT.YELLOW : FONT.WHITE])));
    const rows = Math.floor((dy + 7 * 48 - 14 - ty) / 17);
    lines.slice(0, rows).forEach(([l, c], i) => ui.text(dx + 18, ty + i * 17, l, c));
    if (lines.length > rows) ui.text(dx + 4 * 64 - 30, dy + 7 * 48 - 28, "…", FONT.YELLOW);

  }

  // ---------------- 每帧 ----------------

  frame(rects: [number, number, number, number][], esc: boolean) {
    if (!this.open) return;
    if (esc) { if (this.picker) this.closePicker(); else { this.close(); return; } }
    const ui = this.host.ui, [x, y] = this.origin();
    const it = this.items[this.sel], hist = this.group === HIST_TAB;
    if (!hist && this.boxes.length === 0 && (it?.args?.length ?? 0) > 0) this.pick(this.sel);
    this.hideOthers();
    ui.window(x, y, W, H, "gm-wnd");
    rects.push([x, y, x + W * 64, y + H * 48]);
    ui.text(x + W * 32 - 28, y + 16, "GM 指令", FONT.YELLOW);
    // 分页
    let tx = x + 28;
    for (const g of this.groups()) {
      const on = g === this.group;
      if (ui.text(tx, y + 44, g, on ? FONT.YELLOW : FONT.AQUA, `gm-g-${g}`) && !on) {
        if (g === HIST_TAB) { this.closePicker(); this.removeBoxes(); this.group = g; this.histPage = -1; }
        else this.pick(this.items.findIndex((m) => m.group === g));
        return;
      }
      if (on) ui.box(tx - 4, y + 60, tx + measure(g) + 4, y + 62, "#ffd84a", true);
      tx += measure(g) + 24;
    }
    ui.box(x + 24, y + 66, x + W * 64 - 24, y + 67, "rgba(255,255,255,0.25)", true);
    if (hist) { this.historyFrame(x, y); return; }
    // 这一页的指令（清单开着时被清单窗口盖住，不画）
    let ly = y + 80;
    if (!this.picker) this.items.forEach((m, i) => {
      if (m.group !== this.group) return;
      if (ui.text(x + 28, ly, m.name, i === this.sel ? FONT.YELLOW : FONT.WHITE, `gm-i${i}`) && i !== this.sel) this.pick(i);
      if (ui.hovered(`gm-i${i}`)) ui.bottomLine = `${m.cmd} ${(m.args ?? []).join(" ")}`;
      ly += ROW;
    });
    ui.box(x + 150, y + 76, x + 151, y + H * 48 - 24, "rgba(255,255,255,0.25)", true);
    if (it && !this.picker) { // 清单开着时右边让给清单（窗口是半透明的，叠着看不清）
      const rx = x + 170;
      ui.text(rx, y + 80, `${it.name}　${it.cmd}`, FONT.YELLOW);
      wrapBytes(it.hint || "", 52).slice(0, 2).forEach((l, k) => ui.text(rx, y + 104 + k * 20, l, FONT.AQUA));
      (it.args ?? []).forEach((a, k) => this.argRow(it, k, a, rx, x));
      const ry = this.argY(it.args?.length ?? 0) + 6;
      if (ui.text(rx, ry, "【 执  行 】", FONT.GREEN, "gm-run")) this.run();
      ui.text(rx + 120, ry, "结果写在聊天栏与\"记录\"分页", FONT.GRAY);
    }
    if (ui.text(x + W * 32 - 28, y + H * 48 - 36, "关　闭", FONT.AQUA, "gm-close")) this.close();
    if (this.picker) this.pickerFrame(x, y);
  }

  /** "记录"分页：执行过的指令与回应，新的在下面，一页 HIST_ROWS 行；可以翻页、清除 */
  private historyFrame(x: number, y: number) {
    const ui = this.host.ui;
    const lines: [string, string, string][] = [];
    for (const h of this.history) {
      const color = h.kind === "cmd" ? FONT.YELLOW : h.kind === "note" ? FONT.RED : FONT.WHITE;
      wrapBytes(h.kind === "cmd" ? `> ${h.text}` : h.text, 54).forEach((l, i) => lines.push([i === 0 ? h.at : "", l, color]));
    }
    const pages = Math.max(1, Math.ceil(lines.length / HIST_ROWS));
    const page = this.histPage < 0 ? pages - 1 : Math.min(this.histPage, pages - 1);
    if (lines.length === 0) ui.text(x + 40, y + 84, "还没有执行过指令。", FONT.GRAY);
    lines.slice(page * HIST_ROWS, (page + 1) * HIST_ROWS).forEach(([at, l, c], i) => {
      if (at) ui.text(x + 28, y + 80 + i * 20, at, FONT.GRAY);
      ui.text(x + 150, y + 80 + i * 20, l, c);
    });
    const by = y + 80 + HIST_ROWS * 20 + 6;
    ui.text(x + 28, by, `${this.history.length} 笔　第 ${page + 1} / ${pages} 页`, FONT.AQUA);
    if (page > 0 && ui.text(x + 250, by, "◀ 上一页", FONT.YELLOW, "gm-h-prev")) this.histPage = page - 1;
    if (page < pages - 1 && ui.text(x + 330, by, "下一页 ▶", FONT.YELLOW, "gm-h-next")) this.histPage = page + 1 >= pages - 1 ? -1 : page + 1;
    if (this.history.length && ui.text(x + 470, by, "清除记录", FONT.RED, "gm-h-clear")) {
      this.history = []; this.histPage = -1;

      try { localStorage.removeItem(HIST_KEY); } catch { /* */ }
    }
    if (ui.text(x + W * 32 - 28, y + H * 48 - 36, "关　闭", FONT.AQUA, "gm-close")) this.close();
  }

  /** 一个参数：名称 + 输入框（可从清单挑的多一个"选择"与挑到的名字）、或一排可点的选项 */
  private argRow(it: GmItem, k: number, label: string, rx: number, x: number) {
    const ui = this.host.ui, kind = it.kinds?.[k] ?? "", by = this.argY(k);
    ui.text(rx, by, label, FONT.WHITE);
    if (argMode(kind) === "text") {
      const w = kind ? 110 : 280;
      ui.box(x + 246, by - 3, x + 254 + w, by + 17, "rgba(0,0,0,0.45)", true);
      ui.box(x + 246, by - 3, x + 254 + w, by + 17, "rgba(255,255,255,0.35)", false);
      if (!kind) return;
      if (!this.picker && ui.text(x + 372, by, "选择", FONT.GREEN, `gm-sel${k}`)) this.openPicker(k, kind);
      // 打的编号对得上清单时，显示名字
      const v = this.boxes[k]?.value.trim() ?? "", hit = this.lists.get(kind)?.find((e) => String(e.id) === v || e.name === v);
      if (hit && kind !== "player") ui.text(x + 412, by, hit.name.length > 9 ? hit.name.slice(0, 9) + "…" : hit.name, FONT.AQUA);
      return;
    }
    const opts = this.options(kind);
    if (opts.length === 0) { ui.text(x + 250, by, kind === "pet" ? "（没有宠物）" : "", FONT.GRAY); return; }
    let ox = x + 250, oy = by;
    opts.forEach(([v, text]) => {
      const w = measure(text) + 14;
      if (ox + w > x + 250 + 290) { ox = x + 250; oy += 30; }
      const on = this.vals[k] === v;
      if (on) ui.box(ox - 3, oy - 3, ox + w - 9, oy + 17, "rgba(255,216,74,0.25)", true);
      if (ui.text(ox, oy, text, on ? FONT.YELLOW : FONT.WHITE, `gm-o${k}-${v}`)) this.vals[k] = v;
      ox += w;
    });
  }
}
