import { describe, expect, it } from "vitest";
import { computeSha256 } from "./crypto";

describe("computeSha256", () => {
  it("returns a 64-character lowercase hex string", async () => {
    const file = new File(["hello"], "test.swu");
    const hash = await computeSha256(file);

    expect(hash).toMatch(/^[0-9a-f]{64}$/);
  });

  it("returns consistent hash for same content", async () => {
    const file1 = new File(["same content"], "a.swu");
    const file2 = new File(["same content"], "b.swu");

    const hash1 = await computeSha256(file1);
    const hash2 = await computeSha256(file2);

    expect(hash1).toBe(hash2);
  });

  it("returns different hash for different content", async () => {
    const file1 = new File(["content a"], "a.swu");
    const file2 = new File(["content b"], "b.swu");

    const hash1 = await computeSha256(file1);
    const hash2 = await computeSha256(file2);

    expect(hash1).not.toBe(hash2);
  });

  it("throws a clear error when crypto.subtle is unavailable", async () => {
    const originalCrypto = globalThis.crypto;
    Object.defineProperty(globalThis, "crypto", { value: {}, writable: true, configurable: true });

    const file = new File(["data"], "firmware.swu");
    await expect(computeSha256(file)).rejects.toThrow("requires a secure context");

    Object.defineProperty(globalThis, "crypto", { value: originalCrypto, writable: true, configurable: true });
  });
});
