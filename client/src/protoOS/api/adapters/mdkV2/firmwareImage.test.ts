import { describe, expect, it } from "vitest";
import { File } from "node:buffer";
import { createHash } from "node:crypto";
import { firmwareDigest } from "./firmwareDigest";
import { decodeFirmwareMetadata, readFirmwareImage } from "./firmwareImage";
import { firmwareTestImage } from "./firmwareTestFixtures";

describe("F1 firmware image", () => {
  it.each(["prod", "dev", "mfgtest"])("reads %s metadata and validates the file name", async (build) => {
    const bytes = firmwareTestImage(build);
    expect(decodeFirmwareMetadata(bytes)).toEqual({ build, version: "9.0.0" });
    const file = new File([bytes], `f1-app-${build}.bin`) as unknown as globalThis.File;
    expect(await readFirmwareImage(file)).toEqual({ file, build, version: "9.0.0" });
  });
  it("rejects other hardware, unsupported builds, truncated and malformed metadata", () => {
    for (const data of [
      firmwareTestImage("prod", "r2"),
      firmwareTestImage("invalid"),
      new Uint8Array(1029),
      firmwareTestImage().slice(0, 1040),
    ])
      expect(() => decodeFirmwareMetadata(data)).toThrow();
    const bad = firmwareTestImage();
    bad[1030] = 0xff;
    expect(() => decodeFirmwareMetadata(bad)).toThrow();
  });
  it("rejects renamed images before uploading", async () => {
    await expect(
      readFirmwareImage(new File([firmwareTestImage("dev")], "f1-app-prod.bin") as unknown as globalThis.File),
    ).rejects.toThrow();
    await expect(
      readFirmwareImage(new File([firmwareTestImage()], "release.zip") as unknown as globalThis.File),
    ).rejects.toThrow();
  });
  it.each([0, 1, 55, 56, 63, 64, 65, 1000, 1_000_000, 1_048_577])(
    "hashes %i bytes on plain HTTP without SubtleCrypto",
    async (size) => {
      const bytes = Uint8Array.from({ length: size }, (_, i) => i % 251);
      expect(await firmwareDigest(bytes)).toBe(createHash("sha256").update(bytes).digest("hex"));
    },
  );
  it("matches the SHA-256 abc vector and observes cancellation", async () => {
    expect(await firmwareDigest(new TextEncoder().encode("abc"))).toBe(
      "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
    );
    await expect(firmwareDigest(new Uint8Array(1), AbortSignal.abort())).rejects.toThrow();
  });
});
