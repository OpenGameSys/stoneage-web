// 画面上方的按钮（field.cpp drawField / fieldProc，VER25_RELEASE）与"游戏设定"窗口（etcSwitch，Ctrl+Q）。
//   左上：底图 CG_FIELD_MENU_LEFT_NEW 28553 在 (148, 27)；按钮图都画在锚点上（图本身的偏移把同一锚点的按钮分开）：
//     设定 26101 (52, 28)、名片 26103 (52, 28)、组队 26105 (52, 28)、交易 26234 (114, 18)、家族 26237 (115, 28)、摆摊 35227 (176, 19)，
//     新邮件的灯 26107 在 (52, 28) 每秒闪一次
//   右上（x = 504 + 160）：底图 26260 在 (rx + 54, 33)、参战 26111 与决斗 26113 在 (rx + 38, 32)、动作 26115 在 (rx + 68, 32)、
//     任务查询（原图 55237 不在资源里，用旧的 26248 / 26249）在 (rx + 115, 33)
//   有窗口开着时整排按钮都不画（drawFieldButtonFlag = 0）；送出的按钮共用 500 毫秒的间隔（FIELD_BTN_PUSH_WAIT）。
// 与原版不同：骑宠查询、队长切换、商城的图不在资源里（这台服务端按了也没有作用），不画；右上的昼夜条原版要裁在底图的小窗里，
// 这里画固定的 CG_FIELD_MENU_RIGHT_BACK 26121；摆摊、任务查询还没做（按了说尚未开放）。

import { FONT, boxColor, type Ui } from "./ui.ts";

const CG = {
  LEFT: 28553, MENU: 26101, CARD: 26103, PARTY: 26105, TRADE: 26234, FAMILY: 26237, VENDOR: 35227, MAIL_LAMP: 26107,
  RIGHT: 26260, RIGHT_BACK: 26121, JOIN: 26111, DUEL: 26113, ACT: 26115, HELP: 26248, CLOSE: 26042,
};
const RX = 504 + 160;
const PUSH_WAIT = 500;

// FS 的位元（PC_ETCFLAG_*）
export const ETC = { PARTY: 1, DUEL: 2, PARTYCHAT: 4, CARD: 8, TRADE: 16, TELL: 32, FM: 64, SAVE: 256, WORLD: 1024, ALL_SERV: 2048 };

// 组队钮不能按的楼层（field.cpp 2538-2558，比服务端的多）
function noPartyFloor(f: number): boolean {
  return [31706, 10204, 10919, 10920, 20711, 20712, 1008, 1021, 3008, 3021, 7025, 17001, 17003, 17005, 7027].includes(f)
    || f >= 10601 && f <= 10605 || f >= 8200 && f <= 8213 || f >= 30017 && f <= 30021;
}

export interface PanelHost {
  ui: Ui;
  floor(): number;
  hidden(): boolean;                 // 有窗口开着
  inParty(): boolean;
  isLeader(): boolean;
  front(): { battle: boolean } | null; // 面前那一格的玩家
  walking(): boolean;
  etc(): number;                     // pc.etcFlag
  setEtc(flags: number): void;       // lssproto_FS_send
  setEtcLocal(flags: number): void;  // 先改画面上的值（送出可能要等间隔）
  party(join: boolean): void;        // lssproto_PR_send
  card(): void;                      // lssproto_AAB_send
  trade(): void;                     // Ctrl+T → TD "D|D"
  joinBattle(): void;                // lssproto_JB_send
  duel(): void;                      // lssproto_DU_send
  family(): void;                    // 家族功能
  broadcast(): boolean;              // 族长广播开着（pc.channel == 5，钮换成 CG_FIELD_CHANNEL_BTN_ON 26238）
  actions(): void;                   // 动作窗口
  quests(): void;                    // 任务查询窗口（quests.ts）
  mailLamp(): boolean;
  chat(text: string, color: string): void;
}

export class FieldPanel {
  private host: PanelHost;
  private pushAt = 0;
  etcOpen = false;          // menuBtn：游戏设定窗口
  private channelPage = false; // 频道开关（替换掉主窗口）
  private pendingFs = false;   // etcSwitchChangeFlag
  constructor(host: PanelHost) { this.host = host; }

  private canSend(): boolean {
    const now = performance.now();
    if (now - this.pushAt < PUSH_WAIT) return false;
    this.pushAt = now;
    return true;
  }

  toggleEtc() { this.etcOpen = !this.etcOpen; this.channelPage = false; }

