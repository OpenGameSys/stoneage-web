// 与服务端的 WebSocket 连接。消息类型由 proto.gen.ts 提供（由 proto/messages.json 生成）。

import { fillDefaults, type C2S, type S2C, type S2CType } from "./proto.gen.ts";

type Of<T extends S2CType> = Extract<S2C, { t: T }>;

/**
 * 流量统计（?debug 的 HUD 用）：每秒一格，留最近 TRAFFIC_WINDOW 秒的收 / 送消息数与位元组数。
 * 位元组是 JSON 文字的 UTF-8 长度，也就是 permessage-deflate 压缩之前的大小（浏览器拿不到线上实际压缩后的长度）。
 */
export const TRAFFIC_WINDOW = 120;
const slots = Array.from({ length: TRAFFIC_WINDOW }, () => ({ sec: -1, inMsg: 0, inBytes: 0, outMsg: 0, outBytes: 0 }));
const trafficStart = performance.now();
function slot() {
  const sec = Math.floor(performance.now() / 1000), s = slots[sec % TRAFFIC_WINDOW];
  if (s.sec !== sec) { s.sec = sec; s.inMsg = s.inBytes = s.outMsg = s.outBytes = 0; }
  return s;
}
function utf8Len(s: string): number {
  let n = s.length;
  for (let i = 0; i < s.length; i++) { const c = s.charCodeAt(i); if (c >= 0x80) n += c >= 0x800 && (c < 0xd800 || c > 0xdfff) ? 2 : 1; }
  return n;
}
/** 最近 TRAFFIC_WINDOW 秒（刚开始不满时按实际经过的秒数）的平均：每秒消息数与 KB */
export function trafficRates() {
  const now = Math.floor(performance.now() / 1000), span = Math.max(1, Math.min(TRAFFIC_WINDOW, (performance.now() - trafficStart) / 1000));
  let inMsg = 0, inBytes = 0, outMsg = 0, outBytes = 0;
  for (const s of slots) if (s.sec > now - TRAFFIC_WINDOW && s.sec <= now) { inMsg += s.inMsg; inBytes += s.inBytes; outMsg += s.outMsg; outBytes += s.outBytes; }
  return { inMsg: inMsg / span, inKB: inBytes / 1024 / span, outMsg: outMsg / span, outKB: outBytes / 1024 / span, span };
}

export class Connection {
  private ws: WebSocket;
  private handlers = new Map<string, ((m: any) => void)[]>();
  onClose: (reason: string) => void = () => {};
  log: (dir: "→" | "←", m: object) => void = () => {};

  constructor(url: string) {
    this.ws = new WebSocket(url);
    this.ws.onmessage = (ev) => {
      const data = String(ev.data), s = slot();
      s.inMsg++; s.inBytes += utf8Len(data);
      const m = fillDefaults(JSON.parse(data) as S2C); // 服务端省略的零值补回来（proto.gen.ts）
      this.log("←", m);
      for (const h of this.handlers.get(m.t) ?? []) h(m);
    };
    // 心跳：服务端 90 秒收不到任何消息就断开（session.go idleTimeout），站着不动时也要定时发 Ping
    let seq = 0;
    const ping = setInterval(() => this.send({ t: "Ping", seq: ++seq }), 30000);
    this.ws.onclose = (ev) => { clearInterval(ping); this.onClose(ev.reason || "连接已断开"); };
  }

  on<T extends S2CType>(t: T, h: (m: Of<T>) => void) {
    this.handlers.set(t, [...(this.handlers.get(t) ?? []), h]);
  }

  once<T extends S2CType>(t: T, timeoutMs = 8000): Promise<Of<T>> {
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => reject(new Error(`等待 ${t} 超时`)), timeoutMs);
      const h = (m: Of<T>) => {
        clearTimeout(timer);
        this.handlers.set(t, (this.handlers.get(t) ?? []).filter((x) => x !== h));
        resolve(m);
      };
      this.on(t, h);
    });
  }

  send(m: C2S) {
    if (this.ws.readyState !== WebSocket.OPEN) return;
    this.log("→", m);
    const data = JSON.stringify(m), s = slot();
    s.outMsg++; s.outBytes += utf8Len(data);
    this.ws.send(data);
  }

  close() { this.ws.close(); }
}
