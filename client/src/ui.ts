// 立即模式界面，对应原客户端的 StockDispBuffer / StockFontBuffer / MakeHitBox / HitDispNo：
// 每帧重新登记要画的图和文字；鼠标命中按"上一帧最上层的那个"判定（原版也是用上一帧的 HitDispNo）。
// 坐标为 800×600 逻辑像素。原版 640×480 坐标的界面用 OFFSET_640 平移到画面中央。

import type { Assets } from "./assets.ts";
import type { Quad } from "./renderer.ts";

export const OFFSET_640: [number, number] = [80, 60];

// 原版文字调色（loadrealbin.h FontPal）
export const FONT = {
  WHITE: "#ffffff", AQUA: "#00ffff", PURPLE: "#ff00ff", BLUE: "#4848fa", YELLOW: "#ffea39",
  GREEN: "#a0f43a", RED: "#b53817", GRAY: "#a0a0a4", BLUE2: "#a6caf0", GREEN2: "#c0dcc0",
};
// 原版系统色（directdraw.h SYSTEM_PAL_*，属性条等用）
export const SYS = { GREEN: "#00ff00", GREEN2: "#008000", AQUA: "#00ffff", AQUA2: "#008080", RED: "#ff0000", RED2: "#800000", YELLOW: "#ffff00", YELLOW2: "#808000", BLACK: "#000000" };

export const FONT_SIZE = 14; // FONT_SIZE1

// 原版悬停框的颜色（mouse.cpp BoxColorTbl，每帧换一格、60 帧一轮）：
// 调色板 250（亮绿）45 帧，再 15 / 2 / 15（directdraw.cpp 系统色 #28e128 / #008000）各 5 帧，看起来是一闪一闪的绿框
const BOX_COLOR_TBL: [string, number][] = [["#00ff00", 45], ["#28e128", 5], ["#008000", 5], ["#28e128", 5]];
export function boxColor(now = performance.now()): string {
  let f = Math.floor(now / (1000 / 60)) % 60;
  for (const [c, n] of BOX_COLOR_TBL) { if (f < n) return c; f -= n; }
  return BOX_COLOR_TBL[0][0];
}

export interface Text { x: number; y: number; text: string; color: string; size?: number; align?: CanvasTextAlign; }
interface Hit { id: string; x1: number; y1: number; x2: number; y2: number; }
export interface Box { x1: number; y1: number; x2: number; y2: number; color: string; fill: boolean; }
/** 直接画在 2D 覆盖层上的小画布（自动地图这种逐像素画的东西），在方框与文字之前 */
export interface Pic { canvas: HTMLCanvasElement; x: number; y: number; }

export class Mouse {
  x = -1; y = -1; down = false; clicked = false; rightClicked = false;
  downAt = 0; // 左键按下的时刻（原版 mouseLeftPushTime 用）
  attach(canvas: HTMLCanvasElement) {
    const pos = (e: MouseEvent) => {
      const r = canvas.getBoundingClientRect();
      this.x = (e.clientX - r.left) * (canvas.width / r.width);
      this.y = (e.clientY - r.top) * (canvas.height / r.height);
    };
    canvas.addEventListener("mousemove", pos);
    canvas.addEventListener("mousedown", (e) => {
      pos(e);
      if (e.button === 0) { this.down = true; this.clicked = true; this.downAt = performance.now(); }
      if (e.button === 2) this.rightClicked = true;
    });
    addEventListener("mouseup", () => { this.down = false; });
    canvas.addEventListener("mouseleave", () => { this.x = this.y = -1; });
    canvas.addEventListener("contextmenu", (e) => e.preventDefault());
  }
}

export class Ui {
  quads: Quad[] = [];
  texts: Text[] = [];
  boxes: Box[] = [];
  pics: Pic[] = [];
  private hits: Hit[] = [];
  private hot = "";     // 上一帧鼠标下最上层的元素
  private clickHot = ""; // 本帧要触发点击的元素
  bottomLine = "";      // 原版 ShowBottomLineString：画面左下角的说明文字
  // 这一帧点到了看得见的按钮（图、可点的字、会亮框的热区；窗口底图那种不亮框的不算）：主程序在帧末放点击声（SE 217）
  clickSound = false;
  private interactive = new Set<string>();
  bottomLineY = 580;    // 游戏画面里任务栏常驻在最下方，说明文字要往上挪
  debug = new URLSearchParams(location.search).has("hits"); // ?hits：画出所有热区，调布局用
  readonly assets: Assets;
  readonly mouse: Mouse;

