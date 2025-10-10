import { describe, expect, it } from "vitest";
import {
  getErrorMessage,
  getErrorTitle,
  getStatusErrorTitle,
  getStatusSummary,
  isAsicError,
  isAsicWarning,
  isControlBoardError,
  isControlBoardWarning,
  isFanError,
  isFanWarning,
  isHashboardError,
  isHashboardWarning,
  isPSUError,
  isPSUWarning,
} from "./utility";
import { NotificationError } from "@/protoOS/api/generatedApi";

const createMockError = (
  overrides: Partial<NotificationError> = {},
): NotificationError => ({
  source: "Miner",
  component_index: 8448,
  hashboard_index: undefined,
  asic_index: undefined,
  error_code: "TestError",
  inserted_at: 1758760183,
  expired_at: 0,
  error_level: "Error",
  message: "",
  details: "{}",
  ...overrides,
});

const createErrorTestData = (componentType: string, isLegacy = true) => {
  const configs = {
    hashboard: {
      legacy: {
        error: {
          error_code: "HashboardOverheat",
          details: '{"HashboardOverheat":{"hb_slot":"1","temperature":"95"}}',
        },
        warning: {
          error_code: "HashboardOverheat",
          details: '{"HashboardOverheat":{"hb_slot":"1","temperature":"85"}}',
        },
      },
      new: {
        error: {
          error_code: "04:0006",
          details: '{"HashboardOverheat":{"hb_slot":"2","temperature":"98"}}',
        },
        warning: {
          error_code: "04:0006",
          details: '{"HashboardOverheat":{"hb_slot":"2","temperature":"88"}}',
        },
      },
    },
    asic: {
      legacy: {
        error: {
          error_code: "AsicOverheat",
          details:
            '{"AsicOverheat":{"hb_slot":"1","asic_index":"1","temperature":"105"}}',
        },
        warning: {
          error_code: "AsicOverheat",
          details:
            '{"AsicOverheat":{"hb_slot":"1","asic_index":"1","temperature":"95"}}',
        },
      },
      new: {
        error: {
          error_code: "04:0001",
          details:
            '{"AsicOverheat":{"hb_slot":"1","asic_index":"3","temperature":"110"}}',
        },
        warning: {
          error_code: "04:0001",
          details:
            '{"AsicOverheat":{"hb_slot":"1","asic_index":"3","temperature":"100"}}',
        },
      },
    },
    fan: {
      legacy: {
        error: {
          error_code: "FanNotSpinning",
          details:
            '{"FanNotSpinning":{"fan_id":"1","fan_bay_index":"1","fan_pwm_target_pct":"50","fan_rpm_tach":"0"}}',
        },
        warning: {
          error_code: "FanSlow",
          details:
            '{"FanSlow":{"fan_id":"1","fan_bay_index":"1","fan_pwm_target_pct":"50","fan_rpm_tach":"20"}}',
        },
      },
      new: {
        error: {
          error_code: "01:0002",
          details:
            '{"FanNotSpinning":{"fan_id":"2","fan_bay_index":"1","fan_pwm_target_pct":"60","fan_rpm_tach":"0"}}',
        },
        warning: {
          error_code: "01:0001",
          details:
            '{"FanSlow":{"fan_id":"2","fan_bay_index":"1","fan_pwm_target_pct":"60","fan_rpm_tach":"30"}}',
        },
      },
    },
    psu: {
      legacy: {
        error: {
          error_code: "PsuHardwareFault",
          details:
            '{"PsuHardwareFault":{"psu_bay_index":"1","psu_index":"1","psu_sn":"PSU123","fault":{"message":"Hardware failure"}}}',
        },
        warning: {
          error_code: "PsuHardwareFault",
          details:
            '{"PsuHardwareFault":{"psu_bay_index":"1","psu_index":"1","psu_sn":"PSU123","fault":{"message":"Hardware warning"}}}',
        },
      },
      new: {
        error: {
          error_code: "00:0011",
          details:
            '{"PsuHardwareFault":{"psu_bay_index":"1","psu_index":"1","psu_sn":"PSU456","fault":{"message":"Thermal fault"}}}',
        },
        warning: {
          error_code: "00:0008",
          details:
            '{"PsuHardwareFault":{"psu_bay_index":"1","psu_index":"1","psu_sn":"PSU456","fault":{"message":"Thermal warning"}}}',
        },
      },
    },
    controlBoard: {
      legacy: {
        error: {
          error_code: "ControlboardFailure",
          details: '{"ControlboardFailure":{"message":"Control board failed"}}',
        },
        warning: {
          error_code: "ControlboardIssue",
          details: '{"ControlboardIssue":{"message":"Control board warning"}}',
        },
      },
      new: {
        error: {
          error_code: "03:0007",
          details:
            '{"ControlboardFailure":{"message":"Kernel panic detected"}}',
        },
        warning: {
          error_code: "03:0008",
          details:
            '{"ControlboardIssue":{"message":"High CPU usage detected"}}',
        },
      },
    },
  };

  const config = configs[componentType as keyof typeof configs];
  return isLegacy ? config.legacy : config.new;
};

