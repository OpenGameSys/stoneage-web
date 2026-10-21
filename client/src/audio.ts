// 声音（原版 system/t_music.cpp，规则见 h5/CLAUDE.md 的"声音"）：Web Audio 播放导出的 Ogg Opus（assets/audio/bgm、se）。
//   背景音乐 play_bgm：从头播（没有淡入），按 bgm_tbl 的循环方式（只播一次 / 整首循环 / 播到 loopEnd 后在 loopStart..loopEnd 循环）；
//     音量 = volume_tbl[曲子的音量 × 设定 ÷ 15]，节奏是每首各记一个 −8..+8（freq_tbl，同时改变速度与音高）。
//   淡出 fade_out_bgm：音量下标每帧减 1，V + 1 帧后停掉（V 是开始时的下标），没有交叉淡入淡出。
//   地图的背景音乐：地图里看不见的记号 40..55 决定（mapBgm），换曲时先淡出再从头播新的；范围里没有记号就继续播原来的。
//   音效 play_se：同一个音效 5 帧内不重播；左右声道按画面横座标（原版 x ÷ 5 是 640 宽，这里按 800 宽换算），y 不影响；最多 64 个同时响。
//   环境音（记号 20..37）：每种取最近的一格，音量按到画面中心的距离（约 841 像素外听不到），循环播放，声道每帧最多移 2 格。
// 帧照本项目的惯例按 60 帧 / 秒算（原版 VER25 的主循环是 6 ms 一次，计时比这里快约 2.8 倍）。
// 浏览器要等使用者第一次点击或按键才允许出声，unlock() 挂在那两个事件上；在那之前要播的背景音乐记下来，解锁后再开始。

import { BGM, PAN_GAIN, PITCH_RATE, SE, VOLUME_GAIN } from "./soundtable.ts";

const TICK = 1000 / 60;
const SE_LOCK_MS = 5 * TICK; // tone_tbl[].play_time = 5
const VOICE_MAX = 64;

export interface AudioSettings { bgmVolume: number; bgmPitches: Record<number, number>; seVolume: number; stereo: boolean; mute: boolean; }

/** 画面横座标 → 声道下标 0..127（64 正中） */
function panIndex(x: number, stereo: boolean): number {
  return stereo ? Math.max(0, Math.min(127, Math.round(x * 128 / 800))) : 64;
}

interface Voice { src: AudioBufferSourceNode; l: GainNode; r: GainNode; }

export class Sound {
  private ctx: AudioContext | null = null;
  private root: string;
  private settings: () => AudioSettings;
  private buffers = new Map<string, Promise<AudioBuffer | null>>();
  private master: GainNode | null = null;
  // 背景音乐：现在的编号（t_music_bgm_no，−1 没有）、来源、增益（音量与淡出都在这上面）
  private bgmNo = -1;
  private bgmSrc: AudioBufferSourceNode | null = null;
  private bgmGain: GainNode | null = null;
  private fading: { until: number; then: (() => void) | null } | null = null;
  private wantBgm = -1;     // 解锁前要播的
  private mapTarget = -1;   // 地图记号要的曲子（play_map_bgm 的 map_bgm_no）
  private lastSe = new Map<number, number>();
  private voices = 0;
  private ambient = new Map<number, { voice: Voice; se: number; pan: number; vol: number }>();

  constructor(root: string, settings: () => AudioSettings) {
    this.root = root;
    this.settings = settings;
    const unlock = () => { this.unlock(); removeEventListener("pointerdown", unlock); removeEventListener("keydown", unlock); };
    addEventListener("pointerdown", unlock);
    addEventListener("keydown", unlock);
  }

  private unlock() {
    if (this.ctx) return;
    const ctx = new AudioContext();
    this.ctx = ctx;
    this.master = ctx.createGain();
    this.master.connect(ctx.destination);
    this.apply();
    if (this.wantBgm >= 0) { const n = this.wantBgm; this.wantBgm = -1; this.playBgm(n); }
  }

  /** 现在的背景音乐编号（−1 没有）；系统菜单的节奏改的是这一首 */
  get currentBgm() { return this.bgmNo >= 0 ? this.bgmNo : this.wantBgm; }

  /** 设定改了：关闭声音、背景音乐的音量与节奏马上生效；音效的音量从下一个开始 */
  apply() {
    if (!this.ctx) return;
    const s = this.settings();
    this.master!.gain.value = s.mute ? 0 : 1;
    if (this.bgmGain && !this.fading) this.bgmGain.gain.value = this.bgmLevelGain(this.bgmNo);
    if (this.bgmSrc) this.bgmSrc.playbackRate.value = this.pitchRate(this.bgmNo);
  }

