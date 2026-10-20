// 与服务端的 WebSocket 连接。消息类型由 proto.gen.ts 提供（由 proto/messages.json 生成）。

import type { C2S, S2C, S2CType } from "./proto.gen.ts";

type Of<T extends S2CType> = Extract<S2C, { t: T }>;

export class Connection {
  private ws: WebSocket;
  private handlers = new Map<string, ((m: any) => void)[]>();
  onClose: (reason: string) => void = () => {};
  log: (dir: "→" | "←", m: object) => void = () => {};

  constructor(url: string) {
    this.ws = new WebSocket(url);
    this.ws.onmessage = (ev) => {
      const m = JSON.parse(String(ev.data)) as S2C;
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
    this.ws.send(JSON.stringify(m));
  }

  close() { this.ws.close(); }
}