// Sample error data for different components
const hashboardError = createMockError({
  error_code: "HashboardOverheat",
  error_level: "Error",
  details: '{"HashboardOverheat":{"hb_slot":"1","temperature":"95"}}',
});

const fanError = createMockError({
  error_code: "FanNotSpinning",
  error_level: "Error",
  details:
    '{"FanNotSpinning":{"fan_id":"1","fan_bay_index":"1","fan_pwm_target_pct":"50","fan_rpm_tach":"0"}}',
});

const psuError = createMockError({
  error_code: "PsuHardwareFault",
  error_level: "Error",
  details:
    '{"PsuHardwareFault":{"psu_bay_index":"1","psu_index":"1","psu_sn":"PSU123","fault":{"message":"Hardware failure"}}}',
});

const controlBoardError = createMockError({
  error_code: "ControlboardFailure",
  error_level: "Error",
  details: '{"ControlboardFailure":{"message":"Control board failed"}}',
});

describe("Component Error Detection Functions", () => {
  const componentTestCases = [
    {
      name: "hashboard",
      displayName: "hashboard",
      errorFn: isHashboardError,
      warningFn: isHashboardWarning,
    },
    {
      name: "asic",
      displayName: "ASIC",
      errorFn: isAsicError,
      warningFn: isAsicWarning,
    },
    {
      name: "fan",
      displayName: "fan",
      errorFn: isFanError,
      warningFn: isFanWarning,
    },
    {
      name: "psu",
      displayName: "PSU",
      errorFn: isPSUError,
      warningFn: isPSUWarning,
    },
    {
      name: "controlBoard",
      displayName: "control board",
      errorFn: isControlBoardError,
      warningFn: isControlBoardWarning,
    },
  ];

  ["legacy", "new"].forEach((codeType) => {
    describe(`${codeType === "legacy" ? "Legacy" : "New Standard"} Error Codes`, () => {
      const isLegacy = codeType === "legacy";

      componentTestCases.forEach(
        ({ name, displayName, errorFn, warningFn }) => {
          const testData = createErrorTestData(name, isLegacy);
          const errorMock = createMockError({
            ...testData.error,
            error_level: "Error",
          });
          const warningMock = createMockError({
            ...testData.warning,
            error_level: "Warning",
          });

          const otherComponent =
            componentTestCases.find(
              (c) =>
                c.name !== name &&
                !(name === "hashboard" && c.name === "asic") &&
                !(name === "asic" && c.name === "hashboard"),
            ) || componentTestCases.find((c) => c.name === "fan")!;
          const otherTestData = createErrorTestData(
            otherComponent.name,
            isLegacy,
          );
          const otherErrorMock = createMockError({
            ...otherTestData.error,
            error_level: "Error",
          });
          const otherWarningMock = createMockError({
            ...otherTestData.warning,
            error_level: "Warning",
          });

          describe(`is${name.charAt(0).toUpperCase() + name.slice(1)}Error`, () => {
            it(`should return true for ${displayName} errors`, () => {
              expect(errorFn(errorMock)).toBe(true);
            });

            it(`should return false for ${displayName} warnings`, () => {
              expect(errorFn(warningMock)).toBe(false);
            });

            it(`should return false for non-${displayName} errors`, () => {
              expect(errorFn(otherErrorMock)).toBe(false);
            });
          });

          describe(`is${name.charAt(0).toUpperCase() + name.slice(1)}Warning`, () => {
            it(`should return true for ${displayName} warnings`, () => {
              expect(warningFn(warningMock)).toBe(true);
            });

            it(`should return false for ${displayName} errors`, () => {
              expect(warningFn(errorMock)).toBe(false);
            });

            it(`should return false for non-${displayName} warnings`, () => {
              expect(warningFn(otherWarningMock)).toBe(false);
            });
          });
        },
      );
    });
  });
});

