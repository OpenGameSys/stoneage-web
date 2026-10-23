// 地图场景：地图区块缓存、主角移动预测、其他角色同步、渲染列表。
// 坐标、方向、投影沿用原客户端（map.cpp）：
//   屏幕 x = (gx + gy) × 32，屏幕 y = (gy − gx) × 24
//   方向 0..7 的格子位移 = moveAddTbl；0 朝下，2 朝左，4 朝上，6 朝右

import { album } from "./album.ts";
import { ANIM, findAction, type AnimAction, type Assets } from "./assets.ts";
import { sfx } from "./audio.ts";
import type { Connection } from "./net.ts";
import type { ObjInfo, S2CEnterFloor, S2CMapData, NpcPos } from "./proto.gen.ts";
import type { Quad } from "./renderer.ts";

export const MOVE: [number, number][] = [[-1, 1], [-1, 0], [-1, -1], [0, -1], [1, -1], [1, 0], [1, 1], [0, 1]];
export let STEP_MS = 260;     // 自己每走一格的时长；原版 260 ms（服务端最短 200 ms，留出余量），服务端按 rules.walkSpeed 缩放后在 EnterFloor.step 送来
export function setStepMs(ms: number) { if (ms >= 50 && ms <= 2000) STEP_MS = ms; }
const VIEW = 15;              // 请求地图的半径
const CG_INVISIBLE = 99;      // 小于等于该值的地块 / 物件编号不绘制（map.cpp drawMap：tile > CG_INVISIBLE 才画；含 20..59 的声音记号、60..79 的事件标记）

// 循环播放的动作（character.cpp：挥手、高兴、生气、悲伤、走动、站立、点头）
const LOOP_ACTS = new Set([6, 7, 8, 9, 4, 3, 11]);

/** 一个会走动的角色（主角、其他玩家、NPC）。 */
export class Actor {
  id: number; name: string; sprite: number; kind: string; level: number;
  x: number; y: number; dir: number;
  private fromX: number; private fromY: number; private stepAt = -1e9;
  private frame = 0; private frameAt = 0; private action: unknown = null;
  speech = ""; speechUntil = 0;
  // 头上的记号（CA 21 队长、20 战斗、求救）：battle 是战斗编号 + 1（0 没在战斗）
  leader = false; battle = 0; help = false;
  // 动作（ANIM_* 编号，-1 没有）：挥手、高兴等循环播放，其余播一次停在最后一格；走路就取消。emote 是自己画面上的打呼 / 冷场
  act = -1; emote = 0;
  family = ""; // 家族名（C 字串第 15 段 fmname），画在名字上面
  trade = false; // 交易中（CHAR_ACTTRADE：交易、摆摊的买卖双方）
  vendor = "";  // 摆着的摊位名（CHAR_STREETVENDOR_OPEN，画在头上）

  constructor(o: ObjInfo) {
    this.id = o.id; this.name = o.name; this.sprite = o.sprite; this.kind = o.kind; this.level = o.level;
    this.x = this.fromX = o.x; this.y = this.fromY = o.y; this.dir = o.dir;
    this.leader = !!o.leader; this.battle = o.battle ?? 0; this.help = !!o.help; this.act = o.action ?? -1; this.family = o.family ?? "";
    this.trade = !!o.trade; this.vendor = o.vendor ?? "";
  }

  stepMs = STEP_MS;   // 这一步要走多久：自己走是 STEP_MS；服务端代走（别人、队员、巴士）按两步之间收到的间隔，走得才连贯
  lastMoveAt = 0;     // 上一次收到服务端代走的时刻
  moving(now: number) { return now - this.stepAt < this.stepMs; }

  /** 这一帧变成当前帧时，帧事件 1..9999 是音效（pattern.cpp：play_se(SoundNo, 位置)），Field.build 按画面座标放出来 */
  se = 0;
  private frameSe(act: AnimAction) {
    const ev = act.frames[this.frame]?.[3] ?? 0;
    if (ev > 0 && ev < 10000) this.se = ev;
  }

  /**
   * 走一格。chain：紧接着上一步连续走时，新的一步从上一步结束的时刻算起（不是从这一帧），
   * 否则每格都会多停不到一帧，帧率低时走路一顿一顿的。
   */
  stepTo(x: number, y: number, dir: number, now: number, chain = false, ms = STEP_MS) {
    const end = this.stepAt + this.stepMs;
    const start = chain && now >= end && now - end < ms / 2 ? end : now;
    const [px, py] = this.pos(start);
    this.fromX = px; this.fromY = py;
    this.x = x; this.y = y; this.dir = dir; this.stepAt = start; this.stepMs = ms;
    this.act = -1; this.emote = 0;
  }

  /** 服务端代走的一步：步长按两步之间的间隔（80..600 ms；隔太久就当作重新起步），这样巴士 / 队长走多快就播多快，不会走一步停一步 */
  serverStep(x: number, y: number, dir: number, now: number) {
    const dt = now - this.lastMoveAt;
    this.lastMoveAt = now;
    this.stepTo(x, y, dir, now, true, dt > 0 && dt < 1000 ? Math.max(80, Math.min(600, dt)) : STEP_MS);
  }

  /** 做一个动作（changeCharAct）：重播 */
  setAct(act: number) { this.act = act; this.action = null; }

  warpTo(x: number, y: number, dir: number) {
    this.x = this.fromX = x; this.y = this.fromY = y; this.dir = dir; this.stepAt = -1e9;
  }

  /** 绘制用的插值位置 */
  pos(now: number): [number, number] {
    const t = Math.min(1, (now - this.stepAt) / this.stepMs);
    return [this.fromX + (this.x - this.fromX) * t, this.fromY + (this.y - this.fromY) * t];
  }

