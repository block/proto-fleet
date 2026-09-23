import { expect, type Page } from "@playwright/test";

export class NavigationPage {
  constructor(private readonly page: Page) {}

  async validateResponsiveNavigation() {
    const originalViewport = this.page.viewportSize();
    const navigation = this.page.getByRole("navigation", { name: "Main" });
    const trigger = this.page.getByRole("button", { name: "Open navigation menu", exact: true });
    const dialog = this.page.getByRole("dialog", { name: "Navigation menu" });

    try {
      for (const viewport of [
        { width: 768, height: 1024 },
        { width: 1024, height: 768 },
      ]) {
        await this.page.setViewportSize(viewport);
        await this.page.mouse.move(viewport.width - 1, 0);
        await trigger.focus();
        await expect(navigation).toHaveCSS("width", "64px");
        await expect(this.page.getByTestId("app-content")).toHaveCSS("left", "64px");
        await expect(this.page.getByTestId("app-header")).toHaveCSS("left", "64px");
        const logo = await navigation.getByTestId("navigation-logo").innerHTML();

        await trigger.click();
        await expect(dialog).toBeVisible();
        await expect(navigation).toHaveCSS("width", "240px");
        expect(await navigation.getByTestId("navigation-logo").innerHTML()).toBe(logo);

        // Hold the slide halfway through to catch the transformed panel falling below its backdrop.
        const panel = navigation.locator("..");
        await panel.evaluate((element) => {
          for (const animation of element.getAnimations()) {
            animation.pause();
            animation.currentTime = 150;
          }
        });
        try {
          await expect
            .poll(() =>
              navigation.evaluate((element) => {
                const bounds = element.getBoundingClientRect();
                const visibleLeft = Math.max(0, bounds.left);
                const hit = document.elementFromPoint((visibleLeft + bounds.right) / 2, 100);
                return element.contains(hit);
              }),
            )
            .toBe(true);
        } finally {
          await panel.evaluate((element) => {
            for (const animation of element.getAnimations()) animation.play();
          });
        }

        await this.page.keyboard.press("Shift+Tab");
        await expect(navigation.getByRole("button", { name: "Log out" })).toBeFocused();
        await this.page.keyboard.press("Tab");
        await expect(navigation.getByTestId("navigation-logo").locator("..").locator("..")).toBeFocused();
        await this.page.keyboard.press("Escape");
        await expect(dialog).toBeHidden();
        await expect(trigger).toBeFocused();
        await expect(navigation).toHaveCSS("width", "64px");
      }

      await this.page.setViewportSize({ width: 393, height: 852 });
      await expect(navigation).toBeHidden();
      await expect(this.page.getByTestId("app-content")).toHaveCSS("left", "0px");
      await trigger.click();
      await expect(dialog).toBeVisible();
      await dialog.getByRole("button", { name: "Close navigation menu" }).click({ position: { x: 380, y: 100 } });
      await expect(dialog).toBeHidden();

      // Resizing an open compact drawer to desktop must not leave it open when returning to tablet.
      await trigger.click();
      await expect(dialog).toBeVisible();
      await this.page.setViewportSize({ width: 1600, height: 900 });
      await expect(dialog).toBeHidden();
      await expect(navigation).toHaveCSS("width", "200px");
      await expect(trigger).toBeHidden();
      await this.page.setViewportSize({ width: 768, height: 1024 });
      await expect(dialog).toBeHidden();
      await this.page.mouse.move(767, 0);
      await trigger.focus();
      await expect(navigation).toHaveCSS("width", "64px");
    } finally {
      if (originalViewport) await this.page.setViewportSize(originalViewport);
    }
  }

  async validateTouchNavigation() {
    const browser = this.page.context().browser();
    if (!browser) throw new Error("Touch navigation requires a browser-backed context");
    const context = await browser.newContext({
      hasTouch: true,
      isMobile: true,
      viewport: { width: 768, height: 1024 },
      storageState: await this.page.context().storageState(),
    });
    try {
      const page = await context.newPage();
      await page.goto(this.page.url());
      for (const viewport of [
        { width: 768, height: 1024 },
        { width: 1024, height: 768 },
      ]) {
        await page.setViewportSize(viewport);
        const navigation = page.getByRole("navigation", { name: "Main" });
        await expect(navigation).toHaveCSS("width", "64px");
        await page.getByRole("button", { name: "Open navigation menu", exact: true }).tap();
        const dialog = page.getByRole("dialog", { name: "Navigation menu" });
        await expect(dialog).toBeVisible();
        await expect(navigation).toHaveCSS("width", "240px");
        await navigation.getByRole("link", { name: "Security", exact: true }).tap();
        await expect(page).toHaveURL(/\/settings\/security(?:[?#].*)?$/);
        await expect(dialog).toBeHidden();
        await expect(navigation).toHaveCSS("width", "64px");
      }
    } finally {
      await context.close();
    }
  }
}
