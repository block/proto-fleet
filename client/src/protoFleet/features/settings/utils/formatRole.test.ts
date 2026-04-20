import { describe, expect, it } from "vitest";
import { formatRole } from "./formatRole";

describe("formatRole", () => {
  it("should format SUPER_ADMIN as Owner", () => {
    expect(formatRole("SUPER_ADMIN")).toBe("Owner");
  });

  it("should format ADMIN as Admin", () => {
    expect(formatRole("ADMIN")).toBe("Admin");
  });

  it("should return unknown roles unchanged", () => {
    expect(formatRole("UNKNOWN_ROLE")).toBe("UNKNOWN_ROLE");
  });

  it("should handle empty string", () => {
    expect(formatRole("")).toBe("");
  });
});
