// 任务查询与追踪（画面上方的"任务查询"钮，原版 _JOBDAILY：客户端按钮送 lssproto_JOBDAILY，服务端 CHAR_JobDaily 照
// data/jobdaily.txt 把条件成立的行送回来，窗口每页 10 项列"编号 / 说明 / 状态"）。服务端这里上线就送、任务旗标一变就重送（QuestLog），
// 所以在这之上加了原版没有的追踪：
//   · 打勾的任务显示在地图画面右上角（楼层名下面），附服务端算好的提示（相关的任务交换 NPC 与楼层）；
//   · 新出现"进行中"的任务自动追踪并在聊天栏提示，变成"已完成"的提示后取消追踪；
//   · 追踪的编号按角色存 localStorage sa.quest.track.<角色名>。

import type { QuestEntry } from "./proto.gen.ts";
import { FONT, measure, type Ui } from "./ui.ts";

export interface QuestHost {
  ui: Ui;
  chat(text: string, color: string): void;
  se(no: number): void;     // 界面音效
  refresh(): void;          // 向服务端要一次（QuestReq）
}

const W = 8, H = 8;         // 窗口 512×384
const X = 16, Y = 84;       // 靠左放，右边留给任务的详细说明框（DW 格宽）
const DW = 4, DX = X + W * 64 + 8; // 说明框 256 宽，放在任务窗口右边
const PAGE = 10, ROW = 24;
const HUD_MAX = 4;          // 右上角最多列几项
const SE_ACCEPT = 202, SE_DONE = 211;

const isDone = (q: QuestEntry) => q.state.includes("完成") || q.state.includes("结束");

export class QuestWindow {
  open = false;
  quests: QuestEntry[] = [];
  private tracked = new Set<number>();
  private page = 0;
  private name = "";
  private loaded = false; // 收过第一份清单（之后的差异才提示）
  private host: QuestHost;

  constructor(host: QuestHost) { this.host = host; }

  /** 进地图画面：读这个角色追踪的编号 */
  load(name: string) {
    this.name = name; this.loaded = false; this.tracked = new Set();
    try {
      const v = JSON.parse(localStorage.getItem(`sa.quest.track.${name}`) ?? "[]");
      if (Array.isArray(v)) for (const n of v) if (typeof n === "number") this.tracked.add(n);
    } catch { /* 没有就算了 */ }
  }

  private save() {
    try { localStorage.setItem(`sa.quest.track.${this.name}`, JSON.stringify([...this.tracked])); } catch { /* */ }
  }

  /** 服务端送来的任务日志：和上一份比，新出现的行提示 */
  set(list: QuestEntry[], available: QuestEntry[] = []) {
    this.available = available;
    const before = new Set(this.quests.map((q) => `${q.id}|${q.state}`));
    if (this.loaded) {
      let changed = false;
      for (const q of list) {
        if (before.has(`${q.id}|${q.state}`)) continue;
        if (isDone(q)) {
          this.host.chat(`任务完成：${q.name}`, FONT.GREEN);
          this.host.se(SE_DONE);
          this.tracked.delete(q.id);
        } else {
          this.host.chat(`接到任务：${q.name}（${q.state}）${q.hint ? "　→ " + q.hint : ""}`, FONT.YELLOW);
          this.host.se(SE_ACCEPT);
          this.tracked.add(q.id);
        }
        changed = true;
      }
      if (changed) this.save();
    }
    this.quests = list; this.loaded = true;
    this.page = Math.min(this.page, Math.max(0, Math.ceil(list.length / PAGE) - 1));
  }

  toggle() { this.open = !this.open; if (this.open) { this.page = 0; this.host.refresh(); } }
  available: QuestEntry[] = []; // 可以接的任务（服务端 availableQuests）
  tab = 0;                      // 0 我的任务、1 可接任务

  /** 追踪中、还没完成的任务 */
  tracking(): QuestEntry[] { return this.quests.filter((q) => this.tracked.has(q.id) && !isDone(q)); }

  /** 地图画面右上角（楼层名下面）：追踪中的任务与提示 */
  hud(ui: Ui, top = 88) {
    let y = top;
    for (const q of this.tracking().slice(0, HUD_MAX)) {
      const t = `◆ ${q.name}`;
      ui.text(792 - measure(t), y, t, FONT.YELLOW); y += 17;
      if (q.hint) { // 提示只列第一个 NPC（全部在任务窗口里）
        const [first, ...rest] = q.hint.split("、"), h = `→ ${first}${rest.length ? " …" : ""}`;
        ui.text(792 - measure(h), y, h, FONT.AQUA); y += 17;
      }
    }
  }

