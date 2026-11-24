import { expect, test } from "@playwright/test";
import { BasePage } from "../pages/base";

test.use({ storageState: { cookies: [], origins: [] } });

test("finds the new toast when previous notifications disappear during inspection", async ({ page, isMobile }) => {
  await page.setContent(`
    <div id="notifications">
      <div data-testid="toast">Previous group created</div>
      <div id="expiring" data-testid="toast">Another group created</div>
    </div>
  `);
  await page.evaluate(() => {
    const expiring = document.getElementById("expiring")!;
    const measure = expiring.getBoundingClientRect.bind(expiring);
    expiring.getBoundingClientRect = () => {
      const rect = measure();
      queueMicrotask(() => {
        document.getElementById("notifications")!.innerHTML =
          '<div data-testid="toast">Group "new group" created</div>';
      });
      return rect;
    };
  });

  await new BasePage(page, isMobile).validateTextInToast('Group "new group" created', 2_000);
});

for (const testId of ["toast", "grouped-toaster-header", "toaster-container"]) {
  test(`waits for visible ${testId} text and normalizes whitespace`, async ({ page, isMobile }) => {
    await page.setContent(`<div data-testid="${testId}" hidden>Group\n  "new group" created</div>`);
    const base = new BasePage(page, isMobile);
    await expect(base.validateTextInToast('Group "new group" created', 200)).rejects.toThrow();
    await page.getByTestId(testId).evaluate((element) => {
      element.removeAttribute("hidden");
    });
    await base.validateTextInToast('Group "new group" created', 2_000);
  });
}
