// 战斗设定窗口（系统窗口的"战斗设定"；原版 AI_SettingProc 的底图与箭头不再用）。战斗相关的开关全在这里：
//   基本：自动战斗、快速战斗、自动遇敌、补血时机、打不过就逃跑；
//   人物：第一回合 / 之后每回合的行动（智能判断 / 普通攻击 / 防御 / 装备上的精灵）与对象，用精灵进攻；
//   宠物：第一回合 / 之后每回合的行动（智能判断 / 出战宠物的技能，含攻击、防御）与对象；
//   其他：自动抓宠、自动丢肉、恢复预设，下面写智能战斗每回合怎么打。
// 每一项一行：左边项目名、中间现在的值（◀ ▶ 切换，点值本身也会往下切）、下面灰字说明（按窗口宽度折行，最多两行）；
// 改了马上存（ai.ts saveAi）。对象的选项按精灵 / 技能的范围给（只能对自己的就只有"自己"；"其他人"类的可以选自己、宠物 / 主人、敌人）。

import type { InvItem, PetSkill } from "./proto.gen.ts";
import { AUTO, TARGET_LABEL, defaultAi, loadAi, saveAi, type AiAction, type AiSettings, type AiTarget } from "./ai.ts";
import { FONT, measure, type Ui } from "./ui.ts";

export interface AiHost {
  ui: Ui;
  inv(): InvItem[];
  petSkills(): PetSkill[] | null; // 出战宠物的技能（没有出战宠物为 null）
  autoBattle(): boolean;   // settings.autoBattle（原版 AI_SELECT）
  toggleAuto(): void;
  quickBattle(): boolean;  // settings.quickBattle
  toggleQuick(): void;
  autoEncount(): boolean;  // 自动遇敌开着没有
  setAutoEncount(on: boolean): void;
}

const W = 8, H = 9;                 // 512×432
const X = (800 - W * 64) / 2, Y = 60;
const INNER = W * 64 - 56;          // 文字可用的宽度
const TABS = ["基本", "人物", "宠物", "其他"] as const;
type Tab = typeof TABS[number];

/** 按像素宽度折行（中英文混排，measure 用的是界面字体），最多 max 行，超出的截掉加"…" */
function wrapPx(text: string, maxW: number, max = 2): string[] {
  const out: string[] = [];
  let cur = "";
  for (const ch of text) {
    if (measure(cur + ch) > maxW && cur) { out.push(cur); cur = ch; if (out.length === max) break; } else cur += ch;
  }
  if (out.length < max) { if (cur) out.push(cur); } else if (cur) out[max - 1] = out[max - 1].slice(0, -1) + "…";
  return out;
}

// 咒术的范围（magic.h MAGIC_TARGET_*）能选的对象
function magicTargets(type: number): AiTarget[] {
  switch (type) {
    case 0: return ["self"];
    case 1: return ["auto", "self", "pet", "enemy"];
    case 2: return ["allies"];
    case 8: return ["allies", "enemies"];
    case 3: case 11: return ["enemies"];
    case 4: return ["all"];
    case 9: case 10: return ["enemy"];
  }
  return ["auto"];
}

// 宠物技能的范围（PETSKILL_TARGET）能选的对象
function skillTargets(type: number): AiTarget[] {
  switch (type) {
    case 0: case 5: return ["self"];
    case 1: return ["auto", "self", "owner", "enemy"];
    case 2: return ["allies"];
    case 3: return ["enemies"];
    case 4: return ["all"];
    case 6: return ["auto", "owner", "enemy"];
  }
  return ["enemy"];
}

export class AiWindow {
  open = false;
  private tab: Tab = "基本";
  private s: AiSettings = defaultAi();
  private name = "";
  private host: AiHost;

  constructor(host: AiHost) { this.host = host; }

  show(name: string) { this.name = name; this.s = loadAi(name); this.open = true; }
  close() { this.open = false; }
  private save() { saveAi(this.name, this.s); }

