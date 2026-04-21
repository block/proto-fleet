import { describe, expect, it } from "vitest";
import { Code, ConnectError } from "@connectrpc/connect";
import { isBackendDownError } from "./backendHealth";

describe("isBackendDownError", () => {
  it("returns true for Code.Unknown errors", () => {
    const error = new ConnectError("Server error", Code.Unknown);
    expect(isBackendDownError(error)).toBe(true);
  });

  it("returns true for Code.Internal errors", () => {
    const error = new ConnectError("Internal error", Code.Internal);
    expect(isBackendDownError(error)).toBe(true);
  });

  it("returns true for Code.Unavailable errors", () => {
    const error = new ConnectError("Service unavailable", Code.Unavailable);
    expect(isBackendDownError(error)).toBe(true);
  });

  it("returns false for other ConnectError codes", () => {
    const error = new ConnectError("Not found", Code.NotFound);
    expect(isBackendDownError(error)).toBe(false);
  });

  it("returns false for Code.Unauthenticated", () => {
    const error = new ConnectError("Unauthenticated", Code.Unauthenticated);
    expect(isBackendDownError(error)).toBe(false);
  });

  it("returns false for Code.PermissionDenied", () => {
    const error = new ConnectError("Permission denied", Code.PermissionDenied);
    expect(isBackendDownError(error)).toBe(false);
  });

  it("returns false for non-ConnectError errors", () => {
    const error = new Error("Regular error");
    expect(isBackendDownError(error)).toBe(false);
  });

  it("returns false for null", () => {
    expect(isBackendDownError(null)).toBe(false);
  });

  it("returns false for undefined", () => {
    expect(isBackendDownError(undefined)).toBe(false);
  });

  it("returns false for string errors", () => {
    expect(isBackendDownError("Error message")).toBe(false);
  });
});
