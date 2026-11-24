// @vitest-environment node
import { describe, expect, it } from "vitest";
import { Code, ConnectError } from "@connectrpc/connect";
import { getErrorMessage } from "./getErrorMessage";

describe("getErrorMessage", () => {
  describe("ConnectError prefix stripping", () => {
    it("strips [internal] prefix", () => {
      const err = new ConnectError("Log bundle too large to download!", Code.Internal);
      expect(err.message).toContain("[internal]");
      expect(getErrorMessage(err)).toBe("Log bundle too large to download!");
    });
  });

  describe("fallback behavior", () => {
    it("returns fallback when ConnectError has an empty message", () => {
      const err = new ConnectError("", Code.Internal);
      expect(getErrorMessage(err, "Something went wrong")).toBe("Something went wrong");
    });

    it("returns empty string when ConnectError has an empty message and no fallback", () => {
      const err = new ConnectError("", Code.Internal);
      expect(getErrorMessage(err)).toBe("");
    });

    it("prefers rawMessage over fallback when both are present", () => {
      const err = new ConnectError("specific error", Code.Internal);
      expect(getErrorMessage(err, "generic fallback")).toBe("specific error");
    });
  });

  describe("non-ConnectError inputs", () => {
    it("extracts message from a plain Error", () => {
      const err = new Error("something broke");
      expect(getErrorMessage(err)).toBe("something broke");
    });

    it("converts a string input to message", () => {
      expect(getErrorMessage("raw string error")).toBe("raw string error");
    });

    it("handles null without crashing", () => {
      expect(getErrorMessage(null)).toBe("null");
    });

    it("handles undefined without crashing", () => {
      expect(getErrorMessage(undefined)).toBe("undefined");
    });

    it("handles a number without crashing", () => {
      expect(getErrorMessage(42)).toBe("42");
    });

    it("uses fallback for non-Error inputs with empty string conversion", () => {
      expect(getErrorMessage("", "default message")).toBe("default message");
    });
  });
});
