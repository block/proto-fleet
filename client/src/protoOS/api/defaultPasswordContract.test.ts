import { describe, expect, test } from "vitest";
import { isDefaultPasswordActiveError } from "./defaultPasswordContract";

describe("isDefaultPasswordActiveError", () => {
  test("matches structured JSON 403 with the firmware code", () => {
    expect(
      isDefaultPasswordActiveError({
        status: 403,
        error: { error: { code: "DEFAULT_PASSWORD_ACTIVE", message: "Default password must be changed" } },
      }),
    ).toBe(true);
  });

  test("matches structured JSON 403 with the firmware message only", () => {
    expect(
      isDefaultPasswordActiveError({
        status: 403,
        error: { error: { message: "default password must be changed before accessing this resource" } },
      }),
    ).toBe(true);
  });

  // Firmware can return a plain-text 403 body (e.g. "default password must be changed").
  // The generated client's r.json() then throws SyntaxError, which lands in error.error.
  // Without inspecting the SyntaxError's message, the matcher would silently miss the
  // marker and the app would never redirect to the password-change flow.
  test("matches plain-text 403 body via the SyntaxError thrown by JSON parsing", () => {
    const parseFailure = new SyntaxError(`Unexpected token 'd', "default password must be changed" is not valid JSON`);
    expect(
      isDefaultPasswordActiveError({
        status: 403,
        error: parseFailure as unknown as { error?: { code?: string; message?: string } },
      }),
    ).toBe(true);
  });

  test("matches plain-text 403 body that surfaces the firmware code instead of prose", () => {
    const parseFailure = new SyntaxError(`Unexpected token 'D', "DEFAULT_PASSWORD_ACTIVE" is not valid JSON`);
    expect(
      isDefaultPasswordActiveError({
        status: 403,
        error: parseFailure as unknown as { error?: { code?: string; message?: string } },
      }),
    ).toBe(true);
  });

  test("ignores 403s that are unrelated to the default-password gate", () => {
    expect(
      isDefaultPasswordActiveError({
        status: 403,
        error: { error: { code: "ACCESS_DENIED", message: "Access denied" } },
      }),
    ).toBe(false);
  });

  test("ignores non-403 errors", () => {
    expect(
      isDefaultPasswordActiveError({
        status: 401,
        error: { error: { code: "DEFAULT_PASSWORD_ACTIVE", message: "Default password must be changed" } },
      }),
    ).toBe(false);
  });
});
