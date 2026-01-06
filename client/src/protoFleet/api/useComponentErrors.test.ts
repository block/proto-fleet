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
import { type FleetStore, useFleetStore } from "@/protoFleet/store";

vi.mock("./clients", () => ({
  errorQueryClient: {
    query: vi.fn(),
    watch: vi.fn(),
  },
}));

vi.mock("@/protoFleet/store", () => ({
  useFleetStore: vi.fn(),
  useAuthErrors: vi.fn(() => ({
    handleAuthErrors: vi.fn(({ onError }) => onError),
  })),
}));

vi.mock("@/protoFleet/utils/streamCleanup", () => ({
  streamCleanupManager: {
    register: vi.fn(),
    unregister: vi.fn(),
  },
}));

describe("useComponentErrors", () => {
  const mockSetComponentErrorCounts = vi.fn();
  const mockHandleComponentErrorStream = vi.fn();
  const mockClearComponentErrors = vi.fn();

  const createMockStoreState = (overrides = {}) => ({
    auth: { authLoading: false },
    dashboard: {
      componentErrors: {
        counts: {},
      },
      setComponentErrorCounts: mockSetComponentErrorCounts,
      handleComponentErrorStream: mockHandleComponentErrorStream,
      clearComponentErrors: mockClearComponentErrors,
      ...overrides,
    },
  });

  beforeEach(() => {
    vi.clearAllMocks();

    // Default mock implementation for useFleetStore
    vi.mocked(useFleetStore).mockImplementation(<T>(selector: (state: FleetStore) => T): T => {
      const state = createMockStoreState() as unknown as FleetStore;
      return selector(state);
    });

    // Default mock for query - empty response
    vi.mocked(errorQueryClient.query).mockResolvedValue(
      create(QueryResponseSchema, {
        result: {
          case: "components",
          value: create(ComponentErrorsSchema, { items: [] }),
        },
      }),
    );

    // Mock watch to return an empty async iterator
    vi.mocked(errorQueryClient.watch).mockReturnValue({
      [Symbol.asyncIterator]: () => ({
        next: () => new Promise(() => {}), // Never resolves to prevent streaming
      }),
    } as unknown as ReturnType<typeof errorQueryClient.watch>);
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

      renderHook(() => useComponentErrors());

      await waitFor(() => {
        expect(mockSetComponentErrorCounts).toHaveBeenCalled();
      });

      // Should call setComponentErrorCounts with fanErrors = 1 (not 3)
      expect(mockSetComponentErrorCounts).toHaveBeenCalledWith(
        expect.objectContaining({ [ComponentType.FAN]: 1 }),
        expect.any(Object),
      );
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

      renderHook(() => useComponentErrors());

      await waitFor(() => {
        expect(mockSetComponentErrorCounts).toHaveBeenCalled();
      });

      // Should count 3 devices
      expect(mockSetComponentErrorCounts).toHaveBeenCalledWith(
        expect.objectContaining({ [ComponentType.FAN]: 3 }),
        expect.any(Object),
      );
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

      renderHook(() => useComponentErrors());

      await waitFor(() => {
        expect(mockSetComponentErrorCounts).toHaveBeenCalled();
      });

      // Should count 3 devices, not 11 component instances
      expect(mockSetComponentErrorCounts).toHaveBeenCalledWith(
        expect.objectContaining({ [ComponentType.FAN]: 3 }),
        expect.any(Object),
      );
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

      renderHook(() => useComponentErrors());

      await waitFor(() => {
        expect(mockSetComponentErrorCounts).toHaveBeenCalled();
      });

      // Should count: fanErrors = 1, hashboardErrors = 1
      expect(mockSetComponentErrorCounts).toHaveBeenCalledWith(
        expect.objectContaining({
          [ComponentType.FAN]: 1,
          [ComponentType.HASH_BOARD]: 1,
        }),
        expect.any(Object),
      );
    });
  });

  describe("hook behavior", () => {
    it("returns correct error counts from store", () => {
      vi.mocked(useFleetStore).mockImplementation(<T>(selector: (state: FleetStore) => T): T => {
        const state = createMockStoreState({
          componentErrors: {
            counts: {
              [ComponentType.FAN]: 5,
              [ComponentType.HASH_BOARD]: 3,
              [ComponentType.PSU]: 2,
              [ComponentType.CONTROL_BOARD]: 1,
            },
          },
        }) as unknown as FleetStore;
        return selector(state);
      });

      const { result } = renderHook(() => useComponentErrors());

      expect(result.current.fanErrors).toBe(5);
      expect(result.current.hashboardErrors).toBe(3);
      expect(result.current.psuErrors).toBe(2);
      expect(result.current.controlBoardErrors).toBe(1);
    });

    it("returns isLoading true initially", () => {
      const { result } = renderHook(() => useComponentErrors());

      expect(result.current.isLoading).toBe(true);
    });

    it("clears component errors on mount", async () => {
      renderHook(() => useComponentErrors());

      await waitFor(() => {
        expect(mockClearComponentErrors).toHaveBeenCalled();
      });
    });
  });
});
