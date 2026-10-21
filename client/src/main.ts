// H5 客户端入口。登录前的画面在 screens.ts，游戏画面在这里（FieldScreen）。

import { Assets } from "./assets.ts";
import { Sound, setSfx } from "./audio.ts";
import { MAP_BGM } from "./soundtable.ts";
import { Connection } from "./net.ts";
import { Field, MOVE, dirTo, type Label } from "./field.ts";
import { Renderer } from "./renderer.ts";
import { CharSelectScreen, LoginScreen, type App, type Screen, CG } from "./screens.ts";
import { FONT, FONT_FAMILY, FONT_SIZE, InputBox, Mouse, Ui, drawOverlay, fitOverlay, measure } from "./ui.ts";
import { FONT_PAL, Menus, settings } from "./menus.ts";
import { ACTION_KEYS } from "./actions.ts";
import { FieldPanel } from "./fieldpanel.ts";
import { BattleScreen } from "./battle.ts";
import { Shop } from "./shop.ts";
import { PetSkillShop, drawPetSelect } from "./petshop.ts";
import { AutoMap } from "./automap.ts";
import { loadAi } from "./ai.ts";
import { AiMemory } from "./aismart.ts";
import type { AddressEntry, CharSummary, GmEntry, GmItem, PartyMember, S2CBattleEnd, S2CFamilyStatus, S2CGmDetail, S2CInventory, S2CNpcWindow, S2CPets, S2CStatus, ServerGroup } from "./proto.gen.ts";

const params = new URLSearchParams(location.search);
const ASSETS = params.get("assets") ?? "/assets/";
// 连到别的服务端只给调试用（?debug&ws=…）：不然一条带 ?ws= 的网址就能把账号密码送到别人的服务器
const WS_URL = (params.has("debug") ? params.get("ws") : null) ?? `${location.protocol === "https:" ? "wss" : "ws"}://${location.host}/ws`;


const canvas = document.getElementById("view") as HTMLCanvasElement;
const overlay = document.getElementById("overlay") as HTMLCanvasElement;
const ctx2d = overlay.getContext("2d")!;
const renderer = new Renderer(canvas);
const assets = new Assets(ASSETS, renderer);
const audio = new Sound(ASSETS, () => settings); // 声音（audio.ts）
setSfx((no, x) => audio.se(no, x));
// 界面音效：这一帧放过开关窗口 / 错误等的声音，就不再放点击声 217（原版每个按钮只放一个声音）
let uiSeFrame = false, openPrev = 0;
function uiSe(no: number) { audio.se(no); uiSeFrame = true; }

// 战斗的背景音乐（battleproc.cpp BATTLE_SUBPROC_INIT）：头目 / 事件 / 决斗 6（伊甸 13，8519 楼 14），一般 5（伊甸 12）；
// 泪之海那几层一律 24。伊甸是指被打断的地图音乐是 15..21。
const TEAR_FLOORS = [817, 8007, 8101, 8100, 8027, 8028, 8029, 8015, 8113, 8114];
function battleBgm(boss: boolean, floor: number, fieldBgm: number): number {
  const eden = fieldBgm >= 15 && fieldBgm <= 21;
  if (TEAR_FLOORS.includes(floor)) return 24;
  if (boss) return floor === 8519 ? 14 : eden ? 13 : 6;
  return eden ? 12 : 5;
}
const mouse = new Mouse();
mouse.attach(canvas);
const ui = new Ui(assets, mouse);

// ---------------------------------------------------------------------------
// 游戏内时间与昼夜调色板（handletime.cpp）：一个 SA 日 = 5400 秒，分 1024「时」；
// 早晨 (700,930]、傍晚 (200,300]、夜晚 (300,700]，其余为白天，对应 0..3 号调色板；切换时用 3600 帧渐变。

const SA_ERA = 912766409 + 5400, SA_DAY = 5400;
let serverTimeAt = { server: Math.floor(Date.now() / 1000), local: performance.now() };
function saHour(now: number): number {
  const secs = serverTimeAt.server + (now - serverTimeAt.local) / 1000 - SA_ERA;
  return Math.floor(((secs % SA_DAY) + SA_DAY) % SA_DAY * 1024 / SA_DAY);
}
function timeZone(hour: number): number {
  if (700 < hour && hour <= 930) return 3;   // 早晨
  if (200 < hour && hour <= 300) return 1;   // 傍晚
  if (300 < hour && hour <= 700) return 2;   // 夜晚
  return 0;                                   // 白天
}
const PAL_CHANGE_MS = 3600 * (1000 / 60);
const pal = { from: 0, to: 0, at: -1e9 };
function updatePalette(now: number, floorPalette: number) {
  const target = floorPalette >= 0 ? floorPalette : timeZone(saHour(now));
  if (target !== pal.to) { pal.from = pal.to; pal.to = target; pal.at = floorPalette >= 0 ? -1e9 : now; }
  const t = Math.min(1, (now - pal.at) / PAL_CHANGE_MS);
  renderer.paletteA = pal.from; renderer.paletteB = pal.to; renderer.paletteMix = t;
}

// ---------------------------------------------------------------------------
// 模态窗口：commonMsgWin（宽度随文字、居中、黄色ＯＫ）与 commonYesNoWindow

type Modal = { kind: "msg"; lines: string[]; then?: () => void } | { kind: "yesno"; text: string; then: (yes: boolean) => void }
  | { kind: "npc"; win: S2CNpcWindow; lines: string[] } | { kind: "shop" } | { kind: "petskill" };
let modal: Modal | null = null;

// 道具店（shop.ts）：回传照 NPC 窗口的 NpcWindowReply，data 是原版的字串
const BAG_SLOTS = 45, EQUIP_SLOTS = 9;
const autoMap = new AutoMap(assets, ASSETS);
addEventListener("pagehide", (e) => { field?.saveSeen(); if (!e.persisted) renderer.dispose(); }); // 真的关掉页面时交还显存
const petSkillShop = new PetSkillShop();
const shop = new Shop({
  reply: (win, data) => app.net?.send({ t: "NpcWindowReply", id: win.id, seq: win.seq, button: 0, data }),
  gold: () => inventory.gold,
  level: () => selfStatus?.level ?? 1,
  freeSlots: () => BAG_SLOTS - inventory.items.filter((it) => it.slot >= EQUIP_SLOTS).length,
});

function wrap(text: string, max = 36): string[] {
  const out: string[] = [];
  for (const para of text.split(/\n/)) {
    let line = "";
    for (const ch of para) {
      if (measure(line + ch) > max * 7) { out.push(line); line = ""; }
      line += ch;
    }
    out.push(line);
  }
  return out;
}

