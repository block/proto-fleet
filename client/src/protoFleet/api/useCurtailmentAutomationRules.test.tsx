import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { Code, ConnectError } from "@connectrpc/connect";

import { AutomationResponseProfileRevisionConflictError } from "@/protoFleet/api/automationResponseProfileRevisionConflict";
import type {
  AutomationRuleFormValues,
  ResponseProfile,
  ScheduleRuleFormValues,
} from "@/protoFleet/api/curtailmentTypes";
import {
  type CurtailmentAutomationRule,
  CurtailmentAutomationRuleSchema,
  CurtailmentAutomationSignal,
  CurtailmentAutomationTriggerType,
  CurtailmentScheduleDayOfWeek,
  CurtailmentScheduleFrequency,
  CurtailmentScheduleSchema,
  CurtailmentScheduleType,
} from "@/protoFleet/api/generated/curtailment/v1/curtailment_pb";
import useCurtailmentAutomationRules from "@/protoFleet/api/useCurtailmentAutomationRules";

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
  responseProfileRevision: "11111111-1111-4111-8111-111111111111",
};

const weeknightSchedule = {
  scheduleType: CurtailmentScheduleType.RECURRING,
  timezone: "America/Chicago",
  startDate: "2026-06-01",
  startTime: "22:00",
  endTime: "06:00",
  endDate: "",
  frequency: CurtailmentScheduleFrequency.WEEKLY,
  daysOfWeek: [CurtailmentScheduleDayOfWeek.MONDAY, CurtailmentScheduleDayOfWeek.FRIDAY],
  dayOfMonth: 0,
  untilDate: "",
};

const scheduleFormValues: ScheduleRuleFormValues = {
  name: "Weeknight peak",
  responseProfileId: "21",
  responseProfileRevision: "11111111-1111-4111-8111-111111111111",
  schedule: {
    scheduleType: "recurring",
    timezone: "America/Chicago",
    startDate: "2026-06-01",
    startTime: "22:00",
    endTime: "06:00",
    endDate: "",
    frequency: "weekly",
    daysOfWeek: [CurtailmentScheduleDayOfWeek.FRIDAY, CurtailmentScheduleDayOfWeek.MONDAY],
    dayOfMonth: "",
    untilDate: "",
  },
};

