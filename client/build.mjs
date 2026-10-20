// 把 src/ 下的 TypeScript 去掉类型输出为浏览器可用的 JS（Node 自带 stripTypeScriptTypes，无需安装依赖）。
// 只支持可擦除的 TS 语法：不能用 enum、namespace、参数属性。
// 运行：node client/build.mjs   输出到 client/dist/
import { stripTypeScriptTypes } from "node:module";
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, join, relative } from "node:path";
import { fileURLToPath } from "node:url";

const root = join(dirname(fileURLToPath(import.meta.url)), "src");
const out = join(dirname(fileURLToPath(import.meta.url)), "dist");
const seen = new Set();
const version = Date.now().toString(36); // 模块地址带版本号，避免浏览器用到旧的缓存

function build(rel) {
  if (seen.has(rel)) return;
  seen.add(rel);
  const src = readFileSync(join(root, rel), "utf8");
  const js = stripTypeScriptTypes(src).replace(/(from\s+["'])(\.{1,2}\/[^"']+)\.ts(["'])/g, (_, a, path, b) => {
    build(relative(root, join(dirname(join(root, rel)), path + ".ts")).replaceAll("\\", "/"));
    return `${a}${path}.js?v=${version}${b}`;
  });
  const file = join(out, rel.replace(/\.ts$/, ".js"));
  mkdirSync(dirname(file), { recursive: true });
  writeFileSync(file, js);
}

build("main.ts");
const htmlPath = join(dirname(fileURLToPath(import.meta.url)), "index.html");
writeFileSync(htmlPath, readFileSync(htmlPath, "utf8").replace(/src="dist\/main\.js[^"]*"/, `src="dist/main.js?v=${version}"`));
console.log(`已构建 ${seen.size} 个文件 → client/dist/：${[...seen].join(", ")}`);
