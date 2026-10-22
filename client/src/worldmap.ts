// 世界地图（地图窗口，Ctrl+M / 任务栏"地图"）。原版只有以自己为中心 54×54 格的自动地图（map.cpp createAutoMap：每格 4 个像素的小点，
// sprdisp.cpp drawAutoMap）；这里重做成一个地图窗口：
//   · 本区全图：走过看过的格子画成连续的等角菱形（颜色沿用原版 getAutoMapColor：那张图在 0 号调色板下的平均色，物件照占地铺开），
//     没看过的是暗底，传送点亮青色，自己是会呼吸的金色标记带朝向，旁边的玩家 / NPC 是白 / 黄点，画面现在看到的范围画成框；
//   · 缩放（2..16 像素一格）、按住拖动、"全图"按看过的范围缩放、"回到自己"（跟着自己移动）；
//   · 左边列出去过的区域（楼层名，field.ts 存在 localStorage sa.floors.<角色>），点一下看那一层看过的部分
//     （格子资料向服务端补要，MapReq 不限楼层；看过的格子本来就记在 sa.seen.<角色>.<楼层>）。
// 平均色的计算：整页图集解码一次、把排队的图都算完就丢掉（整页解码很占记忆体）。

import type { Assets } from "./assets.ts";
import type { Field, FloorView } from "./field.ts";
import { FONT, measure, type Ui } from "./ui.ts";

type Rgb = [number, number, number] | null;

/** 地图图号 → 平均色（getAutoMapColor） */
export class TileColors {
  version = 0; // 算出新颜色时加一（地图据此重画）
  private colors = new Map<number, Rgb>();           // null 为全透明，不画
  private queue = new Map<string, Set<number>>();   // 待算的图，按图集页分组
  private busy = false;
  private palette: ImageData | null = null;
  private readonly assets: Assets;
  private readonly root: string;

  constructor(assets: Assets, root: string) { this.assets = assets; this.root = root; }

  /** 算好了直接给；还没算好就排进队列（回传 null，这次先不画） */
  color(mapNumber: number): Rgb {
    if (!mapNumber) return null;
    const c = this.colors.get(mapNumber);
    if (c !== undefined) return c;
    const idx = mapNumber >= 1000000 ? mapNumber : this.assets.tile(mapNumber);
    if (idx === undefined) { this.colors.set(mapNumber, null); return null; }
    const e = this.assets.image(idx);
    if (!e) { this.version++; return null; } // 图号表还在载入：下一帧再试
    if (e.page < 0) { this.colors.set(mapNumber, null); return null; }
    const key = `${e.set}/${e.page}`;
    let q = this.queue.get(key);
    if (!q) this.queue.set(key, (q = new Set()));
    q.add(mapNumber);
    this.colors.set(mapNumber, null);
    this.pump();
    return null;
  }

  private async pump() {
    if (this.busy) return;
    this.busy = true;
    try {
      this.palette ??= await this.decode(`${this.root}palettes.png`);
      while (this.queue.size) {
        const [key, nums] = this.queue.entries().next().value!;
        this.queue.delete(key);
        const [set, page] = key.split("/").map(Number);
        const data = await this.decode(`${this.root}pages/${set === 0 ? "i8" : "rgba"}/${String(page).padStart(5, "0")}.${this.assets.pageFormat}`);
        for (const n of nums) {
          const idx = n >= 1000000 ? n : this.assets.tile(n)!;
          const e = this.assets.image(idx)!;
          this.colors.set(n, average(data, e.x, e.y, e.w, e.h, set === 0 ? this.palette : null));
        }
        this.version++;
      }
    } catch (err) {
      console.warn("地图取色失败", err);
    } finally {
      this.busy = false;
    }
  }

