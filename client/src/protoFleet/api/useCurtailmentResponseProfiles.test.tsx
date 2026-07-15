import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import {
  CurtailmentLevel,
  CurtailmentMode,
  CurtailmentPriority,
  type CurtailmentResponseProfile,
  CurtailmentResponseProfileSchema,
  CurtailmentScopeSchema,
  CurtailmentStrategy,
  FixedKwParamsSchema,
  ScopeDeviceListSchema,
  ScopeSiteSchema,
  ScopeWholeOrgSchema,
} from "@/protoFleet/api/generated/curtailment/v1/curtailment_pb";
import useCurtailmentResponseProfiles, {
  clearCurtailmentResponseProfileSessionCacheForTest,
} from "@/protoFleet/api/useCurtailmentResponseProfiles";
import type { ResponseProfileFormValues } from "@/protoFleet/features/settings/components/Curtailment/types";

const {
  mockCreateCurtailmentResponseProfile,
  mockDeleteCurtailmentResponseProfile,
  mockHandleAuthErrors,
  mockListCurtailmentResponseProfiles,
  mockUpdateCurtailmentResponseProfile,
} = vi.hoisted(() => ({
  mockCreateCurtailmentResponseProfile: vi.fn(),
  mockDeleteCurtailmentResponseProfile: vi.fn(),
  mockHandleAuthErrors: vi.fn(),
  mockListCurtailmentResponseProfiles: vi.fn(),
  mockUpdateCurtailmentResponseProfile: vi.fn(),
}));

vi.mock("@/protoFleet/api/clients", () => ({
  curtailmentClient: {
    createCurtailmentResponseProfile: mockCreateCurtailmentResponseProfile,
    deleteCurtailmentResponseProfile: mockDeleteCurtailmentResponseProfile,
    listCurtailmentResponseProfiles: mockListCurtailmentResponseProfiles,
    updateCurtailmentResponseProfile: mockUpdateCurtailmentResponseProfile,
  },
}));

vi.mock("@/protoFleet/store", () => ({
  useAuthErrors: () => ({
    handleAuthErrors: mockHandleAuthErrors,
  }),
}));

const fixedKwFormValues: ResponseProfileFormValues = {
  name: "Partial reduction",
  actionType: "fixedKwReduction",
  targetKw: "2000",
  deviceIdentifiers: [],
  siteId: "",
  siteName: "",
  selectionStrategy: "leastEfficientFirst",
  restoreBehavior: "automaticImmediateRestore",
  minDurationSec: "",
  maxDurationSec: "",
  curtailBatchSize: "50",
  curtailBatchIntervalSec: "30",
  restoreBatchSize: "0",
  restoreIntervalSec: "0",
  responseDeadlineMinutes: "15",
  includeMaintenance: false,
  forceIncludeAllPairedMiners: false,
};

function apiProfile(overrides: Partial<CurtailmentResponseProfile> = {}): CurtailmentResponseProfile {
  const profile = create(CurtailmentResponseProfileSchema, {
    profileId: 7n,
    profileName: "Partial reduction",
    site: create(ScopeSiteSchema, { siteId: 101n }),
    mode: CurtailmentMode.FIXED_KW,
    strategy: CurtailmentStrategy.LEAST_EFFICIENT_FIRST,
    level: CurtailmentLevel.FULL,
    priority: CurtailmentPriority.NORMAL,
    modeParams: {
      case: "fixedKw",
      value: create(FixedKwParamsSchema, { targetKw: 2000 }),
    },
    curtailBatchSize: 50,
    curtailBatchIntervalSec: 30,
    restoreBatchSize: 0,
    restoreBatchIntervalSec: 0,
    facilityFanDeviceIds: [31n, 32n],
    fanOffDelaySec: 45,
    fanRestoreDelaySec: 90,
  });

  return Object.assign(profile, overrides);
}

function expectWholeOrgScope(scopes: CurtailmentResponseProfile["scopes"] | undefined): void {
  expect(scopes).toHaveLength(1);
  expect(scopes?.[0]?.scope.case).toBe("wholeOrg");
}

