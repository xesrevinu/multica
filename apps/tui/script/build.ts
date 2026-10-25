#!/usr/bin/env bun

import { mkdir, rm } from "node:fs/promises"
import path from "node:path"
import { fileURLToPath } from "node:url"

const dir = path.resolve(fileURLToPath(new URL("..", import.meta.url)))
const binary = "multica-tui"
const outdir = path.resolve(dir, "dist")
const target = {
  os: "darwin" as const,
  arch: "arm64" as const,
}

if (process.platform !== target.os || process.arch !== target.arch) {
  throw new Error(`multica-tui build only supports ${target.os}-${target.arch}`)
}

process.chdir(dir)
await rm(outdir, { recursive: true, force: true })
await mkdir(path.join(outdir, "bin"), { recursive: true })

const result = await Bun.build({
  entrypoints: ["./src/bin.ts"],
  tsconfig: "./tsconfig.json",
  format: "esm",
  minify: true,
  sourcemap: "none",
  conditions: ["browser", "bun", "node"],
  compile: {
    autoloadBunfig: false,
    autoloadDotenv: false,
    autoloadTsconfig: true,
    autoloadPackageJson: true,
    target: `bun-${target.os}-${target.arch}` as Bun.Build.CompileTarget,
    outfile: path.join(outdir, "bin", binary),
    execArgv: [`--user-agent=${binary}`, "--use-system-ca", "--no-warnings", "--"],
  },
})

if (!result.success) {
  for (const log of result.logs) console.error(log)
  process.exit(1)
}

console.log(`built ${path.join(outdir, "bin", binary)}`)
