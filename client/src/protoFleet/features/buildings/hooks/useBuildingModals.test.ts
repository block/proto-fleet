import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import { useBuildingModals } from "./useBuildingModals";
import { emptyBuildingFormValues } from "@/protoFleet/api/buildings";
import { buildingsClient } from "@/protoFleet/api/clients";
import {
  BuildingSchema,
  BuildingWithCountsSchema,
  type CreateBuildingResponse,
  CreateBuildingResponseSchema,
  type DeleteBuildingResponse,
  DeleteBuildingResponseSchema,
  type UpdateBuildingResponse,
  UpdateBuildingResponseSchema,
} from "@/protoFleet/api/generated/buildings/v1/buildings_pb";

vi.mock("@/protoFleet/api/clients", () => ({
  buildingsClient: {
    createBuilding: vi.fn(),
    updateBuilding: vi.fn(),
    deleteBuilding: vi.fn(),
  },
}));

vi.mock("@/protoFleet/store", async () => {
  const actual = await vi.importActual<typeof import("@/protoFleet/store")>("@/protoFleet/store");
  return {
    ...actual,
    useAuthErrors: () => ({
      handleAuthErrors: ({ onError }: { onError?: (e: unknown) => void }) => onError?.(new Error("auth")),
    }),
  };
});

vi.mock("@/shared/features/toaster", () => ({
  pushToast: vi.fn(),
  STATUSES: { success: "success", error: "error", queued: "queued", loading: "loading" },
}));

const makeBuilding = (id: bigint, name: string, siteId: bigint = 7n) => create(BuildingSchema, { id, siteId, name });

const makeBuildingRow = (id: bigint, name: string, rackCount: bigint = 0n, siteId: bigint = 7n) =>
  create(BuildingWithCountsSchema, {
    building: makeBuilding(id, name, siteId),
    rackCount,
  });

const makeCreateResp = (id: bigint, name: string): CreateBuildingResponse =>
  create(CreateBuildingResponseSchema, { building: makeBuilding(id, name) });

const makeUpdateResp = (id: bigint, name: string): UpdateBuildingResponse =>
  create(UpdateBuildingResponseSchema, { building: makeBuilding(id, name) });

const makeDeleteResp = (): DeleteBuildingResponse => create(DeleteBuildingResponseSchema, { unassignedRackCount: 0n });

