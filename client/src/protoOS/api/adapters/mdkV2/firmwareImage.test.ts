import { describe, expect, it } from "vitest";
import { Buffer, File } from "node:buffer";
import { createHash } from "node:crypto";
import { firmwareDigest } from "./firmwareDigest";
import { decodeFirmwareMetadata, readFirmwareImage } from "./firmwareImage";
import { firmwareTestImage, firmwareTestImageWithMetadata, firmwareTestString } from "./firmwareTestFixtures";

const string = firmwareTestString;
const text = (value: string) => [...new TextEncoder().encode(value)];
const selected = (bytes: Uint8Array<ArrayBuffer>, name: string) =>
  new File([bytes], name) as unknown as globalThis.File;
/** A metadata map of the five known fields, each given as its already-encoded msgpack value. */
const metadata = (
  {
    build = string("prod"),
    hardware = string("f1"),
    major = [9],
    minor = [0],
    patch = [0],
  }: Partial<Record<"build" | "hardware" | "major" | "minor" | "patch", number[]>> = {},
  extra: number[][] = [],
) => [
  0x85 + extra.length,
  ...extra.flat(),
  ...string("build_type"),
  ...build,
  ...string("hw_rev"),
  ...hardware,
  ...string("ver_major"),
  ...major,
  ...string("ver_minor"),
  ...minor,
  ...string("ver_patch"),
  ...patch,
];
const decode = (payload: number[]) => decodeFirmwareMetadata(firmwareTestImageWithMetadata(payload));
const f1 = { hardwareRevision: "f1", build: "prod", version: "9.0.0" };

describe("hashboard firmware image metadata", () => {
  it.each(["prod", "dev", "mfgtest"] as const)("reads %s metadata for any hardware revision", (build) => {
    expect(decodeFirmwareMetadata(firmwareTestImage(build))).toEqual({ ...f1, build });
    expect(decodeFirmwareMetadata(firmwareTestImage(build, "r2"))).toEqual({ ...f1, build, hardwareRevision: "r2" });
  });
  it("reads the metadata Fleet's Go encoder writes", () => {
    // firmwaretest.Image(t, "prod", "f1", 9) in sdk/mdk/firmware/firmwaretest: versions are four-byte integers.
    const golden =
      "85aa6275696c645f74797065a470726f64a668775f726576a26631a97665725f6d616a6f72ce00000009" +
      "a97665725f6d696e6f72ce00000000a97665725f7061746368ce00000000";
    expect(decode([...Buffer.from(golden, "hex")])).toEqual(f1);
  });
  it.each([
    ["one byte", [0xcc, 200], "200"],
    ["two bytes", [0xcd, 0x01, 0x2c], "300"],
    ["four bytes", [0xce, 0x00, 0x01, 0x11, 0x70], "70000"],
    ["eight bytes", [0xcf, 0, 0, 0, 0, 0xff, 0xff, 0xff, 0xff], "4294967295"],
  ])("reads a version number encoded in %s", (_width, encoded, value) => {
    expect(decode(metadata({ major: encoded })).version).toBe(`${value}.0.0`);
    expect(decode(metadata({ patch: encoded })).version).toBe(`9.0.${value}`);
  });
  it("reads a map with a two-byte length and strings with one- and two-byte lengths", () => {
    expect(decode([0xde, 0x00, 0x05, ...metadata().slice(1)])).toEqual(f1);
    expect(decode(metadata({ build: [0xd9, 6, ...text(" PROD ")], hardware: [0xda, 0, 2, ...text("F1")] }))).toEqual(
      f1,
    );
  });
  it("skips fields it does not know, whatever their type", () => {
    const unknown = [
      [...string("nested"), 0x82, ...string("a"), 0x91, 0x01, ...string("b"), 0xc0],
      [...string("list"), 0xdc, 0x00, 0x02, 0xc3, 0xff],
      [...string("binary"), 0xc4, 0x03, 1, 2, 3],
      [...string("extension"), 0xd4, 0x05, 0xaa],
      [...string("sized_extension"), 0xc7, 0x02, 0x05, 0xaa, 0xbb],
      [...string("float"), 0xcb, 0, 0, 0, 0, 0, 0, 0, 0],
      [...string("signed"), 0xd1, 0xff, 0xfe],
      [...string("long_text"), 0xd9, 40, ...new Array<number>(40).fill(0x78)],
    ];
    expect(decode(metadata({}, unknown))).toEqual(f1);
    // An unknown field between known ones is skipped the same way.
    const [header, ...fields] = metadata();
    const afterBuild = string("build_type").length + string("prod").length;
    expect(decode([header + 1, ...fields.slice(0, afterBuild), ...unknown[0], ...fields.slice(afterBuild)])).toEqual(
      f1,
    );
  });
  it.each<[string, number[]]>([
    ["a known field given twice", metadata({}, [[...string("hw_rev"), ...string("r2")]])],
    ["bytes after the map", [...metadata(), 0x00]],
    ["a version number above 32 bits", metadata({ major: [0xcf, 0, 0, 0, 1, 0, 0, 0, 0] })],
    ["a negative version number", metadata({ minor: [0xff] })],
    ["a version number that is text", metadata({ patch: string("1") })],
    ["an empty hardware revision", metadata({ hardware: string("  ") })],
    ["a build that is not released", metadata({ build: string("debug") })],
    ["a field name that is not text", [0x81, 0x01, 0x01]],
    [
      "unknown values nested deeper than 16 levels",
      metadata({}, [[...string("deep"), ...new Array<number>(18).fill(0x91), 0]]),
    ],
    ["an unknown value longer than the metadata", metadata({}, [[...string("big"), 0xc6, 0xff, 0xff, 0xff, 0xff]])],
    ["an unknown value of a reserved type", metadata({}, [[...string("reserved"), 0xc1]])],
    ["a map that declares more fields than it holds", [0x8f, ...metadata().slice(1)]],
    ["a missing version field", [0x84, ...metadata().slice(1, -(string("ver_patch").length + 1))]],
  ])("rejects metadata with %s", (_case, payload) => {
    expect(() => decode(payload)).toThrow("metadata");
  });
  it("rejects truncated and malformed images", () => {
    for (const data of [new Uint8Array(1029), firmwareTestImage().slice(0, 1040)])
      expect(() => decodeFirmwareMetadata(data)).toThrow("metadata");
    const bad = firmwareTestImage();
    bad[1030] = 0xff;
    expect(() => decodeFirmwareMetadata(bad)).toThrow("metadata");
  });
});