  frame(rects: [number, number, number, number][], esc: boolean) {
    if (!this.open) return;
    if (esc) { this.open = false; return; }
    const ui = this.host.ui, x = X, y = Y;
    ui.window(x, y, W, H, "quest-wnd");
    rects.push([x, y, x + W * 64, y + H * 48]);
    const list = this.tab === 0 ? this.quests : this.available, avail = this.tab === 1;
    let hover = null as QuestEntry | null; // 在 forEach 里设，as 免得 TS 把它收窄成 null
    const pages = Math.max(1, Math.ceil(list.length / PAGE));
    // 分页：我的任务（原版的任务查询）/ 可接任务（额外新功能：现在找 NPC 就能接的委托在前；还差前置的"未满足"排在后面，灰字）
    const canTake = this.available.filter((q) => q.state === "可接").length;
    (["我的任务", `可接任务（${canTake}）`] as const).forEach((t, k) => {
      if (ui.text(x + 28 + k * 110, y + 16, k === this.tab ? `【${t}】` : `　${t}　`, k === this.tab ? FONT.YELLOW : FONT.AQUA, `quest-tab${k}`) && k !== this.tab) { this.tab = k; this.page = 0; }
    });
    const sub = `共 ${list.length} 项　第 ${this.page + 1} / ${pages} 页`;
    ui.text(x + W * 64 - 28 - measure(sub), y + 16, sub, FONT.GRAY);
    ui.text(x + 28, y + 44, "追踪", FONT.AQUA); ui.text(x + 84, y + 44, "编号", FONT.AQUA);
    ui.text(x + 136, y + 44, "任务", FONT.AQUA); ui.text(x + W * 64 - 28 - measure("状态"), y + 44, "状态", FONT.AQUA);
    ui.box(x + 24, y + 62, x + W * 64 - 24, y + 63, "rgba(255,255,255,0.25)", true);
    if (list.length === 0) ui.text(x + 28, y + 72, avail ? "现在没有可以接的任务。" : "目前没有任务记录。", FONT.GRAY);
    list.slice(this.page * PAGE, this.page * PAGE + PAGE).forEach((q, i) => {
      const ry = y + 70 + i * ROW, done = isDone(q), on = !avail && this.tracked.has(q.id), unmet = avail && q.state === "未满足";
      const rowId = `quest-r${i}`;
      if (ui.hitBox(x + 24, ry - 3, x + W * 64 - 24, ry + ROW - 5, rowId, false)) {
        ui.box(x + 24, ry - 3, x + W * 64 - 24, ry + ROW - 5, "rgba(255,255,255,0.08)", true);
        const who = (q.hint ?? "").split("　还差：")[0];
        ui.bottomLine = who ? `${q.name}：找 ${who}` : done ? `${q.name}：已经完成了。` : `${q.name}：还没有相关的提示。`;
        hover = q;
      }
      if (done || avail) ui.text(x + 32, ry, "　—", FONT.GRAY);
      else if (ui.text(x + 32, ry, on ? "[√]" : "[　]", on ? FONT.GREEN : FONT.WHITE, `quest-t${i}`)) {
        if (on) this.tracked.delete(q.id); else this.tracked.add(q.id);
        this.save();
      }
      ui.text(x + 84, ry, String(q.id), FONT.WHITE);
      ui.text(x + 136, ry, q.name, done || unmet ? FONT.GRAY : on ? FONT.YELLOW : FONT.WHITE);
      ui.text(x + W * 64 - 28 - measure(q.state), ry, q.state, done ? FONT.GREEN : unmet ? FONT.GRAY : FONT.YELLOW);
    });
    const by = y + H * 48 - 60;
    if (this.page > 0 && ui.text(x + 28, by, "《 上一页", FONT.AQUA, "quest-prev")) this.page--;
    if (this.page < pages - 1 && ui.text(x + W * 64 - 28 - measure("下一页 》"), by, "下一页 》", FONT.AQUA, "quest-next")) this.page++;
    ui.text(x + W * 32 - 120, by, "打勾的任务显示在画面右上角", FONT.GRAY);
    if (ui.text(x + W * 32 - 28, y + H * 48 - 36, "关　闭", FONT.AQUA, "quest-close")) this.open = false;
    if (hover) this.detail(hover, rects);
  }

  /** 滑鼠停着的那一项：右边的说明框列任务名、状态、找谁（每个 NPC 一行）、还差什么（每样一行，太长就折行） */
  private detail(q: QuestEntry, rects: [number, number, number, number][]) {
    const ui = this.host.ui, inner = DW * 64 - 40;
    const [who, need] = (q.hint ?? "").split("　还差：");
    const lines: [string, string][] = [];
    const add = (text: string, color: string, indent = "") => {
      let line = "";
      for (const ch of text) {
        if (line && measure(line + ch) > inner) { lines.push([line, color]); line = indent; }
        line += ch;
      }
      if (line) lines.push([line, color]);
    };
    add(q.name, FONT.YELLOW);
    add(`状态：${q.state}`, isDone(q) ? FONT.GREEN : q.state === "未满足" ? FONT.GRAY : FONT.WHITE);
    if (who) {
      lines.push(["找：", FONT.AQUA]);
      for (const n of who.split("、")) add(`　${n}`, FONT.WHITE, "　　");
    }
    if (need) {
      lines.push(["还差：", FONT.AQUA]);
      for (const n of need.split("；")) add(`・${n}`, FONT.WHITE, "　");
    }
    const rows = Math.max(2, Math.ceil((lines.length * 18 + 36) / 48));
    ui.window(DX, Y, DW, rows, "quest-detail");
    rects.push([DX, Y, DX + DW * 64, Y + rows * 48]);
    lines.forEach(([t, c], i) => ui.text(DX + 20, Y + 18 + i * 18, t, c));
  }
}