function drawModal() {
  // 输入框是 DOM 元素：窗口关掉（或换成别的窗口）就拿掉
  if (npcInput && !(modal?.kind === "npc" && (modal.win.kind === "wideinput" || modal.win.kind === "input"))) { npcInput.remove(); npcInput = null; }
  if (!modal) return;
  ui.hitBox(0, 0, 800, 600, "modal-block", false); // 挡住下面画面的点击
  if (modal.kind === "msg") {
    const width = Math.max(...modal.lines.map((l) => measure(l)), measure("ＯＫ"));
    const w = Math.floor(width / 64) + 2, h = Math.max(2, Math.ceil((modal.lines.length * 30 + 40) / 48));
    const x = Math.floor((800 - w * 64) / 2), y = Math.floor((600 - h * 48) / 2);
    ui.window(x, y, w, h);
    modal.lines.forEach((l, j) => ui.text(x + (w * 64 - measure(l)) / 2, y + j * 30 + 30, l, FONT.WHITE));
    const okY = y + modal.lines.length * 30 + 26;
    if (ui.textButton(x + (w * 64 - measure("ＯＫ")) / 2, okY, "ＯＫ", FONT.YELLOW, "modal-ok") || keyPressed.has("Enter")) {
      const then = modal.then; modal = null; then?.();
    }
  } else if (modal.kind === "npc") {
    drawNpcWindow(modal.win, modal.lines);
  } else if (modal.kind === "shop") {
    shop.draw(ui, keyPressed.has("Escape"));
    if (!shop.open) modal = null;
  } else if (modal.kind === "petskill") {
    const w = petSkillShop.win!;
    petSkillShop.draw(ui, keyPressed.has("Escape"), inventory.gold, pets?.pets ?? [],
      (data) => app.net?.send({ t: "NpcWindowReply", id: w.id, seq: w.seq, button: 0, data }));
    if (!petSkillShop.open) modal = null;
  } else {
    ui.image(CG.COMMON_WIN_YORO, 400, 300);
    ui.text(400 - measure(modal.text) / 2, 260, modal.text, FONT.WHITE);
    const yes = ui.button(CG.COMMON_YES_BTN, 400, 300, "modal-yes"), no = ui.button(CG.COMMON_NO_BTN, 400, 300, "modal-no");
    if (yes || no) { const then = modal.then; modal = null; then(yes); }
  }
}

/**
 * NPC 对话窗口（login.cpp serverWindowType0 / serverWindowType1），7×5 格居中：
 *   message：每行 20 像素，最多 8 行；select：每行 21 像素，第 selStartLine（没写时为 1）行起可点，回传第几个可选行（从 1 起）
 *   按钮依位元由左到右均分：OK「确  定」、CANCEL「取  消」、YES「确  定」、NO「取  消」；Esc 关闭（选单回传取消）
 */
const NPC_BTN = ["确  定", "取  消", "确  定", "取  消", "上一页", "下一页"];
function npcWindowLines(text: string, maxBytes = 40): string[] {
  // getStrSplit：按换行切开，每行最多 40 个半角宽（全角字算 2）
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
  return out;
}
/**
 * 文字加输入行（serverWindowType0 mode 1）：input 是 WINDOW_MESSAGETYPE_MESSAGEANDLINEINPUT（7×5 格、每行 40 个半角宽，猜谜写答案），
 * wideinput 是 WIDEMESSAGEANDLINEINPUT（9×9 格、每行 58 个半角宽，传言板）。行距 20，可显示行数 = (高 − 56) ÷ 20，
 * 最后一行放按钮、倒数第二行放输入框；回传的 data 是输入的文字，输入框里按 Enter 等于按"确定"。
 */
let npcInput: InputBox | null = null;
function drawInputWindow(win: S2CNpcWindow, text: string) {
  const wide = win.kind === "wideinput";
  const W = wide ? 9 : 7, H = wide ? 9 : 5, len = wide ? 58 : 40, x = (800 - W * 64) / 2, y = (600 - H * 48) / 2;
  const total = Math.floor((H * 48 - 56) / 20), step = Math.floor((H * 48 - 56) / total), top = (H * 48 - total * 20) / 2;
  const tx = x + (W * 64 - len * 9) / 2, lineY = (i: number) => y + step * i + top;
  ui.window(x, y, W, H, "npc-win");
  const reply = (button: number) => {
    const data = npcInput?.value ?? "";
    modal = null;
    app.net?.send({ t: "NpcWindowReply", id: win.id, seq: win.seq, button, data });
  };
  if (!npcInput) {
    npcInput = new InputBox(canvas, tx, lineY(total - 2), len * 9, 16, { maxLength: len });
    npcInput.el.addEventListener("keydown", (e) => {
      if (e.key === "Enter" && win.buttons & 1) { e.stopPropagation(); reply(1); }
    });
    setTimeout(() => npcInput?.focus(), 0);
  }
  npcWindowLines(text, len).slice(0, total - 2).forEach((l, i) => { if (l) ui.text(tx, lineY(i), l, FONT.WHITE); });
  const bits = [0, 1, 2, 3, 4, 5].filter((i) => win.buttons & (1 << i)).slice(0, 4);
  const bw = W * 64 / (bits.length + 1);
  bits.forEach((bit, k) => {
    if (ui.text(x + bw * (k + 1) - 27, lineY(total - 1) + 8, NPC_BTN[bit], FONT.YELLOW, `npc-btn${bit}`)) reply(1 << bit);
  });
  if (keyPressed.has("Escape") && modal) modal = null;
}

/** WIDEMESSAGE（9×9 格、每行 58 个半角宽，强者表）：文字从上往下，最后一行放按钮 */
function drawWideWindow(win: S2CNpcWindow) {
  const W = 9, H = 9, len = 58, x = (800 - W * 64) / 2, y = (600 - H * 48) / 2;
  const total = Math.floor((H * 48 - 56) / 20), top = (H * 48 - total * 20) / 2, tx = x + (W * 64 - len * 9) / 2;
  ui.window(x, y, W, H, "npc-win");
  npcWindowLines(win.text, len).slice(0, total - 1).forEach((l, i) => { if (l) ui.text(tx, y + top + 20 * i, l, FONT.WHITE); });
  const reply = (button: number) => { modal = null; app.net?.send({ t: "NpcWindowReply", id: win.id, seq: win.seq, button, data: "" }); };
  const bits = [0, 1, 2, 3, 4, 5].filter((i) => win.buttons & (1 << i)).slice(0, 4);
  const bw = W * 64 / (bits.length + 1);
  bits.forEach((bit, k) => { if (ui.text(x + bw * (k + 1) - 27, y + top + 20 * (total - 1) + 8, NPC_BTN[bit], FONT.YELLOW, `npc-btn${bit}`)) reply(1 << bit); });
  if (keyPressed.has("Escape") && modal) modal = null;
}

