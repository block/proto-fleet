import { createElement, type ReactNode } from "react";
import { MemoryRouter } from "react-router-dom";
import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import { useSiteModals } from "./useSiteModals";
import { buildingsClient, sitesClient } from "@/protoFleet/api/clients";
import { BuildingSchema, CreateBuildingResponseSchema } from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
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
  buildingsClient: {
    createBuilding: vi.fn(),
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

// useSiteModals now reads route scope + navigation (to keep the active-site
// slug in sync after a rename), so the hook must render inside a Router.
const wrapper = ({ children }: { children: ReactNode }) => createElement(MemoryRouter, null, children);

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
    const { result } = renderHook(() => useSiteModals({ refetchSites: vi.fn() }), { wrapper });
    act(() => result.current.openCreate());
    expect(result.current.state).toEqual({
      kind: "detailsCreate",
      draft: emptySiteFormValues(),
    });
  });

  it("detailsContinueCreate persists the site and opens manageEdit against the new row", async () => {
    const { create: createResp } = makeSiteResponse(7n, "North DC", "10.0.0.0/24");
    vi.mocked(sitesClient.createSite).mockResolvedValue(createResp);
    const refetchSites = vi.fn();
    const { result } = renderHook(() => useSiteModals({ refetchSites }), { wrapper });
    act(() => result.current.openCreate());

    await act(async () => {
      await result.current.detailsContinueCreate({
        ...emptySiteFormValues(),
        name: "North DC",
        networkConfig: "10.0.0.0/24",
      });
    });

    expect(sitesClient.createSite).toHaveBeenCalledTimes(1);
    expect(refetchSites).toHaveBeenCalled();
    // The manage modal now runs in edit mode against the freshly-created site.
    expect(result.current.state.kind).toBe("manageEdit");
    if (result.current.state.kind === "manageEdit") {
      expect(result.current.state.site.id).toBe(7n);
      expect(result.current.state.draft.name).toBe("North DC");
    }
  });

  it("detailsContinueCreate stays on details when CreateSite fails", async () => {
    vi.mocked(sitesClient.createSite).mockRejectedValue(new Error("boom"));
    const { result } = renderHook(() => useSiteModals({ refetchSites: vi.fn() }), { wrapper });
    act(() => result.current.openCreate());

    await act(async () => {
      await result.current.detailsContinueCreate({ ...emptySiteFormValues(), name: "North DC" });
    });

    // No transition — the operator can fix the input and retry.
    expect(result.current.state.kind).toBe("detailsCreate");
  });

  it("manageEditDetails on manageEdit stacks to manageEditEditingDetails; dismiss drops back to manageEdit", () => {
    const { result } = renderHook(() => useSiteModals({ refetchSites: vi.fn() }), { wrapper });
    const site = create(SiteSchema, { id: 1n, name: "S" });
    act(() => result.current.openManageEdit(site));
    act(() => result.current.manageEditDetails());
    expect(result.current.state.kind).toBe("manageEditEditingDetails");
    act(() => result.current.dismiss());
    expect(result.current.state.kind).toBe("manageEdit");
  });

  it("manageCreateBuilding creates a building against the managed site and returns it", async () => {
    vi.mocked(buildingsClient.createBuilding).mockResolvedValue(
      create(CreateBuildingResponseSchema, {
        building: create(BuildingSchema, { id: 42n, name: "Bldg A", siteId: 3n }),
        assignedRackCount: 0n,
        reassignedDeviceCount: 0n,
        conflicts: [],
      }),
    );
    const refetchSites = vi.fn();
    const site = create(SiteSchema, { id: 3n, name: "North DC" });
    const { result } = renderHook(() => useSiteModals({ refetchSites }), { wrapper });
    act(() => result.current.openManageEdit(site));

    let created: Awaited<ReturnType<typeof result.current.manageCreateBuilding>> | undefined;
    await act(async () => {
      created = await result.current.manageCreateBuilding({
        name: "Bldg A",
        description: "",
        powerCapacityMw: 0,
        overheadKw: 0,
        aisles: 0,
        racksPerAisle: 0,
        physicalRackCount: 0,
        defaultRackRows: 0,
        defaultRackColumns: 0,
        defaultRackOrderIndex: 0,
      });
    });

    // Created against the currently-managed site (id 3).
    expect(buildingsClient.createBuilding).toHaveBeenCalledWith(
      expect.objectContaining({ siteId: 3n, name: "Bldg A" }),
      expect.anything(),
    );
    expect(created?.id).toBe(42n);
    // Site catalog is refreshed for the building count; the modal injects the
    // row itself, so the building list is deliberately not refetched here.
    expect(refetchSites).toHaveBeenCalled();
  });

  it("manageAssignBuildings applies the picker delta via AssignBuildingsToSite", async () => {
    vi.mocked(sitesClient.assignBuildingsToSite).mockResolvedValue(
      create(AssignBuildingsToSiteResponseSchema, { reassignedRackCount: 0n, reassignedDeviceCount: 0n }),
    );
    const refetchSites = vi.fn();
    const refetchBuildings = vi.fn();
    const site = create(SiteSchema, { id: 3n, name: "North DC" });
    const { result } = renderHook(() => useSiteModals({ refetchSites, refetchBuildings }), { wrapper });
    act(() => result.current.openManageEdit(site));

    let ok: boolean | undefined;
    await act(async () => {
      ok = await result.current.manageAssignBuildings({ added: [10n], removed: [20n] });
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
    expect(ok).toBe(true);
    expect(refetchSites).toHaveBeenCalled();
    // Membership changed building rows, so the building table refresh fires too.
    expect(refetchBuildings).toHaveBeenCalled();
  });

  it("manageAssignBuildings with an empty delta fires no RPC", async () => {
    const { result } = renderHook(() => useSiteModals({ refetchSites: vi.fn() }), { wrapper });
    act(() => result.current.openManageEdit(create(SiteSchema, { id: 3n, name: "North DC" })));

    let ok: boolean | undefined;
    await act(async () => {
      ok = await result.current.manageAssignBuildings({ added: [], removed: [] });
    });

    expect(sitesClient.assignBuildingsToSite).not.toHaveBeenCalled();
    expect(ok).toBe(true);
  });

  it("manageRemoveBuilding unassigns the building immediately", async () => {
    vi.mocked(sitesClient.assignBuildingsToSite).mockResolvedValue(
      create(AssignBuildingsToSiteResponseSchema, { reassignedRackCount: 0n, reassignedDeviceCount: 0n }),
    );
    const refetchSites = vi.fn();
    const { result } = renderHook(() => useSiteModals({ refetchSites }), { wrapper });
    act(() => result.current.openManageEdit(create(SiteSchema, { id: 3n, name: "North DC" })));

    let ok: boolean | undefined;
    await act(async () => {
      ok = await result.current.manageRemoveBuilding(20n, "Building A");
    });

    expect(sitesClient.assignBuildingsToSite).toHaveBeenCalledWith(
      { buildingIds: [20n], targetSiteId: undefined },
      expect.anything(),
    );
    expect(ok).toBe(true);
    expect(refetchSites).toHaveBeenCalled();
  });

  it("detailsSaveEdit refreshes manage with server-canonical site on success", async () => {
    const initialSite = create(SiteSchema, { id: 9n, name: "Old" });
    const { update: updateResp } = makeSiteResponse(9n, "New");
    vi.mocked(sitesClient.updateSite).mockResolvedValue(updateResp);
    const { result } = renderHook(() => useSiteModals({ refetchSites: vi.fn() }), { wrapper });
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
    const { result } = renderHook(() => useSiteModals({ refetchSites: vi.fn() }), { wrapper });
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
    const { result } = renderHook(() => useSiteModals({ refetchSites: vi.fn() }), { wrapper });
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
    const site = create(SiteSchema, { id: 11n, name: "Active", slug: "active" });
    const sites = [create(SiteWithCountsSchema, { site, deviceCount: 0n, rackCount: 0n, buildingCount: 0n })];
    act(() => {
      useFleetStore.setState((s) => {
        s.ui.activeSite = { kind: "site", id: "11", slug: "active" };
      });
    });
    const { result } = renderHook(() => useSiteModals({ refetchSites: vi.fn() }), { wrapper });
    act(() => result.current.openManageEdit(site));
    act(() => result.current.requestDeleteCurrent(sites));

    await act(async () => {
      await result.current.deleteConfirm();
    });

    expect(useFleetStore.getState().ui.activeSite).toEqual({ kind: "all" });
    expect(result.current.deleteTarget).toBeNull();
    expect(result.current.state.kind).toBe("none");
  });

  it("detailsSaveEdit syncs the active-site slug when the active site is renamed", async () => {
    const renamed = create(SiteSchema, { id: 11n, name: "Renamed", slug: "renamed" });
    vi.mocked(sitesClient.updateSite).mockResolvedValue(
      create(UpdateSiteResponseSchema, { site: renamed, networkConfigWarnings: [] }),
    );
    const editing = create(SiteSchema, { id: 11n, name: "Active", slug: "active" });
    act(() => {
      useFleetStore.setState((s) => {
        s.ui.activeSite = { kind: "site", id: "11", slug: "active" };
      });
    });
    const { result } = renderHook(() => useSiteModals({ refetchSites: vi.fn() }), { wrapper });
    act(() => result.current.openManageEdit(editing));
    act(() => result.current.manageEditDetails());

    await act(async () => {
      await result.current.detailsSaveEdit({ ...emptySiteFormValues(), name: "Renamed" });
    });

    // Stale slug would otherwise make ResolveSiteBySlug clear the selection on
    // the next refresh; the store must now carry the regenerated slug.
    expect(useFleetStore.getState().ui.activeSite).toEqual({ kind: "site", id: "11", slug: "renamed" });
  });

  it("detailsSaveEdit leaves the active site untouched when a different site is renamed", async () => {
    const renamed = create(SiteSchema, { id: 99n, name: "Renamed", slug: "renamed" });
    vi.mocked(sitesClient.updateSite).mockResolvedValue(
      create(UpdateSiteResponseSchema, { site: renamed, networkConfigWarnings: [] }),
    );
    const editing = create(SiteSchema, { id: 99n, name: "Other", slug: "other" });
    act(() => {
      useFleetStore.setState((s) => {
        s.ui.activeSite = { kind: "site", id: "11", slug: "active" };
      });
    });
    const { result } = renderHook(() => useSiteModals({ refetchSites: vi.fn() }), { wrapper });
    act(() => result.current.openManageEdit(editing));
    act(() => result.current.manageEditDetails());

    await act(async () => {
      await result.current.detailsSaveEdit({ ...emptySiteFormValues(), name: "Renamed" });
    });

    expect(useFleetStore.getState().ui.activeSite).toEqual({ kind: "site", id: "11", slug: "active" });
  });
});