  private async decode(url: string): Promise<ImageData> {
    const bmp = await createImageBitmap(await (await fetch(url)).blob(), { colorSpaceConversion: "none", premultiplyAlpha: "none" });
    const c = new OffscreenCanvas(bmp.width, bmp.height), g = c.getContext("2d")!;
    g.drawImage(bmp, 0, 0);
    bmp.close();
    return g.getImageData(0, 0, c.width, c.height);
  }
}

/** getAutoMapColor：不透明像素的平均色。8 位图的灰度值是调色板下标（0 透明），查 0 号调色板 */
function average(page: ImageData, x0: number, y0: number, w: number, h: number, pal: ImageData | null): Rgb {
  let r = 0, g = 0, b = 0, n = 0;
  for (let y = y0; y < y0 + h; y++) for (let x = x0; x < x0 + w; x++) {
    const s = (y * page.width + x) * 4;
    if (pal) {
      const k = page.data[s];
      if (k === 0) continue;
      r += pal.data[k * 4]; g += pal.data[k * 4 + 1]; b += pal.data[k * 4 + 2];
    } else {
      if (page.data[s + 3] === 0) continue;
      r += page.data[s]; g += page.data[s + 1]; b += page.data[s + 2];
    }
    n++;
  }
  return n ? [Math.round(r / n), Math.round(g / n), Math.round(b / n)] : null;
}

// ---------------- 地图窗口 ----------------

export interface MapHost {
  ui: Ui;
  assets: Assets;
  root: string;
  field(): Field | null;
}

const ZOOMS = [2, 3, 4, 6, 8, 12, 16]; // 一格的半宽（像素）；格子菱形半高是它的 3/4（画面 64×48 的比例）
const W = 10, H = 10;                   // 窗口 640×480
const X = (800 - W * 64) / 2, Y = 40;
const LIST_W = 150, LIST_ROWS = 14, LIST_ROW_H = 20;
const MAP_X = X + 26 + LIST_W + 8, MAP_Y = Y + 66;
const MAP_W = W * 64 - (26 + LIST_W + 8) - 26, MAP_H = H * 48 - 66 - 58;
const BG = "#0a0f1c";

export class MapWindow {
  private host: MapHost;
  private colors: TileColors;
  private floor = -1;          // 看哪一层（-1 = 自己所在的）
  private cx = 0; cy = 0;      // 视野中心（格子座标，可以是小数）
  private zoom = 3;            // ZOOMS 的下标
  private follow = true;       // 跟着自己移动
  private drag: { x: number; y: number } | null = null;
  private dragged = false;
  private listTop = 0;
  private base: HTMLCanvasElement;
  private marks: HTMLCanvasElement;
  private drawn = "";
  private seenCache = new Map<number, { version: number; count: number; bounds: [number, number, number, number] | null }>();

  constructor(host: MapHost) {
    this.host = host;
    this.colors = new TileColors(host.assets, host.root);
    this.base = document.createElement("canvas"); this.base.width = MAP_W; this.base.height = MAP_H;
    this.marks = document.createElement("canvas"); this.marks.width = MAP_W; this.marks.height = MAP_H;
  }

  /** 打开时回到自己那一层、跟着自己 */
  reset() { this.floor = -1; this.follow = true; this.drag = null; }