  /** 当前动画帧：[图号, 偏移x, 偏移y]；动画数据未到时返回 null */
  frameOf(assets: Assets, now: number, walking: boolean): [number, number, number] | null {
    // 原客户端 pattern.cpp：编号小于 SPRSTART(100000) 的不是动画，而是按地图图号显示的静态图
    if (this.sprite < 100000) {
      const idx = assets.tile(this.sprite);
      return idx === undefined ? null : [idx, 0, 0];
    }
    const def = assets.anim(this.sprite);
    if (!def) return null;
    const want = walking ? ANIM.WALK : this.act >= 0 ? this.act : ANIM.STAND;
    const act = findAction(def, this.dir, want);
    if (!act || act.frames.length === 0) return null;
    if (act !== this.action) { this.action = act; this.frame = 0; this.frameAt = now; this.frameSe(act); }
    const ms = Math.max(1, act.ticks) * (1000 / 60);
    const loop = walking || this.act < 0 || LOOP_ACTS.has(this.act); // ANM_LOOP / ANM_NO_LOOP
    if (now - this.frameAt > ms * 30) this.frameAt = now; // 长时间没画（切后台）时不追帧
    while (now - this.frameAt >= ms) {
      if (!loop && this.frame >= act.frames.length - 1) { this.frameAt = now; break; } // 停在最后一格
      this.frame = (this.frame + 1) % act.frames.length; this.frameAt += ms;
      this.frameSe(act);
    }
    const [img, fx, fy] = act.frames[this.frame];
    return img < 0 ? null : [img, fx, fy];
  }
}

/**
 * 方向：原版 getDirData / Atan(x, y) + 22.5 的语义。Atan 是 atan2 的角度再减 135°，
 * 所以 (dx, dy) 的方向 = ⌊((atan2(dy, dx) − 135° + 22.5°) mod 360°) / 45°⌋。
 */
export function dirTo(dx: number, dy: number): number {
  if (dx === 0 && dy === 0) return 0;
  const deg = (Math.atan2(dy, dx) * 180) / Math.PI - 135 + 22.5;
  return Math.floor((((deg % 360) + 360) % 360) / 45) & 7;
}

/**
 * 原客户端的点击移动路线（map.cpp getRouteMap）：
 *   1. 每一步都重新瞄准目标，只允许在「起点看目标的方向」及其左右 45° 内前进，所以路线接近直线；
 *   2. 斜走被挡时，若只挡了一侧的直角邻格，就先走另一侧的邻格绕过去；两侧都挡则停下；
 *   3. 第 1 段停下后，再沿 x、y 轴各走一段把剩余距离补齐；
 *   4. 最终没到达时返回朝向目标的 lastDir（moveLastDir）。最长 100 步（MOVE_MAX）。
 * hit(x, y) 为 true 表示不可进入（checkHitMap）。
 */
export function routeTo(sx: number, sy: number, mx: number, my: number, hit: (x: number, y: number) => boolean): { route: number[]; lastDir: number } {
  const route: number[] = [];
  let nx = sx, ny = sy;
  if (nx === mx && ny === my) return { route, lastDir: -1 };
  const target = dirTo(mx - nx, my - ny);
  const near = (d: number) => d === target || d === ((target + 1) & 7) || d === ((target - 1) & 7);
  while (nx !== mx || ny !== my) {
    if (route.length >= 100) return { route: [], lastDir: -1 };
    const nowDir = dirTo(mx - nx, my - ny);
    if (!near(nowDir)) break;
    let stop = true, dir = -1, dx = 0, dy = 0;
    for (const off of [0, 1, -1]) {
      const d = (nowDir + off) & 7;
      if (!near(d)) continue;
      [dx, dy] = MOVE[d];
      if (hit(nx + dx, ny + dy)) {
        // 下一格正好是目标但不可进入：斜向就试别的方向，直向则停下（不踏上目标）
        if (nx + dx === mx && ny + dy === my && d % 2 === 1) break;
        continue;
      }
      if (d % 2 === 0) {
        // 斜走（偶数方向）：检查两侧的直角邻格
        const d2 = (d + 1) & 7, d3 = (d - 1) & 7;
        const h2 = hit(nx + MOVE[d2][0], ny + MOVE[d2][1]), h3 = hit(nx + MOVE[d3][0], ny + MOVE[d3][1]);
        if (h2 && h3) break;
        if (h2 || h3) {
          // 只挡一侧：先拐进没挡的邻格，再走一步到原来的斜格（L 形两步）
          const side = h2 ? d3 : d2;
          route.push(side);
          const tx = nx + dx, ty = ny + dy;
          nx += MOVE[side][0]; ny += MOVE[side][1];
          dx = tx - nx; dy = ty - ny;
          dir = dirTo(dx, dy); stop = false;
          break;
        }
      }
      dir = d; stop = false;
      break;
    }
    if (stop) break;
    nx += dx; ny += dy;
    route.push(dir);
  }
  // 沿 x 轴、再沿 y 轴补齐
  const ax = Math.sign(mx - nx);
  if (ax !== 0) while (!hit(nx + ax, ny) && mx !== nx) { nx += ax; route.push(dirTo(ax, 0)); }
  const ay = Math.sign(my - ny);
  if (ay !== 0) while (!hit(nx, ny + ay) && my !== ny) { ny += ay; route.push(dirTo(0, ay)); }
  const lastDir = route.length > 0 && (nx !== mx || ny !== my) ? dirTo(mx - nx, my - ny) : -1;
  return { route, lastDir };
}

