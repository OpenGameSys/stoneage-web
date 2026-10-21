// 自动战斗的设定（额外新功能）。原版 ai_setting.cpp 的 AI 模式（固定顺序：首次精灵 → 补血 → 复活 → 补气 → 攻击）已经拿掉，
// 战斗一律由 aismart.ts 的智能战斗决定；这里只有设定的资料结构、预设值、读写（localStorage `sa.ai.<角色名>`）与旧格式的转换。
//
// 设定分两类：
//   · 开关与数值：补血时机（HP 低于几 % 就补）、打不过就逃跑、用精灵进攻、自动抓宠、自动丢肉；
//   · 人物与宠物的行动计划：第一回合、之后每回合各一个 AiAction——"智能判断"交给 smartChoose，或固定普通攻击 / 防御 /
//     某个精灵（人物，按名字找装备上的咒术）/ 某个技能（宠物，按名字找出战宠物的技能），加上对象（自己、宠物、主人、敌人、我方全体……，
//     "自动"按精灵 / 技能的范围选）。补血、复活、解状态、逃跑这些紧急的事永远先做，固定的行动是"没事时做什么"。
// 设定窗口在 aisetting.ts。

export type AiTarget = "auto" | "self" | "pet" | "owner" | "enemy" | "allies" | "enemies" | "all";
export const TARGET_LABEL: Record<AiTarget, string> = {
  auto: "自动", self: "自己", pet: "宠物", owner: "主人", enemy: "敌人", allies: "我方全体", enemies: "敌方全体", all: "全体",
};

/** 一个回合的行动：kind 是 auto（智能判断）、attack（普通攻击）、guard（防御）、magic（人物的精灵，name 是咒术名）、skill（宠物的技能，name 是技能名） */
export interface AiAction { kind: "auto" | "attack" | "guard" | "magic" | "skill"; name: string; target: AiTarget }

export interface AiSettings {
  v: 2;
  healAt: number;      // 补血时机：HP 低于这个百分比就补（0..100，5 一格）
  escape: boolean;     // 打不过就逃跑
  magic: boolean;      // 智能判断时攻击精灵也算进候选
  capture: boolean;    // 自动抓宠（只抓 1 级的，没有就逃跑）
  meat: boolean;       // 战斗结束时把肉丢掉
  first: AiAction;     // 人物：第一回合
  then: AiAction;      // 人物：之后每回合
  petFirst: AiAction;  // 宠物：第一回合
  petThen: AiAction;   // 宠物：之后每回合
}

export const AUTO: AiAction = { kind: "auto", name: "", target: "auto" };

export function defaultAi(): AiSettings {
  return { v: 2, healAt: 30, escape: true, magic: true, capture: false, meat: false,
    first: { ...AUTO }, then: { ...AUTO }, petFirst: { ...AUTO }, petThen: { ...AUTO } };
}

function action(x: unknown): AiAction {
  const a = x as Partial<AiAction> | undefined;
  const kinds = ["auto", "attack", "guard", "magic", "skill"], targets = Object.keys(TARGET_LABEL);
  if (!a || typeof a !== "object" || !kinds.includes(a.kind as string)) return { ...AUTO };
  return { kind: a.kind as AiAction["kind"], name: typeof a.name === "string" ? a.name : "", target: targets.includes(a.target as string) ? a.target as AiTarget : "auto" };
}

export function loadAi(name: string): AiSettings {
  const d = defaultAi();
  try {
    const s = JSON.parse(localStorage.getItem(`sa.ai.${name}`) ?? "null");
    if (!s || typeof s !== "object") return d;
    if (s.v === 2) {
      const n = (v: unknown, def: number) => typeof v === "number" && v >= 0 && v <= 100 ? Math.round(v / 5) * 5 : def;
      const b = (v: unknown, def: boolean) => typeof v === "boolean" ? v : def;
      return { v: 2, healAt: n(s.healAt, d.healAt), escape: b(s.escape, d.escape), magic: b(s.magic, d.magic), capture: b(s.capture, d.capture),
        meat: b(s.meat, d.meat), first: action(s.first), then: action(s.then), petFirst: action(s.petFirst), petThen: action(s.petThen) };
    }
    // 旧格式（原版的 method / target / limit / other）：留下数值与开关，行动计划用预设
    if (Array.isArray(s.limit) && Array.isArray(s.other)) {
      const lim = Number(s.limit[0]);
      if (lim >= 0 && lim <= 100) d.healAt = Math.round(lim / 5) * 5;
      d.meat = !!s.other[0]; d.capture = !!s.other[1];
      if (s.other.length > 3) d.escape = !!s.other[3];
      if (s.other.length > 4) d.magic = !!s.other[4];
    }
  } catch { /* 存不了或坏掉就用预设 */ }
  return d;
}

export function saveAi(name: string, s: AiSettings) {
  try { localStorage.setItem(`sa.ai.${name}`, JSON.stringify(s)); } catch { /* 无痕模式等存不了就算了 */ }
}

/** 送出的指令（battle.ts 换成 BattleCommand / BattlePetCommand） */
export interface AiCommand {
  kind: "magic" | "item" | "attack" | "guard" | "capture" | "escape" | "none" | "skip";
  target: number;
  item: number;
  label: string;         // 播放时右下角显示的字
  chat: string[];        // 聊天栏的提示
  pet: [number, number]; // "W|技能|目标"，-1 是 FF
  key?: string;          // 学习用：这次行动的种类（aismart.ts AiMemory）
  petKey?: string;
}
