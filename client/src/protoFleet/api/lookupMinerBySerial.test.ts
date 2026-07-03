import { beforeEach, describe, expect, it, vi } from "vitest";
import { Code, ConnectError } from "@connectrpc/connect";

import { lookupMinerBySerial } from "./lookupMinerBySerial";

const mockLookupMinerBySerialNumber = vi.fn();

vi.mock("@/protoFleet/api/clients", () => ({
  fleetManagementClient: {
    lookupMinerBySerialNumber: (...args: unknown[]) => mockLookupMinerBySerialNumber(...args),
  },
}));

describe("lookupMinerBySerial", () => {
  beforeEach(() => {
    mockLookupMinerBySerialNumber.mockReset();
  });

  it("short-circuits to notFound for an empty serial without an RPC", async () => {
    const result = await lookupMinerBySerial("");

    expect(result).toEqual({ status: "notFound" });
    expect(mockLookupMinerBySerialNumber).not.toHaveBeenCalled();
  });

  it("returns the snapshot on a hit", async () => {
    const snapshot = { deviceIdentifier: "d1", serialNumber: "SN1" };
    mockLookupMinerBySerialNumber.mockResolvedValueOnce({ snapshot });

    const result = await lookupMinerBySerial("SN1");

    expect(result).toEqual({ status: "found", snapshot });
    expect(mockLookupMinerBySerialNumber).toHaveBeenCalledWith({ serialNumber: "SN1" }, { signal: undefined });
  });

  it("maps a missing snapshot in an otherwise-successful response to notFound", async () => {
    mockLookupMinerBySerialNumber.mockResolvedValueOnce({ snapshot: undefined });

    const result = await lookupMinerBySerial("SN1");

    expect(result).toEqual({ status: "notFound" });
  });

  it("maps a NotFound ConnectError to notFound", async () => {
    mockLookupMinerBySerialNumber.mockRejectedValueOnce(new ConnectError("nope", Code.NotFound));

    const result = await lookupMinerBySerial("SN-MISSING");

    expect(result).toEqual({ status: "notFound" });
  });

  it("maps other errors to an error result with a message", async () => {
    mockLookupMinerBySerialNumber.mockRejectedValueOnce(new ConnectError("boom", Code.Internal));

    const result = await lookupMinerBySerial("SN1");

    expect(result.status).toBe("error");
    if (result.status === "error") {
      expect(result.message).toContain("boom");
    }
  });
});
