// Client analytics is intentionally inert. There is no hosted product
// analytics service; callers keep the same functions so login, error
// boundaries, and the freeze watchdog do not need a second code path.

export const EVENT_SCHEMA_VERSION = 2;

export interface AnalyticsConfig {
  key: string;
  host: string;
  appVersion?: string;
  environment?: string;
}

export type ClientType = "desktop" | "web";

export function detectClientType(): ClientType {
  if (typeof window === "undefined") return "web";
  const w = window as unknown as { electron?: unknown; desktopAPI?: unknown };
  if (w.electron || w.desktopAPI) return "desktop";
  if (typeof navigator !== "undefined" && /Electron/i.test(navigator.userAgent)) {
    return "desktop";
  }
  return "web";
}

/** Always disabled. Returns false so callers do not wait on a collector. */
export function initAnalytics(_config: AnalyticsConfig | null | undefined): boolean {
  return false;
}

export function identify(_userId: string, _userProperties?: Record<string, unknown>): void {}

export function resetAnalytics(): void {}

export interface CaptureEventOptions {
  sendInstantly?: boolean;
  onCaptured?: () => void;
}

export function captureEvent(
  _name: string,
  _props?: Record<string, unknown>,
  _options?: CaptureEventOptions,
): void {}

export function captureException(_error: unknown, _props?: Record<string, unknown>): void {}

export function setPersonProperties(_props: Record<string, unknown>): void {}

export function captureSignupSource(): void {}
