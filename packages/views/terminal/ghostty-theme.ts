import type { ITheme } from "@xterm/xterm";

/** Ghostty bundled theme `TokyoNight Night`. */
export const TOKYO_NIGHT_NIGHT: ITheme = {
  background: "#1a1b26",
  foreground: "#c0caf5",
  cursor: "#c0caf5",
  cursorAccent: "#1a1b26",
  selectionBackground: "#283457",
  selectionForeground: "#c0caf5",
  black: "#15161e",
  red: "#f7768e",
  green: "#9ece6a",
  yellow: "#e0af68",
  blue: "#7aa2f7",
  magenta: "#bb9af7",
  cyan: "#7dcfff",
  white: "#a9b1d6",
  brightBlack: "#414868",
  brightRed: "#f7768e",
  brightGreen: "#9ece6a",
  brightYellow: "#e0af68",
  brightBlue: "#7aa2f7",
  brightMagenta: "#bb9af7",
  brightCyan: "#7dcfff",
  brightWhite: "#c0caf5",
};

/** Ghostty bundled theme `TokyoNight Day`. */
export const TOKYO_NIGHT_DAY: ITheme = {
  background: "#e1e2e7",
  foreground: "#3760bf",
  cursor: "#3760bf",
  cursorAccent: "#e1e2e7",
  selectionBackground: "#99a7df",
  selectionForeground: "#3760bf",
  black: "#e9e9ed",
  red: "#f52a65",
  green: "#587539",
  yellow: "#8c6c3e",
  blue: "#2e7de9",
  magenta: "#9854f1",
  cyan: "#007197",
  white: "#6172b0",
  brightBlack: "#a1a6c5",
  brightRed: "#f52a65",
  brightGreen: "#587539",
  brightYellow: "#8c6c3e",
  brightBlue: "#2e7de9",
  brightMagenta: "#9854f1",
  brightCyan: "#007197",
  brightWhite: "#3760bf",
};

export const GHOSTTY_FONT_FAMILY =
  '"Berkeley Mono", "Symbols Nerd Font Mono", ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace';

export const GHOSTTY_FONT_SIZE = 16;

export function tokyoNightTheme(mode: "light" | "dark" | undefined): ITheme {
  return mode === "light" ? TOKYO_NIGHT_DAY : TOKYO_NIGHT_NIGHT;
}
