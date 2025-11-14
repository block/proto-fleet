import { describe, expect, it } from "vitest";
import {
  transformErrors,
  transformNotificationError,
} from "./errorTransformer";
import type { NotificationError } from "@/protoOS/api/generatedApi";

describe("errorTransformer", () => {
  describe("transformNotificationError", () => {
    it("should transform a PSU error correctly", () => {
      const apiError: NotificationError = {
        error_code: "00:0006",
        error_level: "Error",
        inserted_at: 1234567890,
        expired_at: undefined,
        message: "PSU communication lost",
        details: JSON.stringify({
          PsuCommunicationLost: {
            psu_bay_index: 2,
            psu_index: 1,
          },
        }),
      };

      const result = transformNotificationError(apiError);

      expect(result).toEqual({
        errorCode: "00:0006",
        errorLevel: "ERROR",
        insertedAt: 1234567890,
        expiredAt: undefined,
        source: "PSU",
        componentIndex: 1, // psu_bay_index - 1 (0-based)
        message: "Communication lost with power supply #1",
      });
    });

    it("should transform a FAN error correctly", () => {
      const apiError: NotificationError = {
        error_code: "01:0001",
        error_level: "Warning",
        inserted_at: 1234567890,
        expired_at: 1234567900,
        message: "Fan running slow",
        details: JSON.stringify({
          FanSlow: {
            fan_bay_index: 3,
            fan_id: 2,
            fan_pwm_target_pct: 75,
            fan_rpm_tach: 1500,
          },
        }),
      };

      const result = transformNotificationError(apiError);

      expect(result).toEqual({
        errorCode: "01:0001",
        errorLevel: "WARNING",
        insertedAt: 1234567890,
        expiredAt: 1234567900,
        source: "FAN",
        componentIndex: 2, // fan_bay_index - 1 (0-based)
        message: "Fan #3 running slow. Target: 75%, Actual: 1500 RPM",
      });
    });

    it("should transform an ASIC error with hashboard index", () => {
      const apiError: NotificationError = {
        error_code: "04:0001",
        error_level: "Error",
        inserted_at: 1234567890,
        message: "ASIC overheating",
        details: JSON.stringify({
          AsicOverheating: {
            asic_index: 4,
            hb_slot: 2,
            temperature: 95,
          },
        }),
      };

      const result = transformNotificationError(apiError);

      expect(result).toEqual({
        errorCode: "04:0001",
        errorLevel: "ERROR",
        insertedAt: 1234567890,
        expiredAt: undefined,
        source: "HASHBOARD", // ASIC errors are transformed to HASHBOARD
        componentIndex: 1, // hb_slot - 1 (0-based) - now using hashboard index as component index
        message: "ASIC 4 on hashboard 2 overheating: 95°C",
      });
    });

    it("should transform a HASHBOARD error", () => {
      const apiError: NotificationError = {
        error_code: "04:0006",
        error_level: "Error",
        inserted_at: 1234567890,
        details: JSON.stringify({
          HashboardOverheating: {
            hb_slot: 3,
            temperature: 85,
          },
        }),
      };

      const result = transformNotificationError(apiError);

      expect(result).toEqual({
        errorCode: "04:0006",
        errorLevel: "ERROR",
        insertedAt: 1234567890,
        expiredAt: undefined,
        source: "HASHBOARD",
        componentIndex: 2, // hb_slot - 1 (0-based)
        hashboardIndex: undefined, // Only ASIC errors have hashboardIndex
        message: "Hashboard slot 3 overheating: 85°C",
      });
    });

    it("should transform a POOL error", () => {
      const apiError: NotificationError = {
        error_code: "03:0006",
        error_level: "Error",
        inserted_at: 1234567890,
        details: JSON.stringify({
          PoolConnectionLost: {
            pool_id: 0,
            pool_url: "pool.example.com:3333",
          },
        }),
      };

      const result = transformNotificationError(apiError);

      expect(result).toEqual({
        errorCode: "03:0006",
        errorLevel: "ERROR",
        insertedAt: 1234567890,
        expiredAt: undefined,
        source: "POOL",
        componentIndex: 0,
        message: "Pool connection lost: pool.example.com:3333",
      });
    });

    it("should handle SYSTEM errors", () => {
      const apiError: NotificationError = {
        error_code: "03:0015",
        error_level: "Error",
        inserted_at: 1234567890,
        details: JSON.stringify({
          IncompatibleHashboards: {
            hashboards: [
              { hb_slot: 1, hb_type: "TypeA" },
              { hb_slot: 2, hb_type: "TypeB" },
            ],
          },
        }),
      };

      const result = transformNotificationError(apiError);

      expect(result).toEqual({
        errorCode: "03:0015",
        errorLevel: "ERROR",
        insertedAt: 1234567890,
        expiredAt: undefined,
        source: "SYSTEM",
        componentIndex: undefined,
        message:
          "Incompatible hashboard types detected in the same bay: TypeA, TypeB",
      });
    });

    it("should handle missing details", () => {
      const apiError: NotificationError = {
        error_code: "00:0001",
        error_level: "Error",
        inserted_at: 1234567890,
        message: "PSU failure",
      };

      const result = transformNotificationError(apiError);

      expect(result).toEqual({
        errorCode: "00:0001",
        errorLevel: "ERROR",
        insertedAt: 1234567890,
        expiredAt: undefined,
        source: "PSU",
        componentIndex: undefined,
        message: "Power supply failure to start",
      });
    });

    it("should handle invalid JSON in details", () => {
      const apiError: NotificationError = {
        error_code: "00:0001",
        error_level: "Error",
        inserted_at: 1234567890,
        details: "invalid json",
        message: "PSU error",
      };

      const result = transformNotificationError(apiError);

      expect(result).toEqual({
        errorCode: "00:0001",
        errorLevel: "ERROR",
        insertedAt: 1234567890,
        expiredAt: undefined,
        source: "PSU",
        componentIndex: undefined,
        message: "Power supply failure to start",
      });
    });

    it("should use fallback for unknown error codes", () => {
      const apiError: NotificationError = {
        error_code: "99:9999",
        error_level: "Warning",
        inserted_at: 1234567890,
        message: "Unknown error occurred",
      };

      const result = transformNotificationError(apiError);

      expect(result).toEqual({
        errorCode: "99:9999",
        errorLevel: "WARNING",
        insertedAt: 1234567890,
        expiredAt: undefined,
        source: "SYSTEM",
        componentIndex: undefined,
        message: "Unknown error occurred",
      });
    });

    it("should normalize error level to uppercase", () => {
      const apiError: NotificationError = {
        error_code: "00:0001",
        error_level: "error" as any,
        inserted_at: 1234567890,
      };

      const result = transformNotificationError(apiError);

      expect(result.errorLevel).toBe("ERROR");
    });

    it("should use current timestamp if inserted_at is missing", () => {
      const now = Date.now();
      const apiError: NotificationError = {
        error_code: "00:0001",
        error_level: "Error",
      };

      const result = transformNotificationError(apiError);

      expect(result.insertedAt).toBeGreaterThanOrEqual(now);
      expect(result.insertedAt).toBeLessThanOrEqual(Date.now());
    });
  });

  describe("transformErrors", () => {
    it("should transform an array of errors", () => {
      const apiErrors: NotificationError[] = [
        {
          error_code: "00:0001",
          error_level: "Error",
          inserted_at: 1234567890,
        },
        {
          error_code: "01:0001",
          error_level: "Warning",
          inserted_at: 1234567891,
        },
      ];

      const result = transformErrors(apiErrors);

      expect(result).toHaveLength(2);
      expect(result[0].source).toBe("PSU");
      expect(result[1].source).toBe("FAN");
    });

    it("should return empty array for undefined input", () => {
      const result = transformErrors(undefined);
      expect(result).toEqual([]);
    });

    it("should return empty array for null input", () => {
      const result = transformErrors(null as any);
      expect(result).toEqual([]);
    });

    it("should return empty array for non-array input", () => {
      const result = transformErrors("not an array" as any);
      expect(result).toEqual([]);
    });

    it("should handle empty array", () => {
      const result = transformErrors([]);
      expect(result).toEqual([]);
    });
  });
});