describe("getStatusErrorTitle", () => {
  describe("General Status Tests", () => {
    it("should return operational status when no errors", () => {
      const result = getStatusErrorTitle([]);
      expect(result).toEqual({
        title: "All systems are operational",
        subtitle: "",
      });
    });

    it("should return multiple issues when multiple error types", () => {
      const errors = [hashboardError, fanError, psuError];
      const result = getStatusErrorTitle(errors);
      expect(result).toEqual({
        title: "Multiple issues detected",
        subtitle: "Repair now to prevent downtime.",
      });
    });

    it("should return multiple component issues when multiple errors of same type", () => {
      const hashboardError2 = createMockError({
        error_code: "HashboardUnderVoltage",
        error_level: "Error",
      });
      const errors = [hashboardError, hashboardError2];
      const result = getStatusErrorTitle(errors);
      expect(result).toEqual({
        title: "Multiple hashboard issues detected",
        subtitle: "Repair now to prevent downtime.",
      });
    });
  });

  describe("Legacy Error Codes - Single Error Cases", () => {
    it("should handle AsicOverheat error", () => {
      const error = createMockError({
        error_code: "AsicOverheat",
        error_level: "Error",
      });
      const result = getStatusErrorTitle([error]);
      expect(result).toEqual({
        title: "Your miner's ASICs are overheating",
        subtitle: "Repair now to prevent reduced hashrate and board shutdowns.",
      });
    });

    it("should handle AsicOverVoltage error", () => {
      const error = createMockError({
        error_code: "AsicOverVoltage",
        error_level: "Error",
      });
      const result = getStatusErrorTitle([error]);
      expect(result).toEqual({
        title: "Your miner's ASIC voltage is excessive",
        subtitle: "Repair now to prevent reduced hashrate and board shutdowns.",
      });
    });

    it("should handle AsicFailure error", () => {
      const error = createMockError({
        error_code: "AsicFailure",
        error_level: "Error",
      });
      const result = getStatusErrorTitle([error]);
      expect(result).toEqual({
        title: "Your miner's ASICs are malfunctioning",
        subtitle: "Repair now to prevent reduced hashrate and board shutdowns.",
      });
    });

    it("should handle FanSlow error", () => {
      const error = createMockError({
        error_code: "FanSlow",
        error_level: "Error",
      });
      const result = getStatusErrorTitle([error]);
      expect(result).toEqual({
        title: "Your miner's fan is running slowly",
        subtitle: "Repair now to prevent overheating.",
      });
    });

    it("should handle FanNotSpinning error", () => {
      const error = createMockError({
        error_code: "FanNotSpinning",
        error_level: "Error",
      });
      const result = getStatusErrorTitle([error]);
      expect(result).toEqual({
        title: "Your miner's fan has stopped spinning",
        subtitle: "Repair now to prevent overheating.",
      });
    });

    it("should handle HashboardOverCurrent error", () => {
      const error = createMockError({
        error_code: "HashboardOverCurrent",
        error_level: "Error",
      });
      const result = getStatusErrorTitle([error]);
      expect(result).toEqual({
        title: "Your miner's hashboard is drawing too much current",
        subtitle: "Repair now to prevent overheating.",
      });
    });

    it("should handle HashboardOverheat error", () => {
      const error = createMockError({
        error_code: "HashboardOverheat",
        error_level: "Error",
      });
      const result = getStatusErrorTitle([error]);
      expect(result).toEqual({
        title: "Your miner's hashboard is overheating",
        subtitle: "Repair now to prevent reduced hashrate and board shutdowns.",
      });
    });

    it("should handle HashboardOverVoltage error", () => {
      const error = createMockError({
        error_code: "HashboardOverVoltage",
        error_level: "Error",
      });
      const result = getStatusErrorTitle([error]);
      expect(result).toEqual({
        title: "Your miner's hashboard voltage is too high",
        subtitle: "Repair now to prevent overheating.",
      });
    });

    it("should handle HashboardPowerLost error", () => {
      const error = createMockError({
        error_code: "HashboardPowerLost",
        error_level: "Error",
      });
      const result = getStatusErrorTitle([error]);
      expect(result).toEqual({
        title: "Your miner's hashboard has lost power",
        subtitle: "Repair now to prevent downtime.",
      });
    });

    it("should handle HashboardUnderVoltage error", () => {
      const error = createMockError({
        error_code: "HashboardUnderVoltage",
        error_level: "Error",
      });
      const result = getStatusErrorTitle([error]);
      expect(result).toEqual({
        title: "Your miner's hashboard voltage is too low",
        subtitle: "Repair now to prevent downtime.",
      });
    });

    it("should handle HashboardUsbConnectionLost error", () => {
      const error = createMockError({
        error_code: "HashboardUsbConnectionLost",
        error_level: "Error",
      });
      const result = getStatusErrorTitle([error]);
      expect(result).toEqual({
        title: "Your miner's hashboard has lost USB connection",
        subtitle: "Repair now to prevent downtime.",
      });
    });

    it("should handle PsuHardwareFault error", () => {
      const error = createMockError({
        error_code: "PsuHardwareFault",
        error_level: "Error",
      });
      const result = getStatusErrorTitle([error]);
      expect(result).toEqual({
        title: "Your miner's power supply has failed",
        subtitle: "Repair now to prevent downtime.",
      });
    });

    it("should handle PsuCommsLost error", () => {
      const error = createMockError({
        error_code: "PsuCommsLost",
        error_level: "Error",
      });
      const result = getStatusErrorTitle([error]);
      expect(result).toEqual({
        title: "Your miner has lost communication with a power supply",
        subtitle: "Repair now to prevent downtime.",
      });
    });

    it("should handle MixedHashboardTypesInBay error", () => {
      const error = createMockError({
        error_code: "MixedHashboardTypesInBay",
        error_level: "Error",
      });
      const result = getStatusErrorTitle([error]);
      expect(result).toEqual({
        title: "Your miner has incompatible hashboards in the same bay",
        subtitle: "Repair now to prevent downtime.",
      });
    });

    it("should handle unknown single error", () => {
      const error = createMockError({
        error_code: "SomeRandomError",
        error_level: "Error",
      });
      const result = getStatusErrorTitle([error]);
      expect(result).toEqual({
        title: "All systems are operational",
        subtitle: "",
      });
    });
  });

  describe("New Standard Error Codes - Single Error Cases", () => {
    it("should handle new ASIC overheating error (04:0001)", () => {
      const error = createMockError({
        error_code: "04:0001",
        error_level: "Error",
      });
      const result = getStatusErrorTitle([error]);
      expect(result).toEqual({
        title: "Your miner's ASICs are overheating",
        subtitle: "Repair now to prevent reduced hashrate and board shutdowns.",
      });
    });

    it("should handle new hashboard overheating error (04:0006)", () => {
      const error = createMockError({
        error_code: "04:0006",
        error_level: "Error",
      });
      const result = getStatusErrorTitle([error]);
      expect(result).toEqual({
        title: "Your miner's hashboard is overheating",
        subtitle: "Repair now to prevent reduced hashrate and board shutdowns.",
      });
    });

    it("should handle new fan not spinning error (01:0002)", () => {
      const error = createMockError({
        error_code: "01:0002",
        error_level: "Error",
      });
      const result = getStatusErrorTitle([error]);
      expect(result).toEqual({
        title: "Your miner's fan has stopped spinning",
        subtitle: "Repair now to prevent overheating.",
      });
    });

    it("should handle new PSU hardware fault error (00:0011)", () => {
      const error = createMockError({
        error_code: "00:0011",
        error_level: "Error",
      });
      const result = getStatusErrorTitle([error]);
      expect(result).toEqual({
        title: "Your miner's power supply has failed",
        subtitle: "Repair now to prevent downtime.",
      });
    });

    it("should handle new control board kernel panic error (03:0007)", () => {
      const error = createMockError({
        error_code: "03:0007",
        error_level: "Error",
      });
      const result = getStatusErrorTitle([error]);
      expect(result).toEqual({
        title: "Your miner has an unknown error",
        subtitle: "Contact support for assistance.",
      });
    });

    it("should handle new unsupported hashboard config error (03:0015)", () => {
      const error = createMockError({
        error_code: "03:0015",
        error_level: "Error",
      });
      const result = getStatusErrorTitle([error]);
      expect(result).toEqual({
        title: "Your miner has incompatible hashboards in the same bay",
        subtitle: "Repair now to prevent downtime.",
      });
    });

    it("should handle PSU recovery error (00:0014)", () => {
      const error = createMockError({
        error_code: "00:0014",
        error_level: "Error",
      });
      const result = getStatusErrorTitle([error]);
      expect(result).toEqual({
        title: "Your miner's power supply is recovering",
        subtitle: "Hashboards in affected bay may be impacted.",
      });
    });

    it("should handle insufficient cooling error (01:0004)", () => {
      const error = createMockError({
        error_code: "01:0004",
        error_level: "Error",
      });
      const result = getStatusErrorTitle([error]);
      expect(result).toEqual({
        title: "Your miner has insufficient cooling",
        subtitle: "Repair now to prevent overheating.",
      });
    });

    it("should handle hashboard recovery error (04:0017)", () => {
      const error = createMockError({
        error_code: "04:0017",
        error_level: "Error",
      });
      const result = getStatusErrorTitle([error]);
      expect(result).toEqual({
        title: "Your miner's hashboard is recovering",
        subtitle: "Repair now to prevent reduced hashrate and board shutdowns.",
      });
    });
  });
});

