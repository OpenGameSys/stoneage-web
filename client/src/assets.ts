// 按需加载导出资源（格式见 h5/CLAUDE.md 的"资源格式规格"第 10 节）。
// 取图接口是同步的：数据没到就返回 null 并在后台开始加载，下一帧再画。

import type { Renderer, Texture } from "./renderer.ts";

export interface ImageEntry { set: number; page: number; x: number; y: number; w: number; h: number; ox: number; oy: number; }
export interface AnimAction { dir: number; type: number; ticks: number; freeze?: number; frames: [number, number, number, number][]; }
export interface AnimDef { id: number; actions: AnimAction[]; pages: string[]; }

export const ANIM = { ATTACK: 0, DAMAGE: 1, DEAD: 2, STAND: 3, WALK: 4, GUARD: 10 }; // loadsprbin.h ANIM_LIST
const SETS = ["i8", "rgba"];

// 显存里的图集页：没用了很久、总量超过 gpuBudget 时先释放（core 是登录前预加载的界面页，不释放）
interface PageSlot { tex: Texture | null; used: number; core: boolean; }

export class Assets {
  private manifest: any;
  private chunks = new Map<number, (number[] | 0)[] | null>();
  private pages = new Map<string, PageSlot>();
  private anims = new Map<number, AnimDef | null>();
  private tileIndex = new Map<number, number>(); // 地图图号 → 图像索引
  private tileSize = new Map<number, [number, number]>(); // 地图图号 → 占地格数 [x, y]（adrn atari）
  private root: string;
  private renderer: Renderer;
  pending = 0;
  // 二进制索引（server/tools/assetindex 产生的 index.bin.gz）：图像清单与动画都在这里，读进来之后不必再请求
  private index: DataView | null = null;
  private imgChunks = new Map<number, [number, number]>(); // 块号 → [偏移, 个数]
  private animAt = new Map<number, number>();              // 动画编号 → 偏移
  private corePages: [number, number][] = [];
  // 显存：每帧 tick() 一次；超过预算就把 120 帧（约 2 秒）没用过的页释放，从最久没用的开始
  private frameNo = 0;
  gpuBytes = 0;
  gpuBudget = 512 * 1024 * 1024;

  constructor(root: string, renderer: Renderer) {
    this.root = root;
    this.renderer = renderer;
  }

  /** 启动时：manifest、地图图号表、调色板、二进制索引（progress 是 0..1 的下载进度） */
  async init(progress: (p: number) => void = () => {}) {
    this.manifest = await (await fetch(this.root + "manifest.json")).json();
    const t = await (await fetch(this.root + "tiles.json")).json();
    for (const r of t.tiles as number[][]) {
      this.tileIndex.set(r[0], r[1]);
      if (r[2] > 1 || r[3] > 1) this.tileSize.set(r[0], [r[2], r[3]]);
    }
    this.renderer.setPalette(await this.renderer.loadTexture(this.root + "palettes.png", false));
    try { await this.loadIndex(progress); } catch (e) { console.warn("没有二进制索引，改用逐个 JSON", e); this.index = null; }
  }

  private async loadIndex(progress: (p: number) => void) {
    const res = await fetch(this.root + "index.bin.gz");
    if (!res.ok || !res.body) throw new Error(`index.bin.gz: ${res.status}`);
    const total = Number(res.headers.get("Content-Length") ?? 0);
    let got = 0;
    const counted = res.body.pipeThrough(new TransformStream<Uint8Array, Uint8Array>({
      transform(chunk, ctl) { got += chunk.byteLength; if (total) progress(got / total); ctl.enqueue(chunk); },
    }));
    const buf = await new Response(counted.pipeThrough(new DecompressionStream("gzip"))).arrayBuffer();
    const v = new DataView(buf);
    if (v.getUint32(0, true) !== 0x58494153) throw new Error("index.bin.gz 格式不对"); // "SAIX"
    let o = 8;
    const chunks = v.getUint32(o, true); o += 4;
    for (let i = 0; i < chunks; i++) {
      const c = v.getUint32(o, true), n = v.getUint32(o + 4, true);
      this.imgChunks.set(c, [o + 8, n]);
      o += 8 + n * 16;
    }
    const anims = v.getUint32(o, true); o += 4;
    for (let i = 0; i < anims; i++) {
      this.animAt.set(v.getUint32(o, true), o);
      const pages = v.getUint16(o + 4, true), acts = v.getUint16(o + 6, true);
      o += 8 + pages * 4;
      for (let k = 0; k < acts; k++) o += 8 + v.getUint16(o + 6, true) * 12;
    }
    const core = v.getUint32(o, true); o += 4;
    for (let i = 0; i < core; i++, o += 4) this.corePages.push([v.getUint8(o), v.getUint16(o + 2, true)]);
    this.index = v;
  }

  /** 登录前把核心页（界面、登录画面、选形象用的角色）传进显存；progress 是 0..1 */
  async preloadCore(progress: (p: number) => void) {
    let done = 0, next = 0;
    const list = this.corePages;
    const worker = async () => {
      while (next < list.length) {
        const [set, page] = list[next++];
        const key = `${set}/${page}`;
        const slot: PageSlot = { tex: null, used: 0, core: true };
        this.pages.set(key, slot);
        try { this.setTexture(slot, await this.renderer.loadTexture(this.pageUrl(set, page), set === 0)); } catch { this.pages.delete(key); }
        progress(++done / list.length);
      }
    };
    await Promise.all(Array.from({ length: 6 }, worker));
  }

