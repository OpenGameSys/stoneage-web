// 宠物相簿 / 图鉴（原版 __ALBUM_47，menu.cpp）：完全在客户端，原版存在 data\album_47.dat，这里存 localStorage（sa.album.<账号>）。
//   看到：画面上出现过（地图上的角色、战斗中的每一个人，CheckNewPet / CheckBattleNewPet）→ 灰色的名字
//   照相：宠物详细资料的"照相"钮 → 记下那时的能力值，名字前出现 NEW；点开看过后去掉 NEW
//   窗口：MakeWindowDisp(4, 4, 272, 448)，底图 CG_ALBUM_WND 26230；上半是选中那一只的资料与头像，下半每页 8 只（共 570 种、72 页）
//   开关：任务栏的"图鉴"或 Ctrl+A

import type { PetInfo } from "./proto.gen.ts";
import { PET_ALBUM_TBL } from "./albumdata.ts";
import { FONT, SYS, measure, type Ui } from "./ui.ts";

const MAX_PET_KIND = 570, PAGE = 8;
const CG = { WND: 26230, NEW_ICON: 26231, PREV: 26180, NEXT: 26182, CLOSE: 26042 };
// 照相钮不能用的宠物形象（menu.cpp 照相的检查）
const NO_SNAP = new Set([100451, 100432, 101280, 101281, 100015, 100018, 101279, 100401, 101414, 101167, 101172, 102011, 102012]);

interface Entry { flag: number; name: string; freeName?: string; face?: number; level?: number; maxHp?: number; str?: number; quick?: number; def?: number; attr?: number[] }

type Step = [string, number, number]; // ["gt" / "eq", 比较值, 减去的量（eq 时是结果）]
// 形象图号 − 100250 → 表的下标（CheckNewPet 的分段对照；照相的版本多一段、改一段）
const SEEN: Step[] = [["gt", 1800, 1208], ["gt", 1755, 1201], ["gt", 1739, 1200], ["gt", 1686, 1175], ["gt", 1641, 1167], ["gt", 1635, 1148], ["gt", 1634, 1149],
  ["gt", 1616, 1146], ["gt", 1568, 1103], ["gt", 1564, 1101], ["gt", 1516, 1055], ["eq", 1516, 455], ["gt", 1509, 1054], ["eq", 1497, 395],
  ["gt", 1495, 1044], ["gt", 1490, 1043], ["gt", 1485, 1040], ["gt", 1454, 1034], ["eq", 1214, 331], ["eq", 1217, 328],
  ["gt", 1361, 952], ["gt", 1356, 999], ["gt", 1281, 919], ["gt", 1239, 889], ["gt", 1186, 886], ["gt", 1173, 877], ["gt", 1172, 877], ["gt", 1161, 868],
  ["gt", 930, 745], ["eq", 927, 293], ["gt", 929, 644], ["gt", 927, 643], ["gt", 900, 621], ["gt", 180, 389]];
const SNAP: Step[] = SEEN.flatMap((s): Step[] => s[0] === "gt" && s[1] === 1634 ? [s, ["gt", 1628, 1147]] : s[0] === "gt" && s[1] === 1616 ? [["gt", 1615, 1145]] : [s]);

function tblNo(gra: number, steps: Step[]): number {
  const n = gra - 100250;
  for (const [op, v, d] of steps) {
    if (op === "gt" && n > v) return n - d;
    if (op === "eq" && n === v) return d;
  }
  return n;
}

export class Album {
  private key = "";
  private list: Entry[] = [];
  open = false;
  private page = 0;
  private sel = -1;

  /** 上线时读这个账号的相簿（LoadAlbum） */
  load(account: string) {
    this.key = `sa.album.${account}`;
    this.list = Array.from({ length: MAX_PET_KIND }, () => ({ flag: 0, name: "" }));
    try {
      const saved = JSON.parse(localStorage.getItem(this.key) ?? "null");
      if (saved && typeof saved === "object") for (const k in saved) if (Number(k) < MAX_PET_KIND) this.list[Number(k)] = saved[k];
    } catch { /* 坏掉就重来 */ }
  }

  private save() {
    if (!this.key) return;
    const out: Record<number, Entry> = {};
    this.list.forEach((e, i) => { if (e.flag) out[i] = e; });
    try { localStorage.setItem(this.key, JSON.stringify(out)); } catch { /* 存不了就算了 */ }
  }

  private albumNo(gra: number, steps: Step[]): number {
    const t = tblNo(gra, steps);
    return t >= 0 && t < PET_ALBUM_TBL.length ? PET_ALBUM_TBL[t][0] : -1;
  }