function drawNpcWindow(win: S2CNpcWindow, lines: string[]) {
  if (win.kind === "wideinput" || win.kind === "input") { drawInputWindow(win, win.text); return; }
  if (win.kind === "widemessage") { drawWideWindow(win); return; }
  if (win.kind === "petselect") {
    // 宠物店选宠物（serverWindowType2）：Esc 关掉不回传
    drawPetSelect(ui, win, pets?.pets ?? [], (button, data) => {
      modal = null;
      if (button >= 0) app.net?.send({ t: "NpcWindowReply", id: win.id, seq: win.seq, button, data });
    }, keyPressed.has("Escape"));
    return;
  }
  const W = 7, H = 5, x = (800 - W * 64) / 2, y = (600 - H * 48) / 2;
  ui.window(x, y, W, H, "npc-win");
  const reply = (button: number, data = "") => { modal = null; app.net?.send({ t: "NpcWindowReply", id: win.id, seq: win.seq, button, data }); };
  let start = 1, body = lines;
  if (win.kind === "select" && /^[0-7]$/.test(lines[0] ?? "")) { start = Number(lines[0]); body = lines.slice(1); }
  if (win.kind === "select") {
    body.slice(0, 10).forEach((l, i) => {
      if (!l) return;
      ui.text(x + 44, y + 21 + i * 21, l, FONT.WHITE);
      if (i >= start && ui.hitBox(x + 41, y + 18 + i * 21, x + 41 + 366, y + 18 + i * 21 + 20, `npc-sel${i}`) && ui.clicked(`npc-sel${i}`)) reply(0, String(i - start + 1));
    });
  } else {
    body.slice(0, 8).forEach((l, i) => { if (l) ui.text(x + 44, y + 30 + i * 20, l, FONT.WHITE); });
  }
  const bits = [0, 1, 2, 3, 4, 5].filter((i) => win.buttons & (1 << i)).slice(0, 4);
  const bw = W * 64 / (bits.length + 1), by = win.kind === "select" ? 196 : 198;
  bits.forEach((bit, k) => {
    if (ui.text(x + bw * (k + 1) - 27, y + by, NPC_BTN[bit], FONT.YELLOW, `npc-btn${bit}`)) reply(1 << bit);
  });
  if (keyPressed.has("Escape") && modal) { if (win.kind === "select") reply(2); else modal = null; }
}

// ---------------------------------------------------------------------------

