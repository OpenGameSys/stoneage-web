// 按需加载导出资源（格式见 h5/CLAUDE.md 的"资源格式规格"第 10 节）。
// 取图接口是同步的：数据没到就返回 null 并在后台开始加载，下一帧再画。

import type { Renderer, Texture } from "./renderer.ts";

export interface ImageEntry { set: number; page: number; x: number; y: number; w: number; h: number; ox: number; oy: number; }
export interface AnimAction { dir: number; type: number; ticks: number; freeze?: number; frames: [number, number, number, number][]; }
export interface AnimDef { id: number; actions: AnimAction[]; pages: string[]; }

export const ANIM = { ATTACK: 0, DAMAGE: 1, DEAD: 2, STAND: 3, WALK: 4, GUARD: 10 }; // loadsprbin.h ANIM_LIST
const SETS = ["i8", "rgba"];

export class Assets {
  private manifest: any;
  private chunks = new Map<number, (number[] | 0)[] | null>();
  private pages = new Map<string, Texture | null>();
  private anims = new Map<number, AnimDef | null>();
  private tileIndex = new Map<number, number>(); // 地图图号 → 图像索引
  private tileSize = new Map<number, [number, number]>(); // 地图图号 → 占地格数 [x, y]（adrn atari）
  private root: string;
  private renderer: Renderer;
  pending = 0;

  constructor(root: string, renderer: Renderer) {
    this.root = root;
    this.renderer = renderer;
  }

  async init() {
    this.manifest = await (await fetch(this.root + "manifest.json")).json();
    const t = await (await fetch(this.root + "tiles.json")).json();
    for (const r of t.tiles as number[][]) {
      this.tileIndex.set(r[0], r[1]);
      if (r[2] > 1 || r[3] > 1) this.tileSize.set(r[0], [r[2], r[3]]);
    }
    this.renderer.setPalette(await this.renderer.loadTexture(this.root + "palettes.png", false));
  }

  get paletteCount(): number { return this.manifest.palettes; }
  get pageFormat(): string { return this.manifest.pageFormat ?? "png"; }

  /** 地图图号 → 图像索引 */
  tile(mapNumber: number): number | undefined { return this.tileIndex.get(mapNumber); }

  /** 物件的占地：从锚点向 +x 展开 tx 格、向 −y 展开 ty 格 */
  footprint(mapNumber: number): [number, number] { return this.tileSize.get(mapNumber) ?? [1, 1]; }

  image(id: number): ImageEntry | null {
    const shift = this.manifest.chunkShift, c = id >>> shift;
    const chunk = this.chunks.get(c);
    if (chunk === undefined) {
      this.chunks.set(c, null);
      this.load(fetch(`${this.root}images/${String(c).padStart(4, "0")}.json`).then((r) => r.json()).then((j) => { this.chunks.set(c, j.img); }));
      return null;
    }
    if (chunk === null) return null;
    const e = chunk[id - (c << shift)];
    return e ? { set: e[0], page: e[1], x: e[2], y: e[3], w: e[4], h: e[5], ox: e[6], oy: e[7] } : null;
  }

  page(set: number, page: number): Texture | null {
    const key = `${set}/${page}`;
    const t = this.pages.get(key);
    if (t !== undefined) return t;
    this.pages.set(key, null);
    const url = `${this.root}pages/${SETS[set]}/${String(page).padStart(5, "0")}.${this.manifest.pageFormat ?? "png"}`;
    this.load(this.renderer.loadTexture(url, set === 0).then((tex) => { this.pages.set(key, tex); }));
    return null;
  }

  /**
   * 把一张图解码成 RGBA（不经 WebGL，供 CSS 光标等 DOM 用途）：8 位图用 0 号调色板查色，下标 0 透明。
   * 返回 [画布, 锚点偏移 ox, oy]。
   */
  async imageCanvas(mapNumber: number): Promise<[HTMLCanvasElement, number, number] | null> {
    const idx = mapNumber >= 1000000 ? mapNumber : this.tile(mapNumber);
    if (idx === undefined) return null;
    let e = this.image(idx);
    for (let i = 0; !e && i < 50; i++) { await new Promise((r) => setTimeout(r, 50)); e = this.image(idx); }
    if (!e || e.page < 0) return null;
    const load = async (url: string) => {
      const bmp = await createImageBitmap(await (await fetch(url)).blob(), { colorSpaceConversion: "none", premultiplyAlpha: "none" });
      const c = new OffscreenCanvas(bmp.width, bmp.height), g = c.getContext("2d")!;
      g.drawImage(bmp, 0, 0);
      return g.getImageData(0, 0, bmp.width, bmp.height);
    };
    const page = await load(`${this.root}pages/${SETS[e.set]}/${String(e.page).padStart(5, "0")}.${this.manifest.pageFormat ?? "png"}`);
    const pal = e.set === 0 ? await load(`${this.root}palettes.png`) : null;
    const out = document.createElement("canvas");
    out.width = e.w; out.height = e.h;
    const g = out.getContext("2d")!, img = g.createImageData(e.w, e.h);
    for (let y = 0; y < e.h; y++) for (let x = 0; x < e.w; x++) {
      const s = ((e.y + y) * page.width + e.x + x) * 4, d = (y * e.w + x) * 4;
      if (pal) {
        const k = page.data[s];
        if (k === 0) continue;
        img.data.set(pal.data.subarray(k * 4, k * 4 + 3), d);
        img.data[d + 3] = 255;
      } else img.data.set(page.data.subarray(s, s + 4), d);
    }
    g.putImageData(img, 0, 0);
    return [out, e.ox, e.oy];
  }

  anim(id: number): AnimDef | null {
    const a = this.anims.get(id);
    if (a !== undefined) return a;
    this.anims.set(id, null);
    this.load(fetch(`${this.root}anim/${id}.json`).then((r) => r.json()).then((j: AnimDef) => {
      this.anims.set(id, j);
      for (const p of j.pages) { const [s, n] = p.split("/"); this.page(SETS.indexOf(s), Number(n)); }
    }));
    return null;
  }

  private load(p: Promise<unknown>) {
    this.pending++;
    p.catch((e) => console.warn("资源加载失败", e)).finally(() => { this.pending--; });
  }
}

/** 原客户端的动作条目选择（规格 6.3）：精确匹配 → 同方向站立 → 第 0 条。 */
export function findAction(def: AnimDef, dir: number, type: number): AnimAction | undefined {
  return def.actions.find((a) => a.dir === dir && a.type === type)
    ?? def.actions.find((a) => a.dir === dir && a.type === ANIM.STAND) ?? def.actions[0];
}
