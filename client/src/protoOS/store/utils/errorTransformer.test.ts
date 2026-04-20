import { describe, expect, it } from "vitest";
import { transformErrors, transformNotificationError } from "./errorTransformer";
import type { ErrorListResponse, NotificationError } from "@/protoOS/api/generatedApi";

describe("errorTransformer", () => {
  describe("transformNotificationError", () => {
    it("should map all fields correctly when provided", () => {
      const apiError: NotificationError = {
        error_code: "E001",
        timestamp: 1234567890,
        source: "fan",
        slot: 2,
        message: "Fan speed low",
      };

      const result = transformNotificationError(apiError);

      expect(result).toEqual({
        errorCode: "E001",
        timestamp: 1234567890,
        source: "FAN",
        slot: 2,
        message: "Fan speed low",
      });
    });

    it("should handle optional fields correctly", () => {
      const apiError: NotificationError = {
        error_code: "E002",
        source: "psu",
        message: "PSU fault",
        // timestamp and slot omitted
      };

      const result = transformNotificationError(apiError);

      expect(result).toEqual({
        errorCode: "E002",
        timestamp: undefined,
        source: "PSU",
        slot: undefined,
        message: "PSU fault",
      });
    });

    it("should uppercase source field", () => {
      const sources: Array<["rig" | "fan" | "psu" | "hashboard", string]> = [
        ["rig", "RIG"],
        ["fan", "FAN"],
        ["psu", "PSU"],
        ["hashboard", "HASHBOARD"],
      ];

      sources.forEach(([input, expected]) => {
        const apiError: NotificationError = {
          error_code: "E003",
          source: input,
          message: "Test error",
        };

        const result = transformNotificationError(apiError);
        expect(result.source).toBe(expected);
      });
    });

    it("should use default values for missing required fields", () => {
      const apiError: NotificationError = {};

      const result = transformNotificationError(apiError);

      expect(result).toEqual({
        errorCode: "",
        timestamp: undefined,
        source: "RIG", // Default when source is missing
        slot: undefined,
        message: "Error undefined", // Default message when both message and error_code are missing
      });
    });

    it("should generate default message when message is missing but error_code is present", () => {
      const apiError: NotificationError = {
        error_code: "E004",
        source: "hashboard",
        // message omitted
      };

      const result = transformNotificationError(apiError);

      expect(result.message).toBe("Error E004");
    });
  });

  describe("transformErrors", () => {
    it("should transform an array of errors", () => {
      const apiErrors: ErrorListResponse = [
        {
          error_code: "E001",
          timestamp: 1234567890,
          source: "fan",
          slot: 0,
          message: "Fan 1 error",
        },
        {
          error_code: "E002",
          timestamp: 1234567891,
          source: "psu",
          slot: 1,
          message: "PSU 2 error",
        },
      ];

      const result = transformErrors(apiErrors);

      expect(result).toHaveLength(2);
      expect(result[0]).toEqual({
        errorCode: "E001",
        timestamp: 1234567890,
        source: "FAN",
        slot: 0,
        message: "Fan 1 error",
      });
      expect(result[1]).toEqual({
        errorCode: "E002",
        timestamp: 1234567891,
        source: "PSU",
        slot: 1,
        message: "PSU 2 error",
      });
    });

    it("should return empty array for undefined input", () => {
      const result = transformErrors(undefined);
      expect(result).toEqual([]);
    });

    it("should return empty array for empty input", () => {
      const result = transformErrors([]);
      expect(result).toEqual([]);
    });
  });
});
