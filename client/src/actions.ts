// 动作窗口（field.cpp FIELD_FUNC_ACT，Ctrl+W）：MakeWindowDisp(440, 16, 3, 6)，800 模式下 x > 320 平移到 600。
//   两栏 × 7 行加最后一行置中，文字在 (x + 84j + 20, y + 30i + 24)，热区 (x + 84j + 18, y + 30i + 21) 73×22；
//   点了就送 lssproto_AC（窗口不关），自己的姿势马上换；打呼、冷场只画在自己头上（不送服务端）；关闭钮 CG_CLOSE_BTN 在 (x + 96, y + 258)。
//   快捷键（actionShortCutKeyProc，站着不动时才有效）：Ctrl+1 坐下 2 挥手 3 点头 4 高兴 5 生气 6 悲伤 7 站立 8 走动 9 晕倒 0 攻击，
//   Ctrl+- 防御、Ctrl+= 受伤、Ctrl+\ 投掷。

import { FONT, boxColor, type Ui } from "./ui.ts";

const LABELS = [" 坐  下 ", " 站  立 ", " 挥  手 ", " 走  动 ", " 点  头 ", " 晕  倒 ", " 高  兴 ", " 攻  击 ", " 生  气 ", " 防  御 ",
  " 悲  伤 ", " 受  伤 ", " 投  掷 ", " 打  呼 ", " 冷  场 "];
const HINTS = ["坐下的动作。 (Ctrl+1)", "站立的动作。 (Ctrl+7)", "挥手的动作。 (Ctrl+2)", "走动的动作。 (Ctrl+8)", "点头的动作。 (Ctrl+3)",
  "晕倒的动作。 (Ctrl+9)", "高兴的动作。 (Ctrl+4)", "攻击的动作。 (Ctrl+0)", "生气的动作。 (Ctrl+5)", "防御的动作。 (Ctrl+-)",
  "悲伤的动作。 (Ctrl+6)", "受伤的动作。 (Ctrl+=)", "投掷的动作。 (Ctrl+\\)", "睡觉的动作。", "冷场的动作。"];
const CHG = [5, 3, 6, 4, 11, 2, 7, 0, 8, 10, 9, 1, 12]; // 按钮 → 动作编号（chgTbl）
const EMOTES = [100553, 100550];                          // SPR_sleep、SPR_stone
/** 快捷键（按动作编号排）：Ctrl+0、Ctrl+=、Ctrl+9、Ctrl+7、Ctrl+8、Ctrl+1、Ctrl+2、Ctrl+4、Ctrl+5、Ctrl+6、Ctrl+-、Ctrl+3、Ctrl+\ */
export const ACTION_KEYS = ["0", "=", "9", "7", "8", "1", "2", "4", "5", "6", "-", "3", "\\"];
const CLOSE_BTN = 26042;

export interface ActionHost {
  ui: Ui;
  act(no: number): void;     // 送 AC 并换自己的姿势
  emote(spr: number): void;  // 只在自己画面上
}

export class ActionWindow {
  open = false;
  private host: ActionHost;
  private lastAt = 0;
  constructor(host: ActionHost) { this.host = host; }

  toggle() { this.open = !this.open; }

  /** 送一个动作（500 毫秒内只能一次，fieldBtnPushTime） */
  act(no: number) {
    const now = performance.now();
    if (now - this.lastAt < 500) return;
    this.lastAt = now;
    this.host.act(no);
  }

  frame(rects: [number, number, number, number][]) {
    if (!this.open) return;
    const ui = this.host.ui, x = 600, y = 16;
    ui.window(x, y, 3, 6, "act-wnd");
    rects.push([x, y, x + 192, y + 288]);
    LABELS.forEach((label, k) => {
      const i = Math.floor(k / 2), j = k % 2;
      const bx = k === 14 ? x + 42 : x + 84 * j, by = y + i * 30;
      const id = `act-${k}`;
      const on = ui.hitBox(bx + 18, by + 21, bx + 18 + 73, by + 21 + 22, id, false);
      if (on) { ui.box(bx + 18, by + 21, bx + 18 + 73, by + 21 + 22, boxColor(), false); ui.bottomLine = HINTS[k]; }
      ui.text(bx + 20, by + 24, label, FONT.WHITE);
      if (!ui.clicked(id)) return;
      if (k < CHG.length) this.act(CHG[k]);
      else this.host.emote(EMOTES[k - CHG.length]);
    });
    if (ui.image(CLOSE_BTN, x + 96, y + 258, "act-close") && ui.clicked("act-close")) this.open = false;
    if (ui.hovered("act-close")) ui.bottomLine = "关闭动作视窗";
  }
}
