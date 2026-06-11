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

const siteLabelsById = new Map([["101", "Austin, TX"]]);

const fixedKwFormValues: ResponseProfileFormValues = {
  name: "Partial reduction",
  actionType: "fixedKwReduction",
  targetKw: "2000",
  deviceIdentifiers: [],
  siteId: "101",
  siteName: "Austin, TX",
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

    const { result } = renderHook(() => useCurtailmentResponseProfiles(false, siteLabelsById));

    await act(async () => {
      await result.current.listResponseProfiles();
    });

    expect(result.current.responseProfiles[0]).toMatchObject({
      id: "7",
      name: "Partial reduction",
      targetSummary: "2,000 kW target",
      siteId: "101",
      scope: "Austin, TX",
      restoreBehavior: "Restore immediately",
      deadlineSummary: "Within 15 min",
      formValues: fixedKwFormValues,
    });
    expect(result.current.isLoading).toBe(false);
  });

  it("creates and updates profiles using the generated CRUD payload shape", async () => {
    mockCreateCurtailmentResponseProfile.mockResolvedValueOnce({ profile: apiProfile() });
    mockUpdateCurtailmentResponseProfile.mockResolvedValueOnce({ profile: apiProfile({ profileName: "Updated" }) });

    const { result } = renderHook(() => useCurtailmentResponseProfiles(false, siteLabelsById));

    await act(async () => {
      await result.current.createResponseProfile(fixedKwFormValues);
    });

    expect(mockCreateCurtailmentResponseProfile).toHaveBeenCalledWith(
      expect.objectContaining({
        profileName: "Partial reduction",
        site: expect.objectContaining({ siteId: 101n }),
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

    await act(async () => {
      await result.current.updateResponseProfile("7", { ...fixedKwFormValues, name: "Updated" });
    });

    expect(mockUpdateCurtailmentResponseProfile).toHaveBeenCalledWith(
      expect.objectContaining({
        profileId: 7n,
        profileName: "Updated",
        site: expect.objectContaining({ siteId: 101n }),
      }),
    );
  });

  it("omits site from the CRUD payload when no site is selected", async () => {
    mockCreateCurtailmentResponseProfile.mockResolvedValueOnce({ profile: apiProfile() });
    const { result } = renderHook(() => useCurtailmentResponseProfiles(false, siteLabelsById));

    await act(async () => {
      await result.current.createResponseProfile({
        ...fixedKwFormValues,
        siteId: "",
        siteName: "",
      });
    });

    const createRequest = mockCreateCurtailmentResponseProfile.mock.calls[0]?.[0];
    expect(createRequest).toEqual(expect.objectContaining({ profileName: "Partial reduction" }));
    expect(createRequest?.site).toBeUndefined();
  });

  it("keeps unresolved API sites as site-scoped profiles", async () => {
    mockListCurtailmentResponseProfiles.mockResolvedValueOnce({ profiles: [apiProfile()] });

    const { result } = renderHook(() => useCurtailmentResponseProfiles(false));

    await act(async () => {
      await result.current.listResponseProfiles();
    });

    expect(result.current.responseProfiles[0]).toMatchObject({
      siteId: "101",
      scope: "Site 101",
      formValues: expect.objectContaining({
        siteId: "101",
        siteName: "Site 101",
      }),
    });
  });

  it("maps API profiles without sites as whole-fleet profiles", async () => {
    mockListCurtailmentResponseProfiles.mockResolvedValueOnce({ profiles: [apiProfile({ site: undefined })] });

    const { result } = renderHook(() => useCurtailmentResponseProfiles(false, siteLabelsById));

    await act(async () => {
      await result.current.listResponseProfiles();
    });

    expect(result.current.responseProfiles[0]).toMatchObject({
      siteId: "",
      scope: "Whole fleet",
      formValues: expect.objectContaining({
        siteId: "",
        siteName: "",
      }),
    });
  });

  it("does not preserve submitted miner selections for API-backed response profiles", async () => {
    mockCreateCurtailmentResponseProfile.mockResolvedValueOnce({ profile: apiProfile() });
    mockListCurtailmentResponseProfiles.mockResolvedValueOnce({ profiles: [apiProfile()] });
    const { result } = renderHook(() => useCurtailmentResponseProfiles(false, siteLabelsById));

    await act(async () => {
      await result.current.createResponseProfile({
        ...fixedKwFormValues,
        deviceIdentifiers: ["miner-1", "miner-2", "miner-3"],
      });
    });

    await act(async () => {
      await result.current.listResponseProfiles();
    });

    expect(result.current.responseProfiles[0]).toMatchObject({
      scope: "Austin, TX",
      formValues: expect.objectContaining({
        deviceIdentifiers: [],
      }),
    });
  });

  it("deletes response profiles by id", async () => {
    mockDeleteCurtailmentResponseProfile.mockResolvedValueOnce({});

    const { result } = renderHook(() => useCurtailmentResponseProfiles(false, siteLabelsById));

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
