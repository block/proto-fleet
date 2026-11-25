import { describe, expect, test } from "vitest";
import { Code } from "@connectrpc/connect";
import { containerWriteError } from "./writeErrors";

describe("containerWriteError", () => {
  test.each([
    { status: 401 },
    { error: { code: Code.Unauthenticated } },
    { status: 401, error: { code: Code.FailedPrecondition } },
    { status: 403, error: { code: Code.Unauthenticated } },
  ])("preserves authentication failure %j for the caller", (failure) => {
    expect(containerWriteError(failure, "Nothing changed.", "The outcome is unknown.")).toBe(failure);
  });

  test("keeps a permission refusal distinct from an authentication failure", () => {
    const failure = { status: 403, error: { code: Code.PermissionDenied } };

    expect(
      containerWriteError(failure, "Nothing changed.", "The outcome is unknown.", new Set([Code.PermissionDenied])),
    ).toMatchObject({
      cause: failure,
      writeOutcome: "refused",
    });
  });
});
