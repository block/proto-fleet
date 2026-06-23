import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import {
  CurtailmentLevel,
  CurtailmentMode,
  CurtailmentPriority,
  type CurtailmentResponseProfile,
  CurtailmentResponseProfileSchema,
  CurtailmentStrategy,
  FixedKwParamsSchema,
  ScopeSiteSchema,
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
  restoreBatchSize: "10000",
  restoreIntervalSec: "0",
  responseDeadlineMinutes: "15",
  includeMaintenance: false,
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
    restoreBatchSize: 10_000,
    restoreBatchIntervalSec: 0,
  });

  return Object.assign(profile, overrides);
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
        siteId: "101",
        siteName: "Site 101",
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
      await result.current.createResponseProfile(fixedKwFormValues);
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
        restoreBatchSize: 10_000,
        restoreBatchIntervalSec: 0,
      }),
    );
    expect(mockCreateCurtailmentResponseProfile.mock.calls[0]?.[0]?.site).toBeUndefined();

    await act(async () => {
      await result.current.updateResponseProfile("7", { ...fixedKwFormValues, name: "Updated" });
    });

    expect(mockUpdateCurtailmentResponseProfile).toHaveBeenCalledWith(
      expect.objectContaining({
        profileId: 7n,
        profileName: "Updated",
      }),
    );
    expect(mockUpdateCurtailmentResponseProfile.mock.calls[0]?.[0]?.site).toBeUndefined();
  });

  it("preserves site in the CRUD payload when site values are present", async () => {
    mockCreateCurtailmentResponseProfile.mockResolvedValueOnce({ profile: apiProfile() });
    mockUpdateCurtailmentResponseProfile.mockResolvedValueOnce({ profile: apiProfile({ profileName: "Updated" }) });
    const { result } = renderHook(() => useCurtailmentResponseProfiles(false));
    const siteScopedValues = {
      ...fixedKwFormValues,
      siteId: "101",
      siteName: "Site 101",
    };

    await act(async () => {
      await result.current.createResponseProfile(siteScopedValues);
    });

    const createRequest = mockCreateCurtailmentResponseProfile.mock.calls[0]?.[0];
    expect(createRequest).toEqual(expect.objectContaining({ profileName: "Partial reduction" }));
    expect(createRequest?.site?.siteId).toBe(101n);

    await act(async () => {
      await result.current.updateResponseProfile("7", { ...siteScopedValues, name: "Updated" });
    });

    const updateRequest = mockUpdateCurtailmentResponseProfile.mock.calls[0]?.[0];
    expect(updateRequest).toEqual(expect.objectContaining({ profileId: 7n, profileName: "Updated" }));
    expect(updateRequest?.site?.siteId).toBe(101n);
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

  it("drops submitted miner selections for API-backed response profiles", async () => {
    mockCreateCurtailmentResponseProfile.mockResolvedValueOnce({ profile: apiProfile({ site: undefined }) });
    mockListCurtailmentResponseProfiles.mockResolvedValueOnce({ profiles: [apiProfile({ site: undefined })] });
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
      scope: "Whole fleet",
      formValues: expect.objectContaining({
        deviceIdentifiers: [],
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