  frame(rects: [number, number, number, number][], esc: boolean) {
    if (!this.open) return;
    if (esc) { this.close(); return; }
    const ui = this.host.ui, x = X, y = Y;
    ui.window(x, y, W, H, "ai-wnd");
    rects.push([x, y, x + W * 64, y + H * 48]);
    ui.box(x + 22, y + 10, x + W * 64 - 22, y + H * 48 - 10, "rgba(0,0,0,0.45)", true); // 窗口是半透明的，字压在地图上看不清，垫一层暗底
    ui.text(x + W * 32 - 32, y + 16, "战斗设定", FONT.YELLOW);
    const on = this.host.autoBattle();
    const state = on ? "自动战斗：开启" : "自动战斗：关闭";
    ui.text(x + W * 64 - 28 - measure(state), y + 16, state, on ? FONT.GREEN : FONT.GRAY);
    let tx = x + 28;
    for (const t of TABS) {
      const cur = t === this.tab;
      if (ui.text(tx, y + 42, t, cur ? FONT.YELLOW : FONT.AQUA, `ai-tab-${t}`) && !cur) this.tab = t;
      if (cur) ui.box(tx - 4, y + 58, tx + measure(t) + 4, y + 60, "#ffd84a", true);
      tx += measure(t) + 28;
    }
    ui.box(x + 24, y + 64, x + W * 64 - 24, y + 65, "rgba(255,255,255,0.25)", true);
    switch (this.tab) {
      case "基本": this.basicTab(x, y + 74); break;
      case "人物": this.actorTab(x, y + 74, false); break;
      case "宠物": this.actorTab(x, y + 74, true); break;
      case "其他": this.otherTab(x, y + 74); break;
    }
    if (ui.text(x + W * 32 - 28, y + H * 48 - 36, "关　闭", FONT.AQUA, "ai-close")) this.close();
  }

  // ---------------- 一行一个项目 ----------------

  /** 一行：项目名、值（◀ ▶ 切换，点值往下切）、灰字说明（折行）。回传这一行占的高度。 */
  private row(x: number, ry: number, id: string, label: string, value: string, desc: string,
    step: ((dir: number) => void) | null, opts: { dim?: boolean; color?: string } = {}): number {
    const ui = this.host.ui;
    const lx = x + 28, vx = x + 150;
    const lines = wrapPx(desc, INNER);
    const h = 20 + lines.length * 17 + 5;
    if (ui.hitBox(x + 24, ry - 4, x + W * 64 - 24, ry + h - 6, `${id}-row`, false)) ui.bottomLine = desc;
    ui.text(lx, ry, label, opts.dim ? FONT.GRAY : FONT.WHITE);
    const vc = opts.dim ? FONT.GRAY : opts.color ?? FONT.YELLOW;
    const vw = Math.min(measure(value), INNER - 150 - 40);
    if (step) {
      if (ui.text(vx, ry, "◀", FONT.AQUA, `${id}-prev`)) step(-1);
      if (ui.text(vx + 20, ry, wrapPx(value, vw + 1, 1)[0] ?? value, vc, `${id}-val`)) step(1);
      if (ui.text(vx + 20 + vw + 8, ry, "▶", FONT.AQUA, `${id}-next`)) step(1);
    } else ui.text(vx + 20, ry, value, vc);
    lines.forEach((l, i) => ui.text(lx, ry + 19 + i * 17, l, FONT.GRAY));
    return h;
  }

  private toggleRow(x: number, ry: number, id: string, label: string, on: boolean, desc: string, flip: () => void): number {
    return this.row(x, ry, id, label, on ? "开启" : "关闭", desc, () => flip(), { color: on ? FONT.GREEN : FONT.WHITE });
  }

  // ---------------- 基本 ----------------

