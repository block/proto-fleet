import { describe, expect, it, vi } from "vitest";

import { encodeSortToURL, parseSortFromURL } from "./sortUrlParams";
import { SortDirection, SortField } from "@/protoFleet/api/generated/common/v1/sort_pb";

describe("sortUrlParams", () => {
  describe("parseSortFromURL", () => {
    it("returns undefined when no sort param is present", () => {
      // Act
      const result = parseSortFromURL(new URLSearchParams());

      // Assert
      expect(result).toBeUndefined();
    });

    it("parses hashrate with desc direction", () => {
      // Act
      const result = parseSortFromURL(new URLSearchParams("sort=hashrate&dir=desc"));

      // Assert
      expect(result).toEqual(
        expect.objectContaining({
          field: SortField.HASHRATE,
          direction: SortDirection.DESC,
        }),
      );
    });

    it("parses name with asc direction", () => {
      // Act
      const result = parseSortFromURL(new URLSearchParams("sort=name&dir=asc"));

      // Assert
      expect(result).toEqual(
        expect.objectContaining({
          field: SortField.NAME,
          direction: SortDirection.ASC,
        }),
      );
    });

    it("defaults to DESC when dir param is missing", () => {
      // Act
      const result = parseSortFromURL(new URLSearchParams("sort=hashrate"));

      // Assert
      expect(result).toEqual(
        expect.objectContaining({
          field: SortField.HASHRATE,
          direction: SortDirection.DESC,
        }),
      );
    });

    it("handles case-insensitive field names", () => {
      // Act
      const result = parseSortFromURL(new URLSearchParams("sort=HASHRATE&dir=desc"));

      // Assert
      expect(result).toEqual(
        expect.objectContaining({
          field: SortField.HASHRATE,
          direction: SortDirection.DESC,
        }),
      );
    });

    it("returns undefined and logs warning for unknown sort field", () => {
      // Arrange
      const consoleSpy = vi.spyOn(console, "warn").mockImplementation(() => {});

      // Act
      const result = parseSortFromURL(new URLSearchParams("sort=unknown&dir=asc"));

      // Assert
      expect(result).toBeUndefined();
      expect(consoleSpy).toHaveBeenCalledWith("Unknown sort field in URL: unknown");
      consoleSpy.mockRestore();
    });

    it("parses all supported sort fields", () => {
      const fieldMappings: Array<{ url: string; expected: SortField }> = [
        { url: "name", expected: SortField.NAME },
        { url: "worker-name", expected: SortField.WORKER_NAME },
        { url: "ip", expected: SortField.IP_ADDRESS },
        { url: "mac", expected: SortField.MAC_ADDRESS },
        { url: "model", expected: SortField.MODEL },
        { url: "hashrate", expected: SortField.HASHRATE },
        { url: "temp", expected: SortField.TEMPERATURE },
        { url: "power", expected: SortField.POWER },
        { url: "efficiency", expected: SortField.EFFICIENCY },
        { url: "firmware", expected: SortField.FIRMWARE },
      ];

      for (const { url, expected } of fieldMappings) {
        // Act
        const result = parseSortFromURL(new URLSearchParams(`sort=${url}&dir=asc`));

        // Assert
        expect(result?.field, `Failed for field: ${url}`).toBe(expected);
      }
    });
  });

  describe("encodeSortToURL", () => {
    it("removes sort params when sort is undefined", () => {
      // Arrange
      const params = new URLSearchParams("sort=hashrate&dir=desc");

      // Act
      encodeSortToURL(params, undefined);

      // Assert
      expect(params.has("sort")).toBe(false);
      expect(params.has("dir")).toBe(false);
    });

    it("encodes hashrate with desc direction", () => {
      // Arrange
      const params = new URLSearchParams();

      // Act
      encodeSortToURL(params, {
        field: SortField.HASHRATE,
        direction: SortDirection.DESC,
        $typeName: "common.v1.SortConfig",
      } as any);

      // Assert
      expect(params.get("sort")).toBe("hashrate");
      expect(params.get("dir")).toBe("desc");
    });

    it("encodes name with asc direction", () => {
      // Arrange
      const params = new URLSearchParams();

      // Act
      encodeSortToURL(params, {
        field: SortField.NAME,
        direction: SortDirection.ASC,
        $typeName: "common.v1.SortConfig",
      } as any);

      // Assert
      expect(params.get("sort")).toBe("name");
      expect(params.get("dir")).toBe("asc");
    });

    it("preserves existing filter params", () => {
      // Arrange
      const params = new URLSearchParams("status=hashing,offline");

      // Act
      encodeSortToURL(params, {
        field: SortField.HASHRATE,
        direction: SortDirection.DESC,
        $typeName: "common.v1.SortConfig",
      } as any);

      // Assert
      expect(params.get("status")).toBe("hashing,offline");
      expect(params.get("sort")).toBe("hashrate");
      expect(params.get("dir")).toBe("desc");
    });

    it("encodes all supported sort fields", () => {
      const fieldMappings: Array<{ field: SortField; expected: string }> = [
        { field: SortField.NAME, expected: "name" },
        { field: SortField.WORKER_NAME, expected: "worker-name" },
        { field: SortField.IP_ADDRESS, expected: "ip" },
        { field: SortField.MAC_ADDRESS, expected: "mac" },
        { field: SortField.MODEL, expected: "model" },
        { field: SortField.HASHRATE, expected: "hashrate" },
        { field: SortField.TEMPERATURE, expected: "temp" },
        { field: SortField.POWER, expected: "power" },
        { field: SortField.EFFICIENCY, expected: "efficiency" },
        { field: SortField.FIRMWARE, expected: "firmware" },
      ];

      for (const { field, expected } of fieldMappings) {
        // Arrange
        const params = new URLSearchParams();

        // Act
        encodeSortToURL(params, {
          field,
          direction: SortDirection.ASC,
          $typeName: "common.v1.SortConfig",
        } as any);

        // Assert
        expect(params.get("sort"), `Failed for field: ${field}`).toBe(expected);
      }
    });
  });

  describe("round-trip", () => {
    it("maintains sort config through encode-decode cycle", () => {
      // Arrange
      const original = parseSortFromURL(new URLSearchParams("sort=efficiency&dir=desc"));

      // Act
      const params = new URLSearchParams();
      encodeSortToURL(params, original);
      const decoded = parseSortFromURL(params);

      // Assert
      expect(decoded?.field).toBe(SortField.EFFICIENCY);
      expect(decoded?.direction).toBe(SortDirection.DESC);
    });
  });
});
