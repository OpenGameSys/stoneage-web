// 宠物店与宠物技能店用到的两种窗口（原客户端 login.cpp）：
//   petselect     serverWindowType2（WINDOW_MESSAGETYPE_PETSELECT）：列出自己的宠物，点一只回传它是第几只（从 1 起），按钮回传按钮位元
//   petskillshop  serverWindowType6（WINDOW_MESSAGETYPE_PETSKILLSHOP）：skillShopWindow1..4
//                 技能清单 → 要让谁学 → 要在哪一格 → "让[宠物]将[技能]学起来吗？" → 回传 "第几个技能|第几只|第几格|价格"
// 窗口座标照原版 800×600（_NEW_RESOMODE）。

import { FONT, Ui, measure } from "./ui.ts";
import type { PetInfo, S2CNpcWindow } from "./proto.gen.ts";

const CG = {
  SKILLSHOP_WIN: 26139, EXIT_BTN: 26096, RETURN_BTN: 26043, YES_BTN: 26094, NO_BTN: 26095,
  PREV_BTN: 26180, PREV_BTN_DOWN: 26181, NEXT_BTN: 26182, NEXT_BTN_DOWN: 26183,
};
const PAGE_SKILLS = 8; // MAX_SKILL_SHOP_SKILL

// serverWindowType2 的 btnTitle：按钮位元 0..5 的文字
const PET_SELECT_BTN = ["确  定", "取  消", "确  定", "取  消", "上一页", "下一页"];

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
const petName = (p: PetInfo) => p.freeName || p.name;

/**
 * serverWindowType2：7×5 格，标题"请选择宠物"，每只宠物一行（名字、LV、MaxHP），点一只回传它在宠物栏的编号 + 1；
 * 按钮照窗口的按钮位元排在底下。Esc 关掉不回传。
 */
export function drawPetSelect(ui: Ui, win: S2CNpcWindow, pets: PetInfo[], reply: (button: number, data: string) => void, esc: boolean) {
  const W = 7, H = 5, x = (800 - W * 64) / 2, y = (600 - H * 48) / 2;
  ui.window(x, y, W, H, "npc-win");
  ui.text(x + 54, y + 21, "＝＝＝         请选择宠物         ＝＝＝", FONT.WHITE);
  [...pets].sort((a, b) => a.slot - b.slot).forEach((p, k) => {
    const j = k + 1, ty = y + 21 + (j + 1) * 21, id = `psel-${p.slot}`;
    ui.hitBox(x + 41, y + 18 + (j + 1) * 21, x + 41 + 366, y + 18 + (j + 1) * 21 + 20, id);
    ui.text(x + 42, ty, petName(p), FONT.WHITE);
    ui.text(x + 260, ty, `LV.${p.level}`, FONT.WHITE);
    ui.text(x + 314, ty, `MaxHP ${p.maxHp}`, FONT.WHITE);
    if (ui.clicked(id)) reply(0, String(p.slot + 1));
  });
  const bits = [0, 1, 2, 3, 4, 5].filter((i) => win.buttons & (1 << i)).slice(0, 4);
  const bw = W * 64 / (bits.length + 1);
  bits.forEach((bit, k) => {
    if (ui.text(x + bw * (k + 1) - 27, y + 196, PET_SELECT_BTN[bit], FONT.YELLOW, `psel-btn${bit}`)) reply(1 << bit, "0");
  });
  if (esc) reply(-1, "");
}

interface SealSkill { name: string; price: number; info: string[]; }
type Step = "list" | "pet" | "slot" | "ask";

/** 宠物技能店（serverWindowType6） */
export class PetSkillShop {
  win: S2CNpcWindow | null = null;
  private title = "";
  private msg: string[] = [];
  private skills: SealSkill[] = [];
  private step: Step = "list";
  private page = 0;
  private sel = -1;
  private pet = -1;  // selShopSkillPetNo（宠物栏编号 + 1）
  private slot = -1; // selShopSkillSlotNo（从 1 起）