interface Sprite { depth: number; q: Quad; gx: number; gy: number; tx: number; ty: number; }

const overlap = (a: Quad, b: Quad) => a.x < b.x + b.w && b.x < a.x + a.w && a.y < b.y + b.h && b.y < a.y + a.h;

/** 头顶文字（名字、聊天气泡），交给 2D 覆盖层绘制 */
export interface Label { x: number; y: number; text: string; color: string; bubble?: boolean; }

/** 一层楼看过的格子（世界地图用）：floor 是楼层，version 是资料到了几批、seenVersion 是看过的格子变了几次 */
export interface FloorView {
  floor: number; name: string; w: number; h: number;
  tiles: Uint16Array; objs: Uint16Array; walk: Uint8Array; seen: Uint8Array;
  version: number; seenVersion: number;
}

interface FloorStore { name: string; tiles: Uint16Array; objs: Uint16Array; walk: Uint8Array; seen: Uint8Array; version: number; seenVersion: number }

export class Field {
  floor = -1; floorName = ""; palette = 0; width = 0; height = 0;
  private tiles = new Uint16Array(0);
  private objs = new Uint16Array(0);
  private walk = new Uint8Array(0);   // 0 未知、1 不可走、2 可走、3 传送点
  // 走过看过的格子（原版 map\<楼层>.dat 的 MAP_SEE_FLAG）：自动地图只画这些格子。按楼层存在 localStorage，重新整理也留着
  seen = new Uint8Array(0);
  private seenDirty = false;
  // 这次上线去过的楼层的地图资料，回到同一层时不必重新请求
  private cache = new Map<number, FloorStore>(); // 去过的楼层的格子资料（本次连线里），世界地图看别层用
  private meta = new Map<number, { name: string; w: number; h: number }>(); // 去过的楼层（名字、大小），存 localStorage sa.floors.<角色>
  private metaLoaded = false;
  seenVersion = 0; // 这一层看过的格子变了几次（世界地图的全图范围据此重算）
  private pending = new Set<string>();

  self: Actor | null = null;
  actors = new Map<number, Actor>();
  wantDir = -1;
  target: [number, number] | null = null;
  hideOthers = false; // 系统菜单「人物屏蔽」：不画其他玩家
  corrections = 0;
  private waitingWarp = false;
  private assets: Assets;
  private net: Connection;

  constructor(assets: Assets, net: Connection) {
    this.assets = assets;
    this.net = net;
    net.on("Self", (m) => { this.self = new Actor({ id: m.id, kind: "player", name: m.name, sprite: m.sprite, x: 0, y: 0, dir: 0 }); });
    net.on("EnterFloor", (m) => this.enterFloor(m));
    net.on("MapData", (m) => this.mapData(m));
    net.on("NpcList", (m) => { this.npcLists.set(m.floor, m.npcs); this.npcPending.delete(m.floor); });
    net.on("PosCorrect", (m) => { this.corrections++; this.self?.warpTo(m.x, m.y, m.dir); this.target = null; this.waitingWarp = false; });
    net.on("ObjAppear", (m) => { for (const o of m.objs) { this.actors.set(o.id, new Actor(o)); album.seen(o.sprite); } }); // 相簿的"看到"（CheckNewPet）
    net.on("ObjDisappear", (m) => { for (const id of m.ids) this.actors.delete(id); });
    net.on("ObjMove", (m) => {
      if (m.id === this.self?.id) this.followSteps.push([m.x, m.y, m.dir, performance.now()]); // 队员跟着队长走（服务端替我们走）
      else this.actors.get(m.id)?.serverStep(m.x, m.y, m.dir, performance.now());
    });
    net.on("ObjAction", (m) => { (this.self?.id === m.id ? this.self : this.actors.get(m.id))?.setAct(m.action); });
    net.on("ObjFamily", (m) => { const a = this.actors.get(m.id); if (a) a.family = m.family; });
    net.on("ObjMark", (m) => {
      const a = this.self?.id === m.id ? this.self : this.actors.get(m.id);
      if (a) { a.leader = m.leader; a.battle = m.battle; a.help = m.help; a.trade = !!m.trade; a.vendor = m.vendor ?? ""; }
    });
    // 服务端送的音效（lssproto_SE：巴士 / 飞机发车、到站）：按那一格相对自己的位置放（play_se 的 x）
    net.on("Sound", (m) => {
      const s = this.self;
      if (s && Math.abs(m.x - s.x) <= 12 && Math.abs(m.y - s.y) <= 12) sfx(m.se, 400 + ((m.x - s.x) + (m.y - s.y)) * 32);
    });
    // 换武器后形象改变（CHAR_getNewImagenumberFromEquip），自己和别人都一样处理
    net.on("ObjSprite", (m) => {
      const a = this.self?.id === m.id ? this.self : this.actors.get(m.id);
      if (a) a.sprite = m.sprite;
    });
    net.on("ObjTurn", (m) => { const a = this.actors.get(m.id); if (a) a.dir = m.dir; });
    net.on("ChatMsg", (m) => {
      const a = m.fromId === this.self?.id ? this.self : this.actors.get(m.fromId);
      if (a && m.channel === "near") { a.speech = m.text; a.speechUntil = performance.now() + 5000; }
    });
  }