  frame(rects: [number, number, number, number][], close: () => void) {
    const ui = this.host.ui, x = X, y = Y, f = this.host.field();
    ui.window(x, y, W, H, "map-wnd");
    rects.push([x, y, x + W * 64, y + H * 48]);
    ui.box(x + 22, y + 10, x + W * 64 - 22, y + H * 48 - 10, "rgba(0,0,0,0.5)", true);
    const self = f?.self ?? null;
    const resolve = () => f && f.floor >= 0 ? (this.floor < 0 || this.floor === f.floor ? f.floorView(f.floor) : f.floorView(this.floor)) : null;
    let view = resolve();
    let current = !!view && !!f && view.floor === f.floor;
    // 标题列
    if (view) {
      const head = `${view.name}　${view.floor}`;
      ui.text(x + 28, y + 16, head, FONT.YELLOW);
      if (current && self) ui.text(x + 28 + measure(head) + 20, y + 16, `东 ${self.x}　南 ${self.y}`, FONT.WHITE);
      const st = this.seenStats(view);
      const info = `已探索 ${st.count} 格　${view.w}×${view.h}`;
      ui.text(x + W * 64 - 28 - measure(info), y + 16, info, FONT.GRAY);
    } else ui.text(x + 28, y + 16, "地图", FONT.YELLOW);
    // 工具列
    let bx = MAP_X;
    const tool = (label: string, id: string, on = false) => {
      const w = measure(label) + 16;
      if (ui.hitBox(bx, y + 38, bx + w, y + 58, id, false)) ui.box(bx, y + 38, bx + w, y + 58, "rgba(255,255,255,0.12)", true);
      ui.box(bx, y + 38, bx + w, y + 58, on ? "#ffd84a" : "rgba(255,255,255,0.3)", false);
      ui.text(bx + 8, y + 41, label, on ? FONT.YELLOW : FONT.WHITE);
      const hit = ui.clicked(id);
      bx += w + 6;
      return hit;
    };
    if (tool("－", "map-out")) this.zoom = Math.max(0, this.zoom - 1);
    ui.text(bx, y + 41, `${ZOOMS[this.zoom]}px`, FONT.GRAY); bx += measure(`${ZOOMS[this.zoom]}px`) + 6;
    if (tool("＋", "map-in")) this.zoom = Math.min(ZOOMS.length - 1, this.zoom + 1);
    if (tool("全图", "map-fit") && view) this.fit(view);
    if (tool("回到自己", "map-me", this.follow && current)) { this.floor = -1; this.follow = true; }
    if (ui.hovered("map-fit")) ui.bottomLine = "按看过的范围缩放到整张图。";
    if (ui.hovered("map-me")) ui.bottomLine = "回到自己所在的区域，跟着自己移动；拖动地图就不跟了。";
    // 左边：去过的区域
    ui.text(x + 28, y + 40, "去过的区域", FONT.AQUA);
    ui.box(x + 26, y + 60, x + 26 + LIST_W, y + 61, "rgba(255,255,255,0.25)", true);
    const floors = f ? f.exploredFloors() : [];
    const maxTop = Math.max(0, floors.length - LIST_ROWS);
    this.listTop = Math.min(this.listTop, maxTop);
    floors.slice(this.listTop, this.listTop + LIST_ROWS).forEach((fl, i) => {
      const ry = y + 68 + i * LIST_ROW_H, id = `map-fl-${fl.floor}`;
      const sel = view && fl.floor === view.floor;
      if (ui.hitBox(x + 26, ry - 2, x + 26 + LIST_W, ry + LIST_ROW_H - 3, id, false)) ui.box(x + 26, ry - 2, x + 26 + LIST_W, ry + LIST_ROW_H - 3, "rgba(255,255,255,0.1)", true);
      if (sel) ui.box(x + 26, ry - 2, x + 26 + LIST_W, ry + LIST_ROW_H - 3, "rgba(255,216,74,0.18)", true);
      const mine = f && fl.floor === f.floor;
      ui.text(x + 32, ry, (mine ? "● " : "") + clip(fl.name || String(fl.floor), LIST_W - 20), sel ? FONT.YELLOW : mine ? FONT.GREEN : FONT.WHITE);
      if (ui.clicked(id)) { this.floor = fl.floor; this.follow = mine; if (!mine) this.fitLater = true; }
    });
    if (this.listTop > 0 && ui.text(x + 30, y + 68 + LIST_ROWS * LIST_ROW_H, "▲", FONT.AQUA, "map-lu")) this.listTop--;
    if (this.listTop < maxTop && ui.text(x + 26 + LIST_W - 30, y + 68 + LIST_ROWS * LIST_ROW_H, "▼", FONT.AQUA, "map-ld")) this.listTop++;
    // 地图（列表这一帧点了别层就换成那一层，"全图"才算对层）
    if (view && this.floor >= 0 && this.floor !== view.floor) { view = resolve(); current = !!view && !!f && view.floor === f.floor; }
    ui.box(MAP_X - 1, MAP_Y - 1, MAP_X + MAP_W + 1, MAP_Y + MAP_H + 1, "rgba(255,255,255,0.3)", false);
    if (!view) {
      ui.box(MAP_X, MAP_Y, MAP_X + MAP_W, MAP_Y + MAP_H, BG, true);
      ui.text(MAP_X + 20, MAP_Y + 20, "还没有这一层的资料。", FONT.GRAY);
    } else {
      if (this.fitLater) { this.fit(view); this.fitLater = false; }
      if (current && this.follow && self) { this.cx = self.x; this.cy = self.y; }
      this.pan(ui, view);
      this.render(view, f!);
      ui.pic(this.base, MAP_X, MAP_Y);
      this.drawMarks(view, f!, current);
      ui.pic(this.marks, MAP_X, MAP_Y);
      ui.hitBox(MAP_X, MAP_Y, MAP_X + MAP_W, MAP_Y + MAP_H, "map-area", false);
    }
    ui.text(x + 28, y + H * 48 - 36, "按住拖动平移　亮青色：传送点　白点：玩家　黄点：NPC　方框：现在画面看到的范围", FONT.GRAY);
    if (ui.text(x + W * 64 - 28 - measure("关　闭"), y + H * 48 - 36, "关　闭", FONT.AQUA, "map-close")) close();
  }