describe("useCurtailmentResponseProfiles", () => {
  beforeEach(() => {
    mockCreateCurtailmentResponseProfile.mockReset();
    mockDeleteCurtailmentResponseProfile.mockReset();
    mockHandleAuthErrors.mockReset();
    mockListCurtailmentResponseProfiles.mockReset();
    mockUpdateCurtailmentResponseProfile.mockReset();
    clearCurtailmentResponseProfileSessionCacheForTest();
  });

  it("lists and maps response profiles for the settings cards", async () => {
    mockListCurtailmentResponseProfiles.mockResolvedValueOnce({ profiles: [apiProfile()] });

    const { result } = renderHook(() => useCurtailmentResponseProfiles(false));

    await act(async () => {
      await result.current.listResponseProfiles();
    });

    expect(result.current.responseProfiles[0]).toMatchObject({
      id: "7",
      name: "Partial reduction",
      targetSummary: "2,000 kW target",
      scope: "Site 101",
      restoreBehavior: "Restore immediately",
      deadlineSummary: "Within 15 min",
      formValues: {
        ...fixedKwFormValues,
        facilityFanDeviceIds: ["31", "32"],
        fanOffDelaySec: "45",
        fanRestoreDelaySec: "90",
        siteId: "101",
        siteName: "Site 101",
        siteIds: ["101"],
        siteNamesById: { "101": "Site 101" },
      },
    });
    expect(result.current.isLoading).toBe(false);
  });

  it("creates and updates profiles using the generated CRUD payload shape", async () => {
    mockCreateCurtailmentResponseProfile.mockResolvedValueOnce({ profile: apiProfile({ site: undefined }) });
    mockUpdateCurtailmentResponseProfile.mockResolvedValueOnce({
      profile: apiProfile({ profileName: "Updated", site: undefined }),
    });

    const { result } = renderHook(() => useCurtailmentResponseProfiles(false));

    await act(async () => {
      await result.current.createResponseProfile({
        ...fixedKwFormValues,
        facilityFanDeviceIds: ["31", "32"],
        fanOffDelaySec: "45",
        fanRestoreDelaySec: "90",
      });
    });

    expect(mockCreateCurtailmentResponseProfile).toHaveBeenCalledWith(
      expect.objectContaining({
        profileName: "Partial reduction",
        mode: CurtailmentMode.FIXED_KW,
        modeParams: expect.objectContaining({
          case: "fixedKw",
          value: expect.objectContaining({ targetKw: 2000 }),
        }),
        curtailBatchSize: 50,
        curtailBatchIntervalSec: 30,
        restoreBatchSize: 0,
        restoreBatchIntervalSec: 0,
        facilityFanDeviceIds: [31n, 32n],
        fanOffDelaySec: 45,
        fanRestoreDelaySec: 90,
      }),
    );
    expectWholeOrgScope(mockCreateCurtailmentResponseProfile.mock.calls[0]?.[0]?.scopes);

    await act(async () => {
      await result.current.updateResponseProfile("7", { ...fixedKwFormValues, name: "Updated" });
    });

    expect(mockUpdateCurtailmentResponseProfile).toHaveBeenCalledWith(
      expect.objectContaining({
        profileId: 7n,
        profileName: "Updated",
      }),
    );
    expectWholeOrgScope(mockUpdateCurtailmentResponseProfile.mock.calls[0]?.[0]?.scopes);
  });

  it("sends all-paired targeting only for full-fleet response profiles", async () => {
    mockCreateCurtailmentResponseProfile.mockResolvedValue({ profile: apiProfile({ site: undefined }) });
    const { result } = renderHook(() => useCurtailmentResponseProfiles(false));

    await act(async () => {
      await result.current.createResponseProfile({
        ...fixedKwFormValues,
        actionType: "fullFleet",
        forceIncludeAllPairedMiners: true,
      });
    });

    // All-paired targeting also opts in maintenance-flagged miners,
    // mirroring the Start request builders.
    expect(mockCreateCurtailmentResponseProfile).toHaveBeenLastCalledWith(
      expect.objectContaining({
        mode: CurtailmentMode.FULL_FLEET,
        forceIncludeAllPairedMiners: true,
        includeMaintenance: true,
        forceIncludeMaintenance: true,
      }),
    );

    await act(async () => {
      await result.current.createResponseProfile({
        ...fixedKwFormValues,
        forceIncludeAllPairedMiners: true,
      });
    });

    expect(mockCreateCurtailmentResponseProfile).toHaveBeenLastCalledWith(
      expect.objectContaining({
        mode: CurtailmentMode.FIXED_KW,
        forceIncludeAllPairedMiners: false,
        includeMaintenance: false,
        forceIncludeMaintenance: false,
      }),
    );
  });

  it("strips all-paired targeting from miner-scoped response profiles", async () => {
    mockCreateCurtailmentResponseProfile.mockResolvedValue({ profile: apiProfile({ site: undefined }) });
    const { result } = renderHook(() => useCurtailmentResponseProfiles(false));

    await act(async () => {
      await result.current.createResponseProfile({
        ...fixedKwFormValues,
        actionType: "fullFleet",
        deviceIdentifiers: ["miner-1"],
        forceIncludeAllPairedMiners: true,
      });
    });

    // Explicit-miner scopes are open loop; the server rejects the all-paired
    // flag there, so the payload builder must not send it.
    expect(mockCreateCurtailmentResponseProfile).toHaveBeenLastCalledWith(
      expect.objectContaining({
        mode: CurtailmentMode.FULL_FLEET,
        forceIncludeAllPairedMiners: false,
        includeMaintenance: false,
        forceIncludeMaintenance: false,
      }),
    );
  });

  it("drops stale maintenance inclusion when all-paired targeting is unchecked", async () => {
    mockUpdateCurtailmentResponseProfile.mockResolvedValue({
      profile: apiProfile({ profileName: "Formerly all-paired", site: undefined }),
    });
    const { result } = renderHook(() => useCurtailmentResponseProfiles(false));

    // A profile previously saved with all-paired enabled hydrates
    // includeMaintenance: true into the edit form. Unchecking "Target all
    // paired miners" must clear the admin-gated maintenance pair on save —
    // the maintenance toggle no longer exists in the UI, so nothing else can.
    await act(async () => {
      await result.current.updateResponseProfile("7", {
        ...fixedKwFormValues,
        name: "Formerly all-paired",
        actionType: "fullFleet",
        includeMaintenance: true,
        forceIncludeAllPairedMiners: false,
      });
    });

    expect(mockUpdateCurtailmentResponseProfile).toHaveBeenLastCalledWith(
      expect.objectContaining({
        mode: CurtailmentMode.FULL_FLEET,
        forceIncludeAllPairedMiners: false,
        includeMaintenance: false,
        forceIncludeMaintenance: false,
      }),
    );
  });

  it("rejects batch restore profiles without a positive restore batch size", async () => {
    const { result } = renderHook(() => useCurtailmentResponseProfiles(false));

    await expect(
      act(async () => {
        await result.current.createResponseProfile({
          ...fixedKwFormValues,
          restoreBehavior: "automaticBatchRestore",
          restoreBatchSize: "",
          restoreIntervalSec: "",
        });
      }),
    ).rejects.toThrow("Enter restore batch size greater than 0 for batch restore.");

    expect(mockCreateCurtailmentResponseProfile).not.toHaveBeenCalled();
  });

  it("preserves site in the CRUD payload when site values are present", async () => {
    mockCreateCurtailmentResponseProfile.mockResolvedValueOnce({ profile: apiProfile() });
    mockUpdateCurtailmentResponseProfile.mockResolvedValueOnce({ profile: apiProfile({ profileName: "Updated" }) });
    const { result } = renderHook(() => useCurtailmentResponseProfiles(false));
    const siteScopedValues = {
      ...fixedKwFormValues,
      siteSelection: "site" as const,
      siteId: "101",
      siteName: "Site 101",
      siteIds: ["101"],
      siteNamesById: { "101": "Site 101" },
    };

    await act(async () => {
      await result.current.createResponseProfile(siteScopedValues);
    });

    const createRequest = mockCreateCurtailmentResponseProfile.mock.calls[0]?.[0];
    expect(createRequest).toEqual(expect.objectContaining({ profileName: "Partial reduction" }));
    expect(createRequest?.scopes).toHaveLength(1);
    expect(createRequest?.scopes[0]?.scope.case).toBe("site");
    if (createRequest?.scopes?.[0]?.scope.case !== "site") {
      throw new Error("Expected site scope");
    }
    expect(createRequest.scopes[0].scope.value.siteId).toBe(101n);

    await act(async () => {
      await result.current.updateResponseProfile("7", { ...siteScopedValues, name: "Updated" });
    });

    const updateRequest = mockUpdateCurtailmentResponseProfile.mock.calls[0]?.[0];
    expect(updateRequest).toEqual(expect.objectContaining({ profileId: 7n, profileName: "Updated" }));
    expect(updateRequest?.scopes).toHaveLength(1);
    expect(updateRequest?.scopes[0]?.scope.case).toBe("site");
    if (updateRequest?.scopes?.[0]?.scope.case !== "site") {
      throw new Error("Expected site scope");
    }
    expect(updateRequest.scopes[0].scope.value.siteId).toBe(101n);
  });

  it("preserves multiple sites in CRUD payloads without expanding miners", async () => {
    const site101Scope = create(CurtailmentScopeSchema, {
      scope: { case: "site", value: create(ScopeSiteSchema, { siteId: 101n }) },
    });
    const site102Scope = create(CurtailmentScopeSchema, {
      scope: { case: "site", value: create(ScopeSiteSchema, { siteId: 102n }) },
    });
    const minerScope = create(CurtailmentScopeSchema, {
      scope: {
        case: "deviceIdentifiers",
        value: create(ScopeDeviceListSchema, { deviceIdentifiers: ["miner-1", "miner-2"] }),
      },
    });
    mockCreateCurtailmentResponseProfile.mockResolvedValueOnce({
      profile: apiProfile({ site: undefined, scopes: [site101Scope, site102Scope, minerScope] }),
    });
    const { result } = renderHook(() => useCurtailmentResponseProfiles(false));

    await act(async () => {
      await result.current.createResponseProfile({
        ...fixedKwFormValues,
        siteSelection: "site",
        siteId: "101",
        siteName: "Austin, TX",
        siteIds: ["101", "102"],
        siteNamesById: { "101": "Austin, TX", "102": "Denver, CO" },
        deviceIdentifiers: ["miner-1", "miner-1", "miner-2"],
      });
    });

    const createRequest = mockCreateCurtailmentResponseProfile.mock.calls[0]?.[0];
    expect(createRequest?.scopes).toHaveLength(3);
    expect(createRequest?.scopes[0]?.scope.case).toBe("site");
    expect(createRequest?.scopes[1]?.scope.case).toBe("site");
    expect(createRequest?.scopes[2]?.scope.case).toBe("deviceIdentifiers");
    if (
      createRequest?.scopes?.[0]?.scope.case !== "site" ||
      createRequest.scopes[1]?.scope.case !== "site" ||
      createRequest.scopes[2]?.scope.case !== "deviceIdentifiers"
    ) {
      throw new Error("Expected two site scopes and one miner scope");
    }
    expect(createRequest.scopes[0].scope.value.siteId).toBe(101n);
    expect(createRequest.scopes[1].scope.value.siteId).toBe(102n);
    expect(createRequest.scopes[2].scope.value.deviceIdentifiers).toEqual(["miner-1", "miner-2"]);
    expect(result.current.responseProfiles[0]).toMatchObject({
      scope: "2 sites + 2 miners",
      formValues: expect.objectContaining({
        siteId: "101",
        siteIds: ["101", "102"],
        siteNamesById: { "101": "Austin, TX", "102": "Denver, CO" },
      }),
    });
  });

  it("preserves all-sites profile selections as site scopes", async () => {
    const site101Scope = create(CurtailmentScopeSchema, {
      scope: { case: "site", value: create(ScopeSiteSchema, { siteId: 101n }) },
    });
    const site102Scope = create(CurtailmentScopeSchema, {
      scope: { case: "site", value: create(ScopeSiteSchema, { siteId: 102n }) },
    });
    mockCreateCurtailmentResponseProfile.mockResolvedValueOnce({
      profile: apiProfile({ site: undefined, scopes: [site101Scope, site102Scope] }),
    });
    const { result } = renderHook(() => useCurtailmentResponseProfiles(false));

    await act(async () => {
      await result.current.createResponseProfile({
        ...fixedKwFormValues,
        siteSelection: "allSites",
        siteId: "101",
        siteName: "Austin, TX",
        siteIds: ["101", "102"],
        siteNamesById: { "101": "Austin, TX", "102": "Denver, CO" },
      });
    });

    const createRequest = mockCreateCurtailmentResponseProfile.mock.calls[0]?.[0];
    expect(createRequest?.scopes).toHaveLength(2);
    expect([createRequest?.scopes?.[0]?.scope.case, createRequest?.scopes?.[1]?.scope.case]).toEqual(["site", "site"]);
    if (createRequest?.scopes?.[0]?.scope.case !== "site" || createRequest.scopes[1]?.scope.case !== "site") {
      throw new Error("Expected all-sites profile scope to preserve selected sites");
    }
    expect(createRequest.scopes[0].scope.value.siteId).toBe(101n);
    expect(createRequest.scopes[1].scope.value.siteId).toBe(102n);
    expect(result.current.responseProfiles[0]).toMatchObject({
      scope: "All sites",
      formValues: expect.objectContaining({
        siteSelection: "allSites",
        siteId: "101",
        siteIds: ["101", "102"],
        siteNamesById: { "101": "Austin, TX", "102": "Denver, CO" },
      }),
    });
  });

  it("collapses all-miner response profile selections to whole org", async () => {
    const wholeOrgScope = create(CurtailmentScopeSchema, {
      scope: { case: "wholeOrg", value: create(ScopeWholeOrgSchema, {}) },
    });
    mockCreateCurtailmentResponseProfile.mockResolvedValueOnce({
      profile: apiProfile({ site: undefined, scopes: [wholeOrgScope] }),
    });
    const { result } = renderHook(() => useCurtailmentResponseProfiles(false));

    await act(async () => {
      await result.current.createResponseProfile({
        ...fixedKwFormValues,
        minerSelectionMode: "all",
        deviceIdentifiers: ["miner-1", "miner-2"],
        siteId: "101",
        siteName: "Site 101",
        siteIds: ["101"],
      });
    });

    const createRequest = mockCreateCurtailmentResponseProfile.mock.calls[0]?.[0];
    expect(createRequest?.scopes).toHaveLength(1);
    expect(createRequest?.scopes[0]?.scope.case).toBe("wholeOrg");
  });

  it("maps API profiles with sites as site-scoped profiles", async () => {
    mockListCurtailmentResponseProfiles.mockResolvedValueOnce({ profiles: [apiProfile()] });

    const { result } = renderHook(() => useCurtailmentResponseProfiles(false));

    await act(async () => {
      await result.current.listResponseProfiles();
    });

    expect(result.current.responseProfiles[0]).toMatchObject({
      scope: "Site 101",
      formValues: expect.objectContaining({
        siteId: "101",
        siteName: "Site 101",
      }),
    });
  });

  it("uses loaded site names for site-scoped API profiles", async () => {
    mockListCurtailmentResponseProfiles.mockResolvedValueOnce({ profiles: [apiProfile()] });
    const siteNameById = new Map([["101", "Austin, TX"]]);

    const { result } = renderHook(() => useCurtailmentResponseProfiles(false, { siteNameById }));

    await act(async () => {
      await result.current.listResponseProfiles();
    });

    expect(result.current.responseProfiles[0]).toMatchObject({
      scope: "Austin, TX",
      formValues: expect.objectContaining({
        siteId: "101",
        siteName: "Austin, TX",
      }),
    });
  });

  it("remaps loaded profiles when site names arrive without refetching profiles", async () => {
    mockListCurtailmentResponseProfiles.mockResolvedValueOnce({ profiles: [apiProfile()] });

    const { result, rerender } = renderHook(
      ({ siteNameById }: { siteNameById?: Map<string, string> }) =>
        useCurtailmentResponseProfiles(true, { siteNameById }),
      {
        initialProps: {
          siteNameById: undefined as Map<string, string> | undefined,
        },
      },
    );

    await waitFor(() => expect(result.current.responseProfiles[0]?.scope).toBe("Site 101"));

    rerender({
      siteNameById: new Map([["101", "Austin, TX"]]),
    });

    expect(result.current.responseProfiles[0]).toMatchObject({
      scope: "Austin, TX",
      formValues: expect.objectContaining({
        siteId: "101",
        siteName: "Austin, TX",
      }),
    });
    expect(mockListCurtailmentResponseProfiles).toHaveBeenCalledTimes(1);
  });

  it("treats default zero curtail batch intervals as unset without a batch size", async () => {
    mockListCurtailmentResponseProfiles.mockResolvedValueOnce({
      profiles: [apiProfile({ curtailBatchSize: 0, curtailBatchIntervalSec: 0 })],
    });

    const { result } = renderHook(() => useCurtailmentResponseProfiles(false));

    await act(async () => {
      await result.current.listResponseProfiles();
    });

    expect(result.current.responseProfiles[0]?.formValues).toEqual(
      expect.objectContaining({
        curtailBatchSize: "",
        curtailBatchIntervalSec: "",
      }),
    );
  });

  it("maps API profiles without sites as whole-fleet profiles", async () => {
    mockListCurtailmentResponseProfiles.mockResolvedValueOnce({ profiles: [apiProfile({ site: undefined })] });

    const { result } = renderHook(() => useCurtailmentResponseProfiles(false));

    await act(async () => {
      await result.current.listResponseProfiles();
    });

    expect(result.current.responseProfiles[0]).toMatchObject({
      scope: "Whole fleet",
      formValues: expect.objectContaining({
        siteId: "",
        siteName: "",
      }),
    });
  });

  it("maps explicit whole-org API scopes as all-miner form state", async () => {
    const wholeOrgScope = create(CurtailmentScopeSchema, {
      scope: { case: "wholeOrg", value: create(ScopeWholeOrgSchema, {}) },
    });
    mockListCurtailmentResponseProfiles.mockResolvedValueOnce({
      profiles: [apiProfile({ site: undefined, scopes: [wholeOrgScope] })],
    });

    const { result } = renderHook(() => useCurtailmentResponseProfiles(false));

    await act(async () => {
      await result.current.listResponseProfiles();
    });

    expect(result.current.responseProfiles[0]).toMatchObject({
      scope: "Whole fleet",
      formValues: expect.objectContaining({
        minerSelectionMode: "all",
        siteSelection: "allSites",
        siteId: "",
        siteIds: [],
        deviceIdentifiers: [],
      }),
    });
  });

  it("maps full-fleet API mode to the whole-fleet card scope", async () => {
    mockListCurtailmentResponseProfiles.mockResolvedValueOnce({
      profiles: [
        apiProfile({
          mode: CurtailmentMode.FULL_FLEET,
          modeParams: { case: undefined },
          site: undefined,
        }),
      ],
    });

    const { result } = renderHook(() => useCurtailmentResponseProfiles(false));

    await act(async () => {
      await result.current.listResponseProfiles();
    });

    expect(result.current.responseProfiles[0]).toMatchObject({
      targetSummary: "100% reduction",
      scope: "Whole fleet",
    });
  });

  it("preserves submitted miner selections for API-backed response profiles", async () => {
    const minerScope = create(CurtailmentScopeSchema, {
      scope: {
        case: "deviceIdentifiers",
        value: create(ScopeDeviceListSchema, { deviceIdentifiers: ["miner-1", "miner-2", "miner-3"] }),
      },
    });
    mockCreateCurtailmentResponseProfile.mockResolvedValueOnce({
      profile: apiProfile({ site: undefined, scopes: [minerScope] }),
    });
    mockListCurtailmentResponseProfiles.mockResolvedValueOnce({
      profiles: [apiProfile({ site: undefined, scopes: [minerScope] })],
    });
    const { result } = renderHook(() => useCurtailmentResponseProfiles(false));
    const minerScopedValues = {
      ...fixedKwFormValues,
      deviceIdentifiers: ["miner-1", "miner-2", "miner-3"],
      siteId: "",
      siteName: "",
    };

    await act(async () => {
      await result.current.createResponseProfile(minerScopedValues);
    });

    await act(async () => {
      await result.current.listResponseProfiles();
    });

    expect(result.current.responseProfiles[0]).toMatchObject({
      scope: "3 miners",
      formValues: expect.objectContaining({
        deviceIdentifiers: ["miner-1", "miner-2", "miner-3"],
        siteId: "",
        siteName: "",
      }),
    });
  });

  it("deletes response profiles by id", async () => {
    mockDeleteCurtailmentResponseProfile.mockResolvedValueOnce({});

    const { result } = renderHook(() => useCurtailmentResponseProfiles(false));

    await act(async () => {
      await result.current.deleteResponseProfile("7");
    });

    expect(mockDeleteCurtailmentResponseProfile).toHaveBeenCalledWith(
      expect.objectContaining({
        profileId: 7n,
      }),
    );
  });
});
