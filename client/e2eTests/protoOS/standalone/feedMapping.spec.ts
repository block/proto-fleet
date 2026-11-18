import { expect, test } from "@playwright/test";
import { fileURLToPath } from "node:url";
import { FeedMappingPage } from "../pages/feedMapping";

test("unlocated hashboards can be remapped with confirmation, reload recovery, and automatic success cleanup", async ({
  page,
  context,
}, testInfo) => {
  // Localhost is a secure context. Remove this API to exercise the LAN HTTP path.
  await context.addInitScript(() => {
    Object.defineProperty(Crypto.prototype, "randomUUID", { value: undefined, configurable: true });
  });
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  page.on("response", (response) => {
    if (response.status() >= 500) errors.push(`${response.status()} ${response.url()}`);
  });
  let starts = 0;
  page.on("request", (request) => {
    if (request.url().endsWith("/mdk.v2.FeedMappingService/StartFeedMappingRun")) starts++;
  });
  const mapping = new FeedMappingPage(page);
  // Each viewport owns an unmapped simulator. No RPC responses are intercepted.
  await page.goto(`http://127.0.0.1:${testInfo.project.name === "desktop" ? 18083 : 18084}`);
  await expect(page.getByText("12 hashboards have no assigned location.", { exact: true })).toBeVisible();
  await page.screenshot({
    path: fileURLToPath(new URL(`../../../../.context/mapping-${testInfo.project.name}.png`, import.meta.url)),
  });
  await mapping.review();
  await expect(mapping.dialog()).toContainText("changes power usage and reduces hashrate");
  await expect(mapping.dialog()).toContainText("Some actions will not be available until mapping is complete");
  await mapping.dialog().screenshot({
    path: fileURLToPath(new URL(`../../../../.context/mapping-dialog-${testInfo.project.name}.png`, import.meta.url)),
    animations: "disabled",
  });
  expect(starts).toBe(0);
  await mapping.start();
  await expect(page.getByText("Hashboard mapping is running", { exact: true })).toBeVisible();
  await expect(mapping.dialog().getByTestId("mapping-progress-spinner")).toBeVisible();
  await expect(mapping.dialog()).toContainText("Waiting for hashboards to come online…");
  expect(starts).toBe(1);
  await mapping.close();
  await expect(mapping.poolConnectionWarning()).toHaveCount(0);
  await page.reload();
  await mapping.progress();
  const otherTab = await context.newPage();
  await otherTab.goto(page.url());
  await expect(otherTab.getByText("Hashboard mapping is running", { exact: true })).toBeVisible();
  await expect(mapping.poolConnectionWarning()).toHaveCount(0);
  await expect(mapping.dialog().getByRole("button", { name: "Cancel mapping" })).toBeVisible();
  await expect(mapping.dialog().getByRole("button", { name: "Refresh mapping", exact: true })).toHaveCount(0);
  await mapping.dialog().screenshot({
    path: fileURLToPath(new URL(`../../../../.context/mapping-progress-${testInfo.project.name}.png`, import.meta.url)),
    animations: "disabled",
  });
  await expect(mapping.dialog().getByText("Mapping complete", { exact: true })).toBeVisible({ timeout: 35_000 });
  await expect(mapping.dialog().getByTestId("mapping-progress-spinner")).toHaveCount(0);
  await expect(page.getByText("12 hashboards have no assigned location.", { exact: true })).toHaveCount(0);
  // Whichever tab confirms success first emits the toast and clears the shared recovery record.
  await expect
    .poll(
      async () =>
        (await page.getByText("Hashboard mapping complete", { exact: true }).count()) +
        (await otherTab.getByText("Hashboard mapping complete", { exact: true }).count()),
    )
    .toBeGreaterThan(0);
  await mapping.close();
  await expect(otherTab.getByText("Hashboard mapping finished", { exact: true })).toHaveCount(0);
  await expect(otherTab.getByRole("button", { name: "Refresh mapping", exact: true })).toHaveCount(0);
  await expect(page.getByText("Hashboard mapping complete", { exact: true })).toHaveCount(0);
  await expect(otherTab.getByText("Hashboard mapping complete", { exact: true })).toHaveCount(0);
  await page.reload();
  await otherTab.close();
  await expect(page.getByText("Hashboard mapping needs attention", { exact: true })).toHaveCount(0);
  await expect(page.getByText("Hashboard mapping finished", { exact: true })).toHaveCount(0);
  expect(starts).toBe(1);
  expect(errors).toEqual([]);
});
