import { expect, type Page, type Response } from "@playwright/test";

export async function submitWithPasswordAttemptRetry(page: Page, endpoint: RegExp, submit: () => Promise<void>) {
  let response: Response;

  // Setup and miner actions share the admin's ten-attempt, one-minute budget.
  // Retry through the UI only while the server reports that specific limit.
  await expect
    .poll(
      async () => {
        const responsePromise = page.waitForResponse(
          (candidate) => endpoint.test(candidate.url()) && candidate.request().method() === "POST",
        );
        await submit();
        response = await responsePromise;
        if (response.status() !== 429) return false;
        const error = await response.json();
        return (
          error.code === "resource_exhausted" &&
          error.message === "Too many password attempts. Try again in one minute."
        );
      },
      { timeout: 70_000, intervals: [5_000] },
    )
    .toBe(false);

  return response!;
}
