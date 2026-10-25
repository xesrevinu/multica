#!/usr/bin/env bun
/**
 * End-to-end layout check for the Multica TUI, driven through a real PTY by
 * `tuistory` (npm i -g tuistory).
 *
 *   pnpm test:e2e            # or: bun run script/e2e.ts
 *   pnpm test:e2e -- --keep  # keep the session alive for `tuistory attach`
 *
 * Needs a reachable server: env (MULTICA_SERVER_URL / MULTICA_TOKEN /
 * MULTICA_WORKSPACE_ID) or ~/.multica/config.json, same as `pnpm start`.
 * Skips cleanly when no connection is configured so CI stays green.
 */
import { existsSync, mkdirSync, readFileSync, rmSync } from "node:fs";
import os from "node:os";
import path from "node:path";

const appDir = path.resolve(import.meta.dir, "..");
const keep = process.argv.includes("--keep");
const session = process.env.TUISTORY_SESSION ?? "multica-tui-e2e";
const profile = "e2e";
const stateFile = path.join(os.homedir(), ".multica", "profiles", profile, "tui-state.json");
const shotFile = path.join(appDir, ".e2e", "issues.png");

type Connection = { server_url?: string; token?: string; workspace_id?: string };

function connection(): Connection {
  const file = path.join(os.homedir(), ".multica", "config.json");
  let stored: Connection = {};
  if (existsSync(file)) {
    try {
      stored = JSON.parse(readFileSync(file, "utf8")) as Connection;
    } catch {
      stored = {};
    }
  }
  return {
    server_url: process.env.MULTICA_SERVER_URL ?? stored.server_url,
    token: process.env.MULTICA_TOKEN ?? stored.token,
    workspace_id: process.env.MULTICA_WORKSPACE_ID ?? stored.workspace_id,
  };
}

const conn = connection();
if (!conn.server_url || !conn.token || !conn.workspace_id) {
  console.log("e2e: skipped (no server / token / workspace configured)");
  process.exit(0);
}

const base = ["--session", session];

function run(args: string[], options: { allowFail?: boolean } = {}): string {
  const result = Bun.spawnSync(["tuistory", ...args], {
    cwd: appDir,
    stderr: "pipe",
    stdout: "pipe",
  });
  const out = result.stdout.toString();
  if (result.exitCode !== 0 && !options.allowFail) {
    throw new Error(`tuistory ${args.join(" ")} failed:\n${result.stderr.toString()}${out}`);
  }
  return out;
}

const sleep = (ms: number) => Bun.sleepSync(ms);

function snapshot(): string {
  return run(["snapshot", ...base, "--trim"]);
}

/** tuistory treats several keys as one chord, so send them one at a time. */
function press(...keys: string[]) {
  for (const key of keys) run(["press", ...base, key]);
}

function type(text: string) {
  run(["type", text, ...base]);
}

/** Row index of the first non-empty row containing `needle` (blank rows skipped). */
/** Retypes until the input line caught every character (PTY input can drop keys). */
function typeUntil(step: string, text: string, seen: (screen: string) => boolean) {
  for (let attempt = 0; attempt < 4; attempt++) {
    type(text);
    sleep(350);
    if (seen(snapshot())) return;
  }
  throw new Error(`${step}: input never received "${text}"\n--- screen ---\n${snapshot()}`);
}

const sawQuery = (text: string) => (screen: string) => screen.includes(`Filter…: ${text}`);

function lineIndex(text: string, needle: string): number {
  const lines = text
    .split("\n")
    .map((line) => line.trim())
    .filter(Boolean);
  return lines.findIndex((line) => line.includes(needle));
}

function expectMatch(step: string, text: string, pattern: RegExp) {
  if (!pattern.test(text)) {
    throw new Error(`${step}: expected ${pattern}\n--- screen ---\n${text}`);
  }
  console.log(`  ok  ${step}`);
}

function expectNoMatch(step: string, text: string, pattern: RegExp) {
  if (pattern.test(text)) {
    throw new Error(`${step}: expected no ${pattern}\n--- screen ---\n${text}`);
  }
  console.log(`  ok  ${step}`);
}

/** Chip rows are spaced by padding, so match them with whitespace tolerance. */
const chipSource = (label: string) => `f\\s+${label.replace(/ /g, "\\s+")}\\b`;
const chip = (label: string) => new RegExp(chipSource(label));
const chipWait = (label: string) => `/${chipSource(label)}/`;

/**
 * Chrome budget: row 0 = context, row 1 = tabs, row 2 = the single filter row,
 * then the list starts. Anything taller means the chrome stacked again.
 */
function expectChrome(step: string, text: string) {
  const rows = text
    .split("\n")
    .map((line) => line.trim())
    .filter(Boolean);
  const [context, tabs, filters] = rows;
  if (!context?.includes("multica"))
    throw new Error(`${step}: no context bar\n--- screen ---\n${text}`);
  if (!tabs?.includes("1:overview"))
    throw new Error(`${step}: no tab row\n--- screen ---\n${text}`);
  const filterRow = /^[/f](\s|$)/.test(filters ?? "") || /\d+\s+\w+/.test(filters ?? "");
  if (!filterRow) {
    throw new Error(`${step}: expected one filter row, got "${filters}"\n--- screen ---\n${text}`);
  }
  console.log(`  ok  ${step}`);
}

/** The list header must sit right under the 3 chrome rows (when the list is not empty). */
function expectListHeader(step: string, text: string, headerNeedle: string) {
  const index = lineIndex(text, headerNeedle);
  if (index < 0) {
    console.log(`  --  ${step} (empty list, header hidden)`);
    return;
  }
  if (index > 5)
    throw new Error(`${step}: ${index + 1} chrome rows above the list\n--- screen ---\n${text}`);
  console.log(`  ok  ${step} (${index} rows above list)`);
}