  private canned = false; // 技能罐头（ITEM_useSkillCanned 的 WINDOWS_MESSAGETYPE_PETSKILLSHOW）：只选技能栏
  get open() { return this.win !== null; }

  /** 技能罐头的窗口：文字是宠物栏编号，直接进到选技能栏；选了送出栏位（从 0 起），"回"送出 -1 */
  showSlots(m: S2CNpcWindow) {
    this.win = m; this.canned = true; this.pet = (Number(m.text) || 0) + 1; this.step = "slot";
  }

  /** initServerWindowType6：解析 "1|店名|说明|技能名|价格|说明|…"，开头不是 1 的不开 */
  show(m: S2CNpcWindow) {
    const f = m.text.split("|");
    if (f[0] !== "1") return;
    this.win = m; this.canned = false;
    this.title = f[1] ?? "";
    this.msg = split((f[2] ?? "").replace(/\\n/g, "\n"), 44, 2);
    this.skills = [];
    for (let i = 3; i < f.length && f[i] !== ""; i += 3) {
      this.skills.push({ name: f[i], price: Number(f[i + 1]) || 0, info: split(f[i + 2] ?? "", 24, 3) });
    }
    this.step = "list"; this.page = Math.min(this.page, this.maxPage() - 1);
  }

  close() { this.win = null; }

  private maxPage() { return this.skills.length > 0 ? Math.floor((this.skills.length + 7) / PAGE_SKILLS) : 1; }

  /** 画一帧；选完时用 send 送出回传的资料，关掉窗口时 win 变成 null */
  draw(ui: Ui, esc: boolean, gold: number, pets: PetInfo[], send: (data: string) => void) {
    if (!this.win) return;
    ui.hitBox(0, 0, 800, 600, "modal-block", false);
    if (esc) { this.close(); return; }
    switch (this.step) {
      case "list": this.drawList(ui, gold); break;
      case "pet": this.drawPets(ui, pets); break;
      case "slot": this.drawSlots(ui, pets, send); break;
      case "ask": this.drawAsk(ui, pets, send); break;
    }
  }

  /** skillShopWindow1：432×428，每页 8 个技能与价格，滑鼠停在某个技能上显示说明；钱不够的标灰、不能选 */
  private drawList(ui: Ui, gold: number) {
    const w = 432, h = 428, x = (800 - w) / 2, y = (600 - h) / 2;
    ui.image(CG.SKILLSHOP_WIN, x + w / 2, y + h / 2);
    ui.text(x + (w - measure(this.title)) / 2, y + 18, this.title, FONT.WHITE);
    this.msg.forEach((l, i) => ui.text(x + 18, y + 48 + i * 18, l, FONT.WHITE));
    ui.text(x + 244, y + 84, `金钱 ${pad(gold, 8)}Ｓ`, FONT.WHITE);
    if (ui.button(CG.PREV_BTN, x + 32, y + 100, "pss-prev", CG.PREV_BTN_DOWN) && this.page > 0) this.page--;
    if (ui.button(CG.NEXT_BTN, x + 200, y + 100, "pss-next", CG.NEXT_BTN_DOWN) && this.page + 1 < this.maxPage()) this.page++;
    ui.text(x + 66, y + 92, `${pad(this.page + 1, 2)}/${pad(this.maxPage(), 2)} 页`, FONT.WHITE);
    let focus: SealSkill | null = null;
    for (let i = 0; i < PAGE_SKILLS; i++) {
      const k = this.page * PAGE_SKILLS + i, s = this.skills[k];
      if (!s) continue;
      const y1 = y + i * 21 + 116;
      if (ui.hitBox(x + 32, y1, x + 32 + 372, y1 + 20, `pss-${k}`)) focus = s;
      const color = s.price > gold ? FONT.GRAY : FONT.WHITE;
      ui.text(x + 34, y + 118 + i * 21, s.name, color);
      ui.text(x + 312, y + 118 + i * 21, `${pad(s.price, 8)}Ｓ`, color);
      if (ui.clicked(`pss-${k}`) && s.price <= gold) { this.sel = k; this.step = "pet"; }
    }
    if (focus) focus.info.forEach((l, i) => ui.text(x + 27, y + 308 + i * 20, l, FONT.WHITE));
    if (ui.button(CG.EXIT_BTN, x + 216, y + 402, "pss-exit")) this.close();
  }

