import { chromium, expect, type Page } from "@playwright/test";

export class NavigationPage {
  constructor(private readonly page: Page) {}

  async validateLargeFontNavigation() {
    // Change the browser's default font, not the page's root font-size: media-query
    // rem units use the former, so a CSS font-size override would miss this regression.
    const browser = await chromium.launch({ args: ["--blink-settings=defaultFontSize=20"] });
    try {
      const context = await browser.newContext({
        viewport: { width: 700, height: 900 },
        storageState: await this.page.context().storageState(),
      });
      const page = await context.newPage();
      await page.goto(this.page.url());
      await expect(page.locator("html")).toHaveCSS("font-size", "20px");
      expect(await page.evaluate(() => window.matchMedia("(min-width: 39.5rem)").matches)).toBe(false);

      const navigation = page.getByRole("navigation", { name: "Main", exact: true });
      const trigger = page.getByRole("button", { name: "Open navigation menu", exact: true });
      for (const width of [700, 632, 959, 960, 1279, 1280]) {
        await page.setViewportSize({ width, height: 900 });
        const railWidth = width >= 1280 ? "250px" : "80px";
        await expect(trigger).toBeHidden();
        await expect(navigation).toHaveCSS("width", railWidth);
        await expect(page.getByTestId("app-content")).toHaveCSS("left", railWidth);
        await expect(page.getByTestId("app-header")).toHaveCSS("left", railWidth);
        await expect(page.getByTestId("app-content")).toHaveCSS("top", width >= 960 ? "75px" : "60px");
        const toggle = page.getByRole("button", { name: "Expand navigation", exact: true });
        if (width < 1280) {
          await toggle.click();
          await expect(navigation).toHaveCSS("width", "250px");
          await expect(page.getByTestId("app-header")).toHaveCSS("left", "250px");
          await page.getByRole("button", { name: "Collapse navigation", exact: true }).click();
          await expect(navigation).toHaveCSS("width", railWidth);
        } else {
          await expect(toggle).toBeHidden();
        }
      }

      await page.setViewportSize({ width: 631, height: 900 });
      await expect(navigation).toBeHidden();
      await expect(trigger).toBeVisible();
      await expect(page.getByTestId("app-content")).toHaveCSS("left", "0px");
      await trigger.click();
      await expect(page.getByRole("dialog", { name: "Navigation menu" })).toBeVisible();
      await expect(navigation).toHaveCSS("width", "300px");
      await page.keyboard.press("Escape");
      await expect(navigation).toBeHidden();
      await expect(trigger).toBeFocused();
    } finally {
      await browser.close();
    }
  }

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
        const toggle = this.page.getByRole("button", { name: "Expand navigation", exact: true });
        const logoBounds = await navigation.getByTestId("navigation-logo").boundingBox();
        const toggleBounds = await toggle.boundingBox();
        if (!logoBounds || !toggleBounds) throw new Error("Navigation logo and header toggle must be visible");
        expect(Math.abs(logoBounds.y + logoBounds.height / 2 - toggleBounds.y - toggleBounds.height / 2)).toBeLessThan(
          1,
        );
        await toggle.hover();
        await expect(navigation).toHaveCSS("width", "64px");
        await toggle.focus();
        await expect(navigation).toHaveCSS("width", "64px");
        await this.page.keyboard.press("Enter");
        await expect(navigation).toHaveCSS("width", "200px");
        await expect(this.page.getByTestId("app-content")).toHaveCSS("left", "64px");
        await expect(this.page.getByTestId("app-header")).toHaveCSS("left", "200px");
        await this.page.keyboard.press("Escape");
        await expect(navigation).toHaveCSS("width", "64px");
        await expect(toggle).toBeFocused();
        await this.page.keyboard.press("Space");
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
      await expect(this.page.getByRole("button", { name: "Expand navigation", exact: true })).toBeHidden();
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
        const toggle = page.getByRole("button", { name: "Expand navigation", exact: true });
        await expect(toggle).toBeInViewport({ ratio: 1 });
        const bounds = await toggle.boundingBox();
        expect(bounds?.width).toBeGreaterThanOrEqual(32);
        expect(bounds?.height).toBeGreaterThanOrEqual(44);
        await expect(navigation.getByText("Owner", { exact: true })).toBeHidden();
        await toggle.tap();
        await expect(navigation).toHaveCSS("width", "200px");
        await expect(navigation.getByText("Owner", { exact: true })).toBeVisible();
        await page.getByRole("button", { name: "Collapse navigation", exact: true }).tap();
        await expect(navigation).toHaveCSS("width", "64px");
        await toggle.tap();
        await expect(logout).toBeInViewport({ ratio: 1 });
        await settings.scrollIntoViewIfNeeded();
        await expect(settings).toBeInViewport({ ratio: 1 });
        await expect(logout).toBeInViewport({ ratio: 1 });
        if (viewport.height === 320) {
          expect(await links.evaluate((element) => element.scrollTop)).toBeGreaterThan(0);
        }
        await settings.tap();
        await expect(navigation).toHaveCSS("width", "64px");
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
