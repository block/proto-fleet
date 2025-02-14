import { ErrorListResponse, NotificationError } from "@/protoOS/api/types";

import { ErrorLevel } from "@/protoOS/components/MinerStatusModal/constants";

export const mockHashboardError = {
  source: "Hashboard",
  component_index: undefined,
  hashboard_index: 1,
  asic_index: undefined,
  error_code: undefined,
  inserted_at: 22222,
  expired_at: 0,
  error_level: undefined,
  message: "Hashboard is malfunctioning",
  details: undefined,
} as NotificationError;

export const mockAsicError = {
  source: "ASIC",
  component_index: undefined,
  hashboard_index: 1,
  asic_index: 1,
  error_code: undefined,
  inserted_at: 33333,
  expired_at: 0,
  error_level: undefined,
  message: "ASIC is malfunctioning",
  details: undefined,
} as NotificationError;

export const mockFanError = {
  source: "Miner",
  component_index: 0,
  hashboard_index: undefined,
  asic_index: undefined,
  error_code: undefined,
  inserted_at: 111111,
  expired_at: 0,
  error_level: undefined,
  message: "Fan is malfunctioning",
  details: undefined,
} as NotificationError;

export const storyArgs = {
  loading: false,
  hashboardStatus: "warning",
  asicStatus: "warning and error",
  fanStatus: "normal",
  hashboardErrorCode: "HashboardOverheat",
  asicErrorCode: "AsicOverVoltage",
  fanErrorCode: "FanSlow",
};

const statues = {
  normal: "normal",
  warning: "warning",
  error: "error",
  warningAndError: "warning and error",
} as const;

const statusOptions = [
  statues.normal,
  statues.warning,
  statues.error,
  statues.warningAndError,
];

const errorDetails = {
  AsicOverheat: {
    AsicOverheat: { port: 1, asic_row: 1, asic_col: 3, temperature: 100.0 },
  },
  AsicOverVoltage: {
    AsicOverVoltage: { port: 1, asic_row: 1, asic_col: 3, voltage: 100.0 },
  },
  AsicFailure: {
    AsicFailure: { port: 1, asic_row: 1, asic_col: 3, temperature: 100.0 },
  },
  FanSlow: { FanSlow: { fan_rpm_target: 23, fan_rpm_tach: 300 } },
  HashboardOverCurrent: { HashboardOverCurrent: { port: 1, current: 100.0 } },
  HashboardOverheat: { HashboardOverheat: { port: 1, temperature: 100.0 } },
  HashboardOverVoltage: { HashboardOverVoltage: { port: 1, voltage: 100.0 } },
  HashboardPowerLost: { HashboardPowerLost: { port: 1 } },
  HashboardUnderVoltage: { HashboardUnderVoltage: { port: 1, voltage: 100.0 } },
  HashboardUsbConnectionLost: {
    HashboardUsbConnectionLost: { port: 1, serial_number: "123" },
  },
  PoolConnectionLost: {
    PoolConnectionLost: { pool_id: 1, pool_url: "pool.com" },
  },
  NoPoolConfigured: {
    NoPoolConfigured: {},
  },
};

const hashboardErrorCodes = [
  "HashboardOverCurrent",
  "HashboardOverheat",
  "HashboardOverVoltage",
  "HashboardPowerLost",
  "HashboardUnderVoltage",
  "HashboardUsbConnectionLost",
] as const;

const asicErrorCodes = [
  "AsicOverheat",
  "AsicOverVoltage",
  "AsicFailure",
] as const;

const fanErrorCodes = ["FanSlow"] as const;

export const storyArgTypes = {
  loading: {
    control: "boolean",
  },
  hashboardStatus: {
    control: "select",
    options: statusOptions,
  },
  asicStatus: {
    control: "select",
    options: statusOptions,
  },
  fanStatus: {
    control: "select",
    options: statusOptions,
  },
  hashboardErrorCode: {
    control: "select",
    options: hashboardErrorCodes,
  },
  asicErrorCode: {
    control: "select",
    options: asicErrorCodes,
  },
  fanErrorCode: {
    control: "select",
    options: fanErrorCodes,
  },
};

export interface mockErrorListProps {
  hashboardStatus: (typeof statusOptions)[number];
  asicStatus: (typeof statusOptions)[number];
  fanStatus: (typeof statusOptions)[number];
  hashboardErrorCode: (typeof hashboardErrorCodes)[number];
  asicErrorCode: (typeof asicErrorCodes)[number];
  fanErrorCode: (typeof fanErrorCodes)[number];
}

export const getMockErrorList = ({
  hashboardStatus,
  asicStatus,
  fanStatus,
  hashboardErrorCode,
  asicErrorCode,
  fanErrorCode,
}: mockErrorListProps) => {
  const mockErrorResponse = [] as ErrorListResponse;

  if (hashboardStatus !== statues.normal) {
    const hashboardError = {
      ...mockHashboardError,
      error_code: hashboardErrorCode,
      error_level:
        hashboardStatus === statues.error ||
        hashboardStatus === statues.warningAndError
          ? ErrorLevel.error
          : ErrorLevel.warning,
      details: JSON.stringify(errorDetails[hashboardErrorCode]),
    } as NotificationError;
    if (hashboardStatus === statues.warningAndError) {
      const hashboardWarning = {
        ...hashboardError,
        error_level: ErrorLevel.warning,
      } as NotificationError;
      mockErrorResponse.push(hashboardWarning);
    }
    mockErrorResponse.push(hashboardError);
  }

  if (asicStatus !== statues.normal) {
    const asicError = {
      ...mockAsicError,
      error_code: asicErrorCode,
      error_level:
        asicStatus === statues.error || asicStatus === statues.warningAndError
          ? ErrorLevel.error
          : ErrorLevel.warning,
      details: JSON.stringify(errorDetails[asicErrorCode]),
    } as NotificationError;
    if (asicStatus === statues.warningAndError) {
      const asicWarning = {
        ...asicError,
        error_level: ErrorLevel.warning,
      } as NotificationError;
      mockErrorResponse.push(asicWarning);
    }
    mockErrorResponse.push(asicError);
  }

  if (fanStatus !== statues.normal) {
    const fanError = {
      ...mockFanError,
      error_code: fanErrorCode,
      error_level:
        fanStatus === statues.error || fanStatus === statues.warningAndError
          ? ErrorLevel.error
          : ErrorLevel.warning,
      details: JSON.stringify(errorDetails[fanErrorCode]),
    } as NotificationError;
    if (fanStatus === statues.warningAndError) {
      const fanWarning = {
        ...fanError,
        error_level: ErrorLevel.warning,
      } as NotificationError;
      mockErrorResponse.push(fanWarning);
    }
    mockErrorResponse.push(fanError);
  }

  // we ignore pool connection errors in the miner status widget
  mockErrorResponse.push({
    ...mockFanError,
    error_code: "PoolConnectionLost",
    error_level: ErrorLevel.error,
  });
  mockErrorResponse.push({
    ...mockFanError,
    error_code: "NoPoolConfigured",
    error_level: ErrorLevel.error,
  });

  return mockErrorResponse;
};
