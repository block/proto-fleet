import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import { useSiteModals } from "./useSiteModals";
import { sitesClient } from "@/protoFleet/api/clients";
import {
  type CreateSiteResponse,
  CreateSiteResponseSchema,
  type DeleteSiteResponse,
  DeleteSiteResponseSchema,
  SiteSchema,
  SiteWithCountsSchema,
  type UpdateSiteResponse,
  UpdateSiteResponseSchema,
} from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { DEFAULT_ACTIVE_SITE } from "@/protoFleet/store/types/activeSite";
import { useFleetStore } from "@/protoFleet/store/useFleetStore";

vi.mock("@/protoFleet/api/clients", () => ({
  sitesClient: {
    createSite: vi.fn(),
    updateSite: vi.fn(),
    deleteSite: vi.fn(),
    reassignDevicesToSite: vi.fn(),
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

const makeSiteResponse = (
  id: bigint,
  name: string,
  networkConfig = "",
  warnings: string[] = [],
): { create: CreateSiteResponse; update: UpdateSiteResponse } => {
  const site = create(SiteSchema, { id, name, networkConfig });
  return {
    create: create(CreateSiteResponseSchema, { site, networkConfigWarnings: warnings }),
    update: create(UpdateSiteResponseSchema, { site, networkConfigWarnings: warnings }),
  };
};

const makeDeleteResponse = (): DeleteSiteResponse =>
  create(DeleteSiteResponseSchema, {
    unassignedDeviceCount: 0n,
    deletedBuildingCount: 0n,
    unassignedRackCount: 0n,
  });

describe("useSiteModals", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useFleetStore.setState((s) => {
      s.ui.activeSite = DEFAULT_ACTIVE_SITE;
    });
  });

  it("openCreate seeds detailsCreate with empty draft", () => {
    const { result } = renderHook(() => useSiteModals({ refetchSites: vi.fn() }));
    act(() => result.current.openCreate());
    expect(result.current.state).toEqual({
      kind: "detailsCreate",
      draft: { name: "", locationCity: "", locationState: "", timezone: "", powerCapacityMw: 0, networkConfig: "" },
    });
  });

  it("detailsContinueCreate carries existing networkConfig through to manageCreate", () => {
    const { result } = renderHook(() => useSiteModals({ refetchSites: vi.fn() }));
    act(() => result.current.openCreate());
    // Simulate the operator typing network config in ManageSiteModal first,
    // then re-entering details: detailsContinueCreate must preserve it.
    act(() =>
      result.current.detailsContinueCreate({
        name: "North DC",
        locationCity: "Chicago",
        locationState: "IL",
        timezone: "America/Chicago",
        powerCapacityMw: 5,
        networkConfig: "(unused — initial empty)",
      }),
    );
    act(() => result.current.manageNetworkConfigChange("10.0.0.0/24"));
    act(() => result.current.manageEditDetails());
    expect(result.current.state.kind).toBe("manageCreateEditingDetails");
    act(() =>
      result.current.detailsContinueCreate({
        name: "North DC 2",
        locationCity: "Chicago",
        locationState: "IL",
        timezone: "America/Chicago",
        powerCapacityMw: 5,
        networkConfig: "",
      }),
    );
    expect(result.current.state.kind).toBe("manageCreate");
    if (result.current.state.kind === "manageCreate") {
      expect(result.current.state.draft.networkConfig).toBe("10.0.0.0/24");
      expect(result.current.state.draft.name).toBe("North DC 2");
    }
  });

  it("dismiss from manageCreateEditingDetails returns to manageCreate", () => {
    const { result } = renderHook(() => useSiteModals({ refetchSites: vi.fn() }));
    act(() => result.current.openCreate());
    act(() =>
      result.current.detailsContinueCreate({
        name: "X",
        locationCity: "",
        locationState: "",
        timezone: "",
        powerCapacityMw: 0,
        networkConfig: "",
      }),
    );
    act(() => result.current.manageEditDetails());
    expect(result.current.state.kind).toBe("manageCreateEditingDetails");
    act(() => result.current.dismiss());
    expect(result.current.state.kind).toBe("manageCreate");
  });

  it("manageEditDetails on manageEdit stacks to manageEditEditingDetails; dismiss drops back to manageEdit", () => {
    const { result } = renderHook(() => useSiteModals({ refetchSites: vi.fn() }));
    const site = create(SiteSchema, { id: 1n, name: "S" });
    act(() => result.current.openManageEdit(site));
    act(() => result.current.manageEditDetails());
    expect(result.current.state.kind).toBe("manageEditEditingDetails");
    act(() => result.current.dismiss());
    expect(result.current.state.kind).toBe("manageEdit");
  });

  it("manageSave on manageCreate transitions to manageEdit on success (prevents duplicate CreateSite on re-save)", async () => {
    const { create: createResp } = makeSiteResponse(7n, "North DC", "10.0.0.0/24");
    vi.mocked(sitesClient.createSite).mockResolvedValue(createResp);
    const refetchSites = vi.fn();
    const { result } = renderHook(() => useSiteModals({ refetchSites }));
    act(() => result.current.openCreate());
    act(() =>
      result.current.detailsContinueCreate({
        name: "North DC",
        locationCity: "",
        locationState: "",
        timezone: "",
        powerCapacityMw: 0,
        networkConfig: "10.0.0.0/24",
      }),
    );

    let saveResult: { closeOnSuccess: boolean } | null | undefined;
    await act(async () => {
      saveResult = await result.current.manageSave();
    });

    await waitFor(() => {
      expect(sitesClient.createSite).toHaveBeenCalledTimes(1);
    });
    expect(saveResult?.closeOnSuccess).toBe(true);
    expect(refetchSites).toHaveBeenCalled();
    expect(result.current.state.kind).toBe("manageEdit");

    // Second Save must call updateSite, not createSite again — proves the
    // transition to manageEdit happens on the first success.
    const { update: updateResp } = makeSiteResponse(7n, "North DC", "10.0.0.0/24");
    vi.mocked(sitesClient.updateSite).mockResolvedValue(updateResp);
    await act(async () => {
      await result.current.manageSave();
    });
    expect(sitesClient.createSite).toHaveBeenCalledTimes(1);
    expect(sitesClient.updateSite).toHaveBeenCalledTimes(1);
  });

  it("detailsSaveEdit refreshes manage with server-canonical site on success", async () => {
    const initialSite = create(SiteSchema, { id: 9n, name: "Old" });
    const { update: updateResp } = makeSiteResponse(9n, "New");
    vi.mocked(sitesClient.updateSite).mockResolvedValue(updateResp);
    const { result } = renderHook(() => useSiteModals({ refetchSites: vi.fn() }));
    act(() => result.current.openManageEdit(initialSite));
    act(() => result.current.manageEditDetails());

    await act(async () => {
      await result.current.detailsSaveEdit({
        name: "New",
        locationCity: "",
        locationState: "",
        timezone: "",
        powerCapacityMw: 0,
        networkConfig: "",
      });
    });

    expect(result.current.state.kind).toBe("manageEdit");
    if (result.current.state.kind === "manageEdit") {
      expect(result.current.state.site.name).toBe("New");
      expect(result.current.state.draft.name).toBe("New");
    }
  });

  it("requestDeleteCurrent resolves the SiteWithCounts row from the supplied sites list", () => {
    const site = create(SiteSchema, { id: 5n, name: "Target" });
    const sites = [
      create(SiteWithCountsSchema, {
        site,
        deviceCount: 2n,
        rackCount: 1n,
        buildingCount: 0n,
      }),
    ];
    const { result } = renderHook(() => useSiteModals({ refetchSites: vi.fn() }));
    act(() => result.current.openManageEdit(site));
    act(() => result.current.manageEditDetails());
    act(() => result.current.requestDeleteCurrent(sites));
    expect(result.current.deleteTarget?.deviceCount).toBe(2n);
    // The underlying stacked state stays intact — cascade dialog overlays it.
    expect(result.current.state.kind).toBe("manageEditEditingDetails");
  });

  it("dismissDeleteConfirm clears deleteTarget without touching state", () => {
    const site = create(SiteSchema, { id: 5n, name: "T" });
    const sites = [create(SiteWithCountsSchema, { site, deviceCount: 0n, rackCount: 0n, buildingCount: 0n })];
    const { result } = renderHook(() => useSiteModals({ refetchSites: vi.fn() }));
    act(() => result.current.openManageEdit(site));
    act(() => result.current.manageEditDetails());
    act(() => result.current.requestDeleteCurrent(sites));
    expect(result.current.deleteTarget).not.toBeNull();
    act(() => result.current.dismissDeleteConfirm());
    expect(result.current.deleteTarget).toBeNull();
    expect(result.current.state.kind).toBe("manageEditEditingDetails");
  });

  it("deleteConfirm resets active SitePicker selection when the deleted site is active", async () => {
    vi.mocked(sitesClient.deleteSite).mockResolvedValue(makeDeleteResponse());
    const site = create(SiteSchema, { id: 11n, name: "Active" });
    const sites = [create(SiteWithCountsSchema, { site, deviceCount: 0n, rackCount: 0n, buildingCount: 0n })];
    act(() => {
      useFleetStore.setState((s) => {
        s.ui.activeSite = { kind: "site", id: "11" };
      });
    });
    const { result } = renderHook(() => useSiteModals({ refetchSites: vi.fn() }));
    act(() => result.current.openManageEdit(site));
    act(() => result.current.requestDeleteCurrent(sites));

    await act(async () => {
      await result.current.deleteConfirm();
    });

    expect(useFleetStore.getState().ui.activeSite).toEqual({ kind: "all" });
    expect(result.current.deleteTarget).toBeNull();
    expect(result.current.state.kind).toBe("none");
  });

  it("cancelAll closes every modal and clears deleteTarget", () => {
    const site = create(SiteSchema, { id: 5n, name: "T" });
    const { result } = renderHook(() => useSiteModals({ refetchSites: vi.fn() }));
    act(() => result.current.openManageEdit(site));
    act(() =>
      result.current.requestDeleteCurrent([
        create(SiteWithCountsSchema, { site, deviceCount: 0n, rackCount: 0n, buildingCount: 0n }),
      ]),
    );
    act(() => result.current.cancelAll());
    expect(result.current.state.kind).toBe("none");
    expect(result.current.deleteTarget).toBeNull();
  });
});
