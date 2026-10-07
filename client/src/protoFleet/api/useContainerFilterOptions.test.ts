import { renderHook } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { ContainerSchema } from "./generated/containers/v1/containers_pb";
import { useContainerFilterOptions } from "./useContainerFilterOptions";
const { useContainers } = vi.hoisted(() => ({ useContainers: vi.fn() }));
vi.mock("./useContainers", () => ({ useContainers }));

it("uses the complete readable catalog and labels identical container names with their sites", () => {
  useContainers.mockReturnValue({
    containers: [
      create(ContainerSchema, { id: 7n, name: "CT1", site: { id: 1n, label: "North" } }),
      create(ContainerSchema, { id: 8n, name: "CT1", site: { id: 2n, label: "South" } }),
      create(ContainerSchema, { id: 9n, deviceIdentifier: "controller-9" }),
    ],
  });
  const { result } = renderHook(() => useContainerFilterOptions(true));
  expect(useContainers).toHaveBeenLastCalledWith([], false, true);
  expect(result.current).toEqual([
    { id: "7", label: "CT1 · North" },
    { id: "8", label: "CT1 · South" },
    { id: "9", label: "controller-9 · Unassigned" },
  ]);
});

it("keeps empty options stable while loading and discards them when inventory access is revoked", () => {
  useContainers.mockImplementation(() => ({ containers: [], loading: true }));
  const { result, rerender } = renderHook(({ enabled }) => useContainerFilterOptions(enabled), {
    initialProps: { enabled: true },
  });
  const empty = result.current;
  rerender({ enabled: true });
  expect(result.current).toBe(empty);
  useContainers.mockReturnValue({ containers: [create(ContainerSchema, { id: 7n, name: "CT1" })], loading: false });
  rerender({ enabled: true });
  expect(result.current).toHaveLength(1);
  rerender({ enabled: false });
  expect(result.current).toEqual([]);
});