  private fitLater = false;

  /** 按住拖动：像素位移换成格子位移（px = (X+Y)s、py = (Y−X)·0.75s 的反算） */
  private pan(ui: Ui, view: FloorView) {
    const m = ui.mouse;
    const inside = m.x >= MAP_X && m.x < MAP_X + MAP_W && m.y >= MAP_Y && m.y < MAP_Y + MAP_H;
    if (m.down && (this.drag || inside)) {
      if (this.drag) {
        const dpx = m.x - this.drag.x, dpy = m.y - this.drag.y;
        if (dpx || dpy) {
          const s = ZOOMS[this.zoom], hy = s * 0.75;
          const a = dpx / s, b = dpy / hy;
          this.cx -= (a - b) / 2; this.cy -= (a + b) / 2;
          this.cx = Math.max(0, Math.min(view.w, this.cx)); this.cy = Math.max(0, Math.min(view.h, this.cy));
          this.follow = false; this.dragged = true;
        }
      }
      this.drag = { x: m.x, y: m.y };
    } else { this.drag = null; this.dragged = false; }
  }

  /** 全图：按看过的范围挑最大的能放下的缩放，居中 */
  private fit(view: FloorView) {
    const st = this.seenStats(view);
    if (!st.bounds) return;
    const [x1, y1, x2, y2] = st.bounds, bw = x2 - x1 + 1, bh = y2 - y1 + 1;
    let z = 0;
    for (let i = 0; i < ZOOMS.length; i++) {
      const s = ZOOMS[i];
      if ((bw + bh + 2) * s <= MAP_W && (bw + bh + 2) * s * 0.75 <= MAP_H) z = i;
    }
    this.zoom = z;
    this.cx = (x1 + x2) / 2; this.cy = (y1 + y2) / 2;
    this.follow = false;
  }

  private seenStats(view: FloorView) {
    const c = this.seenCache.get(view.floor);
    if (c && c.version === view.seenVersion) return c;
    let n = 0, x1 = Infinity, y1 = Infinity, x2 = -1, y2 = -1;
    for (let k = 0; k < view.seen.length; k++) {
      if (!view.seen[k]) continue;
      n++;
      const x = k % view.w, y = Math.floor(k / view.w);
      if (x < x1) x1 = x; if (x > x2) x2 = x; if (y < y1) y1 = y; if (y > y2) y2 = y;
    }
    const st = { version: view.seenVersion, count: n, bounds: n ? [x1, y1, x2, y2] as [number, number, number, number] : null };
    this.seenCache.set(view.floor, st);
    return st;
  }

