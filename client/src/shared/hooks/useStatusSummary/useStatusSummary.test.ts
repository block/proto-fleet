import { renderHook } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { GroupedStatusErrors } from "./types";
import { useComponentStatusSummary, useMinerStatusSummary } from "./useStatusSummary";
import { getComponentDisplayName, getComponentSingularName } from "./utils";

const emptyErrors: GroupedStatusErrors = {
  hashboard: [],
  psu: [],
  fan: [],
  controlBoard: [],
};

describe("getComponentDisplayName", () => {
  it("should return capitalized name without index", () => {
    expect(getComponentDisplayName("hashboard")).toBe("Hashboard");
    expect(getComponentDisplayName("psu")).toBe("PSU");
    expect(getComponentDisplayName("fan")).toBe("Fan");
    expect(getComponentDisplayName("controlBoard")).toBe("Control board");
  });

  it("should return capitalized name with 1-based index", () => {
    expect(getComponentDisplayName("hashboard", 1)).toBe("Hashboard 1");
    expect(getComponentDisplayName("hashboard", 3)).toBe("Hashboard 3");
    expect(getComponentDisplayName("psu", 1)).toBe("PSU 1");
    expect(getComponentDisplayName("fan", 2)).toBe("Fan 2");
    expect(getComponentDisplayName("controlBoard", 1)).toBe("Control board 1");
  });
});

describe("getComponentSingularName", () => {
  it("should return singular lowercase names", () => {
    expect(getComponentSingularName("hashboard")).toBe("hashboard");
    expect(getComponentSingularName("psu")).toBe("PSU");
    expect(getComponentSingularName("fan")).toBe("fan");
    expect(getComponentSingularName("controlBoard")).toBe("control board");
  });
});

