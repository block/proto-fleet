import { renderHook } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { FleetNodeHARole, FleetNodeSummarySchema } from "./generated/fleetnodeadmin/v1/fleetnodeadmin_pb";
import { useFleetNodes } from "./useFleetNodes";

const listFleetNodes = vi.hoisted(() => vi.fn());
const createEnrollmentCode = vi.hoisted(() => vi.fn());

vi.mock("@/protoFleet/api/clients", () => ({
  fleetNodeAdminClient: { listFleetNodes, createEnrollmentCode },
}));
vi.mock("@/protoFleet/store", () => ({
  useAuthErrors: () => ({ handleAuthErrors: vi.fn() }),
}));

it("maps reported metadata and protobuf defaults into node items", async () => {
  const registered = new Date("2026-07-09T12:00:00Z");
  listFleetNodes.mockResolvedValue({
    fleetNodes: [
      create(FleetNodeSummarySchema, {
        fleetNodeId: 7n,
        haRole: FleetNodeHARole.FLEET_NODE_HA_ROLE_STANDBY,
        haPeerNodeId: 3n,
        haPeerNodeName: "primary",
        haStandbyReady: false,
        haStatusReason: "Waiting for both nodes to connect",
        ipAddresses: ["192.168.1.7", "fd00::7"],
        version: "v0.5.1-test",
        enrolledBy: "operator@example.com",
        createdAt: timestampFromDate(registered),
      }),
      create(FleetNodeSummarySchema, { fleetNodeId: 8n }),
    ],
  });
  const { result } = renderHook(() => useFleetNodes());

  expect(await result.current.listFleetNodes()).toMatchObject([
    {
      fleetNodeId: "7",
      haRole: FleetNodeHARole.FLEET_NODE_HA_ROLE_STANDBY,
      haPeerNodeId: "3",
      haPeerNodeName: "primary",
      haStandbyReady: false,
      haStatusReason: "Waiting for both nodes to connect",
      ipAddresses: ["192.168.1.7", "fd00::7"],
      version: "v0.5.1-test",
      enrolledBy: "operator@example.com",
      createdAt: registered,
    },
    {
      fleetNodeId: "8",
      haRole: FleetNodeHARole.FLEET_NODE_HA_ROLE_UNSPECIFIED,
      haPeerNodeId: null,
      haPeerNodeName: "",
      haStandbyReady: false,
      haStatusReason: "",
      ipAddresses: [],
      version: "",
      enrolledBy: "",
      createdAt: null,
    },
  ]);
});

it("sends an empty standalone request and the exact standby intent", async () => {
  // Arrange
  createEnrollmentCode.mockResolvedValue({ code: "code", pendingEnrollmentId: 11n });
  const { result } = renderHook(() => useFleetNodes());

  // Act
  await result.current.createEnrollmentCode();

  // Assert
  expect(createEnrollmentCode).toHaveBeenLastCalledWith({});

  // Act
  await result.current.createEnrollmentCode("7");

  // Assert
  expect(createEnrollmentCode).toHaveBeenLastCalledWith({ standbyForNodeId: 7n });
});