function apiRule(overrides: Partial<CurtailmentAutomationRule> = {}): CurtailmentAutomationRule {
  const rule = create(CurtailmentAutomationRuleSchema, {
    ruleId: 7n,
    ruleName: "ERCOT ERS obligation",
    triggerType: CurtailmentAutomationTriggerType.MQTT,
    mqttSourceId: 11n,
    mqttSourceName: "Site Alpha MaestroOS",
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
      conditionSummary: "Site Alpha MaestroOS grid signal changes to 0",
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
        expectedResponseProfileRevision: formValues.responseProfileRevision,
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
        expectedResponseProfileRevision: formValues.responseProfileRevision,
      }),
    );
  });

  it("maps schedule rules with their schedule and current window", async () => {
    const windowStart = new Date("2026-06-12T03:00:00Z");
    const windowEnd = new Date("2026-06-12T11:00:00Z");
    mockListCurtailmentAutomationRules.mockResolvedValueOnce({
      rules: [
        apiRule({
          triggerType: CurtailmentAutomationTriggerType.SCHEDULE,
          mqttSourceId: 0n,
          mqttSourceName: "",
          schedule: create(CurtailmentScheduleSchema, weeknightSchedule),
          windowStartAt: timestampFromDate(windowStart),
          windowEndAt: timestampFromDate(windowEnd),
        }),
      ],
    });

    const { result } = renderHook(() => useCurtailmentAutomationRules(false));

    await act(async () => {
      await result.current.listAutomationRules();
    });

    expect(result.current.automationRules[0]).toMatchObject({
      conditionType: "schedule",
      schedule: {
        ...scheduleFormValues.schedule,
        daysOfWeek: [CurtailmentScheduleDayOfWeek.MONDAY, CurtailmentScheduleDayOfWeek.FRIDAY],
      },
      windowStartAt: windowStart,
      windowEndAt: windowEnd,
    });
    expect(result.current.automationRules[0]).not.toHaveProperty("sourceId");
    expect(result.current.automationRules[0]).not.toHaveProperty("sourceId");
    expect(result.current.automationRules[0].conditionSummary).toMatch(/^Weekly on Mon, Fri, /);
  });

  it("creates and updates schedule rules with a schedule instead of a source", async () => {
    mockCreateCurtailmentAutomationRule.mockResolvedValueOnce({ rule: apiRule() });
    mockUpdateCurtailmentAutomationRule.mockResolvedValueOnce({ rule: apiRule() });

    const { result } = renderHook(() => useCurtailmentAutomationRules(false));

    await act(async () => {
      await result.current.createScheduleRule(scheduleFormValues);
    });

    const createRequest = mockCreateCurtailmentAutomationRule.mock.calls[0][0];
    expect(createRequest).toMatchObject({
      ruleName: "Weeknight peak",
      triggerType: CurtailmentAutomationTriggerType.SCHEDULE,
      mqttSourceId: 0n,
      responseProfileId: 21n,
      expectedResponseProfileRevision: scheduleFormValues.responseProfileRevision,
      enabled: true,
    });
    expect(createRequest.schedule).toMatchObject(weeknightSchedule);

    await act(async () => {
      await result.current.updateScheduleRule("7", scheduleFormValues);
    });

    expect(mockUpdateCurtailmentAutomationRule.mock.calls[0][0]).toMatchObject({
      ruleId: 7n,
      triggerType: CurtailmentAutomationTriggerType.SCHEDULE,
      schedule: weeknightSchedule,
    });
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
        expectedResponseProfileRevision: "",
      }),
    );

    mockSetCurtailmentAutomationRuleEnabled.mockResolvedValueOnce({ rule: apiRule({ enabled: true }) });

    await act(async () => {
      await result.current.setAutomationRuleEnabled("7", true, formValues.responseProfileRevision);
    });

    expect(mockSetCurtailmentAutomationRuleEnabled).toHaveBeenLastCalledWith(
      expect.objectContaining({
        ruleId: 7n,
        enabled: true,
        expectedResponseProfileRevision: formValues.responseProfileRevision,
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

  it("refreshes response profiles after a stale revision prevents saving or enabling a rule", async () => {
    const latestProfile = {
      id: "21",
      revision: "22222222-2222-4222-8222-222222222222",
    } as ResponseProfile;
    const refreshResponseProfiles = vi.fn().mockResolvedValue([latestProfile]);
    const createConflict = () =>
      new ConnectError(
        "curtailment response profile changed before automation rule save; retry",
        Code.FailedPrecondition,
      );
    mockCreateCurtailmentAutomationRule.mockRejectedValueOnce(createConflict());
    mockUpdateCurtailmentAutomationRule.mockRejectedValueOnce(createConflict());
    mockSetCurtailmentAutomationRuleEnabled.mockRejectedValueOnce(createConflict());

    const { result } = renderHook(() => useCurtailmentAutomationRules(false, { refreshResponseProfiles }));

    const operations = [
      () => result.current.createAutomationRule(formValues),
      () => result.current.updateAutomationRule("7", formValues),
      () => result.current.setAutomationRuleEnabled("7", true, formValues.responseProfileRevision),
    ];

    for (const operation of operations) {
      let caughtError: unknown;
      await act(async () => {
        try {
          await operation();
        } catch (error) {
          caughtError = error;
        }
      });

      expect(caughtError).toBeInstanceOf(AutomationResponseProfileRevisionConflictError);
      expect(
        (caughtError as AutomationResponseProfileRevisionConflictError).latestResponseProfileRevisionById.get("21"),
      ).toBe(latestProfile.revision);
    }

    expect(refreshResponseProfiles).toHaveBeenCalledTimes(3);
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
  it("retains automation execution failures for the rule view", async () => {
    mockListCurtailmentAutomationRules.mockResolvedValue({ rules: [apiRule({ lastError: "No eligible miners" })] });
    const { result } = renderHook(() => useCurtailmentAutomationRules(false));
    await act(async () => {
      await result.current.listAutomationRules();
    });
    expect(result.current.automationRules[0]?.lastError).toBe("No eligible miners");
  });
});