describe("useMinerStatusSummary", () => {
  describe("no errors", () => {
    it('should return condensed="Hashing" when online and not sleeping', () => {
      const { result } = renderHook(() => useMinerStatusSummary(emptyErrors, false));
      expect(result.current.condensed).toBe("Hashing");
      expect(result.current.title).toBe("All systems are operational");
      expect(result.current.subtitle).toBeUndefined();
    });

    it('should return condensed="Sleeping" when sleeping', () => {
      const { result } = renderHook(() => useMinerStatusSummary(emptyErrors, true));
      expect(result.current.condensed).toBe("Sleeping");
      expect(result.current.title).toBe("All systems are operational");
    });

    it('should return condensed="Offline" when offline (takes priority over sleeping)', () => {
      const { result } = renderHook(() => useMinerStatusSummary(emptyErrors, true, true));
      expect(result.current.condensed).toBe("Offline");
      expect(result.current.title).toBe("All systems are operational");
    });

    it('should return condensed="Needs Authentication" when needsAuthentication is true', () => {
      const { result } = renderHook(() => useMinerStatusSummary(emptyErrors, false, false, true));
      expect(result.current.condensed).toBe("Needs Authentication");
      expect(result.current.title).toBe("All systems are operational");
    });

    it('should return condensed="Needs mining pool" when needsMiningPool is true', () => {
      const { result } = renderHook(() => useMinerStatusSummary(emptyErrors, false, false, false, true));
      expect(result.current.condensed).toBe("Needs mining pool");
      expect(result.current.title).toBe("All systems are operational");
    });

    it("should prioritize offline > needsAuthentication > sleeping > needsMiningPool", () => {
      // All flags true - offline wins
      const { result: offlineWins } = renderHook(() => useMinerStatusSummary(emptyErrors, true, true, true, true));
      expect(offlineWins.current.condensed).toBe("Offline");

      // needsAuth and others (not offline) - needsAuth wins
      const { result: needsAuthWins } = renderHook(() => useMinerStatusSummary(emptyErrors, true, false, true, true));
      expect(needsAuthWins.current.condensed).toBe("Needs Authentication");

      // sleeping and needsMiningPool - sleeping wins
      const { result: sleepingWins } = renderHook(() => useMinerStatusSummary(emptyErrors, true, false, false, true));
      expect(sleepingWins.current.condensed).toBe("Sleeping");
    });

    it('should default to condensed="Hashing" when no flags provided', () => {
      const { result } = renderHook(() => useMinerStatusSummary(emptyErrors));
      expect(result.current.condensed).toBe("Hashing");
      expect(result.current.title).toBe("All systems are operational");
    });
  });

  describe("offline/sleeping takes priority for condensed only", () => {
    it('should return condensed="Sleeping" but title shows error when there are errors', () => {
      const errors: GroupedStatusErrors = {
        ...emptyErrors,
        hashboard: [{ componentType: "hashboard", slot: 1 }],
      };
      const { result } = renderHook(() => useMinerStatusSummary(errors, true));
      expect(result.current.condensed).toBe("Sleeping");
      expect(result.current.title).toBe("Hashboard 1 issue");
    });

    it('should return condensed="Offline" but title shows error when there are errors', () => {
      const errors: GroupedStatusErrors = {
        ...emptyErrors,
        hashboard: [{ componentType: "hashboard", slot: 1 }],
      };
      const { result } = renderHook(() => useMinerStatusSummary(errors, false, true));
      expect(result.current.condensed).toBe("Offline");
      expect(result.current.title).toBe("Hashboard 1 issue");
    });
  });

  describe("single error", () => {
    it('should return "[Component] [slot] issue" for hashboard with slot', () => {
      const errors: GroupedStatusErrors = {
        ...emptyErrors,
        hashboard: [{ componentType: "hashboard", slot: 1 }],
      };
      const { result } = renderHook(() => useMinerStatusSummary(errors));
      expect(result.current.condensed).toBe("Hashboard 1 issue");
      expect(result.current.title).toBe("Hashboard 1 issue");
    });

    it('should return "[Component] [slot] issue" for PSU with slot', () => {
      const errors: GroupedStatusErrors = {
        ...emptyErrors,
        psu: [{ componentType: "psu", slot: 2 }],
      };
      const { result } = renderHook(() => useMinerStatusSummary(errors));
      expect(result.current.condensed).toBe("PSU 2 issue");
      expect(result.current.title).toBe("PSU 2 issue");
    });

    it('should return "[Component] [slot] issue" for fan with slot', () => {
      const errors: GroupedStatusErrors = {
        ...emptyErrors,
        fan: [{ componentType: "fan", slot: 2 }],
      };
      const { result } = renderHook(() => useMinerStatusSummary(errors));
      expect(result.current.condensed).toBe("Fan 2 issue");
      expect(result.current.title).toBe("Fan 2 issue");
    });

    it('should return "[Component] issue" for control board without index', () => {
      const errors: GroupedStatusErrors = {
        ...emptyErrors,
        controlBoard: [{ componentType: "controlBoard" }],
      };
      const { result } = renderHook(() => useMinerStatusSummary(errors));
      expect(result.current.condensed).toBe("Control board issue");
      expect(result.current.title).toBe("Control board issue");
    });

    it('should return "[Component] issue" when no slot provided', () => {
      const errors: GroupedStatusErrors = {
        ...emptyErrors,
        hashboard: [{ componentType: "hashboard" }],
      };
      const { result } = renderHook(() => useMinerStatusSummary(errors));
      expect(result.current.condensed).toBe("Hashboard issue");
      expect(result.current.title).toBe("Hashboard issue");
    });
  });

  describe("multiple errors on one component type", () => {
    it('should return "Multiple hashboard issues" for multiple hashboard errors', () => {
      const errors: GroupedStatusErrors = {
        ...emptyErrors,
        hashboard: [
          { componentType: "hashboard", slot: 1 },
          { componentType: "hashboard", slot: 2 },
        ],
      };
      const { result } = renderHook(() => useMinerStatusSummary(errors));
      expect(result.current.condensed).toBe("Multiple hashboard issues");
      expect(result.current.title).toBe("Multiple hashboard issues");
    });

    it('should return "Multiple PSU issues" for multiple PSU errors', () => {
      const errors: GroupedStatusErrors = {
        ...emptyErrors,
        psu: [
          { componentType: "psu", slot: 1 },
          { componentType: "psu", slot: 1 },
        ],
      };
      const { result } = renderHook(() => useMinerStatusSummary(errors));
      expect(result.current.condensed).toBe("Multiple PSU issues");
      expect(result.current.title).toBe("Multiple PSU issues");
    });

    it('should return "Multiple fan issues" for multiple fan errors', () => {
      const errors: GroupedStatusErrors = {
        ...emptyErrors,
        fan: [{ componentType: "fan" }, { componentType: "fan" }, { componentType: "fan" }],
      };
      const { result } = renderHook(() => useMinerStatusSummary(errors));
      expect(result.current.condensed).toBe("Multiple fan issues");
      expect(result.current.title).toBe("Multiple fan issues");
    });

    it('should return "Multiple control board issues" for multiple control board errors', () => {
      const errors: GroupedStatusErrors = {
        ...emptyErrors,
        controlBoard: [{ componentType: "controlBoard" }, { componentType: "controlBoard" }],
      };
      const { result } = renderHook(() => useMinerStatusSummary(errors));
      expect(result.current.condensed).toBe("Multiple control board issues");
      expect(result.current.title).toBe("Multiple control board issues");
    });
  });

  describe("multiple component types with errors", () => {
    it('should return "Multiple issues" when hashboard and PSU have errors', () => {
      const errors: GroupedStatusErrors = {
        ...emptyErrors,
        hashboard: [{ componentType: "hashboard", slot: 1 }],
        psu: [{ componentType: "psu", slot: 1 }],
      };
      const { result } = renderHook(() => useMinerStatusSummary(errors));
      expect(result.current.condensed).toBe("Multiple issues");
      expect(result.current.title).toBe("Multiple issues");
    });

    it('should return "Multiple issues" when all component types have errors', () => {
      const errors: GroupedStatusErrors = {
        hashboard: [{ componentType: "hashboard", slot: 1 }],
        psu: [{ componentType: "psu", slot: 1 }],
        fan: [{ componentType: "fan", slot: 1 }],
        controlBoard: [{ componentType: "controlBoard" }],
      };
      const { result } = renderHook(() => useMinerStatusSummary(errors));
      expect(result.current.condensed).toBe("Multiple issues");
      expect(result.current.title).toBe("Multiple issues");
    });
  });
});