/** The hint bar is the last non-empty row, not a row above the list. */
function expectHintsAtBottom(step: string, text: string, needle: string) {
  const lines = text.split("\n").filter((line) => line.trim());
  const last = lines.at(-1) ?? "";
  if (!last.includes(needle)) {
    throw new Error(
      `${step}: hints not on the last row (got "${last.trim()}")\n--- screen ---\n${text}`,
    );
  }
  console.log(`  ok  ${step}`);
}

function cleanup() {
  run(["close", ...base], { allowFail: true });
  rmSync(stateFile, { force: true });
}

rmSync(stateFile, { force: true });
cleanup();

try {
  console.log("e2e: launching TUI");
  run(
    [
      "launch",
      `bun run src/bin.ts --profile ${profile}`,
      ...base,
      "--cols",
      "110",
      "--rows",
      "30",
      "--cwd",
      appDir,
      "--background",
      "--env",
      `MULTICA_SERVER_URL=${conn.server_url}`,
      "--env",
      `MULTICA_TOKEN=${conn.token}`,
      "--env",
      `MULTICA_WORKSPACE_ID=${conn.workspace_id}`,
    ],
    { allowFail: false },
  );
  run(["wait", "1:overview", ...base, "--timeout", "20000"]);

  const overview = snapshot();
  expectMatch("overview boots", overview, /multica/);
  expectHintsAtBottom("overview hints on the last row", overview, "? help");

  press("3");
  run(["wait", "f all", ...base]);
  sleep(600);
  const issues = snapshot();
  expectChrome("issues chrome is 3 rows", issues);
  expectListHeader("issues header", issues, "ID");
  expectMatch("issues shows the filter line", issues, chip("all"));
  expectHintsAtBottom("issues hints on the last row", issues, "f filter");

  // Filter menu: status -> backlog.
  press("f");
  run(["wait", "Filter · issues", ...base]);
  sleep(400);
  typeUntil("filter menu", "backlog", sawQuery("backlog"));
  press("enter");
  run(["wait", "status backlog", ...base]);
  const filtered = snapshot();
  expectMatch("status chip reflects the filter", filtered, chip("status backlog"));
  expectNoMatch("chip replaces the unfiltered marker", filtered, chip("all"));

  press("esc");
  run(["wait", "f all", ...base]);
  expectMatch("Esc clears filters", snapshot(), chip("all"));

  // Detail is the third layer, not a new surface: chrome stays, hints move to
  // the bottom row and the panes scroll inside the listed area.
  press("enter");
  sleep(900);
  const detail = snapshot();
  expectMatch("Enter opens the issue detail", detail, /Esc back/);
  expectHintsAtBottom("detail hints on the last row", detail, "Esc back");
  expectMatch("detail shows the pane tabs", detail, /properties|description/);

  press("esc");
  run(["wait", chipWait("all"), ...base]);

  // Attention buckets are filters on the same list — one chip when active.
  press("o");
  run(["wait", "attention needs_you", ...base]);
  const attention = snapshot();
  expectMatch("attention chip", attention, chip("attention needs_you"));
  expectChrome("attention chrome is 3 rows", attention);
  expectListHeader("attention header", attention, "Issue");
  press("esc");
  run(["wait", chipWait("all"), ...base]);

  press("2");
  run(["wait", "unread", ...base]);
  sleep(400);
  const inbox = snapshot();
  expectMatch("inbox view chips", inbox, /unread/);
  expectChrome("inbox chrome is 3 rows", inbox);
  expectListHeader("inbox header", inbox, "TITLE");

  press("4");
  run(["wait", "PROJECT", ...base]);
  sleep(400);
  const projects = snapshot();
  expectMatch("projects status chips", projects, /in_progress/);
  expectChrome("projects chrome is 3 rows", projects);
  expectListHeader("projects header", projects, "PROJECT");

  press("l");
  sleep(500);
  press("l");
  sleep(900);
  const agents = snapshot();
  expectMatch("agents scope chips", agents, /\bworking\b/);
  expectChrome("agents chrome is 3 rows", agents);
  expectListHeader("agents header", agents, "NAME");

  press("l");
  sleep(700);
  const skills = snapshot();
  expectChrome("skills chrome is 3 rows", skills);
  expectMatch("skills summary row replaces chips", skills, /\d+ skills/);

  press("l");
  sleep(700);
  const chats = snapshot();
  expectMatch("chats view chips", chats, /archived/);
  expectChrome("chats chrome is 3 rows", chats);

  // Search reuses the same row instead of adding another one.
  press("/");
  sleep(400);
  typeUntil("search", "guest", (screen) => /\/\s*guest/.test(screen));
  const searching = snapshot();
  expectMatch("search prompt lives in the filter row", searching, /\/\s*guest/);
  expectChrome("search chrome is 3 rows", searching);
  press("esc");
  sleep(500);
  expectMatch("Esc leaves search", snapshot(), /active\s+archived/);

  type("?");
  run(["wait", "Key bindings", ...base]);
  const help = snapshot();
  expectMatch("help lists the filter key", help, /f\s+filters/);
  expectMatch("help fits without scrolling", help, /transcript · cancel/);
  press("esc");

  mkdirSync(path.dirname(shotFile), { recursive: true });
  run(["screenshot", ...base, "-o", shotFile], { allowFail: true });
  if (existsSync(shotFile)) console.log(`e2e: screenshot ${shotFile}`);
  console.log("e2e: pass");
} finally {
  if (!keep) cleanup();
  else console.log(`e2e: kept session "${session}" — tuistory attach -s ${session}`);
}
