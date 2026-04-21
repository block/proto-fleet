import { renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { errorQueryClient } from "./clients";
import { useComponentErrors } from "./useComponentErrors";
import {
  ComponentErrorSchema,
  ComponentErrorsSchema,
  ComponentType,
  ErrorMessageSchema,
  QueryResponseSchema,
} from "@/protoFleet/api/generated/errors/v1/errors_pb";

vi.mock("./clients", () => ({
  errorQueryClient: {
    query: vi.fn(),
  },
}));

vi.mock("@/protoFleet/store", () => ({
  useFleetStore: vi.fn((selector) =>
    selector({
      auth: { authLoading: false },
    }),
  ),
  useAuthErrors: vi.fn(() => ({
    handleAuthErrors: vi.fn(({ onError }) => onError),
  })),
}));

describe("useComponentErrors", () => {
  beforeEach(() => {
    vi.clearAllMocks();

    // Default mock for query - empty response
    vi.mocked(errorQueryClient.query).mockResolvedValue(
      create(QueryResponseSchema, {
        result: {
          case: "components",
          value: create(ComponentErrorsSchema, { items: [] }),
        },
      }),
    );
  });

  describe("device counting logic", () => {
    it("counts unique devices, not component instances (THE BUG FIX)", async () => {
      // Device A has 3 fans with errors (fan_0, fan_1, fan_2)
      // This should count as 1 device, not 3
      const mockResponse = create(QueryResponseSchema, {
        result: {
          case: "components",
          value: create(ComponentErrorsSchema, {
            items: [
              create(ComponentErrorSchema, {
                componentId: "device-a_fan_0",
                componentType: ComponentType.FAN,
                deviceIdentifier: "device-a",
                errors: [create(ErrorMessageSchema, { errorId: "err-1" })],
              }),
              create(ComponentErrorSchema, {
                componentId: "device-a_fan_1",
                componentType: ComponentType.FAN,
                deviceIdentifier: "device-a",
                errors: [create(ErrorMessageSchema, { errorId: "err-2" })],
              }),
              create(ComponentErrorSchema, {
                componentId: "device-a_fan_2",
                componentType: ComponentType.FAN,
                deviceIdentifier: "device-a",
                errors: [create(ErrorMessageSchema, { errorId: "err-3" })],
              }),
            ],
          }),
        },
      });

      vi.mocked(errorQueryClient.query).mockResolvedValue(mockResponse);

      const { result } = renderHook(() => useComponentErrors());

      await waitFor(() => {
        expect(result.current.hasLoaded).toBe(true);
      });

      // Should count 1 device with fan errors, not 3
      expect(result.current.fanErrors).toBe(1);
    });

    it("counts each unique device separately", async () => {
      // 3 different devices, each with 1 fan error
      const mockResponse = create(QueryResponseSchema, {
        result: {
          case: "components",
          value: create(ComponentErrorsSchema, {
            items: [
              create(ComponentErrorSchema, {
                componentType: ComponentType.FAN,
                deviceIdentifier: "device-a",
                errors: [create(ErrorMessageSchema, { errorId: "err-1" })],
              }),
              create(ComponentErrorSchema, {
                componentType: ComponentType.FAN,
                deviceIdentifier: "device-b",
                errors: [create(ErrorMessageSchema, { errorId: "err-2" })],
              }),
              create(ComponentErrorSchema, {
                componentType: ComponentType.FAN,
                deviceIdentifier: "device-c",
                errors: [create(ErrorMessageSchema, { errorId: "err-3" })],
              }),
            ],
          }),
        },
      });

      vi.mocked(errorQueryClient.query).mockResolvedValue(mockResponse);

      const { result } = renderHook(() => useComponentErrors());

      await waitFor(() => {
        expect(result.current.hasLoaded).toBe(true);
      });

      expect(result.current.fanErrors).toBe(3);
    });

    it("handles mix of devices with multiple components correctly (regression test)", async () => {
      // Device A: fan_0, fan_1, fan_2, fan_3 (4 fans)
      // Device B: fan_0, fan_1, fan_2 (3 fans)
      // Device C: fan_0, fan_1, fan_2, fan_3 (4 fans)
      // Total: 11 component entries, but only 3 unique devices
      const items = [
        // Device A - 4 fans
        ...["fan_0", "fan_1", "fan_2", "fan_3"].map((fan, i) =>
          create(ComponentErrorSchema, {
            componentId: `device-a_${fan}`,
            componentType: ComponentType.FAN,
            deviceIdentifier: "device-a",
            errors: [create(ErrorMessageSchema, { errorId: `err-a-${i}` })],
          }),
        ),
        // Device B - 3 fans
        ...["fan_0", "fan_1", "fan_2"].map((fan, i) =>
          create(ComponentErrorSchema, {
            componentId: `device-b_${fan}`,
            componentType: ComponentType.FAN,
            deviceIdentifier: "device-b",
            errors: [create(ErrorMessageSchema, { errorId: `err-b-${i}` })],
          }),
        ),
        // Device C - 4 fans
        ...["fan_0", "fan_1", "fan_2", "fan_3"].map((fan, i) =>
          create(ComponentErrorSchema, {
            componentId: `device-c_${fan}`,
            componentType: ComponentType.FAN,
            deviceIdentifier: "device-c",
            errors: [create(ErrorMessageSchema, { errorId: `err-c-${i}` })],
          }),
        ),
      ];

      const mockResponse = create(QueryResponseSchema, {
        result: {
          case: "components",
          value: create(ComponentErrorsSchema, { items }),
        },
      });

      vi.mocked(errorQueryClient.query).mockResolvedValue(mockResponse);

      const { result } = renderHook(() => useComponentErrors());

      await waitFor(() => {
        expect(result.current.hasLoaded).toBe(true);
      });

      // Should count 3 devices, not 11 component instances
      expect(result.current.fanErrors).toBe(3);
    });

    it("tracks each component type independently", async () => {
      // Device A has both fan and hashboard errors
      const mockResponse = create(QueryResponseSchema, {
        result: {
          case: "components",
          value: create(ComponentErrorsSchema, {
            items: [
              create(ComponentErrorSchema, {
                componentType: ComponentType.FAN,
                deviceIdentifier: "device-a",
                errors: [create(ErrorMessageSchema, { errorId: "err-fan" })],
              }),
              create(ComponentErrorSchema, {
                componentType: ComponentType.HASH_BOARD,
                deviceIdentifier: "device-a",
                errors: [create(ErrorMessageSchema, { errorId: "err-hb" })],
              }),
            ],
          }),
        },
      });

      vi.mocked(errorQueryClient.query).mockResolvedValue(mockResponse);

      const { result } = renderHook(() => useComponentErrors());

      await waitFor(() => {
        expect(result.current.hasLoaded).toBe(true);
      });

      expect(result.current.fanErrors).toBe(1);
      expect(result.current.hashboardErrors).toBe(1);
    });
  });

  describe("hook behavior", () => {
    it("returns zero counts for empty response", async () => {
      const { result } = renderHook(() => useComponentErrors());

      await waitFor(() => {
        expect(result.current.hasLoaded).toBe(true);
      });

      expect(result.current.fanErrors).toBe(0);
      expect(result.current.hashboardErrors).toBe(0);
      expect(result.current.psuErrors).toBe(0);
      expect(result.current.controlBoardErrors).toBe(0);
    });

    it("returns isLoading true initially", () => {
      const { result } = renderHook(() => useComponentErrors());

      expect(result.current.isLoading).toBe(true);
    });

    it("sets hasLoaded after successful fetch", async () => {
      const { result } = renderHook(() => useComponentErrors());

      expect(result.current.hasLoaded).toBe(false);

      await waitFor(() => {
        expect(result.current.hasLoaded).toBe(true);
      });
    });
  });
});
