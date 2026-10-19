import { describe, expect, it } from "vitest";

import { providerSupportsMcpConfig } from "./mcp-support";

describe("providerSupportsMcpConfig", () => {
  it("accepts a provider whose runtime consumes mcp_config", () => {
    expect(providerSupportsMcpConfig("claude")).toBe(true);
  });
  it("rejects providers whose runtime ignores mcp_config", () => {
    expect(providerSupportsMcpConfig("pi")).toBe(true);
    expect(providerSupportsMcpConfig("yielded")).toBe(true);
    expect(providerSupportsMcpConfig("gbot")).toBe(false);
    expect(providerSupportsMcpConfig(undefined)).toBe(false);
    expect(providerSupportsMcpConfig(null)).toBe(false);
  });
});
