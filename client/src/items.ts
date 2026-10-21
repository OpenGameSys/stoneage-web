// 道具窗口（原版 menu.cpp itemWndNo 0，Ctrl+I）。
//   MakeWindowDisp(365, 4, 271, 440, 0, -1)：800 模式下 x > 320 平移 160，窗口在 (525, 4)，底图 CG_NEWITEM_WND（26388，含腰带、盾、鞋、手套栏）
//   装备栏是 3×3 的格子（位置按底图量出：列 37/87/137、行 33/84/132，相对底图左上角）
//   道具栏 InitItem：x + 32 + 51n、y + 200 + 48k，每页 5×3，_NEW_ITEM_ 共 3 页（窗口左缘的书签切换；26047 / 26048 箭头是 _PET_ITEM 宠物道具栏用的）
//   金钱 "%7d" 画在 (x + 192, y + 87)；鼠标停在道具上时名字画在 (x + 16, y + 331)，说明每行 28 个半角宽，从 y + 355 起每行 24
// 操作：点一下拿起道具，再点一格放下（道具栏互换、拖到装备栏就是装备）；拿着道具点到窗口外问要不要丢掉；
// 双击：回复道具直接使用、装备品直接装上、装备栏上的卸下；右键放下拿着的道具。
// 宠物页（_PET_ITEM，g_bPetItemWndFlag）：窗口左缘上面两个书签切换人物 / 宠物，宠物页的底图 26456 画在 (mx − 14, my)；
//   7 格的位置是 InitItem 的宠物版本，每格先画能装 / 不能装的底图（nPetItemEquipBmpNumber，按 byShowPetItemBackground）；
//   名字 (x + 148, y + 17)、最大 HP / 攻 / 防 / 敏 从 (x + 222, y + 40) 起；26047 / 26048 箭头在 (x + 188 / 236, y + 142) 换宠物（跳过骑着的）；
//   宠物页没有咒术钮与金钱。拖到宠物身上或双击宠物装备就装上（lssproto_PetItemEquip_send，骑着的宠物"骑宠不可装装备！"）。

import type { InvItem, PetInfo, PetSkill, S2CInventory } from "./proto.gen.ts";
import { FONT, InputBox, boxColor, measure, wrapBytes, type Ui } from "./ui.ts";
import { ANIM, findAction } from "./assets.ts";

// 宠物的地图技能（加工等，menu.cpp 的 mixFlag）：右键点道具栏标记，再到宠物窗口的技能页点技能。
//   标记（第一个决定种类，之后的要同种）：料理（flg 4）要有"料理"、金属 / 宝石（8 / 16）要有"镶宝石"（精工）、耐久不满（32）要有"修复"，
//   其他能加工的（2）有"精炼"且素材单一时是精炼，否则要有"加工"；再点一下取消。道具上写"合成 / 料理 / 精炼 / 精工 / 修复"。
//   技能页点技能时同种的标记有 2 个以上就开始（ANIM_DISP_MIX_PET_*）：宠物从 (750, 240) 走到每个标记的道具把它叼起来（速度 11 起每次 ×0.79），
//   最后走到第一个空格转几圈（2..15 圈），送出 lssproto_PS_send(宠物, 技能, "格位|格位|…")，等结果（0 时"失败！"）后走回去消失。
const MIX = { MERGE: 1, COOK: 2, ALCHEMIST: 9, INSLAY: 10, FIX: 11 };
const MIX_LABEL: Record<number, string> = { 1: "合成", 2: "料理", 9: "精炼", 10: "精工", 11: "修复" };
// 技能编号（pet_skillinfo.h）→ 标记种类
const MIX_SKILL: Record<number, number> = { 200: MIX.MERGE, 201: MIX.COOK, 202: MIX.ALCHEMIST, 572: MIX.INSLAY, 540: MIX.FIX, 664: MIX.FIX };
const ITEM_FLAG = { MIX: 2, COOKING: 4, METAL: 8, JEWEL: 16, FIX: 32 };

// 拼图（_ITEM_JIGSAW，menu.cpp SetJigsaw / CheckJigsaw，原版是状态窗口的第 4 页）：双击底板打开（没拼满的小底板或大底板），
// 再双击拼图就送 lssproto_ID_send(拼图, 底板的格位)；没打开底板时说"请先开启大 / 小拼图底版"。
// 画面：框 25251 在 (190, 220)、底图在 (189, 219)、放进去的块按 3×3 画在 x {103, 189, 275} × y {138, 219, 300}。
// JigsawOK：{道具图号, 第一块的图号, 底图图号}
const JIGSAW: [number, number, number][] = [
  [25151, 25261, 25241], [25152, 25270, 25242], [25153, 25279, 25243], [25154, 25288, 25244], [25155, 25297, 25245],
  [25156, 25306, 25246], [25157, 25315, 25247], [25158, 25324, 25248], [25159, 25333, 25249], [25150, 25252, 25250],
];
const JIGSAW_FRAME = 25251;

interface MixAnim {
  pet: number; skill: number; sprite: number;
  x: number; y: number; spd: number;
  target: number;        // 正在走去的格位
  last: boolean;         // 走去的是最后的空格（delta == 1）
  state: "move" | "spin" | "wait" | "end";
  dir: number; carried: Set<number>; slots: number[];
  spins: number; goal: number; wait: number; gap: number; phase: number;
  tick: number; animAt: number;
}