  get coreCount() { return this.corePages.length; }
  get paletteCount(): number { return this.manifest.palettes; }
  get pageFormat(): string { return this.manifest.pageFormat ?? "png"; }

  /** 地图图号 → 图像索引 */
  tile(mapNumber: number): number | undefined { return this.tileIndex.get(mapNumber); }

  /** 物件的占地：从锚点向 +x 展开 tx 格、向 −y 展开 ty 格 */
  footprint(mapNumber: number): [number, number] { return this.tileSize.get(mapNumber) ?? [1, 1]; }

  image(id: number): ImageEntry | null {
    const shift = this.manifest.chunkShift, c = id >>> shift;
    if (this.index) {
      const ch = this.imgChunks.get(c), i = id - (c << shift);
      if (!ch || i < 0 || i >= ch[1]) return null;
      const v = this.index, o = ch[0] + i * 16;
      if (v.getUint8(o) === 255) return null;
      return { set: v.getUint8(o), page: v.getInt16(o + 2, true), x: v.getInt16(o + 4, true), y: v.getInt16(o + 6, true),
        w: v.getInt16(o + 8, true), h: v.getInt16(o + 10, true), ox: v.getInt16(o + 12, true), oy: v.getInt16(o + 14, true) };
    }
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

  private pageUrl(set: number, page: number) {
    return `${this.root}pages/${SETS[set]}/${String(page).padStart(5, "0")}.${this.manifest.pageFormat ?? "png"}`;
  }

  private setTexture(slot: PageSlot, tex: Texture) {
    slot.tex = tex;
    this.gpuBytes += tex.w * tex.h * (tex.indexed ? 1 : 4);
  }

  page(set: number, page: number): Texture | null {
    const key = `${set}/${page}`;
    const s = this.pages.get(key);
    if (s) { s.used = this.frameNo; return s.tex; }
    const slot: PageSlot = { tex: null, used: this.frameNo, core: false };
    this.pages.set(key, slot);
    this.load(this.renderer.loadTexture(this.pageUrl(set, page), set === 0).then((tex) => {
      if (this.pages.get(key) === slot) this.setTexture(slot, tex); else this.renderer.deleteTexture(tex);
    }));
    return null;
  }

  /**
   * 每帧一次：超过显存预算时，把不是核心页、120 帧没用过的页从最久没用的开始释放，降到预算的 90%。
   * 释放的页下次要用时再读（浏览器缓存里有，不经服务器）。
   */
  tick() {
    this.frameNo++;
    if (this.gpuBytes <= this.gpuBudget || this.frameNo % 30 !== 0) return;
    const old = [...this.pages.entries()].filter(([, s]) => s.tex && !s.core && this.frameNo - s.used > 120).sort((a, b) => a[1].used - b[1].used);
    for (const [key, s] of old) {
      if (this.gpuBytes <= this.gpuBudget * 0.9) break;
      const t = s.tex!;
      this.gpuBytes -= t.w * t.h * (t.indexed ? 1 : 4);
      this.renderer.deleteTexture(t);
      this.pages.delete(key);
    }
  }

  /** 显存里有几张图集页（?debug 显示用） */
  get pageCount() { let n = 0; for (const s of this.pages.values()) if (s.tex) n++; return n; }

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
    const page = await load(this.pageUrl(e.set, e.page));
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
    if (this.index) {
      const o = this.animAt.get(id);
      const def = o === undefined ? null : this.decodeAnim(o);
      this.anims.set(id, def);
      if (def) for (const p of def.pages) { const [s, n] = p.split("/"); this.page(SETS.indexOf(s), Number(n)); }
      return def;
    }
    this.anims.set(id, null);
    this.load(fetch(`${this.root}anim/${id}.json`).then((r) => r.json()).then((j: AnimDef) => {
      this.anims.set(id, j);
      for (const p of j.pages) { const [s, n] = p.split("/"); this.page(SETS.indexOf(s), Number(n)); }
    }));
    return null;
  }

  /** 从二进制索引解出一个动画（格式见 server/tools/assetindex） */
  private decodeAnim(o: number): AnimDef {
    const v = this.index!, id = v.getUint32(o, true), np = v.getUint16(o + 4, true), na = v.getUint16(o + 6, true);
    o += 8;
    const pages: string[] = [];
    for (let i = 0; i < np; i++, o += 4) pages.push(`${SETS[v.getUint8(o)]}/${v.getUint16(o + 2, true)}`);
    const actions: AnimAction[] = [];
    for (let k = 0; k < na; k++) {
      const dir = v.getUint8(o), type = v.getUint8(o + 1), ticks = v.getUint16(o + 2, true), freeze = v.getUint8(o + 4), nf = v.getUint16(o + 6, true);
      o += 8;
      const frames: [number, number, number, number][] = [];
      for (let f = 0; f < nf; f++, o += 12) frames.push([v.getInt32(o, true), v.getInt16(o + 4, true), v.getInt16(o + 6, true), v.getUint16(o + 8, true)]);
      actions.push(freeze ? { dir, type, ticks, freeze, frames } : { dir, type, ticks, frames });
    }
    return { id, actions, pages };
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
