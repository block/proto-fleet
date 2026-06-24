import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import { useSiteModals } from "./useSiteModals";
import { sitesClient } from "@/protoFleet/api/clients";
import {
  AssignBuildingsToSiteResponseSchema,
  type CreateSiteResponse,
  CreateSiteResponseSchema,
  type DeleteSiteResponse,
  DeleteSiteResponseSchema,
  SiteSchema,
  SiteWithCountsSchema,
  type UpdateSiteResponse,
  UpdateSiteResponseSchema,
} from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { emptySiteFormValues } from "@/protoFleet/api/sites";
import { DEFAULT_ACTIVE_SITE } from "@/protoFleet/store/types/activeSite";
import { useFleetStore } from "@/protoFleet/store/useFleetStore";

vi.mock("@/protoFleet/api/clients", () => ({
  sitesClient: {
    createSite: vi.fn(),
    updateSite: vi.fn(),
    deleteSite: vi.fn(),
    assignDevicesToSite: vi.fn(),
    assignBuildingsToSite: vi.fn(),
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
      draft: emptySiteFormValues(),
    });
  });

  it("detailsContinueCreate round-trips manageCreate ↔ details preserving edited fields", () => {
    const { result } = renderHook(() => useSiteModals({ refetchSites: vi.fn() }));
    act(() => result.current.openCreate());
    act(() =>
      result.current.detailsContinueCreate({
        ...emptySiteFormValues(),
        name: "North DC",
        locationCity: "Chicago",
        locationState: "IL",
        powerCapacityMw: 5,
      }),
    );
    act(() => result.current.manageEditDetails());
    expect(result.current.state.kind).toBe("manageCreateEditingDetails");
    act(() =>
      result.current.detailsContinueCreate({
        ...emptySiteFormValues(),
        name: "North DC 2",
        locationCity: "Chicago",
        locationState: "IL",
        powerCapacityMw: 5,
      }),
    );
    expect(result.current.state.kind).toBe("manageCreate");
    if (result.current.state.kind === "manageCreate") {
      expect(result.current.state.draft.name).toBe("North DC 2");
      expect(result.current.state.draft.powerCapacityMw).toBe(5);
    }
  });

  it("dismiss from manageCreateEditingDetails returns to manageCreate", () => {
    const { result } = renderHook(() => useSiteModals({ refetchSites: vi.fn() }));
    act(() => result.current.openCreate());
    act(() =>
      result.current.detailsContinueCreate({
        ...emptySiteFormValues(),
        name: "X",
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

  it("manageSave on manageCreate runs CreateSite and reports closeOnSuccess", async () => {
    const { create: createResp } = makeSiteResponse(7n, "North DC", "10.0.0.0/24");
    vi.mocked(sitesClient.createSite).mockResolvedValue(createResp);
    const refetchSites = vi.fn();
    const { result } = renderHook(() => useSiteModals({ refetchSites }));
    act(() => result.current.openCreate());
    act(() =>
      result.current.detailsContinueCreate({
        ...emptySiteFormValues(),
        name: "North DC",
        networkConfig: "10.0.0.0/24",
      }),
    );

    let saveResult: { closeOnSuccess: boolean } | null | undefined;
    await act(async () => {
      saveResult = await result.current.manageSave({ added: [], removed: [] });
    });

    await waitFor(() => {
      expect(sitesClient.createSite).toHaveBeenCalledTimes(1);
    });
    // With no staged buildings the create skips the buildings RPC entirely.
    expect(sitesClient.assignBuildingsToSite).not.toHaveBeenCalled();
    expect(saveResult?.closeOnSuccess).toBe(true);
    expect(refetchSites).toHaveBeenCalled();
  });

  it("manageSave on manageCreate assigns staged buildings to the new site", async () => {
    const { create: createResp } = makeSiteResponse(7n, "North DC");
    vi.mocked(sitesClient.createSite).mockResolvedValue(createResp);
    vi.mocked(sitesClient.assignBuildingsToSite).mockResolvedValue(
      create(AssignBuildingsToSiteResponseSchema, { reassignedRackCount: 0n, reassignedDeviceCount: 0n }),
    );
    const refetchSites = vi.fn();
    const { result } = renderHook(() => useSiteModals({ refetchSites }));
    act(() => result.current.openCreate());
    act(() => result.current.detailsContinueCreate({ ...emptySiteFormValues(), name: "North DC" }));

    let saveResult: { closeOnSuccess: boolean } | null | undefined;
    await act(async () => {
      saveResult = await result.current.manageSave({ added: [11n, 12n], removed: [] });
    });

    await waitFor(() => {
      expect(sitesClient.createSite).toHaveBeenCalledTimes(1);
    });
    // Staged buildings are assigned to the freshly-created site (id 7).
    expect(sitesClient.assignBuildingsToSite).toHaveBeenCalledWith(
      { buildingIds: [11n, 12n], targetSiteId: 7n },
      expect.anything(),
    );
    expect(saveResult?.closeOnSuccess).toBe(true);
    expect(refetchSites).toHaveBeenCalled();
  });

  it("manageSave on manageEdit applies the building delta via AssignBuildingsToSite", async () => {
    vi.mocked(sitesClient.assignBuildingsToSite).mockResolvedValue(
      create(AssignBuildingsToSiteResponseSchema, { reassignedRackCount: 0n, reassignedDeviceCount: 0n }),
    );
    const refetchSites = vi.fn();
    const refetchBuildings = vi.fn();
    const site = create(SiteSchema, { id: 3n, name: "North DC" });
    const { result } = renderHook(() => useSiteModals({ refetchSites, refetchBuildings }));
    act(() => result.current.openManageEdit(site));

    let saveResult: { closeOnSuccess: boolean } | null | undefined;
    await act(async () => {
      saveResult = await result.current.manageSave({ added: [10n], removed: [20n] });
    });

    // Two calls: removed → "Unassigned" (no target), added → this site.
    await waitFor(() => {
      expect(sitesClient.assignBuildingsToSite).toHaveBeenCalledTimes(2);
    });
    expect(sitesClient.assignBuildingsToSite).toHaveBeenCalledWith(
      { buildingIds: [20n], targetSiteId: undefined },
      expect.anything(),
    );
    expect(sitesClient.assignBuildingsToSite).toHaveBeenCalledWith(
      { buildingIds: [10n], targetSiteId: 3n },
      expect.anything(),
    );
    expect(saveResult?.closeOnSuccess).toBe(true);
    expect(refetchSites).toHaveBeenCalled();
    // Membership changed building rows, so the building table refresh fires too.
    expect(refetchBuildings).toHaveBeenCalled();
  });

  it("manageSave on manageEdit with an empty delta closes without an RPC", async () => {
    const { result } = renderHook(() => useSiteModals({ refetchSites: vi.fn() }));
    act(() => result.current.openManageEdit(create(SiteSchema, { id: 3n, name: "North DC" })));

    let saveResult: { closeOnSuccess: boolean } | null | undefined;
    await act(async () => {
      saveResult = await result.current.manageSave({ added: [], removed: [] });
    });

    expect(sitesClient.assignBuildingsToSite).not.toHaveBeenCalled();
    expect(saveResult?.closeOnSuccess).toBe(true);
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
        ...emptySiteFormValues(),
        name: "New",
      });
    });

    expect(result.current.state.kind).toBe("manageEdit");
    if (result.current.state.kind === "manageEdit") {
      expect(result.current.state.site.name).toBe("New");
      expect(result.current.state.draft.name).toBe("New");
    }
  });

  it("requestDeleteCurrent from manageEditEditingDetails resolves the row and drops details (manage stays open)", () => {
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
    // Details modal closes; ManageSiteModal remains open behind the cascade
    // dialog. Cancelling the dialog returns to manageEdit.
    expect(result.current.state.kind).toBe("manageEdit");
  });

  it("dismissDeleteConfirm clears deleteTarget; underlying manage state stays (details was already closed)", () => {
    const site = create(SiteSchema, { id: 5n, name: "T" });
    const sites = [create(SiteWithCountsSchema, { site, deviceCount: 0n, rackCount: 0n, buildingCount: 0n })];
    const { result } = renderHook(() => useSiteModals({ refetchSites: vi.fn() }));
    act(() => result.current.openManageEdit(site));
    act(() => result.current.manageEditDetails());
    act(() => result.current.requestDeleteCurrent(sites));
    expect(result.current.deleteTarget).not.toBeNull();
    // requestDeleteCurrent dropped details → state is now manageEdit.
    expect(result.current.state.kind).toBe("manageEdit");
    act(() => result.current.dismissDeleteConfirm());
    expect(result.current.deleteTarget).toBeNull();
    expect(result.current.state.kind).toBe("manageEdit");
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