describe("useComponentStatusSummary", () => {
  describe("no errors", () => {
    it('should return title="All systems are operational"', () => {
      const { result } = renderHook(() => useComponentStatusSummary("hashboard", 1, 0));
      expect(result.current.title).toBe("All systems are operational");
      expect(result.current.subtitle).toBeUndefined();
    });

    it('should return title="All systems are operational" for any component type', () => {
      const { result: psu } = renderHook(() => useComponentStatusSummary("psu", 2, 0));
      expect(psu.current.title).toBe("All systems are operational");

      const { result: fan } = renderHook(() => useComponentStatusSummary("fan", undefined, 0));
      expect(fan.current.title).toBe("All systems are operational");
    });
  });

  describe("single error", () => {
    it("should return title=null to indicate UI should show error message instead", () => {
      const { result: hashboard } = renderHook(() => useComponentStatusSummary("hashboard", 1, 1));
      expect(hashboard.current.title).toBeNull();

      const { result: psu } = renderHook(() => useComponentStatusSummary("psu", 2, 1));
      expect(psu.current.title).toBeNull();

      const { result: fan } = renderHook(() => useComponentStatusSummary("fan", 3, 1));
      expect(fan.current.title).toBeNull();

      const { result: controlBoard } = renderHook(() => useComponentStatusSummary("controlBoard", undefined, 1));
      expect(controlBoard.current.title).toBeNull();
    });
  });

  describe("multiple errors", () => {
    it('should return title="[Component] [slot] has multiple issues" with slot', () => {
      const { result: hashboard } = renderHook(() => useComponentStatusSummary("hashboard", 1, 3));
      expect(hashboard.current.title).toBe("Hashboard 1 has multiple issues");

      const { result: psu } = renderHook(() => useComponentStatusSummary("psu", 2, 2));
      expect(psu.current.title).toBe("PSU 2 has multiple issues");

      const { result: fan } = renderHook(() => useComponentStatusSummary("fan", 3, 5));
      expect(fan.current.title).toBe("Fan 3 has multiple issues");
    });

    it('should return title="[Component] has multiple issues" without index', () => {
      const { result } = renderHook(() => useComponentStatusSummary("controlBoard", undefined, 2));
      expect(result.current.title).toBe("Control board has multiple issues");
    });
  });
});
