import { describe, expect, test, vi } from "vitest";

import { createMdkV1Definition } from "./mdkV1/definition";
import { createMdkV2Ct1Definition, mdkV2Definition } from "./mdkV2/definition";
import { resolveMinerAdapter } from "./resolveMinerAdapter";
import type { MinerAdapter, MinerAdapterDefinition } from "./types";

const v1Adapter = {} as MinerAdapter;
const ct1Adapter = {} as MinerAdapter;

describe("adapter probe response parsing", () => {
  test("recognizes the documented MDK v2 system-info response", () => {
    const match = mdkV2Definition.probe.parseResponse({
      info: {
        id: "container-1",
        version: "v2 (generated from frigate e87a5197)",
        product: "CT1",
      },
    });

    expect(match).toEqual({
      deviceId: "container-1",
      product: "CT1",
      reportedVersion: "v2 (generated from frigate e87a5197)",
    });
  });

  test("preserves an MDK v2 R2 product", () => {
    expect(
      mdkV2Definition.probe.parseResponse({
        info: { id: "rig-1", version: "v2.0.0", product: "R2" },
      }),
    ).toEqual({ deviceId: "rig-1", product: "R2", reportedVersion: "v2.0.0" });
  });

  test("preserves an unrecognized MDK v2 product", () => {
    expect(
      mdkV2Definition.probe.parseResponse({
        info: { id: "unknown-1", version: "v2.0.0", product: "Frigate" },
      }),
    ).toEqual({
      deviceId: "unknown-1",
      product: "Frigate",
      reportedVersion: "v2.0.0",
    });
  });

  test("rejects a v2 response without the documented info.version field", () => {
    expect(mdkV2Definition.probe.parseResponse({ info: { product: "CT1" } })).toBeUndefined();
  });

  test("recognizes CT1 only in the supported v2 definition", () => {
    const definition = createMdkV2Ct1Definition(() => ct1Adapter);
    expect(definition.probe.parseResponse({ info: { version: "v2", product: "ct1" } })).toEqual({
      deviceId: undefined,
      product: "ct1",
      reportedVersion: "v2",
    });
    expect(definition.probe.parseResponse({ info: { version: "v2", product: "R2" } })).toBeUndefined();
  });

  test("recognizes the MDK v1 system-info envelope", () => {
    const definition = createMdkV1Definition(() => v1Adapter);

    expect(
      definition.probe.parseResponse({
        "system-info": { product_name: "Proto Rig", cb_sn: "rig-1" },
      }),
    ).toEqual({ deviceId: "rig-1", product: "Proto Rig" });
  });
});