  constructor(assets: Assets, mouse: Mouse) {
    this.assets = assets;
    this.mouse = mouse;
  }

  begin() {
    // 点击按按下那一刻的鼠标位置、对照上一帧的热区判定（鼠标移过去立刻按下，也不能漏掉）
    if (this.mouse.clicked) this.hot = this.hitAt(this.mouse.x, this.mouse.y);
    this.clickHot = this.mouse.clicked ? this.hot : "";
    this.quads = []; this.texts = []; this.boxes = []; this.hits = []; this.pics = [];
    this.bottomLine = "";
    this.clickSound = false;
    this.interactive.clear();
  }

  private hitAt(x: number, y: number): string {
    for (let i = this.hits.length - 1; i >= 0; i--) {
      const h = this.hits[i];
      if (x >= h.x1 && x < h.x2 && y >= h.y1 && y < h.y2) return h.id;
    }
    return "";
  }

  /** 帧末：算出鼠标下最上层的元素，供下一帧判定悬停与点击 */
  end() {
    this.hot = this.hitAt(this.mouse.x, this.mouse.y);
    if (this.bottomLine) this.text(8, this.bottomLineY, this.bottomLine, FONT.WHITE);
    this.bottomLineY = 580;
    if (this.debug) for (const h of this.hits) if (h.id !== "modal-block") this.box(h.x1, h.y1, h.x2, h.y2, "#ff00ff", false);
    this.mouse.clicked = false;
    this.mouse.rightClicked = false;
  }

  hovered(id: string) { return this.hot === id; }
  /** 鼠标是否停在某个界面元素上（此时地图上的点击不生效） */
  overUi() { return this.hot !== ""; }
  clicked(id: string) {
    const c = this.clickHot === id;
    if (c && this.interactive.has(id)) this.clickSound = true;
    return c;
  }

  /**
   * 画一张图：num 默认是地图图号（原版 StockDispBuffer 的 CG 编号，经 realGetNo 换算），≥1000000 为高彩图号；
   * rawIndex 为 true 时 num 直接是图像索引（动画帧）。给了 id 就参与命中，返回是否悬停。
   * 悬停时照原版 HitMouseCursor（hitFlag 2）在图外围 2 像素画闪烁的框；highlight 为 false 相当于 hitFlag 1。
   */
  image(num: number, x: number, y: number, id?: string, rawIndex = false, highlight = true): boolean {
    const idx = rawIndex || num >= 1000000 ? num : this.assets.tile(num);
    if (idx === undefined) return false;
    const e = this.assets.image(idx);
    if (!e || e.page < 0) return false;
    const tex = this.assets.page(e.set, e.page);
    const qx = Math.round(x + e.ox), qy = Math.round(y + e.oy);
    if (tex) this.quads.push({ tex, x: qx, y: qy, u: e.x, v: e.y, w: e.w, h: e.h });
    if (id) {
      this.hits.push({ id, x1: qx, y1: qy, x2: qx + e.w, y2: qy + e.h });
      if (highlight) this.interactive.add(id);
      const on = this.hovered(id);
      if (on && highlight) this.box(qx - 2, qy - 2, qx + e.w + 2, qy + e.h + 2, boxColor(), false);
      return on;
    }
    return false;
  }

  /** 原版的按钮：图本身就是热区，按下时可换一张"按下"图 */
  button(num: number, x: number, y: number, id: string, downNum?: number): boolean {
    const showDown = downNum !== undefined && this.hovered(id) && this.mouse.down;
    this.image(showDown ? downNum : num, x, y, id);
    return this.clicked(id);
  }