// 咒术钮 CG_ITEM_WND_JUJUTU_BTN、咒术窗口 CG_JUJUTU_WND、选对象的窗口 CG_ITEM_WND_SELECT_WND、回上一页 CG_RETURN_BTN
const CG = { ITEM_WND: 26388, CLOSE_BTN: 26042, JUJUTU_BTN: 26069, JUJUTU_WND: 26068, SELECT_WND: 26070, RETURN_BTN: 26043,
  GOLD_DROP_BTN: 26062, GOLD_INC_BTN: 26064, GOLD_DEC_BTN: 26066, // CG_ITEM_WND_GOLD_*_BTN_UP（+1 是按下的图）
  PET_ITEM_WND: 26456, ITEM_WND_TABS: 26455, PET_PREV: 26047, PET_NEXT: 26048 };
const MAGIC_FIELD_BATTLE = 1, MAGIC_TARGET_MYSELF = 0, MAGIC_TARGET_OTHER = 1;
export const EQUIP_SLOTS = 9;
const BAG_PER_PAGE = 15;

// 装备栏位置（相对底图左上角）：头 / 身体 / 武器 / 饰品1 / 饰品2 / 腰带 / 盾 / 鞋 / 手套（data.Slot* 的顺序）
const EQUIP_POS: [number, number][] = [
  [87, 33], [87, 84], [37, 132], [137, 33], [37, 33], [87, 132], [137, 84], [137, 132], [37, 84],
];
const EQUIP_NAME = ["头", "身体", "武器", "饰品", "饰品", "腰带", "盾", "鞋", "手套"];

// 宠物身上 7 格（PET_HEAD 头、翼、牙、身体、背、爪、脚）的位置（InitItem 的 bPetItemFlag，相对窗口左上角）与能装 / 不能装的底图
const PET_ITEM_SLOTS = 7;
const PET_POS: [number, number][] = [[63, 34], [113, 34], [36, 83], [87, 83], [138, 83], [63, 132], [113, 132]];
const PET_SLOT_BMP: [number, number][] = [[26463, 26470], [26460, 26467], [26458, 26465], [26461, 26468], [26459, 26466], [26457, 26464], [26462, 26469]];
const PET_SLOT_NAME = ["头", "翼", "牙", "身体", "背", "爪", "脚"];
// byShowPetItemBackground[HIWORD(fusion)]：这台服务端送的 fusion 只有融合旗标，种族都是 0（乌力），能装头、牙、身体、背、脚
const PET_SLOT_MASK = 0x5d;
const ITEM_PET_HEAD = 29, ITEM_PET_FIN = 37; // 宠物装备的种类（pc.h ITEM_PET_*）
/** 宠物装备该放的格（三种脚共用最后一格），不是宠物装备回 -1 */
export function petEquipSlot(type: number): number {
  return type < ITEM_PET_HEAD || type > ITEM_PET_FIN ? -1 : Math.min(6, type - ITEM_PET_HEAD);
}

export interface ItemHost {
  ui: Ui;
  level(): number;
  mp(): number;
  name(): string;
  pets(): { slot: number; name: string }[]; // 宠物栏（选对象的窗口列出）
  partyMembers(): { slot: number; name: string }[]; // 队友（不含自己），对象编号是 6 + 队伍格位
  move(from: number, to: number): void;
  use(slot: number, target: number): void; // lssproto_ID_send：target 0 自己、1..5 第几只宠物
  drop(slot: number, name: string): void; // 由主程序弹确认框
  magicUse(slot: number, target: number): void; // lssproto_MU_send：target 0 自己、1..5 第几只宠物
  dropGold(amount: number): void;               // lssproto_DG_send
  petList(): PetInfo[];                         // 宠物栏（宠物页、加工要的技能）
  petSkill(pet: number, skill: number, data: string): void; // lssproto_PS_send
  ridePet(): number;                            // 骑着的宠物格位（-1 没有）
  petItem(pet: number, from: number, to: number): void; // lssproto_PetItemEquip_send：to -1 装到该放的格、-2 丢掉
  notice(text: string): void;                   // StockChatBufferLine（黄字）
  canvas: HTMLCanvasElement;                    // 手动输入丢出的金额（_MONEYINPUT）的输入框叠在画布上
}

export class ItemWindow {
  open = false;
  inv: S2CInventory = { t: "Inventory", gold: 0, items: [] };
  private wndNo = 0;     // itemWndNo：0 道具、1 咒术、2 选对象
  private jujutu = -1;   // jujutuNo：选好要对别人用的咒术（装备位）
  private itemNo = -1;   // itemNo：选好要对别人用的道具（道具栏格位）
  // 丢石币（itemWndDropGold / Inc / Cnt）：按住 + / − 时每帧加减 inc，每 30 帧 inc ×5（1 起，最多 10000）
  private dropGold = 0;
  private goldInc = 0;
  private goldCnt = 0;
  private goldHold = "";                 // 按住中的钮："inc" / "dec"
  private goldInput: InputBox | null = null; // _MONEYINPUT：点金额那一格可以直接输入
  private page = 0;
  private held = -1;     // 拿着的道具：格位；宠物页里小于 7 的是宠物身上的那一格
  private petPage = false; // g_bPetItemWndFlag
  private selPet = -1;     // nSelectPet：宠物页显示的宠物格位
  private mix = new Map<number, number>(); // ItemBuffer.mixFlag：格位 → 标记种类（MIX）
  private anim: MixAnim | null = null;     // pActPet2：合成中的宠物
  private jigsaw = -1;                     // JigsawIdx：打开着的拼图底板格位（-1 没开）
  private lastClick = { slot: -1, at: 0 };
  private host: ItemHost;
  readonly wx = 525;
  readonly wy = 4;

  constructor(host: ItemHost) { this.host = host; }

  toggle() {
    this.open = !this.open; this.held = -1; this.wndNo = 0; this.jujutu = -1; this.itemNo = -1; this.closeGoldInput();
    if (this.open) this.selPet = this.host.petList()[0]?.slot ?? -1; // 打开时选第一只宠物
    if (this.open && !this.anim) this.mix.clear(); // InitItem 清掉标记
  }

