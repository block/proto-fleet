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
        await this.page.getByText("Add and manage the pools for your fleet.", { exact: true }).click();
        await expect(trigger).toBeHidden();
        await expect(navigation).toHaveCSS("width", "64px");
        await expect(this.page.getByTestId("app-content")).toHaveCSS("left", "64px");
        await expect(this.page.getByTestId("app-header")).toHaveCSS("left", "64px");
        await expect(
          this.page.getByRole("navigation", { name: "Settings" }).getByRole("link", { name: "Security", exact: true }),
        ).toBeVisible();
        await this.page.keyboard.press("Tab");
        await navigation.getByRole("link", { name: "Fleet", exact: true }).focus();
        await expect(navigation).toHaveCSS("width", "200px");
        await this.page.getByText("Add and manage the pools for your fleet.", { exact: true }).click();
        await expect(navigation).toHaveCSS("width", "64px");
      }
      const logo = await navigation.getByTestId("navigation-logo").innerHTML();

      await this.page.setViewportSize({ width: 393, height: 852 });
      await expect(navigation).toBeHidden();
      await expect(this.page.getByTestId("app-content")).toHaveCSS("left", "0px");
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
      await trigger.click();
      await expect(dialog).toBeVisible();
      await dialog.getByRole("button", { name: "Close navigation menu" }).click({ position: { x: 380, y: 100 } });
      await expect(dialog).toBeHidden();

      // Crossing from an open phone drawer to a rail must clear the drawer state.
      await trigger.click();
      await expect(dialog).toBeVisible();
      await this.page.setViewportSize({ width: 768, height: 1024 });
      await expect(dialog).toBeHidden();
      await expect(trigger).toBeHidden();
      await this.page.setViewportSize({ width: 393, height: 852 });
      await expect(dialog).toBeHidden();
      await expect(navigation).toBeHidden();
      await this.page.setViewportSize({ width: 1600, height: 900 });
      await expect(dialog).toBeHidden();
      await expect(navigation).toHaveCSS("width", "200px");
      await expect(trigger).toBeHidden();
      await this.page.setViewportSize({ width: 768, height: 1024 });
      await expect(dialog).toBeHidden();
      await this.page.getByText("Add and manage the pools for your fleet.", { exact: true }).click();
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
        { width: 667, height: 320 },
      ]) {
        await page.setViewportSize(viewport);
        const navigation = page.getByRole("navigation", { name: "Main" });
        await expect(navigation).toHaveCSS("width", "64px");
        await expect(navigation).toHaveCSS("height", `${viewport.height}px`);
        await expect(page.getByRole("button", { name: "Open navigation menu", exact: true })).toBeHidden();
        const links = navigation.getByTestId("navigation-menu");
        const settings = navigation.getByRole("link", { name: "Settings", exact: true });
        const logout = navigation.getByRole("button", { name: "Log out", exact: true });
        await expect(links.getByRole("link")).toHaveCount(7);
        await expect(logout).toBeInViewport({ ratio: 1 });
        await settings.scrollIntoViewIfNeeded();
        await expect(settings).toBeInViewport({ ratio: 1 });
        await expect(logout).toBeInViewport({ ratio: 1 });
        if (viewport.height === 320) {
          expect(await links.evaluate((element) => element.scrollTop)).toBeGreaterThan(0);
        }
        await settings.tap();
        await page
          .getByRole("navigation", { name: "Settings" })
          .getByRole("link", { name: "Security", exact: true })
          .tap();
        await expect(page).toHaveURL(/\/settings\/security(?:[?#].*)?$/);
        await expect(page.getByRole("dialog", { name: "Navigation menu" })).toBeHidden();
        await expect(navigation).toHaveCSS("width", "64px");
        await expect(logout).toBeInViewport({ ratio: 1 });
      }
    } finally {
      await context.close();
    }
  }
}