  /** skillShopWindow2：7×5 格"要让谁学呢？"，点一只宠物；"回"回到清单 */
  private drawPets(ui: Ui, pets: PetInfo[]) {
    const W = 7, H = 5, x = (800 - W * 64) / 2, y = (600 - H * 48) / 2;
    ui.window(x, y, W, H);
    ui.text(x + 145, y + 25, "要让谁学呢？", FONT.WHITE);
    [...pets].sort((a, b) => a.slot - b.slot).forEach((p, j) => {
      const id = `pss-pet${p.slot}`, ty = y + 28 + (j + 1) * 26;
      ui.hitBox(x + 41, y + 25 + (j + 1) * 26, x + 41 + 366, y + 25 + (j + 1) * 26 + 22, id);
      ui.text(x + 62, ty, petName(p), FONT.WHITE);
      ui.text(x + 260, ty, `LV.${p.level}`, FONT.WHITE);
      ui.text(x + 314, ty, `MaxHP ${p.maxHp}`, FONT.WHITE);
      if (ui.clicked(id)) { this.pet = p.slot + 1; this.step = "slot"; }
    });
    if (ui.button(CG.RETURN_BTN, x + W * 32, y + 204, "pss-back")) this.step = "list";
  }

  /** skillShopWindow3：5×6 格"要在那个位置做呢？"，列出宠物的技能栏（技 n: 技能名），点一格；"回"回到选宠物 */
  private drawSlots(ui: Ui, pets: PetInfo[], send: (data: string) => void) {
    const p = pets.find((x) => x.slot === this.pet - 1);
    if (!p) { if (this.canned) { send("-1"); this.close(); } else this.step = "pet"; return; }
    const W = 5, H = 6, x = (800 - W * 64) / 2, y = (600 - H * 48) / 2;
    ui.window(x, y, W, H);
    ui.text(x + 75, y + 33, "要在那个位置做呢？", FONT.WHITE);
    for (let i = 0; i < 7 && i < p.maxSkill; i++) {
      const s = p.skills.find((k) => k.slot === i), id = `pss-slot${i}`;
      ui.hitBox(x + 41, y + 33 + (i + 1) * 26, x + 41 + 238, y + 33 + (i + 1) * 26 + 22, id);
      ui.text(x + 46, y + 36 + (i + 1) * 26, `技 ${i + 1}: ${s ? s.name : ""}`, FONT.WHITE);
      if (ui.clicked(id)) {
        if (this.canned) { send(String(i)); this.close(); return; }
        this.slot = i + 1; this.step = "ask";
      }
    }
    if (ui.button(CG.RETURN_BTN, x + 160, y + 256, "pss-back2")) {
      if (this.canned) { send("-1"); this.close(); } else this.step = "pet";
    }
  }

  /** skillShopWindow4：8×2 格确认，"是"送出、"否"回到选技能栏 */
  private drawAsk(ui: Ui, pets: PetInfo[], send: (data: string) => void) {
    const p = pets.find((x) => x.slot === this.pet - 1), s = this.skills[this.sel];
    if (!p || !s) { this.step = "list"; return; }
    const W = 8, H = 2, x = (800 - W * 64) / 2, y = (600 - H * 48) / 2;
    ui.window(x, y, W, H);
    ui.text(x + 20, y + 20, `让[${petName(p)}]将[${s.name}]`, FONT.WHITE);
    ui.text(x + 20, y + 40, "学起来吗？", FONT.WHITE);
    if (ui.button(CG.YES_BTN, x + W * 64 / 3, y + 72, "pss-yes")) {
      const data = `${this.sel + 1}|${this.pet}|${this.slot}|${s.price}`;
      this.close();
      send(data);
    } else if (ui.button(CG.NO_BTN, x + W * 64 / 3 * 2, y + 72, "pss-no")) this.step = "slot";
  }
}