  private setPetPage(on: boolean) { this.petPage = on; this.held = -1; this.closeGoldInput(); }

  private pet(): PetInfo | undefined { return this.host.petList().find((p) => p.slot === this.selPet); }

  /** 左右箭头换宠物：跳过骑着的那只 */
  private cyclePet(step: number) {
    const have = new Set(this.host.petList().map((p) => p.slot));
    if (this.selPet < 0 || have.size === 0) return;
    let s = this.selPet;
    for (let n = 0; n < 10; n++) {
      s = (s + step + 5) % 5;
      if (s === this.host.ridePet()) continue;
      if (have.has(s)) { this.selPet = s; return; }
    }
  }

  private closeGoldInput() { this.goldInput?.remove(); this.goldInput = null; }

  /**
   * 丢石币（menu.cpp itemWndNo 0）：金额 "%7d" 画在 (x + 192, y + 138)；三个钮的图都画在窗口中心（图上自带位置）。
   * 按 + / − 先加减 1，按住时加速；点金额那一格（x + 175, y + 133, 86×25）可以直接输入（不超过身上的钱）；按"丢出"送出后归零。
   */
  private goldFrame(wx: number, wy: number) {
    const ui = this.host.ui, mx = wx + 135, my = wy + 220, gold = this.inv.gold;
    if (this.goldInput) {
      const v = Math.max(0, Math.min(gold, parseInt(this.goldInput.value, 10) || 0));
      if (this.goldInput.value !== "" && String(v) !== this.goldInput.value) this.goldInput.value = String(v);
      this.dropGold = v;
    } else {
      ui.text(wx + 192, wy + 138, String(this.dropGold).padStart(7, " "), FONT.WHITE);
    }
    if (ui.hitBox(wx + 175, wy + 133, wx + 175 + 86, wy + 133 + 25, "gold-input") && ui.clicked("gold-input") && !this.goldInput) {
      this.goldInput = new InputBox(this.host.canvas, wx + 180, wy + 136, 76, 18, { maxLength: 9 });
      const box = this.goldInput;
      setTimeout(() => box.focus(), 0); // 等这次点击处理完再取得焦点（同宠物改名）
      box.el.addEventListener("keydown", (ev) => { ev.stopPropagation(); if (ev.key === "Enter" || ev.key === "Escape") box.el.blur(); });
    }
    const down = ui.mouse.down;
    if (ui.button(CG.GOLD_DROP_BTN, mx, my, "gold-drop", CG.GOLD_DROP_BTN + 1) && this.dropGold > 0) {
      this.host.dropGold(this.dropGold);
      this.dropGold = 0;
      this.closeGoldInput();
    }
    for (const [key, num, sign] of [["inc", CG.GOLD_INC_BTN, 1], ["dec", CG.GOLD_DEC_BTN, -1]] as const) {
      const id = `gold-${key}`;
      const on = ui.image(down && this.goldHold === key && ui.hovered(id) ? num + 1 : num, mx, my, id);
      if (ui.clicked(id)) {
        this.closeGoldInput();
        this.dropGold += sign; this.goldInc = 0; this.goldCnt = 0; this.goldHold = key;
      } else if (this.goldHold === key && down && on) {
        this.dropGold += sign * this.goldInc;
        if (++this.goldCnt >= 30) {
          this.goldCnt = 0;
          this.goldInc = this.goldInc === 0 ? 1 : Math.min(10000, this.goldInc * 5);
        }
      }
      this.dropGold = Math.max(0, Math.min(gold, this.dropGold));
    }
    if (!down) this.goldHold = "";
  }

  /**
   * 双击使用（menu.cpp）：战斗专用的不能用；目标是自己（ITEM_TARGET_MYSELF）直接送出，是"其他人"时打开选对象的窗口，
   * 全体类的在地图上没有反应。目标栏 100 以上表示可以对倒下的人用（%100 才是种类）。
   */
  private useItem(it: InvItem) {
    if (it.field === 1) return;
    if (it.image >= 25150 && it.image <= 25240) { this.useJigsaw(it); return; }
    const t = it.target % 100;
    if (t === 0) this.host.use(it.slot, 0);
    else if (t === 1) { this.itemNo = it.slot; this.jujutu = -1; this.wndNo = 2; }
  }

  item(slot: number): InvItem | undefined { return this.inv.items.find((i) => i.slot === slot); }

  private useJigsaw(it: InvItem) {
    const board = it.jigsaw !== "" && (it.image === 25150 || (it.image <= 25159 && it.jigsaw !== "111111111"));
    if (board) { this.jigsaw = it.slot; return; }
    if (this.jigsaw < 0 || !this.item(this.jigsaw)) {
      this.jigsaw = -1;
      this.host.notice(it.image >= 25151 && it.image <= 25159 ? "请先开启大拼图底版" : "请先开启小拼图底版");
      return;
    }
    this.host.use(it.slot, this.jigsaw);
  }

  /** 拼图底板的画面（打开时每帧画；右键关掉） */
  private jigsawFrame(rects: [number, number, number, number][]) {
    const b = this.jigsaw >= 0 ? this.item(this.jigsaw) : undefined;
    const row = b && JIGSAW.find((r) => r[0] === b.image);
    if (!b || !row || !b.jigsaw) { this.jigsaw = -1; return; }
    const ui = this.host.ui;
    const on = ui.hitBox(52, 90, 329, 350, "jigsaw-wnd", false);
    rects.push([52, 90, 329, 350]);
    ui.image(JIGSAW_FRAME, 190, 220);
    ui.image(row[2], 189, 219);
    for (let k = 0; k < 9; k++) if (b.jigsaw[k] === "1") ui.image(row[1] + k, [103, 189, 275][k % 3], [138, 219, 300][Math.floor(k / 3)]);
    if (on) ui.bottomLine = "右键关闭拼图。";
    if (on && ui.mouse.rightClicked) this.jigsaw = -1;
  }