const app: App = {
  ui, canvas, net: null,
  groups: [] as ServerGroup[], lineName: "", chars: [] as CharSummary[], maxChars: 2,
  async connect() {
    if (app.net) return app.net;
    const c = new Connection(WS_URL);
    if (params.has("debug")) c.log = (d, m) => console.debug(d, m);
    c.onClose = (reason) => {
      if (app.net !== c) return;
      app.net = null;
      field = null;
      battle = null;
      app.go(new LoginScreen(app));
      app.message(`与服务器的连接中断：${reason}`);
    };
    c.on("Error", (m) => { if (m.code !== "rate") app.message(m.message); });
    c.on("CharListResult", (m) => { app.chars = m.chars; app.maxChars = m.maxChars; });
    c.on("EnterFloor", (m) => {
      if (!(screen instanceof FieldScreen)) app.go(new FieldScreen());
      chat(`来到了${m.name}。`, FONT.GREEN);
    });
    // 玩家发言用发言者在聊天设定里选的颜色（原版 TK 协议带 color）
    // 没有发言者的是系统文字（原版 CHAR_talkToCli(..., -1, ...)，如"交出某某。"），只显示内容
    // GM 指令选单：只有 GM 上线时服务端才发（config.yaml 的 gms）
    c.on("GmMenu", (m) => { gmItems = m.items; if (screen instanceof FieldScreen) screen.setGmMenu(m.items); });
    c.on("GmList", (m) => { if (screen instanceof FieldScreen) screen.gmList(m.kind, m.entries); });
    c.on("GmDetail", (m) => { if (screen instanceof FieldScreen) screen.gmDetail(m); });


    c.on("ChatMsg", (m) => {
      if (m.from === "GM" && !m.fromId && screen instanceof FieldScreen) screen.gmReply(m.text); // GM 指令的回应也列在 GM 窗口
      chat(m.from ? `${m.channel === "world" ? "[世界]" : m.channel === "family" ? "[族]" : ""}${m.from}：${m.text}` : m.text, FONT_PAL[m.color] ?? FONT.WHITE);
    });

    // 家族（lssproto_FM_recv 与 F 状态）
    const familyWnd = () => screen instanceof FieldScreen ? screen.menus.family : null;
    c.on("FamilyMsg", (m) => familyWnd()?.onMsg(m.text));
    c.on("FamilyStatus", (m) => { familyStatus = m; const w = familyWnd(); if (w) w.status = m; });
    c.on("Status", (m) => { selfStatus = m; });
    c.on("Inventory", (m) => { inventory = m; if (screen instanceof FieldScreen) screen.setInventory(m); });
    c.on("Pets", (m) => { pets = m; if (screen instanceof FieldScreen) screen.setPets(m); });
    c.on("PetSkillResult", (m) => { if (screen instanceof FieldScreen) screen.menus.items.mixResult(m.ok); });
    c.on("Flags", (m) => { etcFlag = m.flags; });
    // lssproto_PR_recv：离开（或被解散）时清掉队伍
    c.on("PartyResult", (m) => { if (m.request === 0 && m.ok) clearParty(); });
    // "N" 字串：空位清掉，只剩一个人时队伍没了
    c.on("PartyMember", (m) => {
      party[m.slot] = m.used ? m : null;
      if (!m.used && party.filter((p) => p).length <= 1) clearParty(); // 空位时才算剩几个人（kubun == 0）
      if (field) field.follower = partyActive() && party[0]?.id !== field.self?.id;
    });
    c.on("HelpFlag", (m) => battle?.setHelp(m.on));
    // 决斗结束（RD）：回到地图后显示 DP 结果
    c.on("DuelResult", (m) => { pendingDuel = m; });
    // 交易（lssproto_TD_recv）
    const tradeWnd = () => screen instanceof FieldScreen ? screen.menus.trade : null;
    c.on("TradeOpen", (m) => { if (screen instanceof FieldScreen) screen.menus.openTrade(m.name); });
    c.on("TradeItem", (m) => { if (m.items[0]) tradeWnd()?.partnerItem(m.count, m.items[0]); });
    c.on("TradeGold", (m) => tradeWnd()?.partnerGold(m.amount));
    c.on("TradePet", (m) => { if (m.pets[0]) tradeWnd()?.partnerPet(m.pets[0]); });
    c.on("TradeState", (m) => tradeWnd()?.partnerState(m.state));
    c.on("TradeClose", () => tradeWnd()?.close());
    // 名片匣与邮件（lssproto_AB / ABI / MSG_recv）
    const mailWnd = () => screen instanceof FieldScreen ? screen.menus.mail : null;
    c.on("AddressBook", (m) => { addressBook = m.entries; mailWnd()?.setBook(m.entries); });
    c.on("AddressUpdate", (m) => { for (const e of m.entries) addressBook[e.slot] = e; mailWnd()?.update(m.entries); });
    // 收到邮件：SE 101（netproc.cpp lssproto_MSG_recv）
    c.on("MailRecv", (m) => { audio.se(101); mailWnd()?.receive(m.index, m.date, m.text, m.petSprite >= 0 ? { sprite: m.petSprite, level: m.petLevel, name: m.petName } : null, m.itemImage); });
    c.on("BattleJoin", (m) => battle?.join(m));
    // 战斗：遇敌时切到战斗画面，打完回到原来的地图画面
    c.on("BattleStart", (m) => {
      if (!(screen instanceof FieldScreen) || !field) return;
      const back = screen;
      field.target = null;
      // 快速战斗：留在地图画面、音乐不换，BattleScreen 只在背后跑（quickFrame）
      const quick = settings.quickBattle;
      // 遇敌：SE 215，地图音乐马上停（不淡出），记下来打完再从头播；环境音停掉
      const fieldBgm = audio.currentBgm, bgm = battleBgm(m.pvp || m.event, field.floor, fieldBgm);
      if (!quick) {
        audio.se(215);
        audio.ambientFrame([], 0);
        audio.playBgm(bgm);
      }
      battle = new BattleScreen({
        ui, assets, root: ASSETS,
        send: (kind, target, item = -1) => app.net?.send({ t: "BattleCommand", kind, target, item }),
        sendHelp: (on) => app.net?.send({ t: "BattleHelp", on }),
        // 抓到宠物：放一次 BGM 0，255 帧后战斗音乐从头再播（oft.cpp 8675 / 8732）
        captured: () => { audio.playBgm(0); setTimeout(() => { if (battle) audio.playBgm(bgm); }, 255 * 1000 / 60); },
        sendPet: (skill, target) => app.net?.send({ t: "BattlePetCommand", skill, target }),
        pets: () => pets,
        inventory: () => inventory,
        chat,
        auto: () => settings.autoBattle || quick,
        quick: () => quick,
        aiSettings: () => loadAi(selfStatus?.name ?? ""),
        aiMemory: () => aiMemory(selfStatus?.name ?? ""),
        myAtk: () => selfStatus?.atk ?? 10,
        place: () => [field?.floor ?? -1, field?.self?.x ?? 0, field?.self?.y ?? 0],
        finish: (end) => {
          battle = null;
          // 战斗结束淡出，回到地图后原来的地图音乐从头播（之后照地图记号）
          if (!quick) audio.fadeOutBgm(() => audio.playBgm(fieldBgm));
          if (end && (end.levelUp > 0 || end.pets?.some((p) => p.levelUp > 0))) audio.se(211); // 升级（结果窗口）
          // 自动丢肉（AI_OtherSetting[0]）：战斗结束时把道具栏里的肉（图 24000..24044）都丢掉
          if ((settings.autoBattle || quick) && loadAi(selfStatus?.name ?? "").other[0]) {
            for (const it of inventory.items) if (it.slot >= 9 && it.image >= 24000 && it.image <= 24044) app.net?.send({ t: "ItemDrop", slot: it.slot });
          }
          if (!quick) app.go(back);
          if (pendingDuel) { back.showDuelResult(pendingDuel.delta, pendingDuel.dp); pendingDuel = null; }
          else if (end?.result === "win" && !quick) back.showResult(end); // 原版：回到地图后显示战斗结果窗口（快速战斗只写在聊天栏）
        },
      }, m);
      const b = battle;
      if (!quick) app.go({ frame: (now) => { if (field) updatePalette(now, field.palette); b.frame(now); } });
    });
    c.on("BattleTurn", (m) => battle?.onTurn(m));
    c.on("BattleEnd", (m) => {
      const pvp = battle?.pvp ?? false;
      battle?.onEnd(m);
      // 原版开着"经验显示"时在聊天栏写：得到经验：[自己：N UpLv!]（决斗没有经验，只有 DP 的结果窗口）
      if (m.result === "win" && !pvp) {
        let s = `得到经验：[自己：${m.exp}${m.levelUp > 0 ? " UpLv!" : ""}] `;
        for (const p of m.pets ?? []) s += `[${p.name}：${p.exp}${p.levelUp > 0 ? " UpLv!" : ""}] `;
        if (m.items?.length) s += `得到物品：[${m.items.map((n) => ` ${n} `).join("")}]`;
        chat(s.trim(), FONT.YELLOW);
      }
    });
    // 村民：说一句话（原版 CHAR_talkToCli，显示在聊天栏并冒头顶气泡）；告示牌：弹窗显示全文
    c.on("NpcWindow", (m) => {
      shop.close();
      if (familyWnd()?.onWindow(m)) { modal = null; return; }
      if (m.kind === "petskillshop") { petSkillShop.show(m); if (petSkillShop.open) modal = { kind: "petskill" }; return; }
      if (m.kind === "petskillslot") { petSkillShop.showSlots(m); modal = { kind: "petskill" }; return; }
      petSkillShop.close();
      npcInput?.remove(); npcInput = null; // 传言板翻页、留言后重发的窗口重新建输入框
      modal = { kind: "npc", win: m, lines: npcWindowLines(m.text) };
    });
    c.on("ShopWindow", (m) => { shop.show(m); modal = { kind: "shop" }; });
    c.on("NpcTalk", (m) => {
      if (m.window) { app.message(m.text); return; }
      chat(`${m.name}：${m.text}`, FONT.AQUA);
      const a = field?.actors.get(m.id);
      if (a) { a.speech = m.text; a.speechUntil = performance.now() + 6000; }
    });
    const hello = await c.once("Hello");
    serverTimeAt = { server: hello.serverTime, local: performance.now() };
    app.net = c;
    return c;
  },
  disconnect() { const c = app.net; app.net = null; c?.close(); },
  go(s: Screen) {
    screen.leave?.(); screen = s; s.enter?.();
    // 选人物的画面放 BGM 2（login.cpp 1672），回到标题淡出（原版标题、选线路都没有音乐）
    if (s instanceof CharSelectScreen) { if (audio.currentBgm !== 2) audio.playBgm(2); }
    else if (s instanceof LoginScreen) audio.fadeOutBgm();
  },
  message(text, then) { modal = { kind: "msg", lines: wrap(text), then }; },
  confirm(text, then) { modal = { kind: "yesno", text, then }; },
  enterGame(id) {
    if (!app.net) return;
    field = new Field(assets, app.net);
    field.hideOthers = settings.hideOthers;
    selfStatus = null;
    gmItems = [];
    chatLog.length = 0;
    audio.fadeOutBgm(); // 选好人物：选人物的音乐淡出（PROC_CHAR_LOGIN_START）
    app.net.send({ t: "CharEnter", id });
  },
};

// ---------------------------------------------------------------------------
// 游戏画面

let field: Field | null = null;
let battle: BattleScreen | null = null;
let selfStatus: S2CStatus | null = null;
let inventory: S2CInventory = { t: "Inventory", gold: 0, items: [] };
let pets: S2CPets | null = null; // 宠物栏（原版 pet[5] 与 pc.battlePetNo）
// 组队（原版 party[5] 与 partyModeFlag）：0 是队长，空位是 null；etcFlag 是 FS 开关
const party: (PartyMember | null)[] = [null, null, null, null, null];
let etcFlag = 0;
let mailLamp = false; // 有没看过的邮件（CG_FIELD_MAIL_LAMP 闪）
let mailPetNo = -1;   // 邮件宠物（pc.mailPetNo，只在客户端）
let pendingDuel: { delta: number; dp: number } | null = null; // RD：决斗打完要显示的 DP
let addressBook: AddressEntry[] = [];
let familyStatus: S2CFamilyStatus | null = null; // F 状态（家族、职位、频道）
let gmItems: GmItem[] = []; // GM 指令选单（不是 GM 时是空的）
// 智能战斗学到的伤害：每个角色一份（换角色时换）
let aiMem: AiMemory | null = null;
function aiMemory(name: string): AiMemory {
  if (!aiMem || aiMem.name !== name) aiMem = new AiMemory(name);
  return aiMem;
}
function partyActive(): boolean { return party.some((p) => p !== null); }
function clearParty() { party.fill(null); if (field) field.follower = false; }
const chatLog: { text: string; color: string }[] = [];
function chat(text: string, color: string = FONT.WHITE) {
  for (const line of wrap(text, 96)) chatLog.push({ text: line, color }); // 聊天栏宽约 680 像素，长句折行
  while (chatLog.length > 50) chatLog.shift();
}

