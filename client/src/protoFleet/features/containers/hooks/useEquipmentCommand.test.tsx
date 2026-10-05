import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { equipmentState, useEquipmentCommand } from "./useEquipmentCommand";
import { ContainerSchema } from "@/protoFleet/api/generated/containers/v1/containers_pb";

const api = vi.hoisted(() => ({ setEquipmentControl: vi.fn(), getEquipmentCommandResult: vi.fn() }));
vi.mock("@/protoFleet/api/clients", () => ({ containersClient: api }));
const stamp = new Date();
const container = (enabled = false, at = stamp) =>
  create(ContainerSchema, {
    deviceIdentifier: "ct1",
    controls: { supported: true, observedAt: timestampFromDate(at), controls: [{ name: "dry_cooler", enabled }] },
  });
afterEach(() => {
  vi.resetAllMocks();
});

describe("equipment commands", () => {
  it("requires fresh known state and advertised capability", () => {
    expect(equipmentState(container(), "dry_cooler")).toBe(false);
    const unknown = container();
    unknown.controls!.supported = undefined;
    expect(equipmentState(unknown, "dry_cooler")).toBeUndefined();
    expect(equipmentState(container(false, new Date(Date.now() - 180000)), "dry_cooler")).toBeUndefined();
    expect(equipmentState(container(), "main_circuit_breaker")).toBeUndefined();
  });
  it("sends explicit state once and waits for post-completion physical readback", async () => {
    api.setEquipmentControl.mockResolvedValue({ batchIdentifier: "batch" });
    api.getEquipmentCommandResult.mockResolvedValue({
      result: {
        status: "finished",
        successCount: 1,
        failureCount: 0,
        deviceResults: [{ updatedAt: timestampFromDate(stamp) }],
      },
    });
    const refresh = vi.fn();
    const { result, rerender } = renderHook(({ value }) => useEquipmentCommand("ct1", value, refresh), {
      initialProps: { value: container() },
    });
    await act(async () => {
      void result.current.submit("dry_cooler", true);
      void result.current.submit("dry_cooler", true);
    });
    expect(api.setEquipmentControl).toHaveBeenCalledExactlyOnceWith(
      {
        deviceIdentifier: "ct1",
        name: "dry_cooler",
        enabled: true,
      },
      { timeoutMs: 10_000 },
    );
    await waitFor(() => expect(result.current.message).toContain("waiting for controller readback"));
    expect(result.current.busy).toBe(true);
    rerender({ value: container(true) });
    expect(result.current.busy).toBe(true);
    rerender({ value: container(true, new Date(stamp.getTime() + 1)) });
    await waitFor(() => expect(result.current.busy).toBe(false));
    expect(result.current.message).toContain("confirmed by the controller");
  });
  it("surfaces controller rejection", async () => {
    api.setEquipmentControl.mockResolvedValue({ batchIdentifier: "batch" });
    api.getEquipmentCommandResult.mockResolvedValue({
      result: {
        status: "finished",
        successCount: 0,
        failureCount: 1,
        deviceResults: [{ errorMessage: "PLC rejected request" }],
      },
    });
    const { result } = renderHook(() => useEquipmentCommand("ct1", container(), vi.fn()));
    await act(async () => {
      await result.current.submit("dry_cooler", true);
    });
    await waitFor(() => expect(result.current.message).toBe("PLC rejected request"));
    expect(result.current.failed).toBe(true);
  });
});

it("bounds a hung results request and clears pending state after a route round-trip", async () => {
  vi.useFakeTimers();
  vi.setSystemTime(stamp);
  api.setEquipmentControl.mockResolvedValue({ batchIdentifier: "batch" });
  api.getEquipmentCommandResult.mockReturnValue(new Promise(() => undefined));
  const { result, rerender, unmount } = renderHook(({ id }) => useEquipmentCommand(id, container(), vi.fn()), {
    initialProps: { id: "ct1" },
  });
  await act(async () => {
    await result.current.submit("dry_cooler", true);
  });
  expect(result.current.busy).toBe(true);
  await act(async () => {
    vi.advanceTimersByTime(121_000);
  });
  expect(result.current.busy).toBe(false);
  expect(result.current.message).toContain("outcome unknown");
  vi.setSystemTime(stamp);
  await act(async () => {
    await result.current.submit("dry_cooler", true);
  });
  rerender({ id: "other" });
  rerender({ id: "ct1" });
  expect(result.current.busy).toBe(false);
  unmount();
  vi.useRealTimers();
});