  /** 格子中心在地图画布上的位置 */
  private toPx(view: FloorView, x: number, y: number): [number, number] {
    const s = ZOOMS[this.zoom], hy = s * 0.75;
    return [MAP_W / 2 + ((x - this.cx) + (y - this.cy)) * s, MAP_H / 2 + ((y - this.cy) - (x - this.cx)) * hy];
  }

  /** 底图：看过的格子（地块 + 物件）与传送点；视野、资料、颜色没变就不重画 */
  private render(view: FloorView, f: Field) {
    const s = ZOOMS[this.zoom], hy = s * 0.75;
    const key = `${view.floor},${Math.round(this.cx * 4)},${Math.round(this.cy * 4)},${this.zoom},${view.version},${view.seenVersion},${this.colors.version}`;
    if (key === this.drawn) return;
    this.drawn = key;
    const ctx = this.base.getContext("2d")!;
    ctx.fillStyle = BG;
    ctx.fillRect(0, 0, MAP_W, MAP_H);
    // 范围：画布边缘往外多一格
    const R = MAP_W / 2 / s + MAP_H / 2 / hy + 3;
    const x1 = Math.max(0, Math.floor(this.cx - R)), x2 = Math.min(view.w - 1, Math.ceil(this.cx + R));
    const y1 = Math.max(0, Math.floor(this.cy - R)), y2 = Math.min(view.h - 1, Math.ceil(this.cy + R));
    f.requestFloor(view, x1, y1, x2 + 1, y2 + 1);
    const inView = (px: number, py: number) => px > -s && px < MAP_W + s && py > -hy && py < MAP_H + hy;
    const cell = (px: number, py: number, c: [number, number, number]) => {
      ctx.fillStyle = `rgb(${c[0]},${c[1]},${c[2]})`;
      if (s <= 3) { ctx.fillRect(px - s, py - 1, 2 * s, 2); return; } // 很小时用小横条，快
      ctx.beginPath();
      ctx.moveTo(px - s - 0.5, py); ctx.lineTo(px, py - hy - 0.5); ctx.lineTo(px + s + 0.5, py); ctx.lineTo(px, py + hy + 0.5);
      ctx.closePath(); ctx.fill();
    };
    const warps: [number, number][] = [];
    const objs: [number, number, [number, number, number], number][] = []; // px, py, 颜色, 物件图号
    for (let y = y1; y <= y2; y++) {
      for (let x = x1; x <= x2; x++) {
        const k = y * view.w + x;
        if (!view.seen[k] || !view.walk[k]) continue;
        const [px, py] = this.toPx(view, x, y);
        if (!inView(px, py)) continue;
        const c = this.colors.color(view.tiles[k]);
        if (c) cell(px, py, c);
        const o = view.objs[k];
        if (o > 99) {
          const oc = this.colors.color(o);
          if (oc) objs.push([x, y, oc, o]);
        }
        if (view.walk[k] === 3) warps.push([px, py]);
      }
    }
    for (const [x, y, c, o] of objs) { // 物件照占地往 +x、−y 铺开，压在地块上（createAutoMap 的第二趟）
      const [tx, ty] = this.host.assets.footprint(o);
      const dark: [number, number, number] = [Math.round(c[0] * 0.85), Math.round(c[1] * 0.85), Math.round(c[2] * 0.85)];
      for (let k = 0; k < ty; k++) for (let l = 0; l < tx; l++) {
        const [px, py] = this.toPx(view, x + l, y - k);
        if (inView(px, py)) cell(px, py, k === 0 && l === 0 ? c : dark);
      }
    }
    if (warps.length) { // 传送点：亮青色加一圈光
      ctx.save();
      ctx.shadowColor = "#4cf0ff"; ctx.shadowBlur = Math.max(4, s);
      ctx.fillStyle = "#7af6ff";
      for (const [px, py] of warps) {
        ctx.beginPath();
        ctx.moveTo(px - s, py); ctx.lineTo(px, py - hy); ctx.lineTo(px + s, py); ctx.lineTo(px, py + hy); ctx.closePath(); ctx.fill();
      }
      ctx.restore();
    }
    // 四周暗角
    const g = ctx.createRadialGradient(MAP_W / 2, MAP_H / 2, Math.min(MAP_W, MAP_H) * 0.45, MAP_W / 2, MAP_H / 2, Math.max(MAP_W, MAP_H) * 0.75);
    g.addColorStop(0, "rgba(0,0,0,0)"); g.addColorStop(1, "rgba(0,0,0,0.55)");
    ctx.fillStyle = g;
    ctx.fillRect(0, 0, MAP_W, MAP_H);
  }