  private enterFloor(m: S2CEnterFloor) {
    this.followSteps = [];
    this.forgetNpcs(m.floor); // 这一层的 NPC 清单重新要（巴士会动、按时间出现的人会变）
    this.saveSeen();
    if (this.floor >= 0) this.cache.set(this.floor, { name: this.floorName, tiles: this.tiles, objs: this.objs, walk: this.walk, seen: this.seen, version: this.mapVersion, seenVersion: this.seenVersion });
    this.floor = m.floor; this.floorName = m.name; this.palette = m.palette;
    this.width = m.width; this.height = m.height;
    if (m.step > 0) setStepMs(m.step);
    this.loadMeta();
    this.meta.set(m.floor, { name: m.name, w: m.width, h: m.height });
    this.saveMeta();
    const n = m.width * m.height, c = this.cache.get(m.floor);
    if (c && c.tiles.length === n) {
      ({ tiles: this.tiles, objs: this.objs, walk: this.walk, seen: this.seen } = c);
      this.seenVersion = c.seenVersion;
    } else {
      this.tiles = new Uint16Array(n);
      this.objs = new Uint16Array(n);
      this.walk = new Uint8Array(n);
      this.seen = this.loadSeen(n, m.floor);
      this.seenVersion++;
    }
    this.pending.clear();
    this.actors.clear();
    this.target = null;
    this.waitingWarp = false;
    this.self?.warpTo(m.x, m.y, m.dir);
    this.requestView();
  }

  private mapData(m: S2CMapData) {
    this.pending.delete(`${m.floor},${m.x1},${m.y1},${m.x2},${m.y2}`);
    const w = m.x2 - m.x1;
    if (m.floor !== this.floor) { // 世界地图看别层时补要的资料
      const c = this.store(m.floor);
      if (!c) return;
      const fw = this.meta.get(m.floor)!.w, fh = this.meta.get(m.floor)!.h;
      for (let i = 0; i < m.tiles.length; i++) {
        const x = m.x1 + (i % w), y = m.y1 + Math.floor(i / w);
        if (x < 0 || y < 0 || x >= fw || y >= fh) continue;
        const k = y * fw + x;
        c.tiles[k] = m.tiles[i]; c.objs[k] = m.objs[i]; c.walk[k] = m.walk.charCodeAt(i) - 48 + 1;
      }
      c.version++;
      return;
    }
    for (let i = 0; i < m.tiles.length; i++) {
      const x = m.x1 + (i % w), y = m.y1 + Math.floor(i / w);
      if (x < 0 || y < 0 || x >= this.width || y >= this.height) continue;
      const k = y * this.width + x;
      this.tiles[k] = m.tiles[i];
      this.objs[k] = m.objs[i];
      this.walk[k] = m.walk.charCodeAt(i) - 48 + 1; // '0'→1 不可走，'1'→2 可走，'2'→3 传送点
      if (!this.seen[k]) { this.seen[k] = 1; this.seenDirty = true; this.seenVersion++; }
    }
    this.mapVersion++;
  }

  /** 去过的楼层（名字、大小）：读 / 存 localStorage sa.floors.<角色> */
  private metaKey() { return `sa.floors.${this.self?.id ?? 0}`; }
  private loadMeta() {
    if (this.metaLoaded) return;
    this.metaLoaded = true;
    try {
      const v = JSON.parse(localStorage.getItem(this.metaKey()) ?? "[]");
      if (Array.isArray(v)) for (const r of v) if (Array.isArray(r) && r.length >= 4) this.meta.set(Number(r[0]), { name: String(r[1]), w: Number(r[2]), h: Number(r[3]) });
    } catch { /* 读不到就从空的开始 */ }
  }
  private saveMeta() {
    try { localStorage.setItem(this.metaKey(), JSON.stringify([...this.meta].map(([f, m]) => [f, m.name, m.w, m.h]))); } catch { /* */ }
  }

  /** 去过的楼层，自己所在的排第一，其余按楼层号 */
  exploredFloors(): { floor: number; name: string }[] {
    this.loadMeta();
    const out = [...this.meta].map(([floor, m]) => ({ floor, name: m.name })).filter((x) => x.floor !== this.floor).sort((a, b) => a.floor - b.floor);
    if (this.floor >= 0) out.unshift({ floor: this.floor, name: this.floorName });
    return out;
  }

  /** 别层的格子资料（本次连线里没去过就按记下的大小建空的，看过的格子从 localStorage 读） */
  private store(floor: number): FloorStore | null {
    let c = this.cache.get(floor);
    if (c) return c;
    const m = this.meta.get(floor);
    if (!m || m.w <= 0 || m.h <= 0 || m.w * m.h > 4_000_000) return null;
    const n = m.w * m.h;
    c = { name: m.name, tiles: new Uint16Array(n), objs: new Uint16Array(n), walk: new Uint8Array(n), seen: this.loadSeen(n, floor), version: 0, seenVersion: 1 };
    this.cache.set(floor, c);
    return c;
  }

  /** 世界地图用：某一层看过的格子 */
  floorView(floor: number): FloorView | null {
    if (floor === this.floor) {
      return { floor, name: this.floorName, w: this.width, h: this.height, tiles: this.tiles, objs: this.objs, walk: this.walk, seen: this.seen, version: this.mapVersion, seenVersion: this.seenVersion };
    }
    this.loadMeta();
    const m = this.meta.get(floor), c = this.store(floor);
    if (!m || !c) return null;
    return { floor, name: m.name, w: m.w, h: m.h, tiles: c.tiles, objs: c.objs, walk: c.walk, seen: c.seen, version: c.version, seenVersion: c.seenVersion };
  }

