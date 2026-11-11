import type { CT1FirmwareImage } from "../ct1Firmware";

const invalid = () =>
  new Error("Choose a valid F1 firmware image: f1-app-prod.bin, f1-app-dev.bin, or f1-app-mfgtest.bin.");

// Read only hbmeta's bounded msgpack map. Unknown fields are skipped, including
// nested containers and binary/extension values; they are never materialized.
export function decodeFirmwareMetadata(bytes: Uint8Array): Omit<CT1FirmwareImage, "file"> {
  if (bytes.length < 1030) throw invalid();
  const length = bytes[1028] | (bytes[1029] << 8);
  if (!length || length > bytes.length - 1030) throw invalid();
  const data = new DataView(bytes.buffer, bytes.byteOffset + 1030, length);
  let offset = 0;
  const take = (n: number) => {
    if (n < 0 || n > length - offset) throw invalid();
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
    if (value > BigInt(Number.MAX_SAFE_INTEGER)) throw invalid();
    return Number(value);
  };
  const string = (tag: number) => {
    const size =
      (tag & 0xe0) === 0xa0 ? tag & 31 : tag === 0xd9 ? uint(1) : tag === 0xda ? uint(2) : tag === 0xdb ? uint(4) : -1;
    const at = take(size);
    return new TextDecoder("utf-8", { fatal: true }).decode(new Uint8Array(data.buffer, data.byteOffset + at, size));
  };
  const skip = (tag: number, depth = 0): void => {
    if (depth > 16) throw invalid();
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
      if (!variable[tag]) throw invalid();
      take(uint(variable[tag]) + (tag >= 0xc7 && tag <= 0xc9 ? 1 : 0));
      return;
    }
    if (count > length - offset) throw invalid();
    for (let i = 0; i < count; i++) skip(uint(1), depth + 1);
  };
  const tag = uint(1);
  const count = (tag & 0xf0) === 0x80 ? tag & 15 : tag === 0xde ? uint(2) : tag === 0xdf ? uint(4) : -1;
  if (count < 0 || count > length / 2) throw invalid();
  const values = new Map<string, string | number>();
  for (let i = 0; i < count; i++) {
    const key = string(uint(1));
    const valueTag = uint(1);
    if (["hw_rev", "build_type", "ver_major", "ver_minor", "ver_patch"].includes(key)) {
      if (values.has(key)) throw invalid();
      if (key === "hw_rev" || key === "build_type") values.set(key, string(valueTag).trim().toLowerCase());
      else {
        const size = { 0xcc: 1, 0xcd: 2, 0xce: 4, 0xcf: 8 }[valueTag];
        const value = valueTag < 0x80 ? valueTag : size ? uint(size) : -1;
        if (value < 0 || value > 0xffffffff) throw invalid();
        values.set(key, value);
      }
    } else skip(valueTag);
  }
  const build = values.get("build_type");
  if (
    offset !== length ||
    values.get("hw_rev") !== "f1" ||
    (build !== "prod" && build !== "dev" && build !== "mfgtest") ||
    !["ver_major", "ver_minor", "ver_patch"].every((key) => values.has(key))
  )
    throw invalid();
  return { build, version: ["ver_major", "ver_minor", "ver_patch"].map((key) => values.get(key)).join(".") };
}

export async function readFirmwareImage(file: File): Promise<CT1FirmwareImage> {
  if (!/^f1-app-(prod|dev|mfgtest)\.bin$/.test(file.name) || file.size < 1030 || file.size > 64 * 1024 * 1024)
    throw invalid();
  const metadata = decodeFirmwareMetadata(new Uint8Array(await file.slice(0, 1030 + 65535).arrayBuffer()));
  if (file.name !== `f1-app-${metadata.build}.bin`) throw invalid();
  return { file, ...metadata };
}