const keys = new Set<string>();
const keyPressed = new Set<string>(); // 本帧刚按下的键
addEventListener("keydown", (e) => {
  keyPressed.add(e.ctrlKey ? `Ctrl+${e.key.toLowerCase()}` : e.key);
  // 游戏里 Ctrl+S 是状态窗口、Ctrl+M 是地图、Ctrl+P 是宠物（原版 JOY_CTRL_S / JOY_CTRL_M / JOY_CTRL_P），不能让浏览器弹出"保存网页""打印"
  if (screen instanceof FieldScreen && e.ctrlKey && ["s", "i", "m", "p", "a", "t", "w", "q", "e", "0", "1", "2", "3", "4", "5", "6", "7", "8", "9", "-", "=", "\\"].includes(e.key.toLowerCase())) e.preventDefault();
  if (screen instanceof FieldScreen && document.activeElement?.tagName !== "INPUT" && e.key.startsWith("Arrow")) {
    keys.add(e.key); e.preventDefault();
  }
});
addEventListener("keyup", (e) => keys.delete(e.key));
addEventListener("blur", () => keys.clear());

function keyDir(): number {
  const u = keys.has("ArrowUp"), d = keys.has("ArrowDown"), l = keys.has("ArrowLeft"), r = keys.has("ArrowRight");
  if (u && r) return 5; if (u && l) return 3; if (d && r) return 7; if (d && l) return 1;
  if (u) return 4; if (d) return 0; if (l) return 2; if (r) return 6;
  return -1;
}

class FieldScreen implements Screen {
  private input: InputBox | null = null;
  private channel: "near" | "party" | "family" | "world" = "near";
  private talkTarget: number | null = null;
  private moveMode = false; // 原版 MOUSE_CURSOR_MODE_MOVE
  private moveTick = 0;
  private turnAt = 0;
  private menus = new Menus(ui, {
    get status() { return selfStatus; },
    party: () => party,
    selfId: () => field?.self?.id ?? -1,
    trade: (cmd, slot, amount) => app.net?.send({ t: "Trade", cmd, slot, amount }),
    action: (no) => { const s = field?.self; if (s) { app.net?.send({ t: "Action", x: s.x, y: s.y, action: no }); s.setAct(no); } },
    emote: (spr) => { if (field?.self) field.self.emote = spr; },
    planet: () => app.lineName,
    mailPet: () => mailPetNo,
    setMailPet: (s) => { mailPetNo = s; },
    mailSend: (index, text) => app.net?.send({ t: "MailSend", index, text }),
    petMail: (index, pet, item, text) => app.net?.send({ t: "PetMail", index, pet, item, text }),
    cardDelete: (index) => app.confirm("确定要删除这张名片吗？", (yes) => { if (yes) app.net?.send({ t: "CardDelete", index }); }),
    mailLamp: (on) => { mailLamp = on; },
    chat,
    returnHome: () => app.net?.send({ t: "ReturnHome" }),
    statUp: (stat) => app.net?.send({ t: "StatUp", stat }),
    itemMove: (from, to) => app.net?.send({ t: "ItemMove", from, to }),
    itemUse: (slot, target) => app.net?.send({ t: "ItemUse", slot, target }),
    magicUse: (slot, target) => { audio.se(100); app.net?.send({ t: "MagicUse", slot, target }); },
    dropGold: (amount) => app.net?.send({ t: "GoldDrop", amount }),
    itemDrop: (slot, name) => app.confirm(`要丢掉${name}吗？`, (yes) => { if (yes) app.net?.send({ t: "ItemDrop", slot }); }),
    petItem: (pet, from, to) => app.net?.send({ t: "PetItemEquip", pet, from, to }),
    petSkill: (pet, skill, data) => app.net?.send({ t: "PetSkillUse", pet, skill, data }),
    petState: (slot, state) => app.net?.send({ t: "PetState", slot, state }),
    petRename: (slot, name) => app.net?.send({ t: "PetRename", slot, name }),
    petRide: (slot) => app.net?.send({ t: "PetRide", slot }),
    // _DROPPETWND：丢出前先问（原版 CG_DROPWND 上的"确定要丢出你的宠物吗？"），骑着的、绑定的由服务端拒绝；丢出的宠物放在地上
    petDrop: (slot) => app.confirm("确定要丢出你的宠物吗？", (yes) => { if (yes) app.net?.send({ t: "PetDrop", slot }); }),
    canvas,
    logout: () => {
      app.net?.send({ t: "Logout" });
      field = null;
      app.go(new CharSelectScreen(app));
    },
    settingsChanged: () => { if (field) field.hideOthers = settings.hideOthers; applyCursor(); audio.apply(); },
    map: () => {
      const s = field?.self;
      if (!field || !s || field.floor < 0) return null;
      return { name: field.floorName, x: s.x, y: s.y, canvas: autoMap.render(field, s.x, s.y) };
    },
    family: (text) => app.net?.send({ t: "Family", text }),
    fmReply: (win, button, data) => app.net?.send({ t: "NpcWindowReply", id: win.id, seq: win.seq, button, data }),
    walking: () => !!field?.self?.moving(performance.now()),
    esc: () => keyPressed.has("Escape"),
    currentBgm: () => audio.currentBgm,
    uiSe,
    autoEncount: () => this.enc !== null,
    setAutoEncount: (on) => {
      const s = field?.self;
      this.enc = on && s && field ? { floor: field.floor, x: s.x, y: s.y, dir: (s.dir + 4) & 7, target: null } : null;
      chat(this.enc ? "开始自动遇敌！" : "停止自动遇敌！", FONT.RED);
    },
    gmSend: (text) => app.net?.send({ t: "GmCommand", text }),
    gmList: (kind) => app.net?.send({ t: "GmListReq", kind }),
    gmDetail: (kind, id) => app.net?.send({ t: "GmDetailReq", kind, id }),
    place: () => [field?.floor ?? -1, field?.self?.x ?? 0, field?.self?.y ?? 0],
  });
  private seenSavedAt = 0;
  // 自动遇敌（额外新功能，像外挂的"原地遇敌"）：开启时站的格子与旁边一格之间来回走；target 是这次自己设的目的地
  private enc: { floor: number; x: number; y: number; dir: number; target: [number, number] | null } | null = null;

