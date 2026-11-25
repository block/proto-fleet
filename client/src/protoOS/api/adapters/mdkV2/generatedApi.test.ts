import { describe, expect, it } from "vitest";

import { Api } from "./generated/generatedApi";

type ChunkWrite = (api: Api<unknown>, data: string) => Promise<unknown>;

const chunkWrites: [string, ChunkWrite][] = [
  [
    "hashboard firmware",
    (api, data) =>
      api.api.firmwareServiceWriteFirmwareArtifactUploadChunk(
        { uploadId: "upload-1" },
        { imageIndex: 0, offset: "0", data },
      ),
  ],
  [
    "PSU capsule",
    (api, data) =>
      api.api.psuFirmwareServiceWritePsuFirmwareCapsuleUploadChunk({ uploadId: "upload-1" }, { offset: "0", data }),
  ],
];

describe("generated MDK v2 REST client", () => {
  it.each(chunkWrites)("sends %s chunk bytes as a base64 JSON string", async (_name, write) => {
    const bodies: unknown[] = [];
    const api = new Api({
      customFetch: async (_input, init) => {
        bodies.push(JSON.parse(String(init?.body)));
        return new Response("{}", { status: 200, headers: { "Content-Type": "application/json" } });
      },
    });

    await write(api, "AAEC/w==");

    expect(bodies).toEqual([expect.objectContaining({ data: "AAEC/w==" })]);
  });
});
