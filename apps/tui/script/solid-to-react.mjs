#!/usr/bin/env bun
/**
 * Mechanical Solid → React helpers for Multica TUI views.
 * Not perfect — follow up with typecheck / manual fixes.
 */
import { readdir, readFile, writeFile } from "node:fs/promises"
import path from "node:path"

const root = path.resolve(import.meta.dir, "../src/tui")

const importRewrites = [
  [/from "@opencode\/tui\/context\/theme"/g, 'from "./context/theme"'],
  [/from "@opencode\/tui\/ui\/dialog"/g, 'from "./ui/dialog"'],
  [/from "@opencode\/tui\/ui\/dialog-select"/g, 'from "./ui/dialog-select"'],
  [/from "@opencode\/tui\/ui\/dialog-prompt"/g, 'from "./ui/dialog-prompt"'],
  [/from "@opencode\/tui\/ui\/toast"/g, 'from "./ui/toast"'],
  [/from "@opencode\/tui\/context\/keymap"/g, 'from "./context/keymap"'],
  [/from "@opencode\/tui\/context\/clipboard"/g, 'from "./context/clipboard"'],
  [/from "@opencode\/tui\/config"/g, 'from "./opentui-config"'],
  [/from "@opentui\/solid"/g, 'from "@opentui/react"'],
]

// Files already rewritten by hand
const skip = new Set([
  "run.tsx",
  "config.ts",
  "opentui-config.tsx",
  "context/theme.tsx",
  "context/clipboard.tsx",
  "context/keymap.tsx",
  "ui/dialog.tsx",
  "ui/dialog-select.tsx",
  "ui/dialog-prompt.tsx",
  "ui/toast.tsx",
])

async function walk(dir) {
  const entries = await readdir(dir, { withFileTypes: true })
  const files = []
  for (const entry of entries) {
    const full = path.join(dir, entry.name)
    if (entry.isDirectory()) files.push(...(await walk(full)))
    else if (/\.(tsx|ts)$/.test(entry.name)) files.push(full)
  }
  return files
}

function rewriteImports(src, file) {
  let out = src
  for (const [re, repl] of importRewrites) out = out.replace(re, repl)
  // Relative depth for nested dirs
  if (file.includes(`${path.sep}context${path.sep}`) || file.includes(`${path.sep}ui${path.sep}`)) {
    out = out.replaceAll('from "./context/', 'from "../context/')
    out = out.replaceAll('from "./ui/', 'from "../ui/')
    out = out.replaceAll('from "./opentui-config"', 'from "../opentui-config"')
  }
  return out
}

function stripSolidImports(src) {
  return src
    .replace(/import\s*\{[^}]*\}\s*from\s*["']solid-js["']\s*;?\n?/g, "")
    .replace(/import\s+type\s*\{[^}]*\}\s*from\s*["']solid-js["']\s*;?\n?/g, "")
    .replace(/import\s*\{[^}]*\}\s*from\s*["']solid-js\/store["']\s*;?\n?/g, "")
}

function addReactImports(src) {
  if (/from ["']react["']/.test(src)) return src
  const needs = []
  if (/\buseState\b/.test(src) || /createSignal/.test(src)) needs.push("useState")
  if (/\buseEffect\b/.test(src) || /createEffect/.test(src) || /onMount/.test(src) || /onCleanup/.test(src))
    needs.push("useEffect")
  if (/\buseMemo\b/.test(src) || /createMemo/.test(src)) needs.push("useMemo")
  if (/\buseRef\b/.test(src)) needs.push("useRef")
  if (/\bReactNode\b/.test(src) || /JSX\.Element/.test(src)) needs.push("type ReactNode")
  if (!needs.length && !/<Show\b|<For\b|<Switch\b|<Match\b/.test(src)) return src
  // Always pull common hooks when Solid control-flow remains to convert
  if (/<Show\b|<For\b|<Switch\b|<Match\b|createSignal|createEffect|createMemo|onMount|onCleanup/.test(src)) {
    for (const h of ["useState", "useEffect", "useMemo", "useRef"]) {
      if (!needs.includes(h) && !needs.includes(`type ${h}`)) {
        // only add if pattern suggests
      }
    }
    if (!needs.includes("useState")) needs.push("useState")
    if (!needs.includes("useEffect")) needs.push("useEffect")
    if (!needs.includes("useMemo")) needs.push("useMemo")
    if (!needs.includes("useRef")) needs.push("useRef")
    if (!needs.includes("type ReactNode")) needs.push("type ReactNode")
  }
  const uniq = [...new Set(needs)]
  return `import { ${uniq.join(", ")} } from "react"\n` + src
}

function convertSignals(src) {
  let out = src
  // const [x, setX] = createSignal(init) → useState
  out = out.replace(/\bcreateSignal\s*</g, "useState<")
  out = out.replace(/\bcreateSignal\s*\(/g, "useState(")
  out = out.replace(/\bcreateMemo\s*\(\s*\(\)\s*=>/g, "useMemo(() =>")
  // createMemo(() => expr) needs deps — leave empty deps marker
  out = out.replace(/\bcreateEffect\s*\(\s*\(\)\s*=>/g, "useEffect(() =>")
  out = out.replace(/\bonMount\s*\(\s*\(\)\s*=>/g, "useEffect(() =>")
  // dimensions().width → dimensions.width (after useTerminalDimensions from react)
  out = out.replace(/\bdimensions\(\)\.(\w+)/g, "dimensions.$1")
  out = out.replace(/\bJSX\.Element\b/g, "ReactNode")
  return out
}

async function main() {
  const files = await walk(root)
  for (const file of files) {
    const rel = path.relative(root, file)
    if (skip.has(rel)) continue
    let src = await readFile(file, "utf8")
    if (!src.includes("solid-js") && !src.includes("@opencode/") && !src.includes("@opentui/solid")) continue
    src = rewriteImports(src, file)
    src = stripSolidImports(src)
    src = convertSignals(src)
    src = addReactImports(src)
    await writeFile(file, src)
    console.log("updated", rel)
  }
}

await main()
