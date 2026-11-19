import { renderHook } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { GroupedStatusErrors } from "./types";
import { useComponentStatusSummary, useMinerIssues, useMinerStatus, useMinerStatusSummary } from "./useStatusSummary";
import { getComponentDisplayName, getComponentSingularName } from "./utils";

const emptyErrors: GroupedStatusErrors = {
  hashboard: [],
  psu: [],
  fan: [],
  controlBoard: [],
  other: [],
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
      expect(result.current.title).toBe("Miner is offline");
      expect(result.current.subtitle).toBeUndefined();
    });

    it('should return condensed="Needs Authentication" when needsAuthentication is true', () => {
      const { result } = renderHook(() => useMinerStatusSummary(emptyErrors, false, false, true));
      expect(result.current.condensed).toBe("Needs Authentication");
      expect(result.current.title).toBe("Authentication required");
    });

    it('should return condensed="Needs mining pool" when needsMiningPool is true', () => {
      const { result } = renderHook(() => useMinerStatusSummary(emptyErrors, false, false, false, true));
      expect(result.current.condensed).toBe("Needs mining pool");
      expect(result.current.title).toBe("Mining pool required");
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
      expect(result.current.title).toBe("Hashboard 1 failure");
    });

    it('should return condensed="Offline" and show single error in subtitle when offline', () => {
      const errors: GroupedStatusErrors = {
        ...emptyErrors,
        hashboard: [{ componentType: "hashboard", slot: 1 }],
      };
      const { result } = renderHook(() => useMinerStatusSummary(errors, false, true));
      expect(result.current.condensed).toBe("Offline");
      expect(result.current.title).toBe("Miner is offline");
      expect(result.current.subtitle).toBe("Hashboard 1 failure");
    });

    it('should return condensed="Offline" and show multiple errors in subtitle when offline', () => {
      const errors: GroupedStatusErrors = {
        ...emptyErrors,
        hashboard: [{ componentType: "hashboard", slot: 1 }],
        psu: [{ componentType: "psu", slot: 1 }],
      };
      const { result } = renderHook(() => useMinerStatusSummary(errors, false, true));
      expect(result.current.condensed).toBe("Offline");
      expect(result.current.title).toBe("Miner is offline");
      expect(result.current.subtitle).toBe("Multiple failures");
    });
  });

  describe("single error", () => {
    it.each<{ name: string; errors: Partial<GroupedStatusErrors>; expected: string }>([
      {
        name: "hashboard with slot",
        errors: { hashboard: [{ componentType: "hashboard", slot: 1 }] },
        expected: "Hashboard 1 failure",
      },
      { name: "PSU with slot", errors: { psu: [{ componentType: "psu", slot: 2 }] }, expected: "PSU 2 failure" },
      { name: "fan with slot", errors: { fan: [{ componentType: "fan", slot: 2 }] }, expected: "Fan 2 failure" },
      {
        name: "control board without index",
        errors: { controlBoard: [{ componentType: "controlBoard" }] },
        expected: "Control board failure",
      },
      {
        name: "hashboard without slot",
        errors: { hashboard: [{ componentType: "hashboard" }] },
        expected: "Hashboard failure",
      },
    ])('should return "$expected" for $name', ({ errors, expected }) => {
      const { result } = renderHook(() => useMinerStatusSummary({ ...emptyErrors, ...errors }));
      expect(result.current.condensed).toBe(expected);
      expect(result.current.title).toBe(expected);
    });
  });

  describe("multiple errors on one component type", () => {
    it.each<{ errors: Partial<GroupedStatusErrors>; expected: string }>([
      {
        errors: {
          hashboard: [
            { componentType: "hashboard", slot: 1 },
            { componentType: "hashboard", slot: 2 },
          ],
        },
        expected: "Multiple hashboard failures",
      },
      {
        errors: {
          psu: [
            { componentType: "psu", slot: 1 },
            { componentType: "psu", slot: 1 },
          ],
        },
        expected: "Multiple PSU failures",
      },
      {
        errors: { fan: [{ componentType: "fan" }, { componentType: "fan" }, { componentType: "fan" }] },
        expected: "Multiple fan failures",
      },
      {
        errors: { controlBoard: [{ componentType: "controlBoard" }, { componentType: "controlBoard" }] },
        expected: "Multiple control board failures",
      },
    ])('should return "$expected"', ({ errors, expected }) => {
      const { result } = renderHook(() => useMinerStatusSummary({ ...emptyErrors, ...errors }));
      expect(result.current.condensed).toBe(expected);
      expect(result.current.title).toBe(expected);
    });
  });

  describe("multiple component types with errors", () => {
    it('should return "Multiple failures" when hashboard and PSU have errors', () => {
      const errors: GroupedStatusErrors = {
        ...emptyErrors,
        hashboard: [{ componentType: "hashboard", slot: 1 }],
        psu: [{ componentType: "psu", slot: 1 }],
      };
      const { result } = renderHook(() => useMinerStatusSummary(errors));
      expect(result.current.condensed).toBe("Multiple failures");
      expect(result.current.title).toBe("Multiple failures");
    });

    it('should return "Multiple failures" when all component types have errors', () => {
      const errors: GroupedStatusErrors = {
        hashboard: [{ componentType: "hashboard", slot: 1 }],
        psu: [{ componentType: "psu", slot: 1 }],
        fan: [{ componentType: "fan", slot: 1 }],
        controlBoard: [{ componentType: "controlBoard" }],
        other: [],
      };
      const { result } = renderHook(() => useMinerStatusSummary(errors));
      expect(result.current.condensed).toBe("Multiple failures");
      expect(result.current.title).toBe("Multiple failures");
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
    it('should return title="[Component] [slot] has multiple failures" with slot', () => {
      const { result: hashboard } = renderHook(() => useComponentStatusSummary("hashboard", 1, 3));
      expect(hashboard.current.title).toBe("Hashboard 1 has multiple failures");

      const { result: psu } = renderHook(() => useComponentStatusSummary("psu", 2, 2));
      expect(psu.current.title).toBe("PSU 2 has multiple failures");

      const { result: fan } = renderHook(() => useComponentStatusSummary("fan", 3, 5));
      expect(fan.current.title).toBe("Fan 3 has multiple failures");
    });

    it('should return title="[Component] has multiple failures" without index', () => {
      const { result } = renderHook(() => useComponentStatusSummary("controlBoard", undefined, 2));
      expect(result.current.title).toBe("Control board has multiple failures");
    });
  });
});

