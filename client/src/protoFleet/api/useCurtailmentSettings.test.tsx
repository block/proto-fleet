import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import { curtailmentClient } from "@/protoFleet/api/clients";
import {
  CurtailmentSettingsSchema,
  type UpdateCurtailmentSettingsRequest,
} from "@/protoFleet/api/generated/curtailment/v1/curtailment_pb";
import useCurtailmentSettings from "@/protoFleet/api/useCurtailmentSettings";

const { mockGetCurtailmentSettings, mockHandleAuthErrors, mockUpdateCurtailmentSettings } = vi.hoisted(() => ({
  mockGetCurtailmentSettings: vi.fn(),
  mockHandleAuthErrors: vi.fn(),
  mockUpdateCurtailmentSettings: vi.fn(),
}));

vi.mock("@/protoFleet/api/clients", () => ({
  curtailmentClient: {
    getCurtailmentSettings: mockGetCurtailmentSettings,
    updateCurtailmentSettings: mockUpdateCurtailmentSettings,
  },
}));

vi.mock("@/protoFleet/store", () => ({
  useAuthErrors: () => ({
    handleAuthErrors: mockHandleAuthErrors,
  }),
}));

describe("useCurtailmentSettings", () => {
  beforeEach(() => {
    mockGetCurtailmentSettings.mockReset();
    mockHandleAuthErrors.mockReset();
    mockUpdateCurtailmentSettings.mockReset();
  });

  it("maps loaded settings from the API", async () => {
    mockGetCurtailmentSettings.mockResolvedValue({
      settings: create(CurtailmentSettingsSchema, { postEventCooldownSec: 600 }),
    });

    const { result } = renderHook(() => useCurtailmentSettings());

    await waitFor(() => expect(result.current.settings).toEqual({ postEventCooldownSec: 600 }));
    expect(result.current.loadError).toBeNull();
  });

  it("surfaces missing settings in load responses as an error", async () => {
    mockGetCurtailmentSettings.mockResolvedValue({});

    const { result } = renderHook(() => useCurtailmentSettings());

    await waitFor(() => expect(result.current.loadError).toBe("Curtailment settings response was missing settings."));
    expect(result.current.settings).toBeNull();
    expect(mockHandleAuthErrors).toHaveBeenCalled();
  });

  it("sends explicit zero when updating settings", async () => {
    mockUpdateCurtailmentSettings.mockResolvedValue({
      settings: create(CurtailmentSettingsSchema, { postEventCooldownSec: 0 }),
    });
    const { result } = renderHook(() => useCurtailmentSettings(false));

    await act(async () => {
      await expect(result.current.updateSettings({ postEventCooldownSec: 0 })).resolves.toEqual({
        postEventCooldownSec: 0,
      });
    });

    expect(
      (curtailmentClient.updateCurtailmentSettings as typeof mockUpdateCurtailmentSettings).mock.calls[0]?.[0],
    ).toMatchObject({
      postEventCooldownSec: 0,
    } satisfies Partial<UpdateCurtailmentSettingsRequest>);
  });

  it("surfaces missing settings in update responses as an error", async () => {
    mockUpdateCurtailmentSettings.mockResolvedValue({});
    const { result } = renderHook(() => useCurtailmentSettings(false));

    await act(async () => {
      await expect(result.current.updateSettings({ postEventCooldownSec: 0 })).rejects.toThrow(
        "Curtailment settings response was missing settings.",
      );
    });

    expect(result.current.settings).toBeNull();
    expect(result.current.isSaving).toBe(false);
    expect(mockHandleAuthErrors).toHaveBeenCalled();
  });
});
