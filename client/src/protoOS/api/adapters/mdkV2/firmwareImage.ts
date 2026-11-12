import {
  type HashboardFirmwareBuild,
  hashboardFirmwareBuilds,
  type HashboardFirmwareImage,
} from "../hashboardFirmware";

/** What an image's embedded hbmeta records, whichever hashboard hardware the image is built for. */
export interface FirmwareMetadata {
  hardwareRevision: string;
  build: HashboardFirmwareBuild;
  version: string;
}

// hbmeta follows the 1 KiB vector table: a six-byte header ending in a little-endian length, then a msgpack map.
const metadataOffset = 1030;
const maxMetadataBytes = 0xffff;
const maxImageBytes = 64 * 1024 * 1024;

const invalidMetadata = () => new Error("The firmware image metadata is missing or malformed.");

// Read only hbmeta's bounded msgpack map. Unknown fields are skipped, including
// nested containers and binary/extension values; they are never materialized.
// Which hardware an image may be installed on is the caller's rule, as in Fleet's DecodeMetadata.
export function decodeFirmwareMetadata(bytes: Uint8Array): FirmwareMetadata {
  if (bytes.length < metadataOffset) throw invalidMetadata();
  const length = bytes[metadataOffset - 2] | (bytes[metadataOffset - 1] << 8);
  if (!length || length > bytes.length - metadataOffset) throw invalidMetadata();
  const data = new DataView(bytes.buffer, bytes.byteOffset + metadataOffset, length);
  let offset = 0;
  const take = (n: number) => {
    if (n < 0 || n > length - offset) throw invalidMetadata();
    const start = offset;
    offset += n;
    return start;
  };
  const uint = (n: number): number => {
    const at = take(n);
    if (n === 1) return data.getUint8(at);
    if (n === 2) return data.getUint16(at);
    if (n === 4) return data.getUint32(at);
    const value = data.getBigUint64(at);
    if (value > BigInt(Number.MAX_SAFE_INTEGER)) throw invalidMetadata();
    return Number(value);
  };
  const string = (tag: number) => {
    const size =
      (tag & 0xe0) === 0xa0 ? tag & 31 : tag === 0xd9 ? uint(1) : tag === 0xda ? uint(2) : tag === 0xdb ? uint(4) : -1;
    const at = take(size);
    return new TextDecoder("utf-8", { fatal: true }).decode(new Uint8Array(data.buffer, data.byteOffset + at, size));
  };
  const skip = (tag: number, depth = 0): void => {
    if (depth > 16) throw invalidMetadata();
    if (tag < 0x80 || tag >= 0xe0 || [0xc0, 0xc2, 0xc3].includes(tag)) return;
    if ((tag & 0xe0) === 0xa0) {
      take(tag & 31);
      return;
    }
    let count: number;
    if ((tag & 0xf0) === 0x80) count = (tag & 15) * 2;
    else if ((tag & 0xf0) === 0x90) count = tag & 15;
    else if (tag === 0xdc || tag === 0xdd) count = uint(tag === 0xdc ? 2 : 4);
    else if (tag === 0xde || tag === 0xdf) count = uint(tag === 0xde ? 2 : 4) * 2;
    else {
      const sizes: Record<number, number> = {
        0xca: 4,
        0xcb: 8,
        0xcc: 1,
        0xcd: 2,
        0xce: 4,
        0xcf: 8,
        0xd0: 1,
        0xd1: 2,
        0xd2: 4,
        0xd3: 8,
        0xd4: 2,
        0xd5: 3,
        0xd6: 5,
        0xd7: 9,
        0xd8: 17,
      };
      if (sizes[tag]) {
        take(sizes[tag]);
        return;
      }
      const variable: Record<number, number> = {
        0xc4: 1,
        0xc5: 2,
        0xc6: 4,
        0xc7: 1,
        0xc8: 2,
        0xc9: 4,
        0xd9: 1,
        0xda: 2,
        0xdb: 4,
      };
      if (!variable[tag]) throw invalidMetadata();
      take(uint(variable[tag]) + (tag >= 0xc7 && tag <= 0xc9 ? 1 : 0));
      return;
    }
    if (count > length - offset) throw invalidMetadata();
    for (let i = 0; i < count; i++) skip(uint(1), depth + 1);
  };
  const tag = uint(1);
  const count = (tag & 0xf0) === 0x80 ? tag & 15 : tag === 0xde ? uint(2) : tag === 0xdf ? uint(4) : -1;
  if (count < 0 || count > length / 2) throw invalidMetadata();
  const versionKeys = ["ver_major", "ver_minor", "ver_patch"];
  const values = new Map<string, string | number>();
  for (let i = 0; i < count; i++) {
    const key = string(uint(1));
    const valueTag = uint(1);
    if (key === "hw_rev" || key === "build_type" || versionKeys.includes(key)) {
      if (values.has(key)) throw invalidMetadata();
      if (key === "hw_rev" || key === "build_type") values.set(key, string(valueTag).trim().toLowerCase());
      else {
        const size = { 0xcc: 1, 0xcd: 2, 0xce: 4, 0xcf: 8 }[valueTag];
        const value = valueTag < 0x80 ? valueTag : size ? uint(size) : -1;
        if (value < 0 || value > 0xffffffff) throw invalidMetadata();
        values.set(key, value);
      }
    } else skip(valueTag);
  }
  const hardwareRevision = values.get("hw_rev");
  const build = hashboardFirmwareBuilds.find((candidate) => candidate === values.get("build_type"));
  if (
    offset !== length ||
    typeof hardwareRevision !== "string" ||
    !hardwareRevision ||
    !build ||
    !versionKeys.every((key) => values.has(key))
  )
    throw invalidMetadata();
  return { hardwareRevision, build, version: versionKeys.map((key) => values.get(key)).join(".") };
}

const imageName = (hardwareRevision: string, build: HashboardFirmwareBuild) => `${hardwareRevision}-app-${build}.bin`;

/**
 * Reads a release image for one hashboard hardware revision. The file must be named `<revision>-app-<build>.bin`
 * and its embedded metadata must name the same revision and build. Every refusal lists the file names accepted.
 */
export async function readFirmwareImage(file: File, hardwareRevision: string): Promise<HashboardFirmwareImage> {
  const names = hashboardFirmwareBuilds.map((build) => imageName(hardwareRevision, build));
  const invalid = () =>
    new Error(
      `Choose a valid ${hardwareRevision.toUpperCase()} firmware image: ${names.slice(0, -1).join(", ")}, or ${names[names.length - 1]}.`,
    );
  if (!names.includes(file.name) || file.size < metadataOffset || file.size > maxImageBytes) throw invalid();
  const header = new Uint8Array(await file.slice(0, metadataOffset + maxMetadataBytes).arrayBuffer());
  let metadata: FirmwareMetadata;
  try {
    metadata = decodeFirmwareMetadata(header);
  } catch {
    throw invalid();
  }
  if (metadata.hardwareRevision !== hardwareRevision || file.name !== imageName(hardwareRevision, metadata.build))
    throw invalid();
  return { file, build: metadata.build, version: metadata.version };
}