  frame(rects: [number, number, number, number][]) {
    if (this.pendingFs && performance.now() - this.pushAt >= PUSH_WAIT) { this.pendingFs = false; this.pushAt = performance.now(); this.host.setEtc(this.host.etc()); }
    if (this.etcOpen) this.etcWindow(rects);
    if (this.host.hidden() || this.etcOpen) return;
    const ui = this.host.ui, h = this.host;
    // 原版画在 (148, 27)，但这套资源的 28553 锚点偏了 17 像素（左边出画面、底图上的 Mail Call 字和新邮件灯 26107 错开），右移对齐
    ui.image(CG.LEFT, 148 + 17, 27);
    rects.push([0, 0, 328, 54]);
    const tip = (id: string, text: string) => { if (ui.hovered(id)) ui.bottomLine = text; };
    const btn = (img: number, x: number, y: number, id: string, pressed = img + 1) => {
      const down = ui.mouse.down && ui.hovered(id);
      ui.image(down ? pressed : img, x, y, id);
      return ui.clicked(id);
    };
    if (btn(CG.MENU, 52, 28, "fp-menu")) this.toggleEtc();
    tip("fp-menu", "游戏设定。");
    if (btn(CG.CARD, 52, 28, "fp-card") && !h.walking() && !h.inParty() && h.front() && this.canSend()) h.card();
    tip("fp-card", "交换名片。");
    const partyOk = !noPartyFloor(h.floor());
    if (btn(CG.PARTY, 52, 28, "fp-party") && partyOk) {
      if (h.inParty()) { if (this.canSend()) h.party(false); }
      else { const f = h.front(); if (f && !f.battle && this.canSend()) h.party(true); }
    }
    tip("fp-party", partyOk ? "加入队伍。" : "不能加入队伍。");
    if (h.mailLamp() && Math.floor(performance.now() / 1000) % 2 === 0) ui.image(CG.MAIL_LAMP, 52, 28);
    if (btn(CG.TRADE, 114, 18, "fp-trade") && !h.walking() && !h.inParty() && h.front() && this.canSend()) h.trade();
    tip("fp-trade", "进行交易。");
    if (btn(h.broadcast() ? CG.FAMILY + 1 : CG.FAMILY, 115, 28, "fp-family", CG.FAMILY + 1)) h.family();
    tip("fp-family", "家族功能。");
    if (btn(CG.VENDOR, 176, 19, "fp-vendor", CG.VENDOR - 1)) h.chat("摆摊功能尚未开放。", FONT.RED);
    tip("fp-vendor", "摆摊功能。");

    // 右上
    ui.image(CG.RIGHT, RX + 54, 33);
    ui.image(CG.RIGHT_BACK, RX + 68, 32);
    rects.push([RX - 28, 0, RX + 136, 63]);
    const f = h.floor();
    const watchOnly = f < 10000 && Math.floor(f / 1000) > 0 && f % 1000 === 7 || f === 130;
    const noJoin = [1008, 1021, 3008, 3021, 10919, 10920, 20711, 20712, 10204].includes(f);
    if (btn(CG.JOIN, RX + 38, 32, "fp-join") && !noJoin && (!h.inParty() || h.isLeader())) {
      const t = h.front();
      if (t?.battle && this.canSend()) h.joinBattle();
    }
    tip("fp-join", watchOnly ? "观战。" : noJoin ? "不能加入战斗。" : "加入战斗。");
    if (btn(CG.DUEL, RX + 38, 32, "fp-duel") && (!h.inParty() || h.isLeader()) && h.front() && this.canSend()) h.duel();
    tip("fp-duel", "跟其他玩家决斗。");
    if (btn(CG.HELP, RX + 115, 33, "fp-help")) h.quests();
    tip("fp-help", "任务查询。");
    if (btn(CG.ACT, RX + 68, 32, "fp-act")) h.actions();
    tip("fp-act", "人物的各种动作。");
  }

  /** 游戏设定（etcSwitch）：MakeWindowDisp(16, 16, 3, 5)，每行 (x + 20, y + 26 + 30i)，值在 x + 105，关闭钮在 (x + 96, 216) */
  private etcWindow(rects: [number, number, number, number][]) {
    const ui = this.host.ui, x = 16, y = 16;
    ui.window(x, y, 3, 5, "etc-wnd");
    rects.push([x, y, x + 192, y + 240]);
    const rows: [string, number, string][] = this.channelPage
      ? [["密语频道", ETC.TELL, ""], ["家族频道", ETC.FM, ""], ["对话储存", ETC.SAVE, ""], ["世界频道", ETC.WORLD, ""], ["星球频道", ETC.ALL_SERV, ""]]
      : [["组    队：", ETC.PARTY, "设定是否接受其他人加入你的队伍。"], ["决    斗：", ETC.DUEL, "设定是否接受其他人的挑战。"],
        ["交换名片：", ETC.CARD, "设定是否接受名片交换。"], ["频    道    开    关", 0, "设定各个频道的开启或关闭。"],
        ["交    易：", ETC.TRADE, "设定是否接受交易。"]];
    rows.forEach(([label, bit, hint], i) => {
      const id = `etc-${i}`, ry = y + 26 + 30 * i;
      const on = ui.hitBox(x + 18, y + 23 + 30 * i, x + 175, y + 45 + 30 * i, id, false);
      if (on) { ui.box(x + 18, y + 23 + 30 * i, x + 175, y + 45 + 30 * i, boxColor(), false); if (hint) ui.bottomLine = hint; }
      ui.text(x + 20, ry, label, FONT.WHITE);
      if (bit) ui.text(x + 105, ry, this.host.etc() & bit ? " ＹＥＳ" : " Ｎ  Ｏ", FONT.WHITE);
      if (!ui.clicked(id)) return;
      if (!bit) { this.channelPage = true; return; }
      let flags = this.host.etc() ^ bit;
      if (bit === ETC.FM && flags & bit) { this.host.chat("你尚未加入家族", FONT.RED); flags &= ~bit; }
      this.host.setEtcLocal(flags);
      if (performance.now() - this.pushAt >= PUSH_WAIT) { this.pushAt = performance.now(); this.host.setEtc(flags); } else this.pendingFs = true;
    });
    if (ui.image(CG.CLOSE, x + 96, 216, "etc-close") && ui.clicked("etc-close")) { this.etcOpen = false; this.channelPage = false; }
    if (ui.hovered("etc-close")) ui.bottomLine = "关闭选单视窗。";
  }
}