describe("hashboard firmware image selection", () => {
  const accepted = "Choose a valid F1 firmware image: f1-app-prod.bin, f1-app-dev.bin, or f1-app-mfgtest.bin.";
  it.each(["prod", "dev", "mfgtest"] as const)("accepts an F1 %s image under its release name", async (build) => {
    const file = selected(firmwareTestImage(build), `f1-app-${build}.bin`);
    expect(await readFirmwareImage(file, "f1")).toEqual({ file, build, version: "9.0.0" });
  });
  it.each([
    ["an image renamed to another build", firmwareTestImage("dev"), "f1-app-prod.bin"],
    ["an image for other hardware under an F1 name", firmwareTestImage("prod", "r2"), "f1-app-prod.bin"],
    ["a release archive", firmwareTestImage(), "release.zip"],
    ["a build that is not released", firmwareTestImage("invalid"), "f1-app-invalid.bin"],
    [
      "metadata it cannot read",
      firmwareTestImageWithMetadata([0x81, ...string("hw_rev"), 0xa1, 0xff]),
      "f1-app-prod.bin",
    ],
    ["a file too short to hold metadata", new Uint8Array(1029), "f1-app-prod.bin"],
  ])("refuses %s, naming the files it accepts", async (_case, bytes, name) => {
    await expect(readFirmwareImage(selected(bytes, name), "f1")).rejects.toThrow(accepted);
  });
  it("applies the same rule to whichever hardware revision it is asked for", async () => {
    const file = selected(firmwareTestImage("dev", "r2"), "r2-app-dev.bin");
    expect(await readFirmwareImage(file, "r2")).toEqual({ file, build: "dev", version: "9.0.0" });
    await expect(readFirmwareImage(file, "f1")).rejects.toThrow(accepted);
    await expect(readFirmwareImage(selected(firmwareTestImage(), "f1-app-prod.bin"), "r2")).rejects.toThrow(
      "Choose a valid R2 firmware image: r2-app-prod.bin, r2-app-dev.bin, or r2-app-mfgtest.bin.",
    );
  });
});

describe("firmware digest", () => {
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
