import { test } from "@playwright/test";
import { RacksPage } from "../pages/racks";

// These fixtures exercise the real rack page-object assertions without using
// backend state. Match the simple, expanded-group, and collapsed-group markup.
test.use({ storageState: { cookies: [], origins: [] } });

const label = "Toast fixture";
const validations = [
  {
    name: "rack saved",
    message: `Rack "${label}" saved`,
    validate: (racks: RacksPage) => racks.validateRackToast(label, "saved"),
  },
  {
    name: "miner positions saved",
    message: `Miner positions saved for "${label}"`,
    validate: (racks: RacksPage) => racks.validateMinerPositionsToast(label),
  },
] as const;

for (const validation of validations) {
  for (const rendering of ["simple", "expanded group", "collapsed group"] as const) {
    test(`${validation.name} waits for a visible ${rendering} notification`, async ({ page, isMobile }) => {
      await page.setContent(`
        <div data-testid="toast" hidden>${validation.message}</div>
        <div data-testid="toaster-container">
          <div data-testid="grouped-toaster-header">Different rack saved</div>
        </div>
        <div id="notification"></div>
      `);

      // A hidden matching toast and an unrelated visible header must not satisfy
      // the assertion while the requested save notification is still pending.
      await page.evaluate(
        ({ message, rendering }) => {
          setTimeout(() => {
            const target = document.getElementById("notification")!;
            if (rendering === "simple") {
              target.innerHTML = '<div data-testid="toast"></div>';
            } else if (rendering === "expanded group") {
              target.innerHTML = '<div data-testid="toaster-container"><div data-testid="toast"></div></div>';
            } else {
              target.innerHTML =
                '<div data-testid="toaster-container"><div data-testid="grouped-toaster-header"></div></div>';
            }
            target.querySelector('[data-testid="toast"], [data-testid="grouped-toaster-header"]')!.textContent =
              message;
          }, 150);
        },
        { message: validation.message, rendering },
      );

      await validation.validate(new RacksPage(page, isMobile));
    });
  }
}
