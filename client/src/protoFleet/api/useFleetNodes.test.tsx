import { renderHook } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { FleetNodeSummarySchema } from "./generated/fleetnodeadmin/v1/fleetnodeadmin_pb";
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
        standbyEnrollmentEligible: true,
        standbyForNodeId: 3n,
        standbyForNodeName: "primary",
        haRole: "preparing",
        haStatusReason: "Waiting for credentials",
        haOwnerNodeId: 3n,
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
      standbyEnrollmentEligible: true,
      standbyForNodeId: "3",
      standbyForNodeName: "primary",
      haRole: "preparing",
      haStatusReason: "Waiting for credentials",
      haOwnerNodeId: "3",
      ipAddresses: ["192.168.1.7", "fd00::7"],
      version: "v0.5.1-test",
      enrolledBy: "operator@example.com",
      createdAt: registered,
    },
    { fleetNodeId: "8", ipAddresses: [], version: "", enrolledBy: "", createdAt: null },
  ]);
});

it("sends an empty standalone request and the exact standby intent", async () => {
  createEnrollmentCode.mockResolvedValue({ code: "code", pendingEnrollmentId: 11n });
  const { result } = renderHook(() => useFleetNodes());
  await result.current.createEnrollmentCode();
  expect(createEnrollmentCode).toHaveBeenLastCalledWith({});
  await result.current.createEnrollmentCode("7");
  expect(createEnrollmentCode).toHaveBeenLastCalledWith({ standbyForNodeId: 7n });
});