  /** MakeHitBox：矩形热区，悬停时画原版的闪烁框 */
  hitBox(x1: number, y1: number, x2: number, y2: number, id: string, highlight = true): boolean {
    this.hits.push({ id, x1, y1, x2, y2 });
    if (highlight) this.interactive.add(id);
    const on = this.hovered(id);
    if (on && highlight) this.box(x1, y1, x2, y2, boxColor(), false);
    return on;
  }

  /** StockFontBuffer：左上角对齐。给了 id 则文字可点击，悬停时照原版（hitFlag 2）加闪烁框 (x-3, y-4)–(x+宽+2, y+字高+4) */
  text(x: number, y: number, text: string, color: string, id?: string, size = FONT_SIZE): boolean {
    if (id) {
      const w = measure(text, size);
      this.hits.push({ id, x1: x - 2, y1: y - 2, x2: x + w + 2, y2: y + size + 2 });
      this.interactive.add(id);
      if (this.hovered(id)) this.box(x - 3, y - 4, x + w + 2, y + size + 4, boxColor(), false);
    }
    this.texts.push({ x, y, text, color, size });
    return id ? this.clicked(id) : false;
  }

  /** 画一个动画角色（原版 pattern()）。帧给出的是图像索引，不是地图图号 */
  actor(a: { frameOf(assets: Assets, now: number, walking: boolean): [number, number, number] | null },
    x: number, y: number, now: number, walking: boolean) {
    const f = a.frameOf(this.assets, now, walking);
    if (f) this.image(f[0], x + f[1], y + f[2], undefined, true);
  }

  textButton(x: number, y: number, text: string, color: string, id: string): boolean {
    return this.text(x, y, text, color, id);
  }

  pic(canvas: HTMLCanvasElement, x: number, y: number) { this.pics.push({ canvas, x, y }); }

  box(x1: number, y1: number, x2: number, y2: number, color: string, fill: boolean) {
    this.boxes.push({ x1, y1, x2, y2, color, fill });
  }

  /** MakeWindowDisp：原版九宫格窗口（CG_WND_G_0 = 26001 起 9 张 64×48），sizeX × sizeY 格 */
  window(x: number, y: number, sizeX: number, sizeY: number, id?: string) {
    const base = 26001;
    for (let j = 0; j < sizeY; j++) for (let i = 0; i < sizeX; i++) {
      const row = j === 0 ? 0 : j === sizeY - 1 ? 6 : 3;
      const col = i === 0 ? 0 : i === sizeX - 1 ? 2 : 1;
      this.image(base + row + col, x + 32 + i * 64, y + 24 + j * 48);
    }
    if (id) this.hits.push({ id, x1: x, y1: y, x2: x + sizeX * 64, y2: y + sizeY * 48 });
  }
}

let measureCtx: CanvasRenderingContext2D | null = null;
export function measure(text: string, size = FONT_SIZE): number {
  measureCtx ??= document.createElement("canvas").getContext("2d");
  measureCtx!.font = `${size}px ${FONT_FAMILY}`;
  return measureCtx!.measureText(text).width;
}

export const FONT_FAMILY = `"SimSun", "NSimSun", "Songti SC", "Microsoft YaHei", sans-serif`;

/**
 * 2D 覆盖层的分辨率跟着它在屏幕上的显示大小（CSS 像素）走，最多 1600×1200：
 * 画面缩小时按实际大小画；放大时也跟着放大，否则 800×600 的字被拉大后，字和 1 像素的黑色阴影糊成重影。
 * 不乘 devicePixelRatio、设上限，是为了每帧清屏重画的开销不至于太大（按 devicePixelRatio 放大过，移动时明显卡顿）。
 * 尺寸用 ResizeObserver 记下，避免每帧读布局。
 */
const overlaySize = new WeakMap<HTMLCanvasElement, [number, number]>();
export function fitOverlay(canvas: HTMLCanvasElement, ctx: CanvasRenderingContext2D) {
  if (!overlaySize.has(canvas)) {
    overlaySize.set(canvas, [800, 600]);
    new ResizeObserver(([e]) => {
      const { width: w, height: h } = e.contentRect;
      overlaySize.set(canvas, [Math.min(1600, Math.round(w)), Math.min(1200, Math.round(h))]);
    }).observe(canvas);
  }
  const [w, h] = overlaySize.get(canvas)!;
  if (w > 0 && h > 0 && (canvas.width !== w || canvas.height !== h)) { canvas.width = w; canvas.height = h; }
  ctx.setTransform(canvas.width / 800, 0, 0, canvas.height / 600, 0, 0);
}

