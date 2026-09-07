import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import MaintenanceOptionsProvider from "../components/MaintenanceOptionsProvider";

let canManage = true;
const listAssignees = vi.fn(async ({ onSuccess, onFinally }) => {
  onSuccess([{ userId: 5n, username: "alex", roleName: "Technician" }]);
  onFinally?.();
});
const listSites = vi.fn(async ({ maintenanceOptionsScope, onSuccess }) => {
  onSuccess(
    maintenanceOptionsScope === 2 ? [{ site: { id: 9n, name: "Phoenix" } }] : [{ site: { id: 8n, name: "Denver" } }],
  );
});
vi.mock("@/protoFleet/api/maintenance", () => ({ useMaintenanceApi: () => ({ listAssignees }) }));
vi.mock("@/protoFleet/api/sites", () => ({ useSites: () => ({ listSites }) }));
vi.mock("@/protoFleet/store", () => ({ useUsername: () => "alex", useHasPermission: () => canManage }));
const { useMaintenanceOptions } = await import("./useMaintenanceOptions");

beforeEach(() => {
  canManage = true;
  vi.clearAllMocks();
});

it("loads maintenance site options independently from the global fleet catalog", async () => {
  const { result } = renderHook(() => useMaintenanceOptions(), { wrapper: MaintenanceOptionsProvider });
  await waitFor(() => expect(result.current.loading).toBe(false));
  expect(listSites).toHaveBeenCalledWith(expect.objectContaining({ maintenanceOptionsScope: 1 }));
  expect(listSites).toHaveBeenCalledWith(expect.objectContaining({ maintenanceOptionsScope: 2 }));
  expect(result.current.sites).toEqual([{ id: "8", name: "Denver" }]);
  expect(result.current.manageableSites).toEqual([{ id: "9", name: "Phoenix" }]);
  expect(result.current.currentAssignee?.id).toBe("5");
});

it("shares one options load across consumers without polling", async () => {
  vi.useFakeTimers();
  try {
    const { result } = renderHook(() => ({ first: useMaintenanceOptions(), second: useMaintenanceOptions() }), {
      wrapper: MaintenanceOptionsProvider,
    });
    await act(async () => vi.advanceTimersByTimeAsync(0));
    expect(result.current.first.sites).toEqual(result.current.second.sites);
    expect(listAssignees).toHaveBeenCalledTimes(1);
    expect(listSites).toHaveBeenCalledTimes(2);
    await act(async () => vi.advanceTimersByTimeAsync(30_000));
    expect(listAssignees).toHaveBeenCalledTimes(1);
    expect(listSites).toHaveBeenCalledTimes(2);
  } finally {
    vi.useRealTimers();
  }
});

it("skips the manage catalog for read-only maintenance users", async () => {
  canManage = false;

  const { result } = renderHook(() => useMaintenanceOptions(), { wrapper: MaintenanceOptionsProvider });
  await waitFor(() => expect(result.current.loading).toBe(false));

  expect(listSites).toHaveBeenCalledTimes(1);
  expect(result.current.manageableSites).toEqual([]);
});
