// @vitest-environment node
import { describe, expect, it } from "vitest";
import { detectClientType, initAnalytics, captureEvent } from "./index";

describe("analytics", () => {
  it("stays disabled without a collector", () => {
    expect(initAnalytics({ key: "k", host: "" })).toBe(false);
    expect(() => captureEvent("signup")).not.toThrow();
  });

  it("classifies a browser without an electron bridge as web", () => {
    expect(detectClientType()).toBe("web");
  });
});