describe("getStatusSummary", () => {
  it("should return null when no issues", () => {
    const result = getStatusSummary([], [], [], []);
    expect(result).toBeNull();
  });

  it('should return "Multiple Issues" when multiple issue types', () => {
    const result = getStatusSummary(
      [hashboardError],
      [psuError],
      [fanError],
      [],
    );
    expect(result).toBe("Multiple Issues");
  });

  describe("single issue type scenarios", () => {
    it('should return "Multiple hashboard Issues" for multiple hashboard issues', () => {
      const hashboardError2 = createMockError({
        error_code: "HashboardUnderVoltage",
        error_level: "Error",
        hashboard_index: 2,
      });
      const result = getStatusSummary(
        [hashboardError, hashboardError2],
        [],
        [],
        [],
      );
      expect(result).toBe("Multiple hashboard Issues");
    });

    it("should return single hashboard issue with number", () => {
      const error = createMockError({
        error_code: "HashboardOverheat",
        error_level: "Error",
        hashboard_index: 1,
      });
      const result = getStatusSummary([error], [], [], []);
      expect(result).toBe("Hashboard 1 issue");
    });

    it("should handle hashboard issue without index", () => {
      const error = createMockError({
        error_code: "HashboardOverheat",
        error_level: "Error",
        hashboard_index: undefined,
      });
      const result = getStatusSummary([error], [], [], []);
      expect(result).toBe("Hashboard  issue");
    });

    it('should return "Multiple fan Issues" for multiple fan issues', () => {
      const fanError2 = createMockError({
        error_code: "FanSlow",
        error_level: "Error",
        details: '{"FanSlow":{"fan_id":"2","fan_bay_index":"2"}}',
      });
      const result = getStatusSummary([], [], [fanError, fanError2], []);
      expect(result).toBe("Multiple fan Issues");
    });

    it("should return single fan issue with number", () => {
      const error = createMockError({
        error_code: "FanSlow",
        error_level: "Error",
        details: '{"FanSlow":{"fan_id":"3","fan_bay_index":"1"}}',
      });
      const result = getStatusSummary([], [], [error], []);
      expect(result).toBe("Fan 3 issue");
    });

    it("should handle fan issue without fan_id in details", () => {
      const error = createMockError({
        error_code: "FanSlow",
        error_level: "Error",
        details: '{"FanSlow":{"fan_bay_index":"1"}}',
      });
      const result = getStatusSummary([], [], [error], []);
      expect(result).toBe("Fan  issue");
    });

    it("should handle fan issue with invalid JSON details", () => {
      const error = createMockError({
        error_code: "FanSlow",
        error_level: "Error",
        details: "invalid json",
      });
      const result = getStatusSummary([], [], [error], []);
      expect(result).toBe("Fan  issue");
    });

    it('should return "Multiple psu Issues" for multiple PSU issues', () => {
      const psuError2 = createMockError({
        error_code: "PsuCommsLost",
        error_level: "Error",
        component_index: 2,
      });
      const result = getStatusSummary([], [psuError, psuError2], [], []);
      expect(result).toBe("Multiple psu Issues");
    });

    it("should return single PSU issue with number", () => {
      const error = createMockError({
        error_code: "PsuHardwareFault",
        error_level: "Error",
        component_index: 1,
      });
      const result = getStatusSummary([], [error], [], []);
      expect(result).toBe("PSU 1 issue");
    });

    it("should handle PSU issue without component_index", () => {
      const error = createMockError({
        error_code: "PsuHardwareFault",
        error_level: "Error",
        component_index: undefined,
      });
      const result = getStatusSummary([], [error], [], []);
      expect(result).toBe("PSU  issue");
    });

    it('should return "Multiple control board Issues" for multiple control board issues', () => {
      const controlBoardError2 = createMockError({
        error_code: "ControlboardTimeout",
        error_level: "Error",
      });
      const result = getStatusSummary(
        [],
        [],
        [],
        [controlBoardError, controlBoardError2],
      );
      expect(result).toBe("Multiple control board Issues");
    });

    it('should return "Control board issue" for single control board issue', () => {
      const result = getStatusSummary([], [], [], [controlBoardError]);
      expect(result).toBe("Control board issue");
    });
  });
});