  /** 自动遇敌的一帧：站定、没有战斗与窗口时，下一步走到另一格；被手动移动、换地图、耐久力太低就停 */
  private encountFrame(f: Field, now: number) {
    const e = this.enc, s = f.self;
    if (!e || !s) return;
    const stop = (why: string) => { this.enc = null; chat(`自动遇敌停止：${why}`, FONT.RED); };
    if (f.floor !== e.floor) return stop("换了地图");
    if (f.wantDir >= 0 || (f.target !== null && f.target !== e.target)) return stop("手动移动");
    if (battle || modal || this.input || f.follower || s.moving(now) || f.target !== null) return;
    if (selfStatus && selfStatus.maxHp > 0 && selfStatus.hp * 5 < selfStatus.maxHp) return stop("耐久力太低");
    if (s.x !== e.x || s.y !== e.y) { e.target = [e.x, e.y]; f.target = e.target; return; }
    // 先试上次的方向，再按顺序找旁边一格能走、不是传送点的
    for (let k = 0; k < 8; k++) {
      const d = (e.dir + k) & 7, nx = e.x + MOVE[d][0], ny = e.y + MOVE[d][1];
      if (f.plainWalkable(nx, ny)) { e.dir = d; e.target = [nx, ny]; f.target = e.target; return; }
    }
    stop("旁边没有可以走的格子");
  }

  leave() { this.input?.remove(); this.input = null; this.menus.leave(); }

  showDuelResult(delta: number, dp: number) { this.menus.showDuelResult(delta, dp); }
  showResult(m: S2CBattleEnd) {
    // 结果窗口列出出战与待命的宠物（menu.cpp：pc.selectPetNo[i]），没有拿到经验的写 +0
    const list = (pets?.pets ?? []).filter((p) => p.state !== "rest").map((p) => {
      const r = m.pets?.find((x) => x.slot === p.slot);
      return { name: p.freeName || p.name, exp: r?.exp ?? 0, lvUp: (r?.levelUp ?? 0) > 0 };
    });
    this.menus.showResult(selfStatus?.name ?? "", m.exp, m.levelUp > 0, m.items ?? [], list);
  }

  setInventory(m: S2CInventory) { this.menus.items.inv = m; }

  setPets(m: S2CPets) { this.menus.setPets(m); }
  setGmMenu(items: GmItem[]) { this.menus.gm.setMenu(items); }
  gmReply(text: string) { this.menus.gm.reply(text); }
  gmList(kind: string, entries: GmEntry[]) { this.menus.gm.setList(kind, entries); }
  gmDetail(m: S2CGmDetail) { this.menus.gm.setDetail(m); }

  enter() {
    this.menus.items.inv = inventory; if (pets) this.menus.setPets(pets);
    this.menus.mail.load(selfStatus?.name ?? ""); if (addressBook.length) this.menus.mail.setBook(addressBook);
    if (familyStatus) this.menus.family.status = familyStatus;
    this.menus.gm.setMenu(gmItems);
  }

  /** 开着的窗口数（开窗口 SE 202、关窗口 203 用） */
  openCount() { return this.menus.openCount() + (this.panel.etcOpen ? 1 : 0); }

