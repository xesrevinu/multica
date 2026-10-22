/**
 * Pin the packages the vinext Node server imports at runtime so the image
 * can npm-install them without the monorepo. Alchemy's build writes
 * dist/server/vinext-externals.json.
 */
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");

const required = ["vinext", "react", "react-dom", "react-server-dom-webpack"];
let externals = [];
try {
  externals = JSON.parse(
    fs.readFileSync(path.join(root, "dist/server/vinext-externals.json"), "utf8"),
  );
} catch {
  externals = [];
}

function versionOf(name) {
  const segments = name.split("/");
  let dir = root;
  for (let i = 0; i < 4; i++) {
    const pkgPath = path.join(dir, "node_modules", ...segments, "package.json");
    if (fs.existsSync(pkgPath)) {
      const pkg = JSON.parse(fs.readFileSync(pkgPath, "utf8"));
      if (pkg.name === name && typeof pkg.version === "string") return pkg.version;
    }
    const parent = path.dirname(dir);
    if (parent === dir) break;
    dir = parent;
  }
  return undefined;
}

const names = new Set([
  ...required,
  ...(Array.isArray(externals) ? externals : []),
]);
const dependencies = {};
const unresolved = [];
for (const name of names) {
  if (typeof name !== "string") continue;
  if (name.startsWith("@multica/")) {
    unresolved.push(`${name} was left external; add it to transpilePackages`);
    continue;
  }
  const version = versionOf(name);
  if (version === undefined) unresolved.push(name);
  else dependencies[name] = version;
}
if (unresolved.length > 0) {
  console.error(unresolved.map((item) => `- ${item}`).join("\n"));
  process.exit(1);
}

fs.writeFileSync(
  path.join(root, "dist/runtime-package.json"),
  `${JSON.stringify(
    { name: "multica-web-runtime", private: true, type: "module", dependencies },
    null,
    2,
  )}\n`,
);
