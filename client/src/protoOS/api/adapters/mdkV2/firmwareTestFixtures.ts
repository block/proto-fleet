import type { V2GetContainerMapResponse } from "./generated/generatedApi";

/** A msgpack fixstr, for values of up to 31 bytes. */
export const firmwareTestString = (value: string) => [0xa0 | value.length, ...new TextEncoder().encode(value)];

/**
 * An image whose hbmeta payload is exactly `metadata`. Bytes outside the metadata repeat with a period (251) that
 * divides no chunk size, so a misplaced or reordered upload chunk changes the reassembled image.
 */
export function firmwareTestImageWithMetadata(metadata: ArrayLike<number>, size = 2048): Uint8Array<ArrayBuffer> {
  const data = Uint8Array.from({ length: Math.max(size, 1030 + metadata.length) }, (_, i) => i % 251);
  data[1028] = metadata.length & 255;
  data[1029] = metadata.length >>> 8;
  data.set(metadata, 1030);
  return data;
}

/** Synthetic selection metadata, not a signed deployable firmware image. */
export function firmwareTestImage(build = "prod", hardware = "f1", size = 2048): Uint8Array<ArrayBuffer> {
  return firmwareTestImageWithMetadata(
    [
      0x85,
      ...firmwareTestString("build_type"),
      ...firmwareTestString(build),
      ...firmwareTestString("hw_rev"),
      ...firmwareTestString(hardware),
      ...firmwareTestString("ver_major"),
      9,
      ...firmwareTestString("ver_minor"),
      0,
      ...firmwareTestString("ver_patch"),
      0,
    ],
    size,
  );
}

export function firmwareTestMap(): V2GetContainerMapResponse {
  return {
    containerMap: {
      tanks: Array.from({ length: 6 }, (_, tank) => ({
        id: tank + 1,
        moduleLayout: { rows: 1, columns: 1 },
        modules: [
          {
            id: 1,
            slots: Array.from({ length: 3 }, (_, slot) => ({
              occupied: true,
              configuredLocation: { hashboardPosition: slot + 1 },
              hashboard: {
                macAddress: `02:00:00:00:0${tank}:0${slot}`,
                ownerServerId: tank < 3 ? "owner-a" : "owner-b",
                location: { hashboardPosition: slot + 1 },
              },
            })),
          },
        ],
      })),
    },
  };
}