describe("useBuildingModals", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("openDetailsCreate seeds the detailsCreate state with an empty draft + parent site", () => {
    const { result } = renderHook(() => useBuildingModals());
    act(() => result.current.openDetailsCreate(7n, "North DC"));
    expect(result.current.state.kind).toBe("detailsCreate");
    if (result.current.state.kind === "detailsCreate") {
      expect(result.current.state.siteId).toBe(7n);
      expect(result.current.state.siteName).toBe("North DC");
      expect(result.current.state.draft.name).toBe("");
    }
  });

  it("openDetailsCreate with no args opens detailsCreate with undefined siteId (Buildings-tab CTA path)", () => {
    const { result } = renderHook(() => useBuildingModals());
    act(() => result.current.openDetailsCreate());
    expect(result.current.state.kind).toBe("detailsCreate");
    if (result.current.state.kind === "detailsCreate") {
      expect(result.current.state.siteId).toBeUndefined();
      expect(result.current.state.siteName).toBeUndefined();
    }
  });

  it("openManage seeds manage state with the row + site label", () => {
    const row = makeBuildingRow(11n, "Main");
    const { result } = renderHook(() => useBuildingModals());
    act(() => result.current.openManage(row, "North DC"));
    expect(result.current.state.kind).toBe("manage");
    if (result.current.state.kind === "manage") {
      expect(result.current.state.row.building?.id).toBe(11n);
      expect(result.current.state.siteName).toBe("North DC");
    }
  });

  it("manageEditDetails on manage stacks to manageEditingDetails; dismiss drops back to manage", () => {
    const row = makeBuildingRow(11n, "Main");
    const { result } = renderHook(() => useBuildingModals());
    act(() => result.current.openManage(row, "North DC"));
    act(() => result.current.manageEditDetails());
    expect(result.current.state.kind).toBe("manageEditingDetails");
    act(() => result.current.dismiss());
    expect(result.current.state.kind).toBe("manage");
  });

  it("detailsCreate calls CreateBuilding and closes on success", async () => {
    vi.mocked(buildingsClient.createBuilding).mockResolvedValue(makeCreateResp(11n, "Main"));
    const refetch = vi.fn();
    const onMutationSuccess = vi.fn();
    const { result } = renderHook(() => useBuildingModals({ refetchBuildings: refetch, onMutationSuccess }));
    act(() => result.current.openDetailsCreate(7n, "North DC"));

    await act(async () => {
      await result.current.detailsCreate(emptyBuildingFormValues(), 7n);
    });

    await waitFor(() => {
      expect(buildingsClient.createBuilding).toHaveBeenCalledTimes(1);
    });
    expect(refetch).toHaveBeenCalled();
    expect(onMutationSuccess).toHaveBeenCalled();
    expect(result.current.state.kind).toBe("none");
  });

  it("global create path: openDetailsCreate() + detailsCreate(values, siteId) threads the chosen siteId to CreateBuilding", async () => {
    vi.mocked(buildingsClient.createBuilding).mockResolvedValue(makeCreateResp(11n, "Main"));
    const refetch = vi.fn();
    const { result } = renderHook(() => useBuildingModals({ refetchBuildings: refetch }));
    // Buildings-tab CTA opens with no preselected site; the modal's Site
    // dropdown collects siteId and passes it through detailsCreate.
    act(() => result.current.openDetailsCreate());

    await act(async () => {
      await result.current.detailsCreate(emptyBuildingFormValues(), 9n);
    });

    await waitFor(() => {
      expect(buildingsClient.createBuilding).toHaveBeenCalledTimes(1);
    });
    // The CreateBuilding RPC request carries the siteId the modal collected,
    // not anything from state (which had none).
    const call = vi.mocked(buildingsClient.createBuilding).mock.calls[0]?.[0];
    expect(call?.siteId).toBe(9n);
    expect(refetch).toHaveBeenCalled();
    expect(result.current.state.kind).toBe("none");
  });

  it("detailsSaveEdit (manageEditingDetails) refreshes the manage row with server canonical values", async () => {
    vi.mocked(buildingsClient.updateBuilding).mockResolvedValue(makeUpdateResp(11n, "Renamed"));
    const initial = makeBuildingRow(11n, "Old");
    const { result } = renderHook(() => useBuildingModals());
    act(() => result.current.openManage(initial, "North DC"));
    act(() => result.current.manageEditDetails());

    await act(async () => {
      await result.current.detailsSaveEdit({ ...emptyBuildingFormValues(), name: "Renamed" });
    });

    // Closes details, drops back to manage with the refreshed row carrying
    // the server's canonical name.
    expect(result.current.state.kind).toBe("manage");
    if (result.current.state.kind === "manage") {
      expect(result.current.state.row.building?.name).toBe("Renamed");
    }
  });

  it("detailsSaveEdit invokes onMutationSuccess on update success", async () => {
    vi.mocked(buildingsClient.updateBuilding).mockResolvedValue(makeUpdateResp(11n, "Renamed"));
    const initial = makeBuildingRow(11n, "Old");
    const onMutationSuccess = vi.fn();
    const { result } = renderHook(() => useBuildingModals({ onMutationSuccess }));
    act(() => result.current.openManage(initial, "North DC"));
    act(() => result.current.manageEditDetails());

    await act(async () => {
      await result.current.detailsSaveEdit({ ...emptyBuildingFormValues(), name: "Renamed" });
    });

    expect(onMutationSuccess).toHaveBeenCalled();
  });

  it("requestDeleteCurrent from detailsEdit sets deleteTarget and closes everything to none", () => {
    const row = makeBuildingRow(11n, "Target", 2n);
    const { result } = renderHook(() => useBuildingModals());
    act(() => result.current.openDetailsEdit(row, "North DC"));
    act(() => result.current.requestDeleteCurrent());
    expect(result.current.deleteTarget?.rackCount).toBe(2n);
    // detailsEdit standalone: we close everything underneath the cascade
    // dialog because there's no manage modal to leave open.
    expect(result.current.state.kind).toBe("none");
  });

  it("requestDeleteCurrent from manageEditingDetails drops details to manage (cascade overlays manage)", () => {
    const row = makeBuildingRow(11n, "Target", 2n);
    const { result } = renderHook(() => useBuildingModals());
    act(() => result.current.openManage(row, "North DC"));
    act(() => result.current.manageEditDetails());
    act(() => result.current.requestDeleteCurrent());
    expect(result.current.deleteTarget).not.toBeNull();
    expect(result.current.state.kind).toBe("manage");
  });

  it("deleteConfirm from manageEditingDetails invokes onDeleteFromManage redirect", async () => {
    vi.mocked(buildingsClient.deleteBuilding).mockResolvedValue(makeDeleteResp());
    const row = makeBuildingRow(11n, "Target");
    const onDeleteFromManage = vi.fn();
    const onMutationSuccess = vi.fn();
    const { result } = renderHook(() => useBuildingModals({ onDeleteFromManage, onMutationSuccess }));
    act(() => result.current.openManage(row, "North DC"));
    act(() => result.current.manageEditDetails());
    act(() => result.current.requestDeleteCurrent());

    await act(async () => {
      await result.current.deleteConfirm();
    });

    expect(buildingsClient.deleteBuilding).toHaveBeenCalledWith({ id: 11n }, { signal: undefined });
    expect(onDeleteFromManage).toHaveBeenCalledWith(11n);
    expect(onMutationSuccess).toHaveBeenCalled();
    expect(result.current.deleteTarget).toBeNull();
    expect(result.current.state.kind).toBe("none");
  });

  it("deleteConfirm from detailsEdit (non-manage origin) does NOT invoke onDeleteFromManage", async () => {
    vi.mocked(buildingsClient.deleteBuilding).mockResolvedValue(makeDeleteResp());
    const row = makeBuildingRow(11n, "Target");
    const onDeleteFromManage = vi.fn();
    const { result } = renderHook(() => useBuildingModals({ onDeleteFromManage }));
    act(() => result.current.openDetailsEdit(row, "North DC"));
    act(() => result.current.requestDeleteCurrent());

    await act(async () => {
      await result.current.deleteConfirm();
    });

    expect(onDeleteFromManage).not.toHaveBeenCalled();
    expect(result.current.state.kind).toBe("none");
  });

  it("dismissDeleteConfirm clears deleteTarget; underlying manage state survives", () => {
    const row = makeBuildingRow(11n, "Target");
    const { result } = renderHook(() => useBuildingModals());
    act(() => result.current.openManage(row, "North DC"));
    act(() => result.current.manageEditDetails());
    act(() => result.current.requestDeleteCurrent());
    expect(result.current.deleteTarget).not.toBeNull();
    expect(result.current.state.kind).toBe("manage");
    act(() => result.current.dismissDeleteConfirm());
    expect(result.current.deleteTarget).toBeNull();
    expect(result.current.state.kind).toBe("manage");
  });
});
