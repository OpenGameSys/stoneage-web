// 自动地图（原客户端 map.cpp createAutoMap / directdraw.cpp DrawAutoMapping）：以自己为中心 54×54 格，
// 每格画成 4 个像素的小点（斜 45° 排列：往 +y 一格画面右下移 (2, 1)、往 +x 一格右上移 (2, −1)），颜色是那张图的平均色
// （getAutoMapColor，用 0 号调色板）。先画地块，再画物件（物件按占地格数往 +x、−y 铺开）。只画看过的格子。

import type { Assets } from "./assets.ts";
import type { Field } from "./field.ts";

export const AUTO_MAP_W = 54, AUTO_MAP_H = 54;
// 画布原点对应窗口里的 (18 − 1, 118 − 55)：第 (i, j) 格的中心像素在 (1 + 2(i + j), 55 + i − j)
export const AUTO_MAP_CANVAS_W = 2 * (AUTO_MAP_W + AUTO_MAP_H - 2) + 3, AUTO_MAP_CANVAS_H = AUTO_MAP_W + AUTO_MAP_H;
const ORIGIN_Y = AUTO_MAP_W + 1;

type Rgb = [number, number, number] | null;

export class AutoMap {
  readonly canvas: HTMLCanvasElement;
  private ctx: CanvasRenderingContext2D;
  private img: ImageData;
  private colors = new Map<number, Rgb>();           // 地图图号 → 平均色（null 为全透明，不画）
  private queue = new Map<string, Set<number>>();   // 待算的图，按图集页分组
  private busy = false;
  private palette: ImageData | null = null;
  private drawn = "";
  private colorVersion = 0;
  private readonly assets: Assets;
  private readonly root: string;

  constructor(assets: Assets, root: string) {
    this.assets = assets;
    this.root = root;
    this.canvas = document.createElement("canvas");
    this.canvas.width = AUTO_MAP_CANVAS_W;
    this.canvas.height = AUTO_MAP_CANVAS_H;
    this.ctx = this.canvas.getContext("2d")!;
    this.img = this.ctx.createImageData(AUTO_MAP_CANVAS_W, AUTO_MAP_CANVAS_H);
  }

  /** 重画（位置、地图资料或颜色有变才真的画）；回传画好的画布 */
  render(f: Field, gx: number, gy: number): HTMLCanvasElement {
    const key = `${f.floor},${gx},${gy},${f.mapVersion},${this.colorVersion}`;
    if (key === this.drawn) return this.canvas;
    this.drawn = key;
    const x1 = gx - (AUTO_MAP_W >> 1), y1 = gy - (AUTO_MAP_H >> 1);
    f.requestSeen(x1, y1, x1 + AUTO_MAP_W, y1 + AUTO_MAP_H);
    const buf: (Rgb | undefined)[] = new Array(AUTO_MAP_W * AUTO_MAP_H).fill(undefined);
    const inMap = (x: number, y: number) => x >= 0 && y >= 0 && x < f.width && y < f.height;
    for (let i = 0; i < AUTO_MAP_H; i++) for (let j = 0; j < AUTO_MAP_W; j++) {
      const x = x1 + j, y = y1 + i;
      if (inMap(x, y) && f.seen[y * f.width + x] && f.loadedAt(x, y)) buf[i * AUTO_MAP_W + j] = this.color(f.tileAt(x, y));
    }
    for (let i = 0; i < AUTO_MAP_H; i++) for (let j = 0; j < AUTO_MAP_W; j++) {
      const x = x1 + j, y = y1 + i;
      if (!inMap(x, y) || !f.seen[y * f.width + x] || !f.loadedAt(x, y)) continue;
      const o = f.objAt(x, y);
      const c = o ? this.color(o) : null;
      if (!c) continue;
      const [tx, ty] = this.assets.footprint(o);
      for (let k = 0; k < ty; k++) for (let l = 0; l < tx; l++)
        if (i - k >= 0 && j + l < AUTO_MAP_W) buf[(i - k) * AUTO_MAP_W + j + l] = c;
    }
    const d = this.img.data;
    d.fill(0);
    const put = (px: number, py: number, c: [number, number, number]) => {
      if (px < 0 || py < 0 || px >= AUTO_MAP_CANVAS_W || py >= AUTO_MAP_CANVAS_H) return;
      const o = (py * AUTO_MAP_CANVAS_W + px) * 4;
      d[o] = c[0]; d[o + 1] = c[1]; d[o + 2] = c[2]; d[o + 3] = 255;
    };
    for (let i = 0; i < AUTO_MAP_H; i++) for (let j = 0; j < AUTO_MAP_W; j++) {
      const c = buf[i * AUTO_MAP_W + j];
      if (!c) continue;
      const px = 1 + 2 * (i + j), py = ORIGIN_Y + i - j;
      put(px - 1, py, c); put(px, py, c); put(px + 1, py, c); put(px, py - 1, c);
    }
    this.ctx.putImageData(this.img, 0, 0);
    return this.canvas;
  }

  /** 自己所在格（中心）在画布上的位置：原版每秒在白、黑之间闪一次 */
  static center(): [number, number] {
    const i = AUTO_MAP_H >> 1, j = AUTO_MAP_W >> 1;
    return [1 + 2 * (i + j), ORIGIN_Y + i - j];
  }

  /** 图的平均色：算好了直接给，还没算好就排进队列（回传 null，这次先不画） */
  private color(mapNumber: number): Rgb {
    if (!mapNumber) return null;
    const c = this.colors.get(mapNumber);
    if (c !== undefined) return c;
    const idx = mapNumber >= 1000000 ? mapNumber : this.assets.tile(mapNumber);
    if (idx === undefined) { this.colors.set(mapNumber, null); return null; }
    const e = this.assets.image(idx);
    if (!e) { this.colorVersion++; return null; } // 图号表还在载入：下一帧再试
    if (e.page < 0) { this.colors.set(mapNumber, null); return null; }
    const key = `${e.set}/${e.page}`;
    let q = this.queue.get(key);
    if (!q) this.queue.set(key, (q = new Set()));
    q.add(mapNumber);
    this.colors.set(mapNumber, null);
    this.pump();
    return null;
  }

  /** 一次载入一页图集，把这一页上排队的图都算完就丢掉（整页解码很占记忆体） */
  private async pump() {
    if (this.busy) return;
    this.busy = true;
    try {
      this.palette ??= await this.decode(`${this.root}palettes.png`);
      while (this.queue.size) {
        const [key, nums] = this.queue.entries().next().value!;
        this.queue.delete(key);
        const [set, page] = key.split("/").map(Number);
        const fmt = this.assets.pageFormat;
        const data = await this.decode(`${this.root}pages/${set === 0 ? "i8" : "rgba"}/${String(page).padStart(5, "0")}.${fmt}`);
        for (const n of nums) {
          const idx = n >= 1000000 ? n : this.assets.tile(n)!;
          const e = this.assets.image(idx)!;
          this.colors.set(n, average(data, e.x, e.y, e.w, e.h, set === 0 ? this.palette : null));
        }
        this.colorVersion++;
      }
    } catch (err) {
      console.warn("自动地图取色失败", err);
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