  /** 世界地图要画的范围里，看过但还没有资料的格子（重新整理之后、或别层的）：按 64×64 一块向服务端补要 */
  requestFloor(v: FloorView, x1: number, y1: number, x2: number, y2: number) {
    x1 = Math.max(0, x1); y1 = Math.max(0, y1); x2 = Math.min(v.w, x2); y2 = Math.min(v.h, y2);
    for (let by = y1; by < y2; by += 64) for (let bx = x1; bx < x2; bx += 64) {
      const ex = Math.min(x2, bx + 64), ey = Math.min(y2, by + 64);
      let need = false;
      for (let y = by; y < ey && !need; y++) for (let x = bx; x < ex; x++) {
        const k = y * v.w + x;
        if (v.seen[k] && !v.walk[k]) { need = true; break; }
      }
      if (!need) continue;
      const key = `${v.floor},${bx},${by},${ex},${ey}`;
      if (this.pending.has(key)) continue;
      this.pending.add(key);
      this.net.send({ t: "MapReq", floor: v.floor, x1: bx, y1: by, x2: ex, y2: ey });
    }
  }

  /** 世界地图用：一层楼全部 NPC 的位置（NpcListReq → NpcList），没有就先向服务端要、这次回 null */
  private npcLists = new Map<number, NpcPos[]>();
  private npcPending = new Set<number>();
  npcList(floor: number): NpcPos[] | null {
    const l = this.npcLists.get(floor);
    if (l) return l;
    if (!this.npcPending.has(floor)) { this.npcPending.add(floor); this.net.send({ t: "NpcListReq", floor }); }
    return null;
  }
  /** 换了楼层就把那一层的清单作废（巴士会动、按时间出现的人会变） */
  forgetNpcs(floor: number) { this.npcLists.delete(floor); }

  /** 地图资料每到一批加一，自动地图据此重画 */
  mapVersion = 0;

  tileAt(x: number, y: number) { return this.tiles[y * this.width + x]; }
  objAt(x: number, y: number) { return this.objs[y * this.width + x]; }
  loadedAt(x: number, y: number) { return this.walk[y * this.width + x] !== 0; }

  private seenKey(floor = this.floor) { return `sa.seen.${this.self?.id ?? 0}.${floor}`; }

  /** localStorage 里存成位元组再转 base64；读不到（私密浏览、被清掉）就从空的开始 */
  private loadSeen(n: number, floor = this.floor): Uint8Array {
    const out = new Uint8Array(n);
    try {
      const b = atob(localStorage.getItem(this.seenKey(floor)) ?? "");
      for (let i = 0; i < n; i++) if (b.charCodeAt(i >> 3) & (1 << (i & 7))) out[i] = 1;
    } catch {}
    return out;
  }

  /** 走动时由画面定期呼叫（不必每格都写） */
  saveSeen() {
    if (!this.seenDirty || this.floor < 0) return;
    this.seenDirty = false;
    const bytes = new Uint8Array((this.seen.length + 7) >> 3);
    for (let i = 0; i < this.seen.length; i++) if (this.seen[i]) bytes[i >> 3] |= 1 << (i & 7);
    let s = "";
    for (let i = 0; i < bytes.length; i += 0x8000) s += String.fromCharCode(...bytes.subarray(i, i + 0x8000));
    try { localStorage.setItem(this.seenKey(), btoa(s)); } catch {}
  }

  /** 视野内未收到的格子：按行求未知区间，连续且区间相同的行合并成一块请求。 */
  private requestView() {
    const s = this.self;
    if (!s || this.floor < 0) return;
    let run: number[] | null = null; // [x1, x2, 起始行]
    const flush = (yEnd: number) => {
      if (!run) return;
      const key = `${this.floor},${run[0]},${run[2]},${run[1]},${yEnd}`;
      if (!this.pending.has(key)) {
        this.pending.add(key);
        this.net.send({ t: "MapReq", floor: this.floor, x1: run[0], y1: run[2], x2: run[1], y2: yEnd });
      }
      run = null;
    };
    const yMax = Math.min(this.height - 1, s.y + VIEW);
    for (let y = Math.max(0, s.y - VIEW); y <= yMax; y++) {
      let x1 = Infinity, x2 = -Infinity;
      for (let x = Math.max(0, s.x - VIEW); x <= Math.min(this.width - 1, s.x + VIEW); x++)
        if (!this.walk[y * this.width + x]) { x1 = Math.min(x1, x); x2 = x + 1; }
      if (x1 === Infinity) { flush(y); continue; }
      if (run && (run[0] !== x1 || run[1] !== x2)) flush(y);
      if (!run) run = [x1, x2, y];
    }
    flush(yMax + 1);
  }

  /** 由服务端算好的通行标记（MapData.walk），与服务端判定完全一致 */
  canEnter(x: number, y: number): boolean {
    if (x < 0 || y < 0 || x >= this.width || y >= this.height) return false;
    return this.walk[y * this.width + x] >= 2;
  }

  /** 可以走、而且不是传送点（自动遇敌来回走的格子） */
  plainWalkable(x: number, y: number): boolean {
    return this.canEnter(x, y) && this.walk[y * this.width + x] === 2;
  }