  private slotPos(slot: number): [number, number] {
    if (this.petPage && slot < PET_ITEM_SLOTS) return [this.wx + PET_POS[slot][0], this.wy + PET_POS[slot][1]];
    if (slot < EQUIP_SLOTS) {
      const [x, y] = EQUIP_POS[slot];
      return [this.wx - 1 + x, this.wy - 1 + y];
    }
    const k = (slot - EQUIP_SLOTS) % BAG_PER_PAGE;
    return [this.wx + 32 + 51 * (k % 5), this.wy + 200 + 48 * Math.floor(k / 5)];
  }

  /** 画窗口；回传画出的范围（给 main.ts 略去窗口下的名字） */
  frame(rects: [number, number, number, number][]) {
    this.mixStep();
    this.jigsawFrame(rects);
    if (!this.open) { this.mixDraw(); return; }
    if (this.wndNo === 1) { this.magicFrame(rects); return; }
    if (this.wndNo === 2) { this.selectFrame(rects); return; }
    const ui = this.host.ui, wx = this.wx, wy = this.wy;
    ui.hitBox(wx, wy, wx + 271, wy + 440, "item-wnd", false);
    rects.push([wx, wy, wx + 271, wy + 440]);
    const pet = this.petPage ? this.pet() : undefined;
    if (this.petPage && !pet) this.selPet = this.host.petList()[0]?.slot ?? -1; // 那只不在了就换第一只
    if (this.petPage) ui.image(CG.PET_ITEM_WND, wx + 121, wy + 220);
    else {
      ui.image(CG.ITEM_WND_TABS, wx + 121, wy + 220); // _PET_ITEM 的人物页底图（和宠物页 26456 一样带书签）
      ui.text(wx + 192, wy + 87, String(this.inv.gold).padStart(7, " "), FONT.WHITE);
    }
    this.sideTabs(rects);
    if (pet) this.petInfo(pet);

    const top = this.petPage ? (pet ? PET_ITEM_SLOTS : 0) : EQUIP_SLOTS;
    const slots = [...Array(top).keys(), ...Array.from({ length: BAG_PER_PAGE }, (_, i) => EQUIP_SLOTS + this.page * BAG_PER_PAGE + i)];
    let hover: InvItem | undefined;
    for (const s of slots) {
      const [x, y] = this.slotPos(s);
      const id = `inv-${s}`;
      const on = ui.hitBox(x - 24, y - 24, x + 26, y + 23, id, false);
      const it = pet && s < PET_ITEM_SLOTS ? pet.items.find((i) => i.slot === s) : this.item(s);
      const mixing = this.anim?.carried.has(s) || (this.anim && this.anim.state !== "move" && this.anim.state !== "end" && this.mix.has(s));
      if (it && s !== this.held && !mixing) ui.image(it.image, x, y);
      if (it && this.mix.has(s) && !mixing) ui.text(x - 17, y, MIX_LABEL[this.mix.get(s)!], FONT.WHITE);
      if (on && ui.mouse.rightClicked && it && s >= EQUIP_SLOTS && this.held < 0) this.toggleMix(s, it);
      if (it && it.pile > 1) ui.text(x + 10, y, String(it.pile), FONT.WHITE);
      if (on) {
        ui.box(x - 24, y - 24, x + 26, y + 23, boxColor(), false); // MakeHitBox(defX - 24, defY - 24, defX + 26, defY + 23)
        if (it) hover = it;
        else if (pet && s < PET_ITEM_SLOTS) ui.bottomLine = `宠物${PET_SLOT_NAME[s]}的装备栏。`;
        else if (s < EQUIP_SLOTS) ui.bottomLine = `${EQUIP_NAME[s]}的装备栏。`;
      }
      if (ui.clicked(id)) this.click(s, it);
    }
    // 翻页（_NEW_ITEM_）：窗口左缘的三个书签，原版图 55113+i（当前页，画在 513）/ 55110+i（画在 518），
    // 热区 (508, 160 + 56i)–(528, 157 + 56i + 60)；拿着道具移到书签上也会翻页。导出的资源里没有这几张图，画成木牌（drawTab）。
    // 木牌画在 2D 覆盖层上（会盖住 WebGL 的图），所以只画在底图左缘书签条（wx − 19 .. wx）的范围里，不压到窗口的边框；
    // 当前页用亮的石面与往左凸出 1 像素表示
    const sx1 = this.wx - 19, sx2 = this.wx - 1;
    for (let i = 0; i < 3; i++) {
      const id = `inv-page${i}`, cur = i === this.page, y1 = 160 + i * 56, y2 = 157 + i * 56 + 54;
      const on = ui.hitBox(sx1, y1, sx2, y2, id, false);
      this.drawTab(cur ? sx1 - 1 : sx1 + 1, y1, sx2, y2, cur, i);
      if (on) ui.box(sx1, y1, sx2, y2, boxColor(), false); // MakeHitBox 的闪烁框
      if (on && (ui.clicked(id) || this.held >= 0)) this.page = i;
    }
    rects.push([sx1 - 1, 160, sx2, 157 + 2 * 56 + 54]);
    if (ui.image(CG.CLOSE_BTN, wx + 135, wy + 422, "inv-close") && ui.clicked("inv-close")) { this.open = false; this.held = -1; this.closeGoldInput(); }
    if (!this.petPage) {
      this.goldFrame(wx, wy);
      // 咒术钮（画在窗口中心，图上自带位置）
      if (ui.image(CG.JUJUTU_BTN, wx + 135, wy + 220, "inv-jujutu") && ui.clicked("inv-jujutu") && this.held < 0) { this.wndNo = 1; this.jujutu = -1; this.closeGoldInput(); return; }
      if (ui.hovered("inv-jujutu")) ui.bottomLine = "咒术。";
    }

    if (hover) this.describe(hover, pet && hover.slot < PET_ITEM_SLOTS && pet.items.includes(hover) ? pet.level : undefined);
    // 拿着的道具跟着鼠标
    const onPet = pet !== undefined && this.held >= 0 && this.held < PET_ITEM_SLOTS;
    const h = onPet ? pet.items.find((i) => i.slot === this.held) : this.held >= 0 ? this.item(this.held) : undefined;
    if (h) {
      ui.image(h.image, ui.mouse.x, ui.mouse.y);
      if (ui.mouse.rightClicked) this.held = -1;
      else if (ui.mouse.clicked && !ui.overUi()) {
        const slot = this.held;
        this.held = -1;
        if (onPet) this.host.petItem(this.selPet, slot, -2); // 服务端回"无法丢弃宠物身上的物品。"
        else this.host.drop(slot, h.name);
      }
    } else if (this.held >= 0) this.held = -1;
    this.mixDraw();
  }

