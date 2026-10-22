// @vitest-environment node
import { describe, expect, it } from "vitest";
import { messagesForSource } from "./i18n-guard.mjs";

// These fixtures ARE the specification of the guard. The rule's first
// iteration matched only a bare literal child, so every "indirect" case
// below reached production untranslated while lint stayed green. The oxlint
// plugin reports whatever this function returns.

function lint(code: string): string[] {
  return messagesForSource(code);
}

function flags(code: string): boolean {
  return lint(code).length > 0;
}

describe("no untranslated attribute copy", () => {
  it.each([
    ['direct literal', '<input placeholder="Search issues" />'],
    ['title', '<span title="Copy link" />'],
    ['aria-label', '<input aria-label="Select all sub-issues" />'],
    ['expression container', '<input title={"Expression copy"} />'],
    ['template literal', '<input aria-label={`Select ${id}`} />'],
    ['interpolated template', '<input aria-label={`${label} key ${i}`} />'],
  ])("flags %s", (_name, code) => {
    expect(flags(code)).toBe(true);
  });

  it.each([
    ['translated call', '<input placeholder={t(($) => $.search)} />'],
    ['URL literal', '<a title="https://example.com" />'],
    ['URL template', '<a title={`https://example.com/${id}`} />'],
    // Pure interpolation of already-translated values: no static English.
    ['no static letters', '<span aria-label={`${a}: ${b}`} />'],
    // Only the three copy-bearing attributes are guarded.
    ['unguarded attribute', '<div className="flex items-center" />'],
    ['numeric literal', '<input placeholder="0" />'],
  ])("allows %s", (_name, code) => {
    expect(flags(code)).toBe(false);
  });
});

describe("no untranslated toast copy", () => {
  it.each([
    ['bare toast', 'toast("Saved");'],
    ['toast.error', 'toast.error("Failed to save");'],
    ['toast.success', 'toast.success("Workspace created");'],
    ['template literal', 'toast.error(`Failed to save`);'],
    // The shape that shipped untranslated in source-backfill-modal.tsx.
    [
      'ternary fallback',
      'toast.error(err instanceof Error ? err.message : "Failed to save");',
    ],
    ['logical fallback', 'toast.error(msg || "Something went wrong");'],
    ['nullish fallback', 'toast.error(msg ?? "Something went wrong");'],
    ['guarded literal', 'toast.error(shouldWarn && "Check your input");'],
  ])("flags %s", (_name, code) => {
    expect(flags(code)).toBe(true);
  });

  it.each([
    ['translated call', 'toast.error(t(($) => $.save_failed));'],
    ['translated ternary', 'toast.error(a ? t(($) => $.x) : t(($) => $.y));'],
    // A comparison in the ternary test is not copy.
    ['comparison test', 'toast.error(code === "E_LIMIT" ? t(x) : t(y));'],
    ['non-toast callee', 'logger.error("Failed to save");'],
  ])("allows %s", (_name, code) => {
    expect(flags(code)).toBe(false);
  });
});

it("names the offending door in the message", () => {
  expect(lint('<input aria-label="Copy" />')[0]).toContain("useT()");
  expect(lint('toast.error("Copy");')[0]).toContain("useT()");
});
