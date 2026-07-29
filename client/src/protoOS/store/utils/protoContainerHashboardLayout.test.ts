import { describe, expect, it } from "vitest";
import {
  getProtoContainerAsicLabel,
  getProtoContainerRowLabel,
  PROTO_CONTAINER_HASHBOARD_COLUMNS,
  PROTO_CONTAINER_HASHBOARD_ROW_LETTERS,
  PROTO_CONTAINER_HASHBOARD_ROW_ORDER,
  PROTO_CONTAINER_HASHBOARD_ROWS,
} from "./protoContainerHashboardLayout";

describe("protoContainerHashboardLayout", () => {
  describe("constants", () => {
    it("has 12 rows (6 letter pairs x 2 sub-rows)", () => {
      expect(PROTO_CONTAINER_HASHBOARD_ROWS).toBe(12);
      expect(PROTO_CONTAINER_HASHBOARD_ROW_LETTERS).toEqual(["F", "E", "D", "C", "B", "A"]);
    });

    it("orders display rows F0,F1,E0,E1...A0,A1 top to bottom", () => {
      expect(PROTO_CONTAINER_HASHBOARD_ROW_ORDER).toEqual([
        "F0",
        "F1",
        "E0",
        "E1",
        "D0",
        "D1",
        "C0",
        "C1",
        "B0",
        "B1",
        "A0",
        "A1",
      ]);
    });
  });

  describe("getProtoContainerRowLabel", () => {
    it("labels each display row in F0..A1 order", () => {
      const labels = Array.from({ length: PROTO_CONTAINER_HASHBOARD_ROWS }, (_, row) => getProtoContainerRowLabel(row));
      expect(labels).toEqual(PROTO_CONTAINER_HASHBOARD_ROW_ORDER);
    });

    it("returns the raw index for out-of-range rows", () => {
      expect(getProtoContainerRowLabel(-1)).toBe("-1");
      expect(getProtoContainerRowLabel(12)).toBe("12");
      expect(getProtoContainerRowLabel(1.5)).toBe("1.5");
    });
  });

  describe("getProtoContainerAsicLabel (28 columns)", () => {
    const N = 28;

    it("numbers the top sub-row left-to-right 1..N", () => {
      expect(getProtoContainerAsicLabel(0, 0, N)).toBe("F1");
      expect(getProtoContainerAsicLabel(0, 1, N)).toBe("F2");
      expect(getProtoContainerAsicLabel(0, N - 1, N)).toBe("F28");
    });

    it("numbers the bottom sub-row serpentine: left=2N down to right=N+1", () => {
      expect(getProtoContainerAsicLabel(1, 0, N)).toBe("F56");
      expect(getProtoContainerAsicLabel(1, 1, N)).toBe("F55");
      expect(getProtoContainerAsicLabel(1, N - 1, N)).toBe("F29");
    });

    it("resets numbering per letter pair", () => {
      // Second pair is "E" (rows 2 and 3).
      expect(getProtoContainerAsicLabel(2, 0, N)).toBe("E1");
      expect(getProtoContainerAsicLabel(2, N - 1, N)).toBe("E28");
      expect(getProtoContainerAsicLabel(3, 0, N)).toBe("E56");
      expect(getProtoContainerAsicLabel(3, N - 1, N)).toBe("E29");
    });

    it("labels the last pair (A rows) correctly", () => {
      expect(getProtoContainerAsicLabel(10, 0, N)).toBe("A1");
      expect(getProtoContainerAsicLabel(11, 0, N)).toBe("A56");
      expect(getProtoContainerAsicLabel(11, N - 1, N)).toBe("A29");
    });

    it("covers all 336 positions with unique labels for a 12x28 grid", () => {
      const labels = new Set<string>();
      for (let row = 0; row < PROTO_CONTAINER_HASHBOARD_ROWS; row++) {
        for (let col = 0; col < N; col++) {
          labels.add(getProtoContainerAsicLabel(row, col, N));
        }
      }
      expect(labels.size).toBe(PROTO_CONTAINER_HASHBOARD_ROWS * N);
      expect(labels.size).toBe(336);
    });
  });

  describe("getProtoContainerAsicLabel (26 columns)", () => {
    const N = 26;

    it("adjusts the serpentine max to 2N per letter pair", () => {
      expect(getProtoContainerAsicLabel(0, 0, N)).toBe("F1");
      expect(getProtoContainerAsicLabel(0, N - 1, N)).toBe("F26");
      expect(getProtoContainerAsicLabel(1, 0, N)).toBe("F52");
      expect(getProtoContainerAsicLabel(1, N - 1, N)).toBe("F27");
    });
  });

  describe("getProtoContainerAsicLabel defaults & bounds", () => {
    it("defaults to PROTO_CONTAINER_HASHBOARD_COLUMNS when columns omitted", () => {
      expect(getProtoContainerAsicLabel(1, 0)).toBe(`F${2 * PROTO_CONTAINER_HASHBOARD_COLUMNS}`);
    });

    it("returns empty string for out-of-range positions", () => {
      expect(getProtoContainerAsicLabel(-1, 0)).toBe("");
      expect(getProtoContainerAsicLabel(0, -1)).toBe("");
      expect(getProtoContainerAsicLabel(PROTO_CONTAINER_HASHBOARD_ROWS, 0)).toBe("");
      expect(getProtoContainerAsicLabel(0, PROTO_CONTAINER_HASHBOARD_COLUMNS)).toBe("");
      expect(getProtoContainerAsicLabel(0.5, 0)).toBe("");
    });
  });
});