  // ---------------- 加工 / 料理 / 精炼 / 精工 / 修复 ----------------

  private hasSkill(...ids: number[]): boolean {
    return this.host.petList().some((p) => p.skills.some((s) => ids.includes(s.id))); // CheckPetSkill：任何一只宠物有就行
  }

  /** 右键标记 / 取消（menu.cpp MOUSE_RIGHT_CRICK 的 mixFlag 处理） */
  private toggleMix(slot: number, it: InvItem) {
    if (this.anim) return;
    if (this.mix.has(slot)) {
      // 取消；精炼的只剩这一个时改成合成（原版的写法）
      if (this.mix.get(slot) === MIX.ALCHEMIST && this.mix.size === 1) this.mix.set(slot, MIX.MERGE);
      else this.mix.delete(slot);
      return;
    }
    const first = [...this.mix.entries()].sort((a, b) => a[0] - b[0])[0];
    let f = 0;
    if (!first) {
      if (it.flags & ITEM_FLAG.COOKING) f = this.hasSkill(201) ? MIX.COOK : 0;
      else if (it.flags & (ITEM_FLAG.METAL | ITEM_FLAG.JEWEL)) f = this.hasSkill(572) ? MIX.INSLAY : 0;
      else if (it.flags & ITEM_FLAG.FIX) f = this.hasSkill(540, 664) ? MIX.FIX : 0;
      else if (this.hasSkill(202) && it.flags & ITEM_FLAG.MIX && it.alch !== "杂") f = MIX.ALCHEMIST;
      else if (this.hasSkill(200) && it.flags & ITEM_FLAG.MIX) f = MIX.MERGE;
    } else {
      const flag = first[1];
      if (it.flags & ITEM_FLAG.COOKING) f = flag === MIX.COOK ? MIX.COOK : 0;
      else if (flag === MIX.ALCHEMIST) f = it.alch === this.item(first[0])?.alch ? MIX.ALCHEMIST : 0;
      else if (flag === MIX.MERGE || flag === MIX.INSLAY || flag === MIX.FIX) f = flag;
    }
    if (f) this.mix.set(slot, f);
  }

  /**
   * 宠物窗口技能页点了地图技能：同种的标记有 2 个以上就开始合成，回传 true；否则回传 false（由宠物窗口打开道具窗口）。
   */
  startMix(pet: PetInfo, sk: PetSkill): boolean {
    const kind = MIX_SKILL[sk.id];
    if (kind === undefined || this.anim) return false;
    const slots = [...this.mix.entries()].filter(([, f]) => f === kind).map(([s]) => s).sort((a, b) => a - b);
    if (slots.length < 2) return false;
    this.anim = { pet: pet.slot, skill: sk.slot, sprite: pet.sprite, x: 750, y: 240, spd: 11, target: slots[0], last: false,
      state: "move", dir: 0, carried: new Set(), slots: [...this.mix.keys()].sort((a, b) => a - b),
      spins: 0, goal: 0, wait: 0, gap: 15, phase: 0, tick: performance.now(), animAt: performance.now() };
    return true;
  }

  /** lssproto_PS_recv：结果为 0 时"失败！"，然后宠物走回去 */
  mixResult(ok: boolean) {
    if (!ok) this.host.notice("失败！");
    if (this.anim && this.anim.state === "wait") { this.anim.state = "end"; this.anim.spd = 11; this.mix.clear(); }
  }

  /** 每 1/60 秒走一步（原版的动作每帧处理一次） */
  private mixStep() {
    const a = this.anim;
    if (!a) return;
    const now = performance.now();
    for (let n = 0; n < 8 && now - a.tick >= 1000 / 60; n++, a.tick += 1000 / 60) this.mixTick(a);
    if (now - a.tick > 1000) a.tick = now;
  }

