import { describe, expect, it } from "vitest";
import { getMinerWebUiUrl } from "./minerWebUiUrl";

describe("getMinerWebUiUrl", () => {
  it("uses the displayed IP address on HTTP port 80 when both IP and URL are present", () => {
    expect(getMinerWebUiUrl({ ipAddress: "192.168.1.100", url: "http://192.168.1.100:4028" })).toBe(
      "http://192.168.1.100",
    );
  });

  it("falls back to the URL host and strips any port", () => {
    expect(getMinerWebUiUrl({ url: "https://miner.local:8080/status" })).toBe("http://miner.local");
  });

  it("returns undefined when neither a usable IP nor URL is available", () => {
    expect(getMinerWebUiUrl({ ipAddress: "", url: "" })).toBeUndefined();
    expect(getMinerWebUiUrl({ url: "not-a-url" })).toBeUndefined();
  });
});