describe("getErrorTitle", () => {
  const errorTitleTestCases = [
    {
      category: "General Tests",
      cases: [
        {
          name: "should return error message when no error_code",
          errorData: { error_code: "", message: "Custom error message" },
          expectedTitle: "Custom error message",
        },
        {
          name: 'should return "Unknown error" when no error_code or message',
          errorData: { error_code: "", message: "" },
          expectedTitle: "Unknown error",
        },
        {
          name: "should return error message for other error types",
          errorData: {
            error_code: "UnknownErrorType",
            error_level: "Error" as const,
            message: "Some other error",
            details: '{"UnknownErrorType":{}}',
          },
          expectedTitle: "Some other error",
        },
      ],
    },
    {
      category: "Legacy Error Codes",
      cases: [
        {
          name: "should return hashboard title for ASIC errors",
          errorData: {
            error_code: "AsicOverheat",
            error_level: "Error" as const,
            details: '{"AsicOverheat":{"hb_slot":"2"}}',
          },
          expectedTitle: "Hashboard 2",
        },
        {
          name: "should return hashboard title for hashboard errors",
          errorData: {
            error_code: "HashboardOverheat",
            error_level: "Error" as const,
            details: '{"HashboardOverheat":{"hb_slot":"1"}}',
          },
          expectedTitle: "Hashboard 1",
        },
        {
          name: "should return hashboard title with empty slot when hb_slot is missing",
          errorData: {
            error_code: "HashboardOverheat",
            error_level: "Error" as const,
            details: '{"HashboardOverheat":{}}',
          },
          expectedTitle: "Hashboard ",
        },
        {
          name: "should return fan title for fan errors",
          errorData: {
            error_code: "FanNotSpinning",
            error_level: "Error" as const,
            details: '{"FanNotSpinning":{"fan_id":"3"}}',
          },
          expectedTitle: "Fan 3",
        },
        {
          name: "should return fan title with empty ID when fan_id is missing",
          errorData: {
            error_code: "FanSlow",
            error_level: "Error" as const,
            details: '{"FanSlow":{}}',
          },
          expectedTitle: "Fan ",
        },
        {
          name: "should return PSU title for PSU errors",
          errorData: {
            error_code: "PsuHardwareFault",
            error_level: "Error" as const,
            details: '{"PsuHardwareFault":{"psu_bay_index":"1"}}',
          },
          expectedTitle: "PSU 1",
        },
        {
          name: "should return PSU title with empty index when psu_bay_index is missing",
          errorData: {
            error_code: "PsuCommsLost",
            error_level: "Error" as const,
            details: '{"PsuCommsLost":{}}',
          },
          expectedTitle: "PSU ",
        },
        {
          name: 'should return "Control Board" for control board errors',
          errorData: {
            error_code: "ControlboardFailure",
            error_level: "Error" as const,
            details: '{"ControlboardFailure":{}}',
          },
          expectedTitle: "Control Board",
        },
      ],
    },
    {
      category: "New Standard Error Codes",
      cases: [
        {
          name: "should return hashboard title for new ASIC errors",
          errorData: {
            error_code: "04:0001",
            error_level: "Error" as const,
            details: '{"AsicOverheat":{"hb_slot":"3"}}',
          },
          expectedTitle: "Hashboard 3",
        },
        {
          name: "should return hashboard title for new hashboard errors",
          errorData: {
            error_code: "04:0006",
            error_level: "Error" as const,
            details: '{"HashboardOverheat":{"hb_slot":"2"}}',
          },
          expectedTitle: "Hashboard 2",
        },
        {
          name: "should return fan title for new fan errors",
          errorData: {
            error_code: "01:0001",
            error_level: "Error" as const,
            details: '{"FanSlow":{"fan_id":"4"}}',
          },
          expectedTitle: "Fan 4",
        },
        {
          name: "should return PSU title for new PSU errors",
          errorData: {
            error_code: "00:0011",
            error_level: "Error" as const,
            details: '{"PsuHardwareFault":{"psu_bay_index":"2"}}',
          },
          expectedTitle: "PSU 2",
        },
        {
          name: 'should return "Control Board" for new control board errors',
          errorData: {
            error_code: "03:0007",
            error_level: "Error" as const,
            details: '{"ControlboardFailure":{}}',
          },
          expectedTitle: "Control Board",
        },
      ],
    },
  ];

  errorTitleTestCases.forEach(({ category, cases }) => {
    describe(category, () => {
      cases.forEach(({ name, errorData, expectedTitle }) => {
        it(name, () => {
          const error = createMockError(errorData);
          const result = getErrorTitle(error);
          expect(result).toBe(expectedTitle);
        });
      });
    });
  });
});

