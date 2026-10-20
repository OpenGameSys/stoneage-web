// 点击移动路线（原版 getRouteMap 语义）的单元测试。运行：node h5/client/test/route.test.ts
import { MOVE, dirTo, routeTo } from "../src/field.ts";

let fail = 0;
const ok = (cond: boolean, what: string) => { console.log(`${cond ? "✓" : "✗"} ${what}`); if (!cond) fail++; };
const walk = (sx: number, sy: number, route: number[]) => route.reduce(([x, y], d) => [x + MOVE[d][0], y + MOVE[d][1]], [sx, sy]);

// 方向换算与 MOVE 表一致
ok(MOVE.every(([dx, dy], d) => dirTo(dx, dy) === d), "dirTo 与 MOVE 表一致");

// 空地上：沿直线走到目标，不走 V 字。(0,0) → (6,2)：方向 6（+x+y）与 5（+x）交替，转弯次数少
{
  const { route } = routeTo(0, 0, 6, 2, () => false);
  const [x, y] = walk(0, 0, route);
  ok(x === 6 && y === 2, `到达 (6,2)，路线 ${route.join("")}`);
  ok(route.length === 6, "步数等于切比雪夫距离");
  // 每一步都在目标方向 ±45° 内
  ok(route.every((d) => [5, 6].includes(d)), "只用目标方向及其相邻方向");
}

// 远距离直线：(0,0) → (20,7) 路线中前后两段不应先斜后直地截成两截（V 字）
{
  const { route } = routeTo(0, 0, 20, 7, () => false);
  const firstStraight = route.indexOf(5), lastDiag = route.lastIndexOf(6);
  ok(lastDiag > firstStraight, `斜步与直步交错分布（不是先斜走完再直走），路线 ${route.join("")}`);
}

// 斜走被一侧挡住：走 L 形绕过
{
  const blocked = new Set(["1,0"]); // 挡住 +x 邻格
  const { route } = routeTo(0, 0, 3, 3, (x, y) => blocked.has(`${x},${y}`));
  const [x, y] = walk(0, 0, route);
  ok(x === 3 && y === 3, `一侧被挡仍能到达，路线 ${route.join("")}`);
}

// 正前方被墙完全挡住：原版不会移动（返回空路线）
{
  const wall = (x: number, y: number) => x === 1;
  const { route } = routeTo(0, 0, 5, 0, wall);
  ok(route.length === 0, "正前方是墙时原版路线为空");
}

// 目标本身不可进入：停在旁边，并朝向目标
{
  const { route, lastDir } = routeTo(0, 0, 4, 0, (x, y) => x === 4 && y === 0);
  const [x] = walk(0, 0, route);
  ok(x === 3 && lastDir === dirTo(1, 0), `停在目标前一格并朝向目标（lastDir=${lastDir}）`);
}

console.log(fail ? `失败 ${fail}` : "全部通过");
process.exit(fail ? 1 : 0);