  update(now: number) {

    const s = this.self;
    if (!s || this.floor < 0 || this.waitingWarp || s.moving(now)) { this.requestView(); return; }
    if (this.locked) {
      this.target = null;
      if (this.wantDir >= 0 && s.dir !== this.wantDir) { s.dir = this.wantDir; this.net.send({ t: "Turn", dir: this.wantDir }); }
      this.requestView(); return;
    }
    if (this.follower) {
      // 队员（partyMoveProc）：自己不能走，只按服务端的步子走；方向键只转身
      // 跟不上时（分页在背景、或队长是巴士）直接跳到最新的那一步，不让队伍越拉越长
      if (this.followSteps.length > 4) { const last = this.followSteps[this.followSteps.length - 1]; this.followSteps = []; s.warpTo(last[0], last[1], last[2]); this.requestView(); return; }
      const step = this.followSteps.shift();
      if (step) { // 步长按服务端两步之间的间隔（队长走多快就跟多快），排队的步子多了就再快一点追上
        const dt = step[3] - this.lastFollowAt; this.lastFollowAt = step[3];
        let ms = dt > 0 && dt < 1000 ? Math.max(80, Math.min(600, dt)) : STEP_MS;
        if (this.followSteps.length >= 2) ms = Math.max(80, ms * 0.6);
        s.stepTo(step[0], step[1], step[2], now, true, ms);
      }
      else if (this.wantDir >= 0 && s.dir !== this.wantDir) { s.dir = this.wantDir; this.net.send({ t: "Turn", dir: s.dir }); }
      this.target = null;
      this.requestView();
      return;
    }
    let d = this.wantDir;
    if (d >= 0) this.path = [];
    if (d < 0 && this.target) {
      if (this.pathGoal !== `${this.target}`) {
        const [tx, ty] = this.target;
        const r = routeTo(s.x, s.y, tx, ty, (x, y) => !this.canEnter(x, y));
        // 优先用原版路线（接近直线）；原版到不了目标时（遇障碍停下），若 BFS 能绕到就改走 BFS（H5 新增），
        // 都到不了则按原版走到停下处再转向目标
        const reaches = (route: number[]) => {
          const [ex, ey] = route.reduce(([x, y], d) => [x + MOVE[d][0], y + MOVE[d][1]], [s.x, s.y]);
          return ex === tx && ey === ty;
        };
        const bfs = reaches(r.route) ? [] : this.findPath(s.x, s.y, tx, ty);
        const useBfs = bfs.length > 0 && (reaches(bfs) || r.route.length === 0);
        this.path = useBfs ? bfs : r.route;
        this.finalDir = useBfs ? -1 : r.lastDir;
        this.pathGoal = `${this.target}`;
      }
      d = this.path.shift() ?? -1;
      if (d < 0) {
        // 走完了但没到达目标：原版会转向目标方向（moveLastDir）
        if (this.finalDir >= 0 && s.dir !== this.finalDir) { s.dir = this.finalDir; this.net.send({ t: "Turn", dir: s.dir }); }
        this.target = null;
        this.pathGoal = "";
      }
    }
    if (d >= 0) {
      const nx = s.x + MOVE[d][0], ny = s.y + MOVE[d][1];
      if (!this.canEnter(nx, ny)) {
        if (s.dir !== d) { s.dir = d; this.net.send({ t: "Turn", dir: d }); }
        this.target = null;
      } else {
        // 先走后等确认：服务端走不通会回 PosCorrect；踩到传送点则等 EnterFloor
        this.net.send({ t: "Move", x: s.x, y: s.y, dir: d });
        s.stepTo(nx, ny, d, now, true);
        if (this.walk[ny * this.width + nx] === 3) { this.waitingWarp = true; this.wantDir = -1; this.target = null; }
      }
    }
    this.requestView();
  }

  follower = false;                              // 在队伍里而且不是队长（partyModeFlag 且不是 LEADER）
  locked = false;                                // 摆着摊 / 看着摊位（vendor.ts，原版 iOnStreetVendor）：不能走，方向键只转身
  private followSteps: [number, number, number, number][] = []; // x, y, dir, 收到的时刻
  private lastFollowAt = 0;
  private path: number[] = [];
  private pathGoal = "";
  private finalDir = -1;
  private actorRects: [Actor, number, number, number, number][] = []; // 上一帧各角色在屏幕上的矩形

  /** 屏幕点上最靠前（最后画）的角色 */
  actorAt(x: number, y: number): Actor | null {
    for (let i = this.actorRects.length - 1; i >= 0; i--) {
      const [a, x1, y1, x2, y2] = this.actorRects[i];
      if (x >= x1 && x < x2 && y >= y1 && y < y2) return a;
    }
    return null;
  }

  /**
   * 8 方向 BFS，只走已收到且可通行的格子，返回方向序列。目标不可达时走到离目标最近的可达格。
   * 传送点只能作为终点，避免路过时被传走。
   */
  findPath(sx: number, sy: number, tx: number, ty: number, limit = 40): number[] {
    limit = Math.max(limit, 4 * Math.max(Math.abs(tx - sx), Math.abs(ty - sy))); // 世界地图点的远目标：深度跟着距离放大
    const W = this.width, key = (x: number, y: number) => y * W + x;
    const prev = new Map<number, [number, number]>(); // 格 → [上一格, 方向]
    const start = key(sx, sy);
    prev.set(start, [-1, -1]);
    let queue = [start], best = start;
    const dist = (k: number) => Math.max(Math.abs((k % W) - tx), Math.abs(Math.floor(k / W) - ty));
    for (let depth = 0; queue.length && depth < limit; depth++) {
      const next: number[] = [];
      for (const k of queue) {
        if (dist(k) < dist(best)) best = k;
        if (dist(k) === 0) { queue = []; break; }
        const x = k % W, y = Math.floor(k / W);
        if (k !== start && this.walk[k] === 3) continue; // 不穿过传送点
        for (let d = 0; d < 8; d++) {
          const nx = x + MOVE[d][0], ny = y + MOVE[d][1], nk = key(nx, ny);
          if (prev.has(nk) || !this.canEnter(nx, ny)) continue;
          prev.set(nk, [k, d]);
          next.push(nk);
        }
      }
      queue = next;
    }
    const out: number[] = [];
    for (let k = best; k !== start; k = prev.get(k)![0]) out.unshift(prev.get(k)![1]);
    return out;
  }