  /** 每帧的记号：自己（呼吸的金点 + 朝向）、玩家、NPC、画面范围 */
  private drawMarks(view: FloorView, f: Field, current: boolean) {
    const ctx = this.marks.getContext("2d")!;
    ctx.clearRect(0, 0, MAP_W, MAP_H);
    if (!current) return;
    const s = ZOOMS[this.zoom], hy = s * 0.75, now = performance.now();
    for (const a of f.actors.values()) {
      const [px, py] = this.toPx(view, a.x, a.y);
      if (px < 0 || py < 0 || px > MAP_W || py > MAP_H) continue;
      ctx.fillStyle = a.kind === "player" ? "#ffffff" : "#ffd84a";
      ctx.beginPath(); ctx.arc(px, py, a.kind === "player" ? 3 : 2, 0, Math.PI * 2); ctx.fill();
    }
    const me = f.self;
    if (!me) return;
    const [mx, my] = this.toPx(view, me.x, me.y);
    // 画面看到的范围：X+Y、Y−X 各 ±12.5 格 → 地图上是个长方形
    ctx.strokeStyle = "rgba(255,255,255,0.55)"; ctx.lineWidth = 1;
    ctx.strokeRect(mx - 12.5 * s, my - 12.5 * hy, 25 * s, 25 * hy);
    const pulse = 1 + 0.25 * Math.sin(now / 250);
    ctx.save();
    ctx.shadowColor = "#ffcc33"; ctx.shadowBlur = 10;
    ctx.fillStyle = "#ffcc33";
    ctx.beginPath(); ctx.arc(mx, my, 4 * pulse, 0, Math.PI * 2); ctx.fill();
    ctx.restore();
    ctx.strokeStyle = "#1a1200"; ctx.lineWidth = 1.5;
    ctx.beginPath(); ctx.arc(mx, my, 4 * pulse, 0, Math.PI * 2); ctx.stroke();
    // 朝向：MOVE 的位移换到地图座标
    const d = [[-1, 1], [-1, 0], [-1, -1], [0, -1], [1, -1], [1, 0], [1, 1], [0, 1]][me.dir] ?? [0, 1];
    const vx = (d[0] + d[1]) * s, vy = (d[1] - d[0]) * hy, len = Math.hypot(vx, vy) || 1;
    ctx.strokeStyle = "#fff2b0"; ctx.lineWidth = 2;
    ctx.beginPath(); ctx.moveTo(mx, my); ctx.lineTo(mx + vx / len * 11, my + vy / len * 11); ctx.stroke();
  }
}

function clip(text: string, maxW: number): string {
  if (measure(text) <= maxW) return text;
  let out = text;
  while (out.length > 1 && measure(out + "…") > maxW) out = out.slice(0, -1);
  return out + "…";
}