  private bgmLevel(no: number): number {
    const b = BGM[no];
    return b ? Math.floor(b[1] * this.settings().bgmVolume / 15) : 0;
  }
  private bgmLevelGain(no: number) { return VOLUME_GAIN[this.bgmLevel(no)] ?? 0; }
  private pitchRate(no: number): number {
    const p = Math.max(-8, Math.min(8, this.settings().bgmPitches[no] ?? 0));
    return PITCH_RATE[p + 8];
  }

  private load(path: string): Promise<AudioBuffer | null> {
    let p = this.buffers.get(path);
    if (!p) {
      p = fetch(`${this.root}audio/${path}.ogg`).then((r) => r.ok ? r.arrayBuffer() : Promise.reject(r.status))
        .then((b) => this.ctx!.decodeAudioData(b)).catch(() => null);
      this.buffers.set(path, p);
    }
    return p;
  }

  /** play_bgm：停掉现在的、从头播 no（−1 是停掉） */
  playBgm(no: number) {
    if (!this.ctx) { this.wantBgm = no; return; }
    this.stopBgm();
    const b = BGM[no];
    if (!b) return;
    const ctx = this.ctx, src = ctx.createBufferSource(), g = ctx.createGain();
    g.gain.value = this.bgmLevelGain(no);
    src.connect(g).connect(this.master!);
    this.bgmNo = no; this.bgmSrc = src; this.bgmGain = g;
    this.load(`bgm/${b[0]}`).then((buf) => {
      if (!buf || this.bgmSrc !== src) return;
      src.buffer = buf;
      if (b[2] === 1) src.loop = true;
      else if (b[2] === 2) { src.loop = true; src.loopStart = b[3]; src.loopEnd = b[4] || buf.duration; }
      src.playbackRate.value = this.pitchRate(no);
      src.start();
    });
  }

  /** stop_bgm：马上停 */
  stopBgm() {
    if (this.bgmSrc) { try { this.bgmSrc.stop(); } catch { /* 还没开始 */ } this.bgmSrc.disconnect(); }
    this.bgmSrc = null; this.bgmGain = null; this.bgmNo = -1; this.fading = null;
    if (!this.ctx) this.wantBgm = -1;
  }

  /** fade_out_bgm：音量下标每帧减 1，到 0 停掉，接着做 then（淡出中再叫一次不会重来） */
  fadeOutBgm(then: (() => void) | null = null) {
    if (!this.ctx || !this.bgmSrc || !this.bgmGain) { this.stopBgm(); then?.(); return; }
    if (this.fading) { this.fading.then = then; return; }
    const v = this.bgmLevel(this.bgmNo), ctx = this.ctx;
    const curve = new Float32Array(v + 1);
    for (let i = 0; i <= v; i++) curve[i] = VOLUME_GAIN[v - i] ?? 0;
    const sec = (v + 1) * TICK / 1000;
    this.bgmGain.gain.cancelScheduledValues(ctx.currentTime);
    if (curve.length >= 2) this.bgmGain.gain.setValueCurveAtTime(curve, ctx.currentTime, sec);
    this.fading = { until: performance.now() + sec * 1000, then };
  }

  /**
   * 地图上的背景音乐（play_map_bgm_check）：no 是这一帧在 37×37 格里看到的记号对应的曲子，−1 是没看到（继续播原来的）。
   * 跟现在的不一样就淡出、淡出完了再从头播 no。
   */
  mapBgm(no: number) {
    if (no < 0) return;
    this.mapTarget = no;
    if (!this.ctx) { this.wantBgm = no; return; }
    if (this.bgmNo === no && !this.fading) return;
    if (this.fading) return; // 淡出完会播最新的 mapTarget
    this.fadeOutBgm(() => this.playBgm(this.mapTarget));
  }

  /** 每帧：淡出的计时（淡出完停掉曲子，再做接下来的事） */
  update(now: number) {
    if (this.fading && now >= this.fading.until) {
      const then = this.fading.then;
      this.stopBgm();
      then?.();
    }
  }