  /** 屏幕点 → 格子（相对主角的绘制位置） */
  /** 格子 → 屏幕（相对主角的绘制位置），与 build() 用同一投影 */
  gridToScreen(gx: number, gy: number, now: number, cx: number, cy: number): [number, number] {
    const [px, py] = this.self!.pos(now);
    return [cx + (gx - px + (gy - py)) * 32, cy + (gy - py - (gx - px)) * 24];
  }

  screenToGrid(sx: number, sy: number, cx: number, cy: number): [number, number] {
    const [px, py] = this.self!.pos(performance.now());
    const a = (sx - cx) / 32, b = (sy - cy) / 24;
    return [Math.round(px + (a - b) / 2), Math.round(py + (a + b) / 2)];
  }

  // ---------------- 渲染 ----------------

  /**
   * 地图上的声音记号（map.cpp drawMap：编号 ≤ 99 的地块 / 物件不画，20..39 是环境音、40..59 是背景音乐）。
   * 范围是自己附近的 37×37 格（gx − 20 .. gx + 16、gy − 16 .. gy + 20，原版读进来的地图范围，不只是画面）。
   * 背景音乐记号有好几格时，原版最后看到的那格算数：画面上最上面、同一排最左边的那格。
   */
  soundMarkers(now: number, W = 800, H = 600): { bgm: number; bgms: number[]; env: { marker: number; x: number; y: number }[] } {
    const out = { bgm: -1, bgms: [] as number[], env: [] as { marker: number; x: number; y: number }[] };
    const s = this.self;
    if (!s || this.floor < 0) return out;
    const [px, py] = s.pos(now);
    const cx = W / 2, cy = H / 2 + 24;
    const gx = Math.round(px), gy = Math.round(py);
    // 背景音乐记号：原版 drawMap 按 y 再 x 扫过读进来的地图，每个记号都调 play_map_bgm，最后一个（y 最大、再 x 最大）算数
    for (let y = Math.max(0, gy - 16); y < Math.min(this.height, gy + 21); y++) {
      for (let x = Math.max(0, gx - 20); x < Math.min(this.width, gx + 17); x++) {
        const k = y * this.width + x;
        if (!this.walk[k]) continue;
        const ex = cx + (x - px + (y - py)) * 32, ey = cy + (y - py - (x - px)) * 24;
        for (const v of [this.tiles[k], this.objs[k]]) {
          if (v >= 20 && v <= 37) out.env.push({ marker: v, x: ex, y: ey });
          else if (v >= 40 && v <= 55) { out.bgm = v; if (!out.bgms.includes(v)) out.bgms.push(v); }
        }
      }
    }
    return out;
  }

