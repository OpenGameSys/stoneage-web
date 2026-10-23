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
const X = (800 - W * 64) / 2, Y = 84;
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
  set(list: QuestEntry[]) {
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
    const pages = Math.max(1, Math.ceil(this.quests.length / PAGE));
    ui.text(x + W * 32 - 32, y + 16, "任务查询", FONT.YELLOW);
    const sub = `共 ${this.quests.length} 项　第 ${this.page + 1} / ${pages} 页`;
    ui.text(x + W * 64 - 28 - measure(sub), y + 16, sub, FONT.GRAY);
    ui.text(x + 28, y + 44, "追踪", FONT.AQUA); ui.text(x + 84, y + 44, "编号", FONT.AQUA);
    ui.text(x + 136, y + 44, "任务", FONT.AQUA); ui.text(x + W * 64 - 28 - measure("状态"), y + 44, "状态", FONT.AQUA);
    ui.box(x + 24, y + 62, x + W * 64 - 24, y + 63, "rgba(255,255,255,0.25)", true);
    if (this.quests.length === 0) ui.text(x + 28, y + 72, "目前没有任务记录。", FONT.GRAY);
    this.quests.slice(this.page * PAGE, this.page * PAGE + PAGE).forEach((q, i) => {
      const ry = y + 70 + i * ROW, done = isDone(q), on = this.tracked.has(q.id);
      const rowId = `quest-r${i}`;
      if (ui.hitBox(x + 24, ry - 3, x + W * 64 - 24, ry + ROW - 5, rowId, false)) {
        ui.box(x + 24, ry - 3, x + W * 64 - 24, ry + ROW - 5, "rgba(255,255,255,0.08)", true);
        ui.bottomLine = q.hint ? `${q.name}：找 ${q.hint}` : done ? `${q.name}：已经完成了。` : `${q.name}：还没有相关的提示。`;
      }
      if (done) ui.text(x + 32, ry, "　—", FONT.GRAY);
      else if (ui.text(x + 32, ry, on ? "[√]" : "[　]", on ? FONT.GREEN : FONT.WHITE, `quest-t${i}`)) {
        if (on) this.tracked.delete(q.id); else this.tracked.add(q.id);
        this.save();
      }
      ui.text(x + 84, ry, String(q.id), FONT.WHITE);
      ui.text(x + 136, ry, q.name, done ? FONT.GRAY : on ? FONT.YELLOW : FONT.WHITE);
      ui.text(x + W * 64 - 28 - measure(q.state), ry, q.state, done ? FONT.GREEN : FONT.YELLOW);
    });
    const by = y + H * 48 - 60;
    if (this.page > 0 && ui.text(x + 28, by, "《 上一页", FONT.AQUA, "quest-prev")) this.page--;
    if (this.page < pages - 1 && ui.text(x + W * 64 - 28 - measure("下一页 》"), by, "下一页 》", FONT.AQUA, "quest-next")) this.page++;
    ui.text(x + W * 32 - 120, by, "打勾的任务显示在画面右上角", FONT.GRAY);
    if (ui.text(x + W * 32 - 28, y + H * 48 - 36, "关　闭", FONT.AQUA, "quest-close")) this.open = false;
  }
}
