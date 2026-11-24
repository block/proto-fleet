import { describe, expect, it } from "vitest";
import {
  CONTAINER_HASHBOARD_COLUMNS,
  CONTAINER_HASHBOARD_ROW_LETTERS,
  CONTAINER_HASHBOARD_ROWS,
  getContainerAsicLabel,
} from "./containerHashboardLayout";

describe("containerHashboardLayout", () => {
  describe("constants", () => {
    it("has 12 rows (6 letter pairs x 2 sub-rows)", () => {
      expect(CONTAINER_HASHBOARD_ROWS).toBe(12);
      expect(CONTAINER_HASHBOARD_ROW_LETTERS).toEqual(["F", "E", "D", "C", "B", "A"]);
    });
  });

  describe("getContainerAsicLabel (28 columns)", () => {
    const N = 28;

    it("numbers the top sub-row left-to-right 1..N", () => {
      expect(getContainerAsicLabel(0, 0, N)).toBe("F1");
      expect(getContainerAsicLabel(0, 1, N)).toBe("F2");
      expect(getContainerAsicLabel(0, N - 1, N)).toBe("F28");
    });

    it("numbers the bottom sub-row serpentine: left=2N down to right=N+1", () => {
      expect(getContainerAsicLabel(1, 0, N)).toBe("F56");
      expect(getContainerAsicLabel(1, 1, N)).toBe("F55");
      expect(getContainerAsicLabel(1, N - 1, N)).toBe("F29");
    });

    it("resets numbering per letter pair", () => {
      // Second pair is "E" (rows 2 and 3).
      expect(getContainerAsicLabel(2, 0, N)).toBe("E1");
      expect(getContainerAsicLabel(2, N - 1, N)).toBe("E28");
      expect(getContainerAsicLabel(3, 0, N)).toBe("E56");
      expect(getContainerAsicLabel(3, N - 1, N)).toBe("E29");
    });

    it("labels the last pair (A rows) correctly", () => {
      expect(getContainerAsicLabel(10, 0, N)).toBe("A1");
      expect(getContainerAsicLabel(11, 0, N)).toBe("A56");
      expect(getContainerAsicLabel(11, N - 1, N)).toBe("A29");
    });

    it("covers all 336 positions with unique labels for a 12x28 grid", () => {
      const labels = new Set<string>();
      for (let row = 0; row < CONTAINER_HASHBOARD_ROWS; row++) {
        for (let col = 0; col < N; col++) {
          labels.add(getContainerAsicLabel(row, col, N));
        }
      }
      expect(labels.size).toBe(CONTAINER_HASHBOARD_ROWS * N);
      expect(labels.size).toBe(336);
    });
  });

  describe("getContainerAsicLabel (26 columns)", () => {
    const N = 26;

    it("adjusts the serpentine max to 2N per letter pair", () => {
      expect(getContainerAsicLabel(0, 0, N)).toBe("F1");
      expect(getContainerAsicLabel(0, N - 1, N)).toBe("F26");
      expect(getContainerAsicLabel(1, 0, N)).toBe("F52");
      expect(getContainerAsicLabel(1, N - 1, N)).toBe("F27");
    });
  });

  describe("getContainerAsicLabel defaults & bounds", () => {
    it("defaults to CONTAINER_HASHBOARD_COLUMNS when columns omitted", () => {
      expect(getContainerAsicLabel(1, 0)).toBe(`F${2 * CONTAINER_HASHBOARD_COLUMNS}`);
    });

    it("returns empty string for out-of-range positions", () => {
      expect(getContainerAsicLabel(-1, 0)).toBe("");
      expect(getContainerAsicLabel(0, -1)).toBe("");
      expect(getContainerAsicLabel(CONTAINER_HASHBOARD_ROWS, 0)).toBe("");
      expect(getContainerAsicLabel(0, CONTAINER_HASHBOARD_COLUMNS)).toBe("");
      expect(getContainerAsicLabel(0.5, 0)).toBe("");
    });
  });
});