describe("getErrorMessage", () => {
  const errorMessageTestCases = [
    {
      category: "General Tests",
      cases: [
        {
          name: "should return error message when no error_code",
          errorData: { error_code: "", message: "Custom error message" },
          expectedMessage: "Custom error message",
        },
        {
          name: 'should return "Unknown error" when no error or error_code',
          errorData: undefined,
          expectedMessage: "Unknown error",
        },
        {
          name: "should return fallback message for unknown error codes",
          errorData: {
            error_code: "UnknownErrorCode",
            details: '{"UnknownErrorCode":{}}',
          },
          expectedMessage: "Unknown error",
        },
      ],
    },
    {
      category: "Legacy Error Codes",
      cases: [
        {
          name: "AsicOverheat",
          errorData: {
            error_code: "AsicOverheat",
            details:
              '{"AsicOverheat":{"hb_slot":"1","asic_index":"2","temperature":"105"}}',
          },
          expectedMessage:
            "Slot 1 Hashboard's ASIC (2) is overheating at 105°C",
        },
        {
          name: "AsicOverVoltage",
          errorData: {
            error_code: "AsicOverVoltage",
            details:
              '{"AsicOverVoltage":{"hb_slot":"2","asic_index":"3","voltage":"3.5"}}',
          },
          expectedMessage:
            "Slot 2 Hashboard's ASIC (3) is drawing too much voltage at 3.5V",
        },
        {
          name: "AsicFailure",
          errorData: {
            error_code: "AsicFailure",
            details: '{"AsicFailure":{"hb_slot":"1","asic_index":"1"}}',
          },
          expectedMessage:
            "Slot 1 Hashboard's ASIC (1) experienced an unspecified failure",
        },
        {
          name: "FanSlow",
          errorData: {
            error_code: "FanSlow",
            details:
              '{"FanSlow":{"fan_id":"1","fan_bay_index":"2","fan_pwm_target_pct":"75","fan_rpm_tach":"1200"}}',
          },
          expectedMessage:
            "Fan 1 in bay 2 is running slow. Target fan speed: 75%, Actual RPM: 1200",
        },
        {
          name: "FanNotSpinning",
          errorData: {
            error_code: "FanNotSpinning",
            details:
              '{"FanNotSpinning":{"fan_id":"2","fan_bay_index":"1","fan_pwm_target_pct":"50","fan_rpm_tach":"0"}}',
          },
          expectedMessage:
            "Fan 2 in bay 1 is not spinning. Target fan speed: 50%, Actual RPM: 0",
        },
        {
          name: "HashboardOverCurrent",
          errorData: {
            error_code: "HashboardOverCurrent",
            details:
              '{"HashboardOverCurrent":{"hb_slot":"3","current":"15.5"}}',
          },
          expectedMessage:
            "Slot 3 Hashboard is drawing too much current at 15.5A",
        },
        {
          name: "HashboardOverheat",
          errorData: {
            error_code: "HashboardOverheat",
            details: '{"HashboardOverheat":{"hb_slot":"1","temperature":"95"}}',
          },
          expectedMessage: "Slot 1 Hashboard is overheating at 95°C",
        },
        {
          name: "HashboardOverVoltage",
          errorData: {
            error_code: "HashboardOverVoltage",
            details:
              '{"HashboardOverVoltage":{"hb_slot":"2","voltage":"13.2"}}',
          },
          expectedMessage:
            "Slot 2 Hashboard is drawing too much voltage at 13.2V",
        },
        {
          name: "HashboardPowerLost",
          errorData: {
            error_code: "HashboardPowerLost",
            details: '{"HashboardPowerLost":{"hb_slot":"1"}}',
          },
          expectedMessage: "Slot 1 Hashboard has lost power",
        },
        {
          name: "HashboardUnderVoltage",
          errorData: {
            error_code: "HashboardUnderVoltage",
            details:
              '{"HashboardUnderVoltage":{"hb_slot":"3","voltage":"10.5"}}',
          },
          expectedMessage:
            "Slot 3 Hashboard does not have enough power at 10.5V",
        },
        {
          name: "HashboardUsbConnectionLost",
          errorData: {
            error_code: "HashboardUsbConnectionLost",
            details:
              '{"HashboardUsbConnectionLost":{"hb_slot":"2","hb_sn":"HB12345"}}',
          },
          expectedMessage:
            "Slot 2 Hashboard has lost USB connection. Serial number: HB12345",
        },
        {
          name: "PsuHardwareFault with fault message",
          errorData: {
            error_code: "PsuHardwareFault",
            details:
              '{"PsuHardwareFault":{"psu_bay_index":"1","psu_index":"1","psu_sn":"PSU123","fault":{"message":"Overvoltage detected"}}}',
          },
          expectedMessage:
            "Power supply in bay 1 (ID: 1) has a hardware fault: Overvoltage detected. Serial number: PSU123",
        },
        {
          name: "PsuHardwareFault with fault type",
          errorData: {
            error_code: "PsuHardwareFault",
            details:
              '{"PsuHardwareFault":{"psu_bay_index":"2","psu_index":"2","psu_sn":"PSU456","fault":{"fault_type":"THERMAL_FAULT"}}}',
          },
          expectedMessage:
            "Power supply in bay 2 (ID: 2) has a hardware fault: THERMAL_FAULT. Serial number: PSU456",
        },
        {
          name: "PsuHardwareFault without fault details",
          errorData: {
            error_code: "PsuHardwareFault",
            details:
              '{"PsuHardwareFault":{"psu_bay_index":"1","psu_index":"1","psu_sn":"PSU789","fault":{"fault_type":"THERMAL_FAULT"}}}',
          },
          expectedMessage:
            "Power supply in bay 1 (ID: 1) has a hardware fault: THERMAL_FAULT. Serial number: PSU789",
        },
        {
          name: "PsuCommsLost",
          errorData: {
            error_code: "PsuCommsLost",
            details:
              '{"PsuCommsLost":{"psu_bay_index":"2","psu_index":"3","psu_sn":"PSU999"}}',
          },
          expectedMessage:
            "Communication lost with power supply in bay 2 (ID: 3). Serial number: PSU999",
        },
        {
          name: "MixedHashboardTypesInBay",
          errorData: {
            error_code: "MixedHashboardTypesInBay",
            details:
              '{"MixedHashboardTypesInBay":{"hashboards":[{"hb_type":"Type_A"},{"hb_type":"Type_B"}]}}',
          },
          expectedMessage:
            "Incompatible hashboard types detected in the same bay: Type_A, Type_B",
        },
        {
          name: "MixedHashboardTypesInBay without hashboards array",
          errorData: {
            error_code: "MixedHashboardTypesInBay",
            details: '{"MixedHashboardTypesInBay":{}}',
          },
          expectedMessage:
            "Incompatible hashboard types detected in the same bay: ",
        },
      ],
    },
    {
      category: "New Standard Error Codes",
      cases: [
        {
          name: "new ASIC overheating error (04:0001)",
          errorData: {
            error_code: "04:0001",
            details:
              '{"AsicOverheat":{"hb_slot":"2","asic_index":"4","temperature":"108"}}',
          },
          expectedMessage:
            "Slot 2 Hashboard's ASIC (4) is overheating at 108°C",
        },
        {
          name: "new hashboard overheating error (04:0006)",
          errorData: {
            error_code: "04:0006",
            details: '{"HashboardOverheat":{"hb_slot":"3","temperature":"92"}}',
          },
          expectedMessage: "Slot 3 Hashboard is overheating at 92°C",
        },
        {
          name: "new fan not spinning error (01:0002)",
          errorData: {
            error_code: "01:0002",
            details:
              '{"FanNotSpinning":{"fan_id":"3","fan_bay_index":"2","fan_pwm_target_pct":"55","fan_rpm_tach":"0"}}',
          },
          expectedMessage:
            "Fan 3 in bay 2 is not spinning. Target fan speed: 55%, Actual RPM: 0",
        },
        {
          name: "new PSU hardware fault error (00:0011)",
          errorData: {
            error_code: "00:0011",
            details:
              '{"PsuHardwareFault":{"psu_bay_index":"2","psu_index":"2","psu_sn":"PSU789","fault":{"message":"Overvoltage detected"}}}',
          },
          expectedMessage:
            "Power supply in bay 2 (ID: 2) has a hardware fault: Overvoltage detected. Serial number: PSU789",
        },
        {
          name: "new PSU communication error (00:0006)",
          errorData: {
            error_code: "00:0006",
            details:
              '{"PsuCommsLost":{"psu_bay_index":"1","psu_index":"1","psu_sn":"PSU123"}}',
          },
          expectedMessage:
            "Communication lost with power supply in bay 1 (ID: 1). Serial number: PSU123",
        },
        {
          name: "new unsupported hashboard config error (03:0015)",
          errorData: {
            error_code: "03:0015",
            details:
              '{"MixedHashboardTypesInBay":{"hashboards":[{"hb_type":"TypeA"},{"hb_type":"TypeB"}]}}',
          },
          expectedMessage:
            "Incompatible hashboard types detected in the same bay: TypeA, TypeB",
        },
        {
          name: "PSU recovery error (00:0014) with details",
          errorData: {
            error_code: "00:0014",
            details:
              '{"PsuRecoveryInProgress":{"psu_bay_index":"2","psu_index":"1","psu_sn":"PSU789"}}',
          },
          expectedMessage:
            "Power supply in bay 2 (ID: 1) is recovering from overtemperature. Serial number: PSU789",
        },
        {
          name: "Insufficient cooling error (01:0004) with multiple fans",
          errorData: {
            error_code: "01:0004",
            details:
              '{"InsufficientCooling":{"bay_index":"2","num_operational_fans":1,"num_expected_fans":1,"failed_fans":[4,5],"required_fans":[4,6]}}',
          },
          expectedMessage:
            "Bay 2 has insufficient cooling. Required fans: [4, 6]. Failed fans: [4, 5]",
        },
        {
          name: "Insufficient cooling error (01:0004) with single fan",
          errorData: {
            error_code: "01:0004",
            details:
              '{"InsufficientCooling":{"bay_index":"1","num_operational_fans":1,"num_expected_fans":1,"failed_fans":[3],"required_fans":[3]}}',
          },
          expectedMessage:
            "Bay 1 has insufficient cooling. Required fan: 3. Failed fan: 3",
        },
        {
          name: "Hashboard recovery error (04:0017) with details",
          errorData: {
            error_code: "04:0017",
            details:
              '{"HbRecoveryInProgress":{"hb_slot":"3","hb_sn":"531PP78310003192"}}',
          },
          expectedMessage:
            "Slot 3 Hashboard is recovering from overtemperature. Serial number: 531PP78310003192",
        },
      ],
    },
  ];

  errorMessageTestCases.forEach(({ category, cases }) => {
    describe(category, () => {
      cases.forEach(({ name, errorData, expectedMessage }) => {
        it(`should handle ${name}`, () => {
          const error = errorData ? createMockError(errorData) : undefined;
          const result = getErrorMessage(error);
          expect(result).toBe(expectedMessage);
        });
      });
    });
  });
});