  /** CheckNewPet：看到一种宠物（还没看过时记下名字） */
  seen(gra: number) {
    if (!this.key || gra < 100250) return;
    const no = this.albumNo(gra, SEEN);
    if (no < 0 || no >= MAX_PET_KIND || this.list[no].flag !== 0) return;
    this.list[no] = { flag: 1, name: PET_ALBUM_TBL[tblNo(gra, SEEN)][1] };
    this.save();
  }

  /** 照相：回传要显示的讯息（"%s 照相完成！"），不能照时回 null */
  snap(p: PetInfo): string | null {
    if (NO_SNAP.has(p.sprite)) return null;
    const t = tblNo(p.sprite, SNAP);
    const no = t >= 0 && t < PET_ALBUM_TBL.length ? PET_ALBUM_TBL[t][0] : -1;
    if (no < 0 || no >= MAX_PET_KIND) return null;
    const [, name, face] = PET_ALBUM_TBL[t];
    const freeName = p.freeName || name;
    this.list[no] = { flag: 2, name, freeName, face, level: p.level, maxHp: p.maxHp, str: p.atk, quick: p.agi, def: p.def, attr: p.attrs.slice(0, 4) };
    this.save();
    return `${freeName} 照相完成！`;
  }

  toggle() { this.open = !this.open; }

  frame(ui: Ui, rects: [number, number, number, number][]) {
    if (!this.open) return;
    const wx = 4, wy = 4, mx = wx + 136, my = wy + 224, x = wx, y0 = wy + 28;
    ui.hitBox(wx, wy, wx + 272, wy + 448, "album-wnd", false);
    rects.push([wx, wy, wx + 272, wy + 448]);
    ui.image(CG.WND, mx, my);
    const num = (v: number | undefined, w: number) => String(v ?? 0).padStart(w, " ");
    const e = this.sel >= 0 ? this.list[this.sel] : undefined;
    if (e && e.flag >= 2) {
      const name = e.freeName ?? e.name;
      ui.text(x + 13 + Math.max(0, (16 * 7 - measure(name)) / 2), y0, name, FONT.WHITE);
      ui.text(x + 36, y0 + 22, num(e.level, 3), FONT.WHITE);
      ui.text(x + 36, y0 + 46, num(e.maxHp, 4), FONT.WHITE);
      ui.text(x + 108, y0 + 46, num(e.str, 3), FONT.WHITE);
      ui.text(x + 36, y0 + 70, num(e.quick, 4), FONT.WHITE);
      ui.text(x + 108, y0 + 70, num(e.def, 3), FONT.WHITE);
      const bars: [string, string][] = [[SYS.GREEN, SYS.GREEN2], [SYS.AQUA, SYS.AQUA2], [SYS.RED, SYS.RED2], [SYS.YELLOW, SYS.YELLOW2]];
      (e.attr ?? []).forEach((a, i) => {
        const by = y0 + 99 + i * 20, w = Math.floor(a * 0.8);
        if (w <= 0) return;
        ui.box(x + 36, by + 2, x + 36 + w + 1, by + 11, bars[i][1], false);
        ui.box(x + 35, by + 1, x + 35 + w + 1, by + 10, bars[i][1], false);
        ui.box(x + 34, by, x + 34 + w, by + 8, bars[i][0], true);
      });
      if (e.face) ui.image(e.face, wx + 199, wy + 117);
    }
    let y = wy + 215;
    for (let i = 0; i < PAGE; i++, y += 22) {
      const n = this.page * PAGE + i;
      const it = this.list[n];
      if (!it) continue;
      if (it.flag === 2) ui.image(CG.NEW_ICON, x + 24, y + 9);
      ui.text(x + 46, y + 1, `No.${String(n + 1).padStart(3, " ")}`, FONT.WHITE);
      if (it.flag === 1) ui.text(x + 112, y, it.name, FONT.GRAY);
      else if (it.flag >= 2 && ui.text(x + 112, y, it.name, FONT.WHITE, `album-${n}`)) {
        this.sel = n;
        if (it.flag === 2) { it.flag = 3; this.save(); }
      }
    }
    const pages = Math.ceil(MAX_PET_KIND / PAGE);
    if (ui.button(CG.PREV, wx + 107, wy + 400, "album-prev", CG.PREV + 1)) this.page = (this.page + pages - 1) % pages;
    if (ui.button(CG.NEXT, wx + 166, wy + 400, "album-next", CG.NEXT + 1)) this.page = (this.page + 1) % pages;
    if (ui.image(CG.CLOSE, mx, wy + 429, "album-close") && ui.clicked("album-close")) this.open = false;
  }
}

/** 全客户端共用一份（地图与战斗都要登记"看到"） */
export const album = new Album();
