import { renderHook } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { useNeedsAttention } from "./useNeedsAttention";

describe("useNeedsAttention", () => {
  describe("no issues", () => {
    it("should return false when all flags are false and no errors", () => {
      const { result } = renderHook(() => useNeedsAttention(false, false, []));
      expect(result.current).toBe(false);
    });

    it("should return false when errors is undefined", () => {
      const { result } = renderHook(() => useNeedsAttention(false, false, undefined));
      expect(result.current).toBe(false);
    });
  });

  describe("authentication flag", () => {
    it("should return true when needsAuthentication is true", () => {
      const { result } = renderHook(() => useNeedsAttention(true, false, []));
      expect(result.current).toBe(true);
    });

    it("should return true when needsAuthentication is true even with empty errors", () => {
      const { result } = renderHook(() => useNeedsAttention(true, false, undefined));
      expect(result.current).toBe(true);
    });
  });

  describe("mining pool flag", () => {
    it("should return true when needsMiningPool is true", () => {
      const { result } = renderHook(() => useNeedsAttention(false, true, []));
      expect(result.current).toBe(true);
    });

    it("should return true when needsMiningPool is true even with empty errors", () => {
      const { result } = renderHook(() => useNeedsAttention(false, true, undefined));
      expect(result.current).toBe(true);
    });
  });

  describe("hardware errors", () => {
    it("should return true when errors array has items", () => {
      const errors = [{ componentType: "hashboard", slot: 1 }];
      const { result } = renderHook(() => useNeedsAttention(false, false, errors));
      expect(result.current).toBe(true);
    });

    it("should return true when errors array has multiple items", () => {
      const errors = [
        { componentType: "hashboard", slot: 1 },
        { componentType: "psu", slot: 2 },
      ];
      const { result } = renderHook(() => useNeedsAttention(false, false, errors));
      expect(result.current).toBe(true);
    });
  });

  describe("device error flag", () => {
    it("should return true when hasDeviceError is true", () => {
      // Act
      const { result } = renderHook(() => useNeedsAttention(false, false, [], true));

      // Assert
      expect(result.current).toBe(true);
    });

    it("should return false when hasDeviceError is false and no other issues", () => {
      // Act
      const { result } = renderHook(() => useNeedsAttention(false, false, [], false));

      // Assert
      expect(result.current).toBe(false);
    });
  });

  describe("firmware status flag", () => {
    it("should return true when hasFirmwareStatus is true", () => {
      const { result } = renderHook(() => useNeedsAttention(false, false, [], false, true));
      expect(result.current).toBe(true);
    });

    it("should return false when hasFirmwareStatus is false and no other issues", () => {
      const { result } = renderHook(() => useNeedsAttention(false, false, [], false, false));
      expect(result.current).toBe(false);
    });

    it("should default hasFirmwareStatus to false when not provided", () => {
      const { result } = renderHook(() => useNeedsAttention(false, false, [], false));
      expect(result.current).toBe(false);
    });
  });

  describe("combined flags", () => {
    it("should return true when needsAuthentication and needsMiningPool are both true", () => {
      const { result } = renderHook(() => useNeedsAttention(true, true, []));
      expect(result.current).toBe(true);
    });

    it("should return true when needsAuthentication is true and there are errors", () => {
      const errors = [{ componentType: "hashboard", slot: 1 }];
      const { result } = renderHook(() => useNeedsAttention(true, false, errors));
      expect(result.current).toBe(true);
    });

    it("should return true when needsMiningPool is true and there are errors", () => {
      const errors = [{ componentType: "psu", slot: 1 }];
      const { result } = renderHook(() => useNeedsAttention(false, true, errors));
      expect(result.current).toBe(true);
    });

    it("should return true when all flags are true", () => {
      const errors = [{ componentType: "fan", slot: 1 }];
      const { result } = renderHook(() => useNeedsAttention(true, true, errors, true));
      expect(result.current).toBe(true);
    });
  });

  describe("input changes", () => {
    it("should recompute when inputs change", () => {
      const errors = [{ componentType: "hashboard", slot: 1 }];
      const { result, rerender } = renderHook(
        ({ needsAuth, needsPool, errs }) => useNeedsAttention(needsAuth, needsPool, errs),
        {
          initialProps: {
            needsAuth: false,
            needsPool: false,
            errs: errors,
          },
        },
      );

      expect(result.current).toBe(true);

      // Rerender with different errors (empty)
      rerender({
        needsAuth: false,
        needsPool: false,
        errs: [],
      });

      // Result should change
      expect(result.current).toBe(false);
    });
  });
});
