import type { V2GetContainerMapResponse } from "./generated/generatedApi";

/**
 * Synthetic selection metadata, not a signed deployable firmware image. Bytes outside the metadata repeat with a
 * period (251) that divides no chunk size, so a misplaced or reordered upload chunk changes the reassembled image.
 */
export function firmwareTestImage(build = "prod", hardware = "f1", size = 2048): Uint8Array<ArrayBuffer> {
  const string = (value: string) => [0xa0 | value.length, ...new TextEncoder().encode(value)];
  const metadata = new Uint8Array([
    0x85,
    ...string("build_type"),
    ...string(build),
    ...string("hw_rev"),
    ...string(hardware),
    ...string("ver_major"),
    9,
    ...string("ver_minor"),
    0,
    ...string("ver_patch"),
    0,
  ]);
  const data = Uint8Array.from({ length: size }, (_, i) => i % 251);
  data[1028] = metadata.length & 255;
  data[1029] = metadata.length >>> 8;
  data.set(metadata, 1030);
  return data;
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