describe("resolveMinerAdapter", () => {
  test("probes definitions sequentially and stops at the first supported match", async () => {
    const request = vi
      .fn<(path: string, signal: AbortSignal) => Promise<unknown>>()
      .mockRejectedValueOnce(new Error("v2 endpoint not found"))
      .mockResolvedValueOnce({ "system-info": { product_name: "Proto Rig" } });

    const result = await resolveMinerAdapter(
      [mdkV2Definition, createMdkV1Definition(() => v1Adapter)],
      request,
      new AbortController().signal,
    );

    expect(request.mock.calls.map(([path]) => path)).toEqual(["/api/v2/system/info", "/api/v1/system"]);
    expect(result).toEqual({ status: "ready", adapterId: "mdk-v1", adapter: v1Adapter });
  });

  test("returns the CT1 adapter when the supported MDK v2 product is detected", async () => {
    const request = vi.fn().mockResolvedValue({
      info: { id: "container-1", version: "v2.0.0", product: "CT1" },
    });

    const result = await resolveMinerAdapter(
      [createMdkV2Ct1Definition(() => ct1Adapter), mdkV2Definition, createMdkV1Definition(() => v1Adapter)],
      request,
      new AbortController().signal,
    );

    expect(request).toHaveBeenCalledTimes(1);
    expect(result).toEqual({ status: "ready", adapterId: "mdk-v2", adapter: ct1Adapter });
  });

  test("keeps non-CT1 MDK v2 products unsupported without repeating the shared probe", async () => {
    const request = vi.fn().mockResolvedValue({ info: { version: "v2.0.0", product: "R2" } });

    const result = await resolveMinerAdapter(
      [createMdkV2Ct1Definition(() => ct1Adapter), mdkV2Definition, createMdkV1Definition(() => v1Adapter)],
      request,
      new AbortController().signal,
    );

    expect(request).toHaveBeenCalledTimes(1);
    expect(result).toEqual({
      status: "unsupported",
      adapterId: "mdk-v2",
      adapter: undefined,
      match: {
        deviceId: undefined,
        product: "R2",
        reportedVersion: "v2.0.0",
      },
    });
  });

  test("returns unsupported without an adapter id when no definition matches", async () => {
    const request = vi.fn().mockResolvedValue({ unexpected: true });

    const result = await resolveMinerAdapter(
      [mdkV2Definition, createMdkV1Definition(() => v1Adapter)],
      request,
      new AbortController().signal,
    );

    expect(request).toHaveBeenCalledTimes(2);
    expect(result).toEqual({ status: "unsupported", adapterId: undefined });
  });

  test("returns unreachable when a probe rejects with a non-abort error and nothing matches", async () => {
    const request = vi.fn().mockRejectedValue(new Error("network unreachable"));

    const result = await resolveMinerAdapter(
      [mdkV2Definition, createMdkV1Definition(() => v1Adapter)],
      request,
      new AbortController().signal,
    );

    expect(request).toHaveBeenCalledTimes(2);
    expect(result).toEqual({ status: "unreachable", adapterId: undefined, adapter: undefined });
  });

  test("treats a parseResponse throw as a probe error, resolving to unreachable", async () => {
    const request = vi.fn().mockResolvedValue({ any: "body" });
    const throwingDefinition: MinerAdapterDefinition = {
      id: "mdk-v1",
      displayName: "MDK v1",
      support: "supported",
      probe: {
        path: "/api/v1/system",
        parseResponse: () => {
          throw new Error("parse failure");
        },
      },
      create: () => v1Adapter,
    };

    const result = await resolveMinerAdapter([throwingDefinition], request, new AbortController().signal);

    expect(result).toEqual({ status: "unreachable", adapterId: undefined, adapter: undefined });
  });

  test("treats a create throw as a probe error, resolving to unreachable", async () => {
    const request = vi.fn().mockResolvedValue({ "system-info": { product_name: "Proto Rig" } });

    const result = await resolveMinerAdapter(
      [
        createMdkV1Definition(() => {
          throw new Error("construct failure");
        }),
      ],
      request,
      new AbortController().signal,
    );

    expect(result).toEqual({ status: "unreachable", adapterId: undefined, adapter: undefined });
  });

  test("does not let a shared probe-only fallback hide a supported adapter construction failure", async () => {
    const request = vi.fn().mockResolvedValue({ info: { version: "v2.0.0", product: "CT1" } });

    const result = await resolveMinerAdapter(
      [
        createMdkV2Ct1Definition(() => {
          throw new Error("construct failure");
        }),
        mdkV2Definition,
        createMdkV1Definition(() => v1Adapter),
      ],
      request,
      new AbortController().signal,
    );

    expect(request.mock.calls.map(([path]) => path)).toEqual(["/api/v2/system/info", "/api/v1/system"]);
    expect(result).toEqual({ status: "unreachable", adapterId: undefined, adapter: undefined });
  });

  test("rethrows and does not swallow an aborted probe", async () => {
    const controller = new AbortController();
    const request = vi.fn(async () => {
      controller.abort();
      throw new DOMException("Aborted", "AbortError");
    });

    await expect(
      resolveMinerAdapter([createMdkV1Definition(() => v1Adapter)], request, controller.signal),
    ).rejects.toThrow("Aborted");
  });
});