  private mixTick(a: MixAnim) {
    const goto = (tx: number, ty: number): boolean => {
      const dx = tx - a.x, dy = ty - a.y, d = Math.hypot(dx, dy);
      if (d < a.spd / 2) return true;
      const k = Math.min(1, a.spd / d);
      a.x += dx * k; a.y += dy * k;
      a.dir = (((6 + Math.round(Math.atan2(dy, dx) / (Math.PI / 4))) % 8) + 8) % 8; // 画面方向 → 0 朝下、2 朝左、4 朝上、6 朝右
      return false;
    };
    if (a.state === "move") {
      const [tx, ty] = this.slotPos(a.target);
      if (!goto(tx, ty)) return;
      if (a.last) {
        // ANIM_DISP_MIX_PET_MOJI：转 2..15 圈
        a.state = "spin"; a.spd = 10; a.goal = 2 + Math.floor(Math.random() * 14); a.gap = 15; a.wait = 0; a.phase = 0;
        return;
      }
      a.spd = Math.max(2, Math.floor(a.spd * 0.79));
      a.carried.add(a.target);
      const next = a.slots.find((s) => s > a.target);
      if (next !== undefined) { a.target = next; return; }
      // 都叼起来了：走到第一个空格（没有空格就是第一个标记的格）
      const empty = Array.from({ length: 45 }, (_, i) => EQUIP_SLOTS + i).find((s) => !this.item(s) || this.mix.has(s));
      a.target = empty ?? a.slots[0]; a.last = true;
      return;
    }
    if (a.state === "spin") {
      if (a.wait++ < a.gap) return;
      a.wait = 0;
      if (a.phase === 0 || a.phase === 3) {
        a.dir = (a.dir + 1) % 8;
        if (a.phase === 3) { a.spins++; a.phase = 0; a.gap = Math.max(4, a.gap - 1); } else a.phase++;
      } else { a.dir = (a.dir + 7) % 8; a.phase++; }
      if (a.spins >= a.goal) {
        a.state = "wait";
        this.host.petSkill(a.pet, a.skill, a.slots.join("|"));
      }
      return;
    }
    if (a.state === "end" && goto(750, 240)) { this.anim = null; this.mix.clear(); }
  }

  /** 画合成中的宠物与叼着的道具（叼着时画在宠物头上 y − 16，转圈时画在宠物身上） */
  private mixDraw() {
    const a = this.anim, ui = this.host.ui;
    if (!a) return;
    if (a.state === "move") for (const s of a.carried) { const it = this.item(s); if (it) ui.image(it.image, a.x, a.y - 16); }
    else if (a.state !== "end") for (const s of a.slots) { const it = this.item(s); if (it) ui.image(it.image, a.x, a.y); }
    const def = ui.assets.anim(a.sprite);
    const act = def && findAction(def, a.dir, a.state === "move" || a.state === "end" ? ANIM.WALK : ANIM.STAND);
    if (!act || act.frames.length === 0) return;
    const ms = Math.max(1, act.ticks) * (1000 / 60);
    const [img, fx, fy] = act.frames[Math.floor((performance.now() - a.animAt) / ms) % act.frames.length];
    if (img >= 0) ui.image(img, a.x + fx, a.y + fy, undefined, true);
  }

  /**
   * 窗口左缘的人物 / 宠物书签：热区 (x − 21, y + 12)–(x + 2, y + 72) 是人物、(x − 21, y + 90)–(x + 2, y + 154) 是宠物；
   * 两页的底图都自带书签（_PET_ITEM：人物页 26455、宠物页 26456）。
   */
  private sideTabs(rects: [number, number, number, number][]) {
    const ui = this.host.ui, x1 = this.wx - 21, y1 = this.wy + 12;
    const tabs: [number, number, boolean][] = [[y1, y1 + 60, false], [y1 + 78, y1 + 142, true]];
    tabs.forEach(([ya, yb, pet], i) => {
      const id = `inv-side${i}`, cur = pet === this.petPage;
      const on = ui.hitBox(x1, ya, x1 + 23, yb, id, false);
      if (on) ui.box(x1, ya, x1 + 23, yb, boxColor(), false);
      if (on && ui.clicked(id) && !cur) this.setPetPage(pet);
    });
    rects.push([x1 - 1, y1, x1 + 26, y1 + 142]);
  }

  /** 宠物页上半：各格的底图、名字与能力、换宠物的箭头 */
  private petInfo(p: PetInfo) {
    const ui = this.host.ui, x = this.wx, y = this.wy;
    for (let i = 0; i < PET_ITEM_SLOTS; i++) {
      const [sx, sy] = this.slotPos(i);
      ui.image(PET_SLOT_BMP[i][PET_SLOT_MASK & (1 << i) ? 0 : 1], sx, sy);
    }
    ui.text(x + 148, y + 17, p.freeName || p.name, FONT.WHITE);
    [p.maxHp, p.atk, p.def, p.agi].forEach((v, i) => ui.text(x + 222, y + 40 + [0, 25, 49, 73][i], String(v), FONT.WHITE));
    if (ui.image(CG.PET_PREV, x + 188, y + 142, "inv-pet-prev") && ui.clicked("inv-pet-prev")) this.cyclePet(-1);
    if (ui.image(CG.PET_NEXT, x + 236, y + 142, "inv-pet-next") && ui.clicked("inv-pet-next")) this.cyclePet(1);
  }