describe("useMinerStatus", () => {
  describe("priority order", () => {
    it('should return "Offline" when isOffline is true (highest priority)', () => {
      const { result } = renderHook(() => useMinerStatus(true, false, false));
      expect(result.current).toBe("Offline");
    });

    it('should return "Offline" even when sleeping and needs attention are also true', () => {
      const { result } = renderHook(() => useMinerStatus(true, true, true));
      expect(result.current).toBe("Offline");
    });

    it('should return "Sleeping" when not offline but is sleeping', () => {
      const { result } = renderHook(() => useMinerStatus(false, true, false));
      expect(result.current).toBe("Sleeping");
    });

    it('should return "Sleeping" over "Needs attention" when both are true', () => {
      const { result } = renderHook(() => useMinerStatus(false, true, true));
      expect(result.current).toBe("Sleeping");
    });

    it('should return "Needs attention" when not offline or sleeping but needs attention', () => {
      const { result } = renderHook(() => useMinerStatus(false, false, true));
      expect(result.current).toBe("Needs attention");
    });

    it('should return "Hashing" when no flags are true (default)', () => {
      const { result } = renderHook(() => useMinerStatus(false, false, false));
      expect(result.current).toBe("Hashing");
    });
  });

  describe("all combinations", () => {
    it('should handle offline + sleeping → "Offline"', () => {
      const { result } = renderHook(() => useMinerStatus(true, true, false));
      expect(result.current).toBe("Offline");
    });

    it('should handle offline + needs attention → "Offline"', () => {
      const { result } = renderHook(() => useMinerStatus(true, false, true));
      expect(result.current).toBe("Offline");
    });
  });
});

