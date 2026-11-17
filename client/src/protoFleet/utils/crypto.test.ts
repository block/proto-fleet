import { afterEach, describe, expect, it, vi } from "vitest";
import { computeSha256, createOperationID, sha256Hex } from "./crypto";

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

  afterEach(() => vi.unstubAllGlobals());
  it.each([
    ["", "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"],
    ["abc", "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"],
    ["a".repeat(1000000), "cdc76e5c9914fb9281a1c7e284d73e67f1809a48a497200e046d39ccc7112cd0"],
  ])("verifies standard SHA-256 vectors without WebCrypto", async (content, expected) => {
    vi.stubGlobal("crypto", {});
    expect(await sha256Hex(new TextEncoder().encode(content))).toBe(expected);
  });
  it("creates UUIDs when randomUUID is unavailable", () => {
    const getRandomValues = vi.fn((bytes: Uint8Array) => bytes.fill(0xff));
    vi.stubGlobal("crypto", { getRandomValues });
    expect(createOperationID()).toBe("ffffffff-ffff-4fff-bfff-ffffffffffff");
    expect(getRandomValues).toHaveBeenCalledOnce();
  });
});
