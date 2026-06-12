import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import {
  type CurtailmentAutomationRule,
  CurtailmentAutomationRuleSchema,
  CurtailmentAutomationSignal,
  CurtailmentAutomationTriggerType,
} from "@/protoFleet/api/generated/curtailment/v1/curtailment_pb";
import useCurtailmentAutomationRules from "@/protoFleet/api/useCurtailmentAutomationRules";
import type { AutomationRuleFormValues } from "@/protoFleet/features/settings/components/Curtailment/types";

const {
  mockCreateCurtailmentAutomationRule,
  mockDeleteCurtailmentAutomationRule,
  mockHandleAuthErrors,
  mockListCurtailmentAutomationRules,
  mockSetCurtailmentAutomationRuleEnabled,
  mockUpdateCurtailmentAutomationRule,
} = vi.hoisted(() => ({
  mockCreateCurtailmentAutomationRule: vi.fn(),
  mockDeleteCurtailmentAutomationRule: vi.fn(),
  mockHandleAuthErrors: vi.fn(),
  mockListCurtailmentAutomationRules: vi.fn(),
  mockSetCurtailmentAutomationRuleEnabled: vi.fn(),
  mockUpdateCurtailmentAutomationRule: vi.fn(),
}));

vi.mock("@/protoFleet/api/clients", () => ({
  curtailmentClient: {
    createCurtailmentAutomationRule: mockCreateCurtailmentAutomationRule,
    deleteCurtailmentAutomationRule: mockDeleteCurtailmentAutomationRule,
    listCurtailmentAutomationRules: mockListCurtailmentAutomationRules,
    setCurtailmentAutomationRuleEnabled: mockSetCurtailmentAutomationRuleEnabled,
    updateCurtailmentAutomationRule: mockUpdateCurtailmentAutomationRule,
  },
}));

vi.mock("@/protoFleet/store", () => ({
  useAuthErrors: () => ({
    handleAuthErrors: mockHandleAuthErrors,
  }),
}));

const formValues: AutomationRuleFormValues = {
  name: "ERCOT ERS obligation",
  sourceId: "11",
  responseProfileId: "21",
};

function apiRule(overrides: Partial<CurtailmentAutomationRule> = {}): CurtailmentAutomationRule {
  const rule = create(CurtailmentAutomationRuleSchema, {
    ruleId: 7n,
    ruleName: "ERCOT ERS obligation",
    triggerType: CurtailmentAutomationTriggerType.MQTT,
    mqttSourceId: 11n,
    mqttSourceName: "Kati MaestroOS",
    responseProfileId: 21n,
    responseProfileName: "Standard shed",
    enabled: true,
    currentSignal: CurtailmentAutomationSignal.OFF,
  });

  return Object.assign(rule, overrides);
}

describe("useCurtailmentAutomationRules", () => {
  beforeEach(() => {
    mockCreateCurtailmentAutomationRule.mockReset();
    mockDeleteCurtailmentAutomationRule.mockReset();
    mockHandleAuthErrors.mockReset();
    mockListCurtailmentAutomationRules.mockReset();
    mockSetCurtailmentAutomationRuleEnabled.mockReset();
    mockUpdateCurtailmentAutomationRule.mockReset();
  });

  it("lists and maps automation rules for the settings table", async () => {
    mockListCurtailmentAutomationRules.mockResolvedValueOnce({ rules: [apiRule()] });

    const { result } = renderHook(() => useCurtailmentAutomationRules(false));

    await act(async () => {
      await result.current.listAutomationRules();
    });

    expect(result.current.automationRules[0]).toMatchObject({
      id: "7",
      priority: 1,
      name: "ERCOT ERS obligation",
      conditionType: "mqttTriggerTargetOff",
      conditionSummary: "Kati MaestroOS grid signal changes to 0",
      sourceId: "11",
      responseProfileId: "21",
      responseProfileName: "Standard shed",
      enabled: true,
    });
    expect(result.current.isLoading).toBe(false);
  });

  it("creates and updates automation rules using the generated CRUD payload shape", async () => {
    mockCreateCurtailmentAutomationRule.mockResolvedValueOnce({ rule: apiRule() });
    mockUpdateCurtailmentAutomationRule.mockResolvedValueOnce({ rule: apiRule({ ruleName: "Updated" }) });

    const { result } = renderHook(() => useCurtailmentAutomationRules(false));

    await act(async () => {
      await result.current.createAutomationRule(formValues);
    });

    expect(mockCreateCurtailmentAutomationRule).toHaveBeenCalledWith(
      expect.objectContaining({
        ruleName: "ERCOT ERS obligation",
        triggerType: CurtailmentAutomationTriggerType.MQTT,
        mqttSourceId: 11n,
        responseProfileId: 21n,
        enabled: true,
      }),
    );

    await act(async () => {
      await result.current.updateAutomationRule("7", { ...formValues, name: "Updated" });
    });

    expect(mockUpdateCurtailmentAutomationRule).toHaveBeenCalledWith(
      expect.objectContaining({
        ruleId: 7n,
        ruleName: "Updated",
        triggerType: CurtailmentAutomationTriggerType.MQTT,
        mqttSourceId: 11n,
        responseProfileId: 21n,
      }),
    );
  });

  it("toggles and deletes automation rules by id", async () => {
    mockSetCurtailmentAutomationRuleEnabled.mockResolvedValueOnce({ rule: apiRule({ enabled: false }) });
    mockDeleteCurtailmentAutomationRule.mockResolvedValueOnce({});

    const { result } = renderHook(() => useCurtailmentAutomationRules(false));

    await act(async () => {
      await result.current.setAutomationRuleEnabled("7", false);
    });

    expect(mockSetCurtailmentAutomationRuleEnabled).toHaveBeenCalledWith(
      expect.objectContaining({
        ruleId: 7n,
        enabled: false,
      }),
    );

    await act(async () => {
      await result.current.deleteAutomationRule("7");
    });

    expect(mockDeleteCurtailmentAutomationRule).toHaveBeenCalledWith(
      expect.objectContaining({
        ruleId: 7n,
      }),
    );
  });

  it("rejects invalid source and response profile IDs before creating a CRUD request", async () => {
    const { result } = renderHook(() => useCurtailmentAutomationRules(false));
    let caughtError: unknown;

    await act(async () => {
      try {
        await result.current.createAutomationRule({
          ...formValues,
          sourceId: "source-alpha",
        });
      } catch (error) {
        caughtError = error;
      }
    });

    expect(caughtError).toEqual(expect.objectContaining({ message: "Select a valid source." }));
    expect(result.current.createError).toBe("Select a valid source.");
    expect(mockCreateCurtailmentAutomationRule).not.toHaveBeenCalled();
  });
});