  build(now: number, W: number, H: number, labels: Label[]): Quad[] {
    this.actorRects = [];
    const out: Quad[] = [];
    const s = this.self;
    if (!s || this.floor < 0) return out;
    const [px, py] = s.pos(now);
    const cx = W / 2, cy = H / 2 + 24;
    const sx = (x: number, y: number) => cx + (x - px + (y - py)) * 32;
    const sy = (x: number, y: number) => cy + (y - py - (x - px)) * 24;
    const R = VIEW + 2;
    const x0 = Math.max(0, Math.floor(px) - R), x1 = Math.min(this.width - 1, Math.ceil(px) + R);
    const y0 = Math.max(0, Math.floor(py) - R), y1 = Math.min(this.height - 1, Math.ceil(py) + R);

    // 地面：互不遮挡，按图集页分组减少绘制次数
    const groups = new Map<object, Quad[]>();
    for (let y = y0; y <= y1; y++) for (let x = x0; x <= x1; x++) {
      const k = y * this.width + x;
      if (!this.walk[k] || this.tiles[k] <= CG_INVISIBLE) continue; // 记号格（如 63）在资源里有占位图（写着 1 / A 的格子），原版不画
      const q = this.mapQuad(this.tiles[k], sx(x, y), sy(x, y));
      if (q) { const g = groups.get(q.tex); if (g) g.push(q); else groups.set(q.tex, [q]); }
    }
    for (const g of groups.values()) out.push(...g);

    // 物件：按锚点（占地范围最靠前的一角）的屏幕 y 由远到近
    const objects: Sprite[] = [];
    for (let y = y0; y <= y1; y++) for (let x = x0; x <= x1; x++) {
      const k = y * this.width + x;
      if (!this.walk[k] || this.objs[k] <= CG_INVISIBLE) continue;
      const q = this.mapQuad(this.objs[k], sx(x, y), sy(x, y));
      if (q && this.assets.flat(this.objs[k])) { out.push(q); continue; } // 桥、地毯：紧跟地面画，永远在角色下面
      if (q) {
        const [tx, ty] = this.assets.footprint(this.objs[k]);
        objects.push({ depth: sy(x, y), q, gx: x, gy: y, tx, ty });
      }
    }
    objects.sort((a, b) => a.depth - b.depth);

    // 角色：先按深度定位，再和屏幕上重叠的物件比较占地关系（原客户端 sort_parts 的思路）：
    // 角色在物件占地范围的前方（x 更小或 y 更大）时必须画在它之后，否则站在大建筑侧面会被整栋压住
    const chars: Sprite[] = [];
    for (const a of [s, ...this.actors.values()]) {
      if (this.hideOthers && a !== s && a.kind === "player") continue;
      const [ax, ay] = a.pos(now);
      const ex = sx(ax, ay), ey = sy(ax, ay);
      if (ex < -100 || ey < -100 || ex > W + 100 || ey > H + 200) continue;
      const walking = a.moving(now) || (a === s && (this.wantDir >= 0 || this.target !== null));
      const f = a.frameOf(this.assets, now, walking);
      if (a.se) { sfx(a.se, ex); a.se = 0; }
      const q = f && this.imageQuad(f[0], ex + f[1], ey + f[2]);
      if (q) {
        const flat = a.kind === "item" || a.kind === "gold"; // 地上的道具与石币贴着地面，同一格的人画在它上面
        chars.push({ depth: ey + (flat ? 0 : 0.5), q, gx: Math.round(ax), gy: Math.round(ay), tx: 1, ty: 1 });
        if (a !== s) this.actorRects.push([a, q.x, q.y, q.x + q.w, q.y + q.h]);
      }
      let top = q ? q.y - 4 : ey - 70;
      if (a.vendor) top = Math.min(top, ey - 114); // 摆摊时名字挪到招牌上面
      if (a.kind === "item" || a.kind === "gold") continue; // 地上的道具与石币不标名字，滑鼠停着时显示在下方
      labels.push({ x: ex, y: top, text: a.name, color: a === s ? "#ffe680" : a.kind === "npc" ? "#9fd3ff" : "#ffffff" });
      // 家族名（character.cpp：别人的家族名在名字上方 13 像素，自己的不画）
      const fam = a !== s && a.family ? 16 : 0;
      if (fam) labels.push({ x: ex, y: top - fam, text: a.family, color: "#ffffff" });
      // 摊位名（character.cpp：szStreetVendorTitle 画在头上）与交易中的记号（CHAR_ACTTRADE）
      let extra = 0;
      if (a.vendor) labels.push({ x: ex, y: ey - 90, text: a.vendor, color: "#fff3c0" }); // 写在招牌 35343 的木板上（板子画出来在脚下往上约 80 像素）
      if (a.trade) { labels.push({ x: ex, y: top - 18 - fam, text: "〔交易中〕", color: "#7af6ff" }); extra = 18; }
      if (a.speech && now < a.speechUntil) labels.push({ x: ex, y: top - 18 - fam - extra, text: a.speech, color: "#ffffff", bubble: true });
      // 头上的记号（character.cpp）：队长 SPR_leader（战斗中不画）、战斗 CG_VS_MARK_nA、求救 CG_SPEECH_HELP（低 20）
      const mark = (q2: Quad | null, depth = ey + 0.6) => { if (q2) chars.push({ depth, q: q2, gx: Math.round(ax), gy: Math.round(ay), tx: 1, ty: 1 }); };
      // 摆摊的图示（character.cpp _STREET_VENDOR_CHANGE_ICON）：地垫 35343 在 (x, y + 35) 垫在人物下面、货架 35344 在 (x − 10, y + 10) 人物后面、
      // 招牌 35345 在 (x, y − 15) 人物前面；摊位名在它上面
      if (a.vendor) {
        mark(this.mapQuad(35343, ex, ey + 35), ey - 0.4);
        mark(this.mapQuad(35344, ex - 10, ey + 10), ey - 0.3);
        mark(this.mapQuad(35345, ex, ey - 15), ey + 0.7);
      }
      const my = top - 20 - fam - extra;
      if (a.battle > 0) mark(this.mapQuad(25610 + 2 * ((a.battle - 1) % 10), ex, my));
      else if (a.leader) {
        const def = this.assets.anim(100500), act = def?.actions[0];
        if (act && act.frames.length) {
          const [img, fx, fy] = act.frames[Math.floor(now / (Math.max(1, act.ticks) * (1000 / 60))) % act.frames.length];
          if (img >= 0) mark(this.imageQuad(img, ex + fx, my + fy));
        }
      }
      if (a.help) mark(this.mapQuad(25525, ex, my + 20));
      if (a.emote) { // 打呼 SPR_sleep / 冷场 SPR_stone（setPcEmotion）
        const def = this.assets.anim(a.emote), act = def?.actions[0];
        if (act && act.frames.length) {
          const [img, fx, fy] = act.frames[Math.floor(now / (Math.max(1, act.ticks) * (1000 / 60))) % act.frames.length];
          if (img >= 0) mark(this.imageQuad(img, ex + fx, my + fy));
        }
      }
    }
    chars.sort((a, b) => a.depth - b.depth);

    const after = new Map<number, Sprite[]>(); // 物件下标 → 紧随其后绘制的角色；-1 表示在所有物件之前
    for (const c of chars) {
      let slot = -1;
      objects.forEach((o, i) => {
        if (o.depth <= c.depth) slot = Math.max(slot, i);
        else if (overlap(o.q, c.q) && (c.gx < o.gx || c.gy > o.gy)) slot = Math.max(slot, i);
      });
      const list = after.get(slot);
      if (list) list.push(c); else after.set(slot, [c]);
    }
    for (const c of after.get(-1) ?? []) out.push(c.q);
    objects.forEach((o, i) => {
      out.push(o.q);
      for (const c of after.get(i) ?? []) out.push(c.q);
    });
    return out;
  }

  private mapQuad(mapNumber: number, x: number, y: number): Quad | null {
    const idx = this.assets.tile(mapNumber);
    return idx === undefined ? null : this.imageQuad(idx, x, y);
  }

  private imageQuad(id: number, x: number, y: number): Quad | null {
    const e = this.assets.image(id);
    if (!e || e.page < 0) return null;
    const tex = this.assets.page(e.set, e.page);
    return tex && { tex, x: Math.round(x + e.ox), y: Math.round(y + e.oy), u: e.x, v: e.y, w: e.w, h: e.h };
  }
}