describe("useMinerIssues", () => {
  describe("no issues", () => {
    it("should return no issues when all flags are false and no errors", () => {
      const { result } = renderHook(() => useMinerIssues(false, false, emptyErrors));
      expect(result.current.hasIssues).toBe(false);
      expect(result.current.summary).toBeNull();
    });
  });

  describe("authentication priority", () => {
    it('should return "Authentication required" when needsAuthentication is true', () => {
      const { result } = renderHook(() => useMinerIssues(true, false, emptyErrors));
      expect(result.current.hasIssues).toBe(true);
      expect(result.current.summary).toBe("Authentication required");
    });

    it("should prioritize auth over pool when both are true", () => {
      const { result } = renderHook(() => useMinerIssues(true, true, emptyErrors));
      expect(result.current.hasIssues).toBe(true);
      expect(result.current.summary).toBe("Authentication required");
    });

    it("should prioritize auth over hardware errors", () => {
      const errors: GroupedStatusErrors = {
        ...emptyErrors,
        hashboard: [{ componentType: "hashboard", slot: 1 }],
      };
      const { result } = renderHook(() => useMinerIssues(true, false, errors));
      expect(result.current.hasIssues).toBe(true);
      expect(result.current.summary).toBe("Authentication required");
    });

    it("should prioritize auth over pool + hardware errors", () => {
      const errors: GroupedStatusErrors = {
        ...emptyErrors,
        hashboard: [{ componentType: "hashboard", slot: 1 }],
        psu: [{ componentType: "psu", slot: 1 }],
      };
      const { result } = renderHook(() => useMinerIssues(true, true, errors));
      expect(result.current.hasIssues).toBe(true);
      expect(result.current.summary).toBe("Authentication required");
    });
  });

  describe("pool priority", () => {
    it('should return "Pool required" when needsMiningPool is true and no auth needed', () => {
      const { result } = renderHook(() => useMinerIssues(false, true, emptyErrors));
      expect(result.current.hasIssues).toBe(true);
      expect(result.current.summary).toBe("Pool required");
    });

    it("should prioritize pool over hardware errors", () => {
      const errors: GroupedStatusErrors = {
        ...emptyErrors,
        hashboard: [{ componentType: "hashboard", slot: 1 }],
      };
      const { result } = renderHook(() => useMinerIssues(false, true, errors));
      expect(result.current.hasIssues).toBe(true);
      expect(result.current.summary).toBe("Pool required");
    });

    it("should prioritize pool over multiple component failures", () => {
      const errors: GroupedStatusErrors = {
        ...emptyErrors,
        hashboard: [{ componentType: "hashboard", slot: 1 }],
        psu: [{ componentType: "psu", slot: 1 }],
      };
      const { result } = renderHook(() => useMinerIssues(false, true, errors));
      expect(result.current.hasIssues).toBe(true);
      expect(result.current.summary).toBe("Pool required");
    });
  });

  describe("firmware status", () => {
    it('should return "Updating firmware" when isUpdating is true', () => {
      const { result } = renderHook(() => useMinerIssues(false, false, emptyErrors, true, false));
      expect(result.current.hasIssues).toBe(true);
      expect(result.current.summary).toBe("Updating firmware");
    });

    it('should return "Reboot required" when isRebootRequired is true', () => {
      const { result } = renderHook(() => useMinerIssues(false, false, emptyErrors, false, true));
      expect(result.current.hasIssues).toBe(true);
      expect(result.current.summary).toBe("Reboot required");
    });

    it("should prioritize auth over firmware status", () => {
      const { result } = renderHook(() => useMinerIssues(true, false, emptyErrors, true, false));
      expect(result.current.hasIssues).toBe(true);
      expect(result.current.summary).toBe("Authentication required");
    });

    it("should prioritize pool over firmware status", () => {
      const { result } = renderHook(() => useMinerIssues(false, true, emptyErrors, false, true));
      expect(result.current.hasIssues).toBe(true);
      expect(result.current.summary).toBe("Pool required");
    });

    it("should prioritize updating over reboot required", () => {
      const { result } = renderHook(() => useMinerIssues(false, false, emptyErrors, true, true));
      expect(result.current.hasIssues).toBe(true);
      expect(result.current.summary).toBe("Updating firmware");
    });

    it("should prioritize firmware status over hardware errors", () => {
      const errors: GroupedStatusErrors = {
        ...emptyErrors,
        hashboard: [{ componentType: "hashboard", slot: 1 }],
      };
      const { result } = renderHook(() => useMinerIssues(false, false, errors, false, true));
      expect(result.current.hasIssues).toBe(true);
      expect(result.current.summary).toBe("Reboot required");
    });
  });

  describe("hardware errors only", () => {
    it("should return specific component failure for single error", () => {
      const errors: GroupedStatusErrors = {
        ...emptyErrors,
        hashboard: [{ componentType: "hashboard", slot: 1 }],
      };
      const { result } = renderHook(() => useMinerIssues(false, false, errors));
      expect(result.current.hasIssues).toBe(true);
      expect(result.current.summary).toBe("Hashboard 1 failure");
    });

    it('should return "Multiple [component] failures" for multiple errors on same component', () => {
      const errors: GroupedStatusErrors = {
        ...emptyErrors,
        hashboard: [
          { componentType: "hashboard", slot: 1 },
          { componentType: "hashboard", slot: 2 },
        ],
      };
      const { result } = renderHook(() => useMinerIssues(false, false, errors));
      expect(result.current.hasIssues).toBe(true);
      expect(result.current.summary).toBe("Multiple hashboard failures");
    });

    it('should return "Multiple failures" for errors on multiple component types', () => {
      const errors: GroupedStatusErrors = {
        ...emptyErrors,
        hashboard: [{ componentType: "hashboard", slot: 1 }],
        psu: [{ componentType: "psu", slot: 1 }],
      };
      const { result } = renderHook(() => useMinerIssues(false, false, errors));
      expect(result.current.hasIssues).toBe(true);
      expect(result.current.summary).toBe("Multiple failures");
    });

    it.each<{ name: string; errors: Partial<GroupedStatusErrors>; expected: string }>([
      { name: "PSU without slot", errors: { psu: [{ componentType: "psu" }] }, expected: "PSU failure" },
      { name: "fan with slot", errors: { fan: [{ componentType: "fan", slot: 3 }] }, expected: "Fan 3 failure" },
      {
        name: "control board",
        errors: { controlBoard: [{ componentType: "controlBoard" }] },
        expected: "Control board failure",
      },
    ])('should return "$expected" for $name', ({ errors, expected }) => {
      const { result } = renderHook(() => useMinerIssues(false, false, { ...emptyErrors, ...errors }));
      expect(result.current.hasIssues).toBe(true);
      expect(result.current.summary).toBe(expected);
    });
  });
});