  private savedBgmTried = false;
  frame(now: number) {
    const f = field;
    if (!f) return;
    // 地图的声音记号：背景音乐（40..55）与环境音（20..37）
    const mk = f.soundMarkers(now);
    const bgmKey = `sa.bgm.${selfStatus?.name ?? ""}`;
    if (mk.bgm >= 0) {
      audio.mapBgm(MAP_BGM[mk.bgm]);
      // 原版把最后看到的记号存在 map\bgm<格位>.dat，进游戏时附近没有记号就用它（只认 40..53）
      if (mk.bgm <= 53) try { localStorage.setItem(bgmKey, String(mk.bgm)); } catch { /* */ }
    } else if (!this.savedBgmTried && f.floor >= 0) {
      this.savedBgmTried = true;
      let saved = 0;
      try { saved = Number(localStorage.getItem(bgmKey) ?? 0); } catch { /* */ }
      if (saved >= 40 && saved <= 53 && audio.currentBgm < 0 || saved >= 40 && saved <= 53 && audio.currentBgm === 2) audio.mapBgm(MAP_BGM[saved]);
    }
    audio.ambientFrame(mk.env, timeZone(saHour(now)));
    if (now - this.seenSavedAt > 5000) { this.seenSavedAt = now; f.saveSeen(); } // 看过的格子每 5 秒存一次
    // Esc 开关系统窗口、Ctrl+S 开关状态窗口（原版 MenuToggleFlag ^= JOY_ESC / JOY_CTRL_S）
    if (!modal && !this.input) {
      if (keyPressed.has("Escape")) { if (this.menus.ai.open) this.menus.ai.close(); else if (!this.menus.gm.open) this.menus.toggleSystem(); }

      // PageUp / PageDown：切换自动战斗（gamemain.cpp，不说话）
      if (keyPressed.has("PageUp") || keyPressed.has("PageDown")) this.menus.toggleAuto(false);
      if (keyPressed.has("Ctrl+s")) this.menus.toggleStatus();
      if (keyPressed.has("Ctrl+i")) this.menus.toggleItems();
      if (keyPressed.has("Ctrl+m")) this.menus.toggleMap();
      if (keyPressed.has("Ctrl+p")) this.menus.togglePets(); // JOY_CTRL_P
      if (keyPressed.has("Ctrl+a")) this.menus.toggleAlbum(); // JOY_CTRL_A：相簿
      if (keyPressed.has("Ctrl+w")) this.menus.actions.toggle(); // JOY_CTRL_W：动作窗口
      if (keyPressed.has("Ctrl+e")) this.menus.toggleMail();     // JOY_CTRL_E：名片匣与邮件
      // actionShortCutKeyProc：站着不动时 Ctrl+数字等做动作
      if (field?.self && !field.self.moving(now)) ACTION_KEYS.forEach((k, no) => { if (keyPressed.has(`Ctrl+${k}`)) this.menus.actions.act(no); });
      // JOY_CTRL_T：交易（组队中不行；面向对方，服务端找面前的人）
      if (keyPressed.has("Ctrl+t") && !partyActive() && !this.menus.trade.open) app.net?.send({ t: "Trade", cmd: "request", slot: 0, amount: 0 });
    }
    // 回车开关聊天输入（原版：聊天输入行在画面下方）
    if (keyPressed.has("Enter") && !modal) {
      if (!this.input) {
        this.input = new InputBox(canvas, 70, 554, 600, 16, { maxLength: 120 });
        this.input.el.style.color = FONT_PAL[settings.chatColor];
        this.input.el.addEventListener("keydown", (e) => {
          if (e.key === "Enter") { e.stopPropagation(); this.sendChat(); }
          if (e.key === "Escape") { e.stopPropagation(); this.input?.remove(); this.input = null; }
        });
        setTimeout(() => this.input?.focus(), 0);
      }
    }
    f.wantDir = this.input || battle ? -1 : keyDir(); // 快速战斗中留在地图画面，但不能走
    this.encountFrame(f, now);

    // 鼠标操作（原版 map.cpp moveProc / drawGrid / turnAround）
    const onField = mouse.x >= 0 && !modal && !ui.overUi() && f.self !== null && !battle;
    const grid = onField ? f.screenToGrid(mouse.x, mouse.y, 400, 324) : null;
    // 按住左键 1 秒（MOVE_MODE_CHANGE_TIME）进入跟随模式：每 250ms 朝光标所在格重新走，再点一下退出
    if (mouse.down && onField && now - mouse.downAt >= 1000) this.moveMode = true;
    if (this.moveMode) {
      if (mouse.clicked) this.moveMode = false;
      else if (grid && now - this.moveTick >= 250) { this.moveTick = now; this.talkTarget = null; f.target = grid; }
    } else if (mouse.clicked && grid && now - this.moveTick >= 250) {
      // 单击：点到 NPC 就走过去说话，点地面就走到那一格（两次点击至少间隔 250ms，MOVE_CLICK_WAIT_TIME）
      this.moveTick = now;
      const npc = f.actorAt(mouse.x, mouse.y);
      if (npc && npc.kind === "npc") { this.talkTarget = npc.id; f.target = [npc.x, npc.y]; }
      else { this.talkTarget = null; f.target = grid; }
    }
    // 地上的东西：滑鼠停着时下方显示名字（道具名、"N Stone"、宠物"名字  Lv：N"）
    const hov = f.actorAt(mouse.x, mouse.y);
    if (hov && (hov.kind === "item" || hov.kind === "gold")) ui.bottomLine = hov.name;
    else if (hov && hov.kind === "pet") ui.bottomLine = `${hov.name}  Lv：${hov.level}`;
    // 右键：点身边一格有地上的东西时捡起来（pc.cpp getItem → lssproto_PI_send），否则朝那格转身；0.5 秒内不重复发送（FIELD_BTN_PUSH_WAIT）
    const groundAt = (x: number, y: number) => [...f.actors.values()].some((a) => a.x === x && a.y === y && (a.kind === "item" || a.kind === "gold" || a.kind === "pet"));
    if (mouse.rightClicked && grid && f.self && !f.self.moving(now) && f.target === null && now - this.turnAt >= 500
      && Math.abs(grid[0] - f.self.x) <= 1 && Math.abs(grid[1] - f.self.y) <= 1 && groundAt(grid[0], grid[1])) {
      const own = grid[0] === f.self.x && grid[1] === f.self.y;
      const d = own ? -1 : dirTo(grid[0] - f.self.x, grid[1] - f.self.y);
      if (d >= 0) f.self.dir = d;
      app.net?.send({ t: "PickUp", dir: d });
      this.turnAt = now;
    }
    if (mouse.rightClicked && grid && f.self && !f.self.moving(now) && f.target === null && now - this.turnAt >= 500) {
      const d = dirTo(grid[0] - f.self.x, grid[1] - f.self.y);
      if ((grid[0] !== f.self.x || grid[1] !== f.self.y) && d !== f.self.dir) {
        f.self.dir = d;
        app.net?.send({ t: "Turn", dir: d });
        this.turnAt = now;
      }
    }
    if (this.talkTarget !== null) {
      const npc = f.actors.get(this.talkTarget), s = f.self;
      if (!npc || !s) this.talkTarget = null;
      else {
        // 走到旁边就说话；NPC 被围住（柜台、石圈）走不到旁边时，路走完停下后只要在 3 格内也送出，
        // 能不能说话由服务端按各种 NPC 的距离判定（记录点、传送员是前方 2 格，原版就能隔着柜台说话）
        const dist = Math.max(Math.abs(npc.x - s.x), Math.abs(npc.y - s.y));
        if (!s.moving(now) && (dist <= 1 || (f.target === null && dist <= 3))) {
          f.target = null;
          app.net?.send({ t: "Talk", id: npc.id });
          this.talkTarget = null;
        } else if (!s.moving(now) && f.target === null) this.talkTarget = null; // 走不过去，放弃
      }
    }
    f.update(now);
    updatePalette(now, f.palette);
    const labels: Label[] = [];
    ui.quads.push(...f.build(now, 800, 600, labels));
    // 格子光标（CG_GRID_CURSOR，DISP_PRIO_GRID：画在地图和角色之上）；跟随模式下不显示
    if (grid && !this.moveMode) { const [gx, gy] = f.gridToScreen(grid[0], grid[1], now, 400, 324); ui.image(CG.GRID_CURSOR, gx, gy); }
    // 名字的范围（画在 (x, y − 14) 置中）碰到窗口就不画，免得一半压在窗口边上
    const covered = (l: Label) => {
      const w = measure(l.text) / 2, top = l.bubble ? l.y - 70 : l.y - 14;
      return this.menus.rects.some(([x1, y1, x2, y2]) => l.x + w > x1 && l.x - w < x2 && l.y > y1 && top < y2);
    };
    for (const l of labels) {
      if (modal || covered(l)) continue; // 对话框和窗口画在名字下面（名字在 2D 覆盖层），打开时先不画名字
      if (l.bubble) {
        // 头顶气泡：每行最多约 16 个字，最多 4 行
        const lines = wrap(l.text, 32).slice(0, 4);
        const w = Math.max(...lines.map((t) => measure(t))) + 12, h = lines.length * 16 + 4;
        const x = Math.max(4 + w / 2, Math.min(796 - w / 2, l.x)), top = Math.max(4, l.y - 2 - h);
        ui.box(x - w / 2, top, x + w / 2, top + h, "rgba(0,0,0,0.6)", true);
        lines.forEach((t, i) => ui.texts.push({ x, y: top + 2 + i * 16, text: t, color: l.color, align: "center" }));
      } else ui.texts.push({ x: l.x, y: l.y - 14, text: l.text, color: l.color, align: "center" });
    }
    // 聊天栏：左下角最近几行（行数在系统 → 聊天设定里调）
    const lines = settings.chatLines > 0 ? chatLog.slice(-settings.chatLines) : [];
    lines.forEach((l, i) => ui.text(8, 548 - (lines.length - i) * 17, l.text, l.color));
    if (this.input) {
      ui.box(4, 550, 680, 574, "rgba(0,0,0,0.55)", true);
      // 频道：附近 → 队伍（组队时，CHAT_MODE）→ 家族（有家族时，TalkMode 3 的 /FM）→ 世界
      const inFamily = (familyStatus?.index ?? 0) > 0;
      if (ui.textButton(8, 554, { near: "[附近]", party: "[队伍]", family: "[家族]", world: "[世界]" }[this.channel], FONT.YELLOW, "chan")) {
        const order = (["near", "party", "family", "world"] as const).filter((c) => c !== "party" || partyActive()).filter((c) => c !== "family" || inFamily);
        this.channel = order[(order.indexOf(this.channel) + 1) % order.length];
      }
      if (this.channel === "party" && !partyActive() || this.channel === "family" && !inFamily) this.channel = "near";
    }
    // 右上角：楼层名与坐标（原版的小地图位置）
    const s = f.self;
    if (s && !this.menus.items.open && !this.menus.mapOpen && !this.menus.actions.open) ui.text(792 - measure(`${f.floorName}  ${s.x},${s.y}`), 68, `${f.floorName}  ${s.x},${s.y}`, FONT.WHITE);
    ui.bottomLineY = 558; // 任务栏常驻在 576..600
    this.menus.frame();
    this.panel.frame(this.menus.rects);
    if (keyPressed.has("Ctrl+q") && !modal && !this.input) this.panel.toggleEtc(); // JOY_CTRL_Q：游戏设定
  }

