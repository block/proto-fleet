import { beforeEach, describe, expect, it, vi } from "vitest";
import { Code, ConnectError } from "@connectrpc/connect";

import { lookupMinerByIdentifier } from "./lookupMinerByIdentifier";
import { MinerIdentifierType } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";

const { UNSPECIFIED, MAC_ADDRESS, SERIAL_NUMBER } = MinerIdentifierType;

const mockLookupMinerByIdentifier = vi.fn();

vi.mock("@/protoFleet/api/clients", () => ({
  fleetManagementClient: {
    lookupMinerByIdentifier: (...args: unknown[]) => mockLookupMinerByIdentifier(...args),
  },
}));

describe("lookupMinerByIdentifier", () => {
  beforeEach(() => {
    mockLookupMinerByIdentifier.mockReset();
  });

  it("short-circuits to notFound for an empty identifier without an RPC", async () => {
    const result = await lookupMinerByIdentifier("", SERIAL_NUMBER);

    expect(result).toEqual({ status: "notFound" });
    expect(mockLookupMinerByIdentifier).not.toHaveBeenCalled();
  });

  it("returns the snapshot on a hit and forwards identifier + type", async () => {
    const snapshot = { deviceIdentifier: "d1", serialNumber: "SN1" };
    mockLookupMinerByIdentifier.mockResolvedValueOnce({ snapshot });

    const result = await lookupMinerByIdentifier("SN1", SERIAL_NUMBER);

    expect(result).toEqual({ status: "found", snapshot });
    expect(mockLookupMinerByIdentifier).toHaveBeenCalledWith(
      { identifier: "SN1", identifierType: SERIAL_NUMBER },
      { signal: undefined },
    );
  });

  it("forwards a MAC identifier with the MAC type", async () => {
    mockLookupMinerByIdentifier.mockResolvedValueOnce({ snapshot: { deviceIdentifier: "d2" } });

    await lookupMinerByIdentifier("00:1A:2B:3C:4D:5E", MAC_ADDRESS);

    expect(mockLookupMinerByIdentifier).toHaveBeenCalledWith(
      { identifier: "00:1A:2B:3C:4D:5E", identifierType: MAC_ADDRESS },
      { signal: undefined },
    );
  });

  it("forwards UNSPECIFIED so the server can infer", async () => {
    mockLookupMinerByIdentifier.mockResolvedValueOnce({ snapshot: { deviceIdentifier: "d3" } });

    await lookupMinerByIdentifier("ambiguous", UNSPECIFIED);

    expect(mockLookupMinerByIdentifier).toHaveBeenCalledWith(
      { identifier: "ambiguous", identifierType: UNSPECIFIED },
      { signal: undefined },
    );
  });

  it("maps a missing snapshot in an otherwise-successful response to notFound", async () => {
    mockLookupMinerByIdentifier.mockResolvedValueOnce({ snapshot: undefined });

    const result = await lookupMinerByIdentifier("SN1", SERIAL_NUMBER);

    expect(result).toEqual({ status: "notFound" });
  });

  it("maps a NotFound ConnectError to notFound", async () => {
    mockLookupMinerByIdentifier.mockRejectedValueOnce(new ConnectError("nope", Code.NotFound));

    const result = await lookupMinerByIdentifier("SN-MISSING", SERIAL_NUMBER);

    expect(result).toEqual({ status: "notFound" });
  });

  it("maps other errors to an error result with a message", async () => {
    mockLookupMinerByIdentifier.mockRejectedValueOnce(new ConnectError("boom", Code.Internal));

    const result = await lookupMinerByIdentifier("SN1", SERIAL_NUMBER);

    expect(result.status).toBe("error");
    if (result.status === "error") {
      expect(result.message).toContain("boom");
    }
  });
});
