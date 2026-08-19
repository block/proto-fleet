import { beforeEach, describe, expect, it, vi } from "vitest";
import { Code, ConnectError } from "@connectrpc/connect";

import { getRolloutLaneAssignments } from "./rolloutLaneAssignments";

const { getAssignmentsMock } = vi.hoisted(() => ({
  getAssignmentsMock: vi.fn(),
}));

vi.mock("@/protoFleet/api/clients", () => ({
  rolloutClient: {
    getRolloutLaneAssignments: getAssignmentsMock,
  },
}));

describe("getRolloutLaneAssignments", () => {
  beforeEach(() => {
    getAssignmentsMock.mockReset();
  });

  it("maps active lane assignments", async () => {
    getAssignmentsMock.mockResolvedValue({
      assignments: [
        {
          deviceIdentifier: "miner-a",
          laneId: "lane-a",
          laneLabel: "Stable",
        },
      ],
    });

    await expect(getRolloutLaneAssignments(["miner-a"])).resolves.toEqual([
      {
        deviceIdentifier: "miner-a",
        laneId: "lane-a",
        laneLabel: "Stable",
      },
    ]);
    expect(getAssignmentsMock.mock.calls[0][0]).toMatchObject({
      deviceIdentifiers: ["miner-a"],
    });
  });

  it("preserves permission failures for the caller's permission gate", async () => {
    const error = new ConnectError("denied", Code.PermissionDenied);
    getAssignmentsMock.mockRejectedValue(error);

    await expect(getRolloutLaneAssignments(["miner-a"])).rejects.toBe(error);
  });
});
