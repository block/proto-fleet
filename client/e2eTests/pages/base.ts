import { expect, Page } from "@playwright/test";

export class BasePage {
  constructor(
    protected page: Page,
    protected isMobile: boolean = false,
  ) {}

  async reloadPage() {
    await this.page.reload();
  }

  async validateLoggedIn() {
    if (this.isMobile) {
      await expect(this.page.getByTestId("navigation-menu-button")).toBeVisible();
    } else {
      await expect(this.page.getByTestId("logout-button")).toBeVisible();
    }
  }

  async logout() {
    await this.clickNavigationMenuIfMobile();
    await this.page.getByTestId("logout-button").click();
  }

  async validateTitle(expectedTitle: string) {
    const titleLocator = this.page.locator(`//*[contains(@class,'heading')][text()='${expectedTitle}']`);
    await expect(titleLocator).toBeVisible();
  }

  async validateTitleInModal(expectedTitle: string) {
    const titleLocator = this.page.locator(
      `//*[@data-testid='modal']//*[contains(@class,'heading')][text()='${expectedTitle}']`,
    );
    await expect(titleLocator).toBeVisible();
  }

  async validateTitleNotVisible(expectedTitle: string) {
    const titleLocator = this.page.locator(`//*[contains(@class,'heading')][text()='${expectedTitle}']`);
    await expect(titleLocator).toBeHidden();
  }

  async validateTitleInModalNotVisible(expectedTitle: string) {
    const titleLocator = this.page.locator(
      `//*[@data-testid='modal']//*[contains(@class,'heading')][text()='${expectedTitle}']`,
    );
    await expect(titleLocator).toBeHidden();
  }

  async clickNavigationMenuIfMobile() {
    if (this.isMobile) {
      await this.page.getByTestId("navigation-menu-button").click();
    }
  }

  async clickExpandSettingsIfMobile() {
    if (this.isMobile && !this.page.url().includes("/settings")) {
      await this.clickIn("Settings", "navigation-menu");
    }
  }

  async navigateToHomePage() {
    await this.clickNavigationMenuIfMobile();
    await this.page.getByTestId("navigation-menu").locator('a[href="/"]').click();
    await expect(this.page).toHaveURL(/.*\/$/);
  }

  async navigateToMinersPage() {
    await this.clickNavigationMenuIfMobile();
    await this.page.getByTestId("navigation-menu").locator('a[href="/miners"]').click();
    await expect(this.page).toHaveURL(/.*\/miners/);
  }

  async navigateToSettingsPage() {
    await this.clickNavigationMenuIfMobile();
    await this.clickExpandSettingsIfMobile();
    if (this.isMobile) {
      await this.page.getByTestId("navigation-menu").locator('a[href="/settings/general"]').click();
    } else {
      await this.page.getByTestId("navigation-menu").locator('a[href="/settings"]').click();
    }
    await expect(this.page).toHaveURL(/.*\/settings/);
  }

  async navigateSettingsIfDesktop() {
    // desktop can't navigate directly to subpages of settings
    if (!this.isMobile && !this.page.url().includes("/settings")) {
      this.navigateToSettingsPage();
    }
  }

  async navigateToSecuritySettings() {
    await this.clickNavigationMenuIfMobile();
    await this.clickExpandSettingsIfMobile();
    await this.navigateSettingsIfDesktop();
    await this.page.getByTestId("secondary-nav").locator('a[href="/settings/security"]').click();
    await expect(this.page).toHaveURL(/.*\/settings\/security/);
  }

  async navigateToTeamSettings() {
    await this.clickNavigationMenuIfMobile();
    await this.clickExpandSettingsIfMobile();
    await this.navigateSettingsIfDesktop();
    await this.page.getByTestId("secondary-nav").locator('a[href="/settings/team"]').click();
    await expect(this.page).toHaveURL(/.*\/settings\/team/);
  }

  async navigateToMiningPoolsSettings() {
    await this.clickNavigationMenuIfMobile();
    await this.clickExpandSettingsIfMobile();
    await this.navigateSettingsIfDesktop();
    await this.page.getByTestId("secondary-nav").locator('a[href="/settings/mining-pools"]').click();
    await expect(this.page).toHaveURL(/.*\/settings\/mining-pools/);
  }

  async click(text: string) {
    await this.page.getByRole("button", { name: text, disabled: false }).click();
  }

  async clickIn(text: string, testId: string) {
    await this.page.getByTestId(testId).getByRole("button", { name: text, disabled: false }).click();
  }
}