  /**
   * 咒术窗口（itemWndNo 1）：MakeWindowDisp(364, 4, 272, 280) 平移到 (524, 4)，底图画在 (mx, my + 10)。
   * 名字在 (x + 30, y + 35 + 28n)：选好的水蓝、MP 不够红、战斗专用灰；鼠标停着时 "%2d/%2d" 在 (x + 155, y + 183)、说明从 (x + 74, y + 208) 起每 22 字节一行。
   * 对自己的咒术点了就用；对别人的（MAGIC_TARGET_OTHER）记下来，再从选对象的窗口挑人（原版是从状态窗口的名单挑，这里用道具的选对象窗口）。
   */
  private magicFrame(rects: [number, number, number, number][]) {
    const ui = this.host.ui, x = 524, y = 4, mx = x + 136, my = y + 140;
    ui.hitBox(x, y, x + 272, y + 280, "jujutu-wnd", false);
    rects.push([x, y, x + 272, y + 280]);
    ui.image(CG.JUJUTU_WND, mx, my + 10);
    const mp = this.host.mp();
    const list = this.inv.items.filter((i) => i.slot < 5 && i.magic).sort((p, q) => p.slot - q.slot);
    if (list.length === 0) ui.text(x + 30, y + 35, "没有咒术", FONT.WHITE);
    list.forEach((m, i) => {
      const id = `jujutu-${m.slot}`;
      let color = FONT.WHITE;
      if (this.jujutu === m.slot) color = FONT.AQUA;
      if (m.magicMp > mp) color = FONT.RED;
      if (m.magicField === MAGIC_FIELD_BATTLE) color = FONT.GRAY;
      const pad = Math.max(0, (16 * 8 - measure(m.magic)) / 2); // CenteringStr(MAGIC_NAME_LEN)
      const clicked = ui.text(x + 30 + pad, y + 35 + i * 28, m.magic, color, id);
      if (ui.hovered(id)) {
        ui.text(x + 155, y + 183, `${String(m.magicMp).padStart(2, " ")}/${String(mp).padStart(2, " ")}`, FONT.WHITE);
        wrapBytes(m.magicMemo, 22).forEach((l, k) => ui.text(x + 74, y + 208 + k * 24, l, FONT.WHITE));
        ui.image(m.image, x + 37, y + 220);
      }
      if (!clicked || m.magicField === MAGIC_FIELD_BATTLE || m.magicMp > mp) return;
      if (m.magicTarget === MAGIC_TARGET_MYSELF) this.host.magicUse(m.slot, 0);
      else if (m.magicTarget === MAGIC_TARGET_OTHER) { this.jujutu = m.slot; this.wndNo = 2; }
    });
    if (ui.image(CG.RETURN_BTN, mx, y + 272, "jujutu-back") && ui.clicked("jujutu-back")) { this.wndNo = 0; this.jujutu = -1; }
    if (ui.mouse.rightClicked) { this.wndNo = 0; this.jujutu = -1; }
  }

  /** 选对象的窗口（itemWndNo 2）：MakeWindowDisp(368, 4, 192, 304) 平移到 (528, 4)；自己与宠物的名字从 (x + 25, y + 30) 起每行 23；回上一页在 (mx, y + 287) */
  private selectFrame(rects: [number, number, number, number][]) {
    const ui = this.host.ui, x = 528, y = 4, mx = x + 96, my = y + 152;
    ui.hitBox(x, y, x + 192, y + 304, "select-wnd", false);
    rects.push([x, y, x + 192, y + 304]);
    ui.image(CG.SELECT_WND, mx, my);
    const rows = [{ target: 0, name: this.host.name(), y: y + 30 }, ...this.host.pets().map((p, i) => ({ target: p.slot + 1, name: p.name, y: y + 30 + (i + 1) * 23 }))];
    // 队友从 y = 183 起（menu.cpp itemWndNo 2）
    this.host.partyMembers().forEach((p, i) => rows.push({ target: 6 + p.slot, name: p.name, y: 183 + i * 23 }));
    rows.forEach((r) => {
      if (!ui.text(x + 25, r.y, r.name, FONT.WHITE, `select-${r.target}`)) return;
      if (this.itemNo >= 0) {
        // 道具：送出后关掉选对象的窗口，回到道具窗口（lssproto_ID_send）
        this.host.use(this.itemNo, r.target);
        this.itemNo = -1; this.wndNo = 0;
        return;
      }
      const m = this.item(this.jujutu);
      if (!m) { this.wndNo = 1; return; }
      this.host.magicUse(m.slot, r.target);
      // MP 不够再用一次就回到咒术窗口
      if (m.magicMp > this.host.mp() - m.magicMp) this.wndNo = 1;
    });
    const back = (ui.image(CG.RETURN_BTN, mx, y + 287, "select-back") && ui.clicked("select-back")) || ui.mouse.rightClicked;
    if (back) { this.wndNo = this.jujutu >= 0 ? 1 : 0; this.itemNo = -1; }
  }

  /**
   * 书签：原版的图（55110..55115）不在这套资源里，照道具窗口的石框画：黑边、灰石面带深浅斑点、左上亮右下暗的斜边，
   * 刻在石头上的深色字（壹 / 贰 / 叁）；目前这一页的石面较亮、伸进窗口里与石框连在一起，其他两页较暗、缩在外面。
   */
  private drawTab(x1: number, y1: number, x2: number, y2: number, cur: boolean, i: number, label = ["壹", "贰", "叁"][i]) {
    const ui = this.host.ui;
    const [face, light, dark, ink] = cur ? ["#aaa49a", "#d6d0c4", "#6a645a", "#2e2a24"] : ["#857f75", "#a9a397", "#4e4940", "#26221d"];
    ui.box(x1, y1, x2, y2, "#1e1b17", true);             // 外框
    ui.box(x1 + 1, y1 + 1, x2 - 1, y2 - 1, dark, true);  // 右下的暗边
    ui.box(x1 + 1, y1 + 1, x2 - 2, y2 - 2, light, true); // 左上的亮边
    ui.box(x1 + 2, y1 + 2, x2 - 2, y2 - 2, face, true);  // 石面
    // 石头的斑点（固定的伪随机位置，每个书签不同）
    let seed = 7 + i * 31;
    for (let k = 0; k < 18; k++) {
      seed = (seed * 1103515245 + 12345) & 0x7fffffff;
      const sx = x1 + 3 + (seed % Math.max(1, x2 - x1 - 6)), sy = y1 + 3 + ((seed >> 8) % Math.max(1, y2 - y1 - 6));
      ui.box(sx, sy, sx + 1, sy + 1, k % 3 === 0 ? light : dark, true);
    }
    ui.text((x1 + x2) / 2 - 7, (y1 + y2) / 2 - 8, label, ink);
  }