  private basicTab(x: number, y: number) {
    const s = this.s;
    let ry = y;
    ry += this.toggleRow(x, ry, "ai-auto", "自动战斗", this.host.autoBattle(),
      "战斗时照这里的设定自动下指令；地图上按 PageUp / PageDown 也能切换。", () => this.host.toggleAuto());
    ry += this.toggleRow(x, ry, "ai-quick", "快速战斗", this.host.quickBattle(),
      "遇敌不进战斗画面、不播动画，照这里的设定自动打完直接回地图（经验写在聊天栏）。", () => this.host.toggleQuick());
    ry += this.toggleRow(x, ry, "ai-enc", "自动遇敌", this.host.autoEncount(),
      "在站的格子与旁边一格之间来回走引来敌人；手动移动、换地图或耐久力太低就停。", () => this.host.setAutoEncount(!this.host.autoEncount()));
    ry += this.row(x, ry, "ai-heal", "补血时机", `耐久力低于 ${s.healAt}％`,
      "自己、宠物或队友的 HP 低于这个比例、或预计会被打死，就先补血（肉优先，再挑够补满的精灵或道具）。",
      (d) => { s.healAt = Math.max(0, Math.min(100, s.healAt + 5 * d)); this.save(); });
    ry += this.toggleRow(x, ry, "ai-esc", "打不过就逃跑", s.escape,
      "自己会被打死又补不了血、或两回合内打不完对方时逃跑（头目、任务战斗、决斗不逃）。", () => { s.escape = !s.escape; this.save(); });
  }

  // ---------------- 人物 / 宠物的行动 ----------------

  private actorTab(x: number, y: number, pet: boolean) {
    const s = this.s, ui = this.host.ui;
    let ry = y;
    const skills = pet ? this.host.petSkills() : null;
    if (pet && !skills) {
      ui.text(x + 28, ry, "没有出战中的宠物：先在宠物窗口把一只宠物设成出战，这里才能挑它的技能。", FONT.GRAY);
      ry += 24;
    }
    const who = pet ? "宠物" : "人物";
    for (const [key, title] of (pet ? [["petFirst", "第一回合"], ["petThen", "之后每回合"]] : [["first", "第一回合"], ["then", "之后每回合"]]) as [keyof AiSettings, string][]) {
      const a = s[key] as AiAction;
      ry += this.row(x, ry, `ai-${key}`, title, this.actionName(a, pet, skills),
        a.kind === "auto" ? `${who}这回合由智能战斗决定（估伤害挑最划算的）。` : `没有紧急的事（补血、复活、逃跑）时${who}就做这个；用不了时改用智能判断。`,
        (d) => { this.stepAction(a, d, pet, skills); this.save(); });
      const targets = this.targetsFor(a, pet, skills);
      ry += this.row(x, ry, `ai-${key}-t`, "　　对象", a.kind === "auto" || a.kind === "guard" ? "—" : TARGET_LABEL[a.target],
        a.kind === "auto" || a.kind === "guard" ? "" : targets.length > 1 ? "按范围能选：" + targets.map((t) => TARGET_LABEL[t]).join("、") + "。\"自动\"是敌人或自己（看范围）。" : "这个范围只有一种对象。",
        targets.length > 1 && a.kind !== "auto" && a.kind !== "guard" ? (d) => { const k = targets.indexOf(a.target); a.target = targets[(k + d + targets.length) % targets.length]; this.save(); } : null,
        { dim: a.kind === "auto" || a.kind === "guard" });
    }
    if (!pet) {
      ry += this.toggleRow(x, ry, "ai-magic", "用精灵进攻", s.magic,
        "智能判断时攻击精灵也算进候选（会留着补血要用的 MP）；关掉只用普通攻击。", () => { s.magic = !s.magic; this.save(); });
    } else {
      ui.text(x + 28, ry + 4, "普通攻击 = 技能\"攻击\"，防御 = 技能\"防御\"；守护主人、补血等技能对象可以选主人或自己。", FONT.GRAY);
    }
  }

