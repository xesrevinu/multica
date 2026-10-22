#!/usr/bin/env node
/**
 * Fail fast if typecheck would not run `tsrs`. The `typescript` package stays
 * native 7+ for Next and the editor; aliasing that name to typescript6 must
 * not silently put anything else back on the JS compiler.
 */
import { createRequire } from "node:module";
import { spawnSync } from "node:child_process";

const require = createRequire(import.meta.url);
const pkgPath = require.resolve("typescript/package.json");
const pkg = require(pkgPath);
const major = Number.parseInt(pkg.version, 10);

if (pkg.name !== "typescript" || Number.isNaN(major) || major < 7) {
  console.error(
    `typescript must stay >= 7.0.2; ${pkgPath} is ${pkg.name}@${pkg.version}`,
  );
  process.exit(1);
}

const tsrs = spawnSync("tsrs", ["--version"], { encoding: "utf8" });
if (tsrs.error) {
  console.error(`failed to spawn tsrs: ${tsrs.error.message}`);
  process.exit(1);
}
if (tsrs.status !== 0) {
  console.error((tsrs.stdout || tsrs.stderr).trim() || `tsrs --version exited ${tsrs.status}`);
  process.exit(tsrs.status ?? 1);
}

console.log(`typescript ${pkg.version} (${pkgPath})`);
console.log((tsrs.stdout || tsrs.stderr).trim());