/** 把界面的文字与方框画到 2D 覆盖层（原版文字带 1 像素黑色阴影） */
export function drawOverlay(ctx: CanvasRenderingContext2D, ui: Ui) {
  ctx.imageSmoothingEnabled = false;
  for (const p of ui.pics) ctx.drawImage(p.canvas, p.x, p.y);
  for (const b of ui.boxes) {
    if (b.fill) { ctx.fillStyle = b.color; ctx.fillRect(b.x1, b.y1, b.x2 - b.x1, b.y2 - b.y1); }
    else { ctx.strokeStyle = b.color; ctx.lineWidth = 1; ctx.strokeRect(b.x1 + 0.5, b.y1 + 0.5, b.x2 - b.x1 - 1, b.y2 - b.y1 - 1); }
  }
  ctx.textBaseline = "top";
  for (const t of ui.texts) {
    ctx.font = `${t.size ?? FONT_SIZE}px ${FONT_FAMILY}`;
    ctx.textAlign = t.align ?? "left";
    ctx.fillStyle = "#000";
    ctx.fillText(t.text, t.x + 1, t.y + 1);
    ctx.fillStyle = t.color;
    ctx.fillText(t.text, t.x, t.y);
  }
}

/**
 * 叠在画布上的 HTML 输入框（中文输入法要靠真实的输入框），位置用 800×600 逻辑坐标给出，随画布缩放。
 */
export class InputBox {
  static live = new Set<InputBox>();
  /** 画布大小随窗口变化，每帧重新定位所有输入框 */
  static layoutAll() { for (const b of InputBox.live) b.layout(); }

  readonly el: HTMLInputElement;
  private canvas: HTMLCanvasElement;
  private rect: [number, number, number, number];

  constructor(canvas: HTMLCanvasElement, x: number, y: number, w: number, h: number, opts: { password?: boolean; maxLength?: number } = {}) {
    this.canvas = canvas;
    this.rect = [x, y, w, h];
    const el = document.createElement("input");
    el.type = opts.password ? "password" : "text";
    if (opts.maxLength) el.maxLength = opts.maxLength;
    el.autocomplete = "off";
    el.spellcheck = false;
    Object.assign(el.style, {
      position: "absolute", background: "transparent", border: "0", outline: "none", padding: "0",
      color: FONT.WHITE, fontFamily: FONT_FAMILY, textShadow: "1px 1px 0 #000", caretColor: "#fff",
    });
    canvas.parentElement!.append(el);
    this.el = el;
    this.layout();
    InputBox.live.add(this);
  }

  layout() {
    const r = this.canvas.getBoundingClientRect(), p = this.canvas.parentElement!.getBoundingClientRect();
    const k = r.width / this.canvas.width;
    const [x, y, w, h] = this.rect;
    Object.assign(this.el.style, {
      left: `${r.left - p.left + x * k}px`, top: `${r.top - p.top + y * k}px`,
      width: `${w * k}px`, height: `${h * k}px`, fontSize: `${FONT_SIZE * k}px`,
    });
  }

  get value() { return this.el.value; }
  set value(v: string) { this.el.value = v; }
  focus() { this.el.focus(); }
  remove() { this.el.remove(); InputBox.live.delete(this); }
}

/** 按原版的字节数（中文 2 字节）切行（宠物技能说明每行 24 字节、咒术说明 22 字节） */
export function wrapBytes(text: string, max: number): string[] {
  const out: string[] = [];
  let line = "", n = 0;
  for (const ch of text) {
    const w = ch.charCodeAt(0) < 0x80 ? 1 : 2;
    if (n + w > max) { out.push(line); line = ""; n = 0; }
    line += ch; n += w;
  }
  if (line) out.push(line);
  return out;
}