  private click(slot: number, it: InvItem | undefined) {
    const now = performance.now();
    const dbl = this.lastClick.slot === slot && now - this.lastClick.at < 400;
    this.lastClick = { slot, at: now };
    const onPet = (s: number) => this.petPage && s < PET_ITEM_SLOTS;
    if (dbl && it) {
      // 双击：第一下已经拿起来了，第二下直接使用 / 装备 / 卸下
      this.held = -1;
      if (this.petPage) {
        // 宠物页：宠物身上的连点二下不动作；宠物装备装上去；人物的道具切回人物页
        if (onPet(slot)) return;
        if (petEquipSlot(it.type) >= 0) this.equipPet(slot, -1);
        else this.setPetPage(false);
        return;
      }
      if (slot >= EQUIP_SLOTS && petEquipSlot(it.type) >= 0) { this.setPetPage(true); return; } // 人物页双击宠物装备：切到宠物页
      if (slot < EQUIP_SLOTS) this.host.move(slot, -1);
      else this.useItem(it);
      return;
    }
    if (this.held >= 0) {
      const from = this.held;
      this.held = -1;
      if (from === slot) return;
      if (onPet(from) || onPet(slot)) this.equipPet(from, slot); // 有一边是宠物身上的格
      else this.host.move(from, slot);
      return;
    }
    if (it) this.held = slot;
  }

  /** 宠物身上的道具：装上去（目标是宠物身上的格，或 to = -1）时骑着的宠物不行（原客户端挡） */
  private equipPet(from: number, to: number) {
    if (this.selPet < 0) return;
    if ((to === -1 || to < PET_ITEM_SLOTS) && this.selPet === this.host.ridePet()) { this.host.notice("骑宠不可装装备！"); return; }
    this.host.petItem(this.selPet, from, to);
  }

  /** 道具名（等级不够时红色）与说明 */
  /**
   * 原版 menu.cpp：名字 (16, 331)（等级不够红字），"耐久度(…)" (150, 331)，说明每行 28 个半角宽、从 355 起每行 24。
   * 说明后面再加一行实际的能力值（原版说明是固定文字，实际数值在产生时随机），窗口里放不下的行略去。
   */
  private describe(it: InvItem, petLevel?: number) {
    const ui = this.host.ui, wx = this.wx, wy = this.wy;
    const pe = petEquipSlot(it.type);
    // 宠物装备按宠物页那只宠物的等级（人物页看不出给哪一只，不标红）
    const myLevel = pe >= 0 ? petLevel ?? (this.petPage ? this.pet()?.level : undefined) ?? Infinity : this.host.level();
    const color = it.level > myLevel ? FONT.RED : it.merged ? FONT.YELLOW : FONT.WHITE; // 加工出来的黄色（ITEM_MERGEFLG）
    ui.text(wx + 16, wy + 331, it.name, color);
    if (it.damage) ui.text(wx + 150, wy + 331, `耐久度(${it.damage})`, color);
    const lines: [string, string][] = [];
    const wrap = (text: string, c: string) => {
      let line = "", w = 0;
      for (const ch of text) {
        const cw = ch.charCodeAt(0) > 0xff ? 2 : 1;
        if (w + cw > 28) { lines.push([line, c]); line = ""; w = 0; }
        line += ch; w += cw;
      }
      if (line) lines.push([line, c]);
    };
    // 使用 / 装备的条件（原版没有这一行，只把等级不够的名字标红）：装备栏与等级、能用的场合与对象
    const lvOk = it.level <= myLevel;
    const lv = it.level > 0 ? `　等级${it.level}以上` : "";
    if (it.equip >= 0) wrap(`装备：${EQUIP_NAME[it.equip]}栏${lv}`, lvOk ? FONT.YELLOW : FONT.RED);
    if (pe >= 0) wrap(`宠物装备：${PET_SLOT_NAME[pe]}栏${lv}`, lvOk ? FONT.YELLOW : FONT.RED);
    if (it.usable) {
      const where = it.field === 1 ? "战斗中" : it.field === 2 ? "地图上" : "地图与战斗";
      const who = ["自己", "自己或宠物", "我方全体", "对方全体", "全体"][it.target % 100] ?? "";
      wrap(`使用：${where}　对象：${who}${it.target >= 100 ? "（可对倒下的人）" : ""}${it.equip >= 0 ? "" : lv}`, lvOk ? FONT.YELLOW : FONT.RED);
    }
    wrap(it.memo.trim(), FONT.WHITE);
    if (it.params) wrap(`实际 ${it.params}`, FONT.AQUA);
    lines.slice(0, 2).forEach(([l, c], i) => ui.text(wx + 16, wy + 355 + i * 24, l, c)); // 第 3 行会压到"关闭"钮
    if (lines.length > 2) {
      // 窗口里只放得下 2 行：超过时在道具窗口左边另开一个原版样式的窗口列出全部说明
      const tw = 4, th = Math.max(2, Math.ceil((lines.length * 22 + 44) / 48));
      const px = wx - 19 - tw * 64 - 4, py = Math.max(4, Math.min(600 - 30 - th * 48, wy + 300)); // 让开左缘的书签条

      ui.window(px, py, tw, th);
      ui.text(px + 20, py + 16, it.name, color);
      lines.forEach(([l, c], i) => ui.text(px + 20, py + 40 + i * 22, l, c));
      ui.text(wx + 244, wy + 355 + 24, "…", FONT.YELLOW);
    }
    if (pe >= 0) ui.bottomLine = this.petPage ? "双击装到宠物身上，也可以拖到宠物的装备栏。" : "双击切到宠物装备栏。";
    else if (it.equip >= 0 || it.usable) ui.bottomLine = it.usable ? "双击使用。" : "双击装备，也可以拖到装备栏。";
  }
}

/** 战斗中可以用的道具（回复类、不是地图专用） */
export function battleItems(inv: S2CInventory): InvItem[] {
  return inv.items.filter((i) => i.slot >= EQUIP_SLOTS && i.usable && i.field !== 2);
}