  /** 行动的候选：智能判断、普通攻击、防御、再来是精灵（人物）或出战宠物的技能（宠物，攻击 / 防御也在技能里） */
  private choices(pet: boolean, skills: PetSkill[] | null): AiAction[] {
    const out: AiAction[] = [{ ...AUTO }];
    if (pet) {
      for (const sk of (skills ?? []).filter((x) => x.field !== 2)) out.push({ kind: "skill", name: sk.name, target: skillTargets(sk.target)[0] });
    } else {
      out.push({ kind: "attack", name: "", target: "auto" }, { kind: "guard", name: "", target: "auto" });
      for (const it of this.host.inv().filter((x) => x.magic && x.magicField !== 2)) out.push({ kind: "magic", name: it.magic, target: magicTargets(it.magicTarget)[0] });
    }
    return out;
  }

  private actionName(a: AiAction, pet: boolean, skills: PetSkill[] | null): string {
    switch (a.kind) {
      case "auto": return "智能判断";
      case "attack": return "普通攻击";
      case "guard": return "防御";
      case "magic": return this.host.inv().some((x) => x.magic === a.name) ? a.name : `${a.name}（没装备）`;
      case "skill": return (skills ?? []).some((x) => x.name === a.name) ? a.name : `${a.name}（这只没有）`;
    }
    return "智能判断";
  }

  private stepAction(a: AiAction, dir: number, pet: boolean, skills: PetSkill[] | null) {
    const list = this.choices(pet, skills);
    let k = list.findIndex((c) => c.kind === a.kind && c.name === a.name);
    k = ((k < 0 ? 0 : k) + dir + list.length) % list.length;
    const c = list[k];
    a.kind = c.kind; a.name = c.name;
    const targets = this.targetsFor(a, pet, skills);
    if (!targets.includes(a.target)) a.target = targets[0];
  }

  private targetsFor(a: AiAction, pet: boolean, skills: PetSkill[] | null): AiTarget[] {
    if (a.kind === "magic") { const m = this.host.inv().find((x) => x.magic === a.name); return m ? magicTargets(m.magicTarget) : ["auto"]; }
    if (a.kind === "skill") { const sk = (skills ?? []).find((x) => x.name === a.name); return sk ? skillTargets(sk.target) : ["auto"]; }
    return ["auto"];
  }

  // ---------------- 其他 ----------------

  private otherTab(x: number, y: number) {
    const s = this.s, ui = this.host.ui;
    let ry = y;
    ry += this.toggleRow(x, ry, "ai-cap", "自动抓宠", s.capture,
      "只抓 1 级的敌人（原版规则）：有就捕捉，宠物对它用\"手下留情\"，场上没有 1 级的就逃跑。", () => { s.capture = !s.capture; this.save(); });
    ry += this.toggleRow(x, ry, "ai-meat", "自动丢肉", s.meat,
      "自动 / 快速战斗结束时把道具栏里的肉都丢掉，省得占格子。", () => { s.meat = !s.meat; this.save(); });
    ry += this.row(x, ry, "ai-reset", "恢复预设", "全部改回预设", "把这个角色的战斗设定改回预设（自动战斗、快速战斗、自动遇敌的开关不动）。",
      () => { this.s = defaultAi(); this.save(); }, { color: FONT.AQUA });
    ry += 6;
    ui.box(x + 24, ry, x + W * 64 - 24, ry + 1, "rgba(255,255,255,0.25)", true);
    ry += 10;
    const lines = [
      "智能战斗每回合：",
      "· 估计敌人这回合的伤害，会被打死或低于补血比例的先补血（肉优先）",
      "· 复活倒下的队友与宠物，解掉不能动的状态，MP 不够就喝回复气力的道具",
      "· 没有紧急的事就做\"人物 / 宠物\"页设定的行动；设成智能判断时，",
      "　对每个敌人、每个攻击精灵的范围各估一个值，挑最划算的打",
      "· 会记住每种敌人与每个技能的实际伤害（存在浏览器），越打越准",
    ];
    lines.forEach((l, i) => wrapPx(l, INNER, 1).forEach((t) => ui.text(x + 28, ry + i * 18, t, i === 0 ? FONT.AQUA : FONT.WHITE)));
  }
}