  // 画面上方的按钮（fieldpanel.ts）
  private panel = new FieldPanel({
    ui, floor: () => field?.floor ?? -1,
    hidden: () => this.menus.windowOpen() || modal !== null,
    inParty: partyActive, isLeader: () => partyActive() && party[0]?.id === field?.self?.id,
    front: () => {
      const s = field?.self;
      if (!s || !field) return null;
      const fx = s.x + MOVE[s.dir][0], fy = s.y + MOVE[s.dir][1];
      for (const a of field.actors.values()) if (a.kind === "player" && a.x === fx && a.y === fy) return { battle: a.battle > 0 };
      return null;
    },
    walking: () => !!field?.self?.moving(performance.now()),
    etc: () => etcFlag,
    setEtc: (flags) => app.net?.send({ t: "SetFlags", flags }),
    setEtcLocal: (flags) => { etcFlag = flags; },
    party: (join) => app.net?.send({ t: "Party", request: join ? 1 : 0 }),
    card: () => app.net?.send({ t: "CardExchange" }),
    trade: () => app.net?.send({ t: "Trade", cmd: "request", slot: 0, amount: 0 }),
    joinBattle: () => app.net?.send({ t: "JoinBattle" }),
    duel: () => app.net?.send({ t: "Duel" }),
    family: () => this.menus.openFamily(),
    broadcast: () => familyStatus?.channel === 5,
    actions: () => this.menus.actions.toggle(),
    mailLamp: () => mailLamp,
    chat,
  });

  private sendChat() {
    const text = this.input?.value.trim();
    if (text) app.net?.send({ t: "Chat", channel: this.channel, text, color: settings.chatColor, area: settings.voice });
    this.input?.remove();
    this.input = null;
  }
}

// ---------------------------------------------------------------------------

let screen: Screen = { frame() {} };
let frames = 0, fps = 0, fpsAt = performance.now(), workMs = 0;

function frame(now: number) {
  const workAt = performance.now();
  ui.begin();
  if (!(screen instanceof FieldScreen) && !battle) updatePalette(now, 0); // 登录画面固定用 0 号调色板（PaletteChange(DEF_PAL)）
  try {
    screen.frame(now);
    if (battle?.quick) battle.quickFrame(now); // 快速战斗：画面还是地图，战斗在背后一口气打完
  } catch (e) {

    console.error(e); // 一帧出错不能让整个渲染循环停掉
  }
  drawModal();
  // 开关窗口的声音（202 / 203），这一帧没有别的界面音效时，点到按钮放 217
  const open = (modal ? 1 : 0) + (screen instanceof FieldScreen ? screen.openCount() : 0);
  if (open > openPrev) uiSe(202); else if (open < openPrev) uiSe(203);
  openPrev = open;
  if (ui.clickSound && !uiSeFrame) audio.se(217);
  uiSeFrame = false;
  audio.update(now);
  ui.end();
  renderer.draw(ui.quads, [0, 0, 0]);
  fitOverlay(overlay, ctx2d);
  ctx2d.clearRect(0, 0, 800, 600);
  drawOverlay(ctx2d, ui);
  InputBox.layoutAll();
  keyPressed.clear();
  assets.tick();
  frames++;
  workMs = workMs * 0.95 + (performance.now() - workAt) * 0.05; // 每帧 JS 的耗时（平滑）：显示器或浏览器限了帧率时，看它才知道还有多少余裕
  if (now - fpsAt >= 1000) { fps = frames; frames = 0; fpsAt = now; }
  const hud = document.getElementById("hud");
  if (hud) hud.textContent = params.has("debug") ? `${fps} fps  每帧 ${workMs.toFixed(1)} ms  ${renderer.drawCalls} 次绘制  显存 ${assets.pageCount} 页 ${Math.round(assets.gpuBytes / 1048576)} MB  加载中 ${assets.pending}  SA 时 ${saHour(now)}` : "";
  requestAnimationFrame(frame);
}

/** 进登录画面前的加载画面：先读资源索引，再把界面用的核心图集页传进显存（画在 2D 覆盖层上，进度 0..1） */
function drawLoading(text: string, p: number) {
  fitOverlay(overlay, ctx2d);
  ctx2d.clearRect(0, 0, 800, 600);
  ctx2d.font = `${FONT_SIZE}px ${FONT_FAMILY}`;
  ctx2d.textAlign = "center";
  ctx2d.textBaseline = "top";
  ctx2d.fillStyle = "#fff";
  ctx2d.fillText(text, 400, 270);
  ctx2d.strokeStyle = "#a0a0a4";
  ctx2d.strokeRect(200.5, 300.5, 400, 14);
  ctx2d.fillStyle = "#a0f43a";
  ctx2d.fillRect(202, 302, Math.round(396 * Math.max(0, Math.min(1, p))), 10);
  ctx2d.fillStyle = "#fff";
  ctx2d.fillText(`${Math.floor(p * 100)}%`, 400, 324);
  ctx2d.textAlign = "left";
}

(async () => {
  try {
    renderer.draw([], [0, 0, 0]);
    drawLoading("正在读取资源索引……", 0);
    await assets.init((p) => drawLoading("正在读取资源索引……", p * 0.15));
    const n = assets.coreCount;
    await assets.preloadCore((p) => drawLoading(`正在加载界面资源（${Math.round(p * n)} / ${n}）……`, 0.15 + p * 0.85));
  } catch (e) {
    ctx2d.fillStyle = "#fff";
    ctx2d.font = `${FONT_SIZE}px ${FONT_FAMILY}`;
    ctx2d.fillText(`资源加载失败：${(e as Error).message}`, 20, 30);
    return;
  }
  app.go(new LoginScreen(app));
  requestAnimationFrame(frame);
  // 原版鼠标光标（CG_MOUSE_CURSOR，原版画在鼠标点 +16,+16 处）。做成 CSS 光标由系统绘制，不随帧率卡顿
  const cur = await assets.imageCanvas(CG.MOUSE_CURSOR);
  if (cur) {
    const [c, ox, oy] = cur;
    const hx = Math.max(0, Math.min(c.width - 1, -(16 + ox))), hy = Math.max(0, Math.min(c.height - 1, -(16 + oy)));
    gameCursor = `url(${c.toDataURL()}) ${hx} ${hy}, auto`;
  }
  applyCursor();
})();

// 滑鼠设定：正常 = 原版光标，平滑 = 系统光标
let gameCursor = "auto";
function applyCursor() { canvas.style.cursor = settings.smoothCursor ? "auto" : gameCursor; }

// 调试与自动化测试入口
(globalThis as any).sa = { app, ui, get field() { return field; }, get battle() { return battle; }, get screen() { return screen; }, assets, renderer, shop, audio };