  /**
   * 一个声音：先混成单声道，再分到左右各乘增益（原版 DirectSound 的 SetVolume + SetPan：
   * 左 = 音量 × 声道表的左，右 = 音量 × 声道表的右）。
   */
  private voice(buf: AudioBuffer, pan: number, gain: number, loop: boolean): Voice {
    const ctx = this.ctx!, src = ctx.createBufferSource();
    src.buffer = buf;
    src.loop = loop;
    const mono = ctx.createGain();
    mono.channelCount = 1; mono.channelCountMode = "explicit"; mono.channelInterpretation = "speakers";
    const l = ctx.createGain(), r = ctx.createGain(), merge = ctx.createChannelMerger(2);
    const v = { src, l, r };
    mix(v, pan, gain);
    src.connect(mono);
    mono.connect(l).connect(merge, 0, 0);
    mono.connect(r).connect(merge, 0, 1);
    merge.connect(this.master!);
    return v;
  }

  /** play_se：x 是画面横座标（不给就在正中） */
  se(no: number, x = 400) {
    if (!this.ctx || this.settings().mute) return;
    const def = SE[no];
    if (!def) return;
    const now = performance.now();
    if (now - (this.lastSe.get(no) ?? -1e9) < SE_LOCK_MS || this.voices >= VOICE_MAX) return;
    this.lastSe.set(no, now);
    const s = this.settings(), gain = VOLUME_GAIN[Math.floor(def[1] * s.seVolume / 15)] ?? 0, pan = panIndex(x, s.stereo);
    this.voices++;
    this.load(`se/${def[0]}`).then((buf) => {
      if (!buf) { this.voices--; return; }
      const v = this.voice(buf, pan, gain, false);
      v.src.onended = () => { this.voices--; v.src.disconnect(); };
      v.src.start();
    });
  }

  /**
   * 环境音（play_environment / play_environment_check）：cells 是这一帧 37×37 格里的环境记号与它们的画面座标，
   * zone 是 SA 的时段（0 白天、1 傍晚、2 夜晚、3 早晨）。不在战斗画面时每帧叫一次，进战斗就传空的（全部停掉）。
   */
  ambientFrame(cells: { marker: number; x: number; y: number }[], zone: number) {
    if (!this.ctx) return;
    const s = this.settings();
    const near = new Map<number, { dist: number; x: number }>();
    for (const c of cells) {
      let m = c.marker;
      if (m < 20 || m > 37) continue;
      if (m === 28 && zone !== 3) continue;              // 早晨才有
      if (m === 29 && zone !== 2) continue;              // 夜晚才有
      if (m === 37) { if (zone === 3) continue; if (zone === 2) m = 29; } // 早晨没有，夜晚换成 29
      const dist = Math.floor(Math.hypot(c.x - 400, c.y - 300)); // 原版以 640×480 的 (320, 240) 为中心，这里用自己站的位置
      const old = near.get(m);
      if (!old || dist < old.dist) near.set(m, { dist, x: c.x });
    }
    for (const [m, a] of this.ambient) {
      if (!near.has(m) || s.mute) { try { a.voice.src.stop(); } catch { /* */ } a.voice.src.disconnect(); this.ambient.delete(m); }
    }
    if (s.mute) return;
    for (const [m, n] of near) {
      const v = Math.floor(n.dist * 16 / 106);
      if (v >= 127) continue;
      const se = 151 + (m - 20), def = SE[se];
      if (!def) continue;
      const vol = Math.floor(Math.floor((127 - v) * def[1] / 127) * s.seVolume / 15);
      const target = panIndex(n.x, s.stereo);
      const a = this.ambient.get(m);
      if (!a) {
        const slot = { voice: null as unknown as Voice, se, pan: target, vol };
        this.ambient.set(m, slot);
        this.load(`se/${def[0]}`).then((buf) => {
          if (!buf || this.ambient.get(m) !== slot) return;
          slot.voice = this.voice(buf, slot.pan, VOLUME_GAIN[slot.vol] ?? 0, true);
          slot.voice.src.start();
        });
        continue;
      }
      a.pan += Math.max(-2, Math.min(2, target - a.pan));
      a.vol = vol;
      if (a.voice) mix(a.voice, a.pan, VOLUME_GAIN[a.vol] ?? 0);
    }
  }
}

function mix(v: Voice, pan: number, gain: number) {
  const [l, r] = PAN_GAIN[pan];
  v.l.gain.value = l * gain;
  v.r.gain.value = r * gain;
}

/** 全局的音效入口（战斗与动画用；main.ts 接上） */
export let sfx: (no: number, x?: number) => void = () => {};
export function setSfx(f: (no: number, x?: number) => void) { sfx = f; }
