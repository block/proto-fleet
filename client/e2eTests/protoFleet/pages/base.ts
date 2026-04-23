import { expect, Page } from "@playwright/test";
import { DEFAULT_TIMEOUT, testConfig } from "../config/test.config";

export class BasePage {
  constructor(
    protected page: Page,
    protected isMobile: boolean = false,
  ) {}

  async reloadPage() {
    await this.page.reload();
  }

  async validateLoggedIn(timeout: number = DEFAULT_TIMEOUT) {
    if (this.isMobile) {
      await expect(this.page.getByTestId("navigation-menu-button")).toBeVisible({ timeout });
    } else {
      await expect(this.page.getByTestId("logout-button")).toBeVisible({ timeout });
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

  async validateTextIsVisible(text: string) {
    await expect(this.page.getByText(text)).toBeVisible();
  }

  async validateTextInToast(text: string) {
    const toast = this.page.getByTestId("toast").getByText(text);
    await expect(toast).toBeVisible();
  }

  async validateTextInToastGroup(text: string) {
    const groupedHeaderMessage = this.page.getByTestId("grouped-toaster-header").getByText(text).first();
    const groupedBodyMessage = this.page.getByTestId("toaster-container").getByTestId("toast").getByText(text).first();

    await expect
      .poll(
        async () =>
          (await groupedHeaderMessage.isVisible().catch(() => false)) ||
          (await groupedBodyMessage.isVisible().catch(() => false)),
        {
          timeout: DEFAULT_TIMEOUT,
        },
      )
      .toBe(true);
  }

  async dismissToast() {
    const toast = this.page.getByTestId("toaster-container");
    const dismissButton = this.page.getByRole("button", { name: "Dismiss" });
    if (!(await dismissButton.isVisible())) {
      await toast.click();
    }
    await toast.getByRole("button", { name: "Dismiss" }).click();
  }

  async validateTextInModal(text: string) {
    await expect(this.page.getByTestId("modal").getByText(text)).toBeVisible();
  }

  async validateTextNotInModal(text: string) {
    await expect(this.page.getByTestId("modal").getByText(text)).toBeHidden();
  }

  async validateButtonIsVisible(text: string) {
    await expect(this.page.getByRole("button", { name: text })).toBeVisible();
  }

  async clickNavigationMenuIfMobile() {
    if (this.isMobile) {
      await this.page.getByTestId("navigation-menu-button").click();
    }
  }

  async clickExpandSettingsIfMobile() {
    if (this.isMobile && !this.page.url().includes("/settings")) {
      await this.page.getByTestId("navigation-menu").getByText("Settings").click();
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

  async navigateToGroupsPage() {
    await this.clickNavigationMenuIfMobile();
    await this.page.getByTestId("navigation-menu").locator('a[href="/groups"]').click();
    await expect(this.page).toHaveURL(/.*\/groups/);
  }

  async navigateToRacksPage() {
    await this.clickNavigationMenuIfMobile();
    await this.page.getByTestId("navigation-menu").locator('a[href="/racks"]').click();
    await expect(this.page).toHaveURL(/.*\/racks/);
  }

  async navigateToActivityPage() {
    await this.clickNavigationMenuIfMobile();
    await this.page.getByTestId("navigation-menu").locator('a[href="/activity"]').click();
    await expect(this.page).toHaveURL(/.*\/activity/);
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
      await this.navigateToSettingsPage();
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

  async navigateToFirmwareSettings() {
    await this.clickNavigationMenuIfMobile();
    await this.clickExpandSettingsIfMobile();
    await this.navigateSettingsIfDesktop();
    await this.page.getByTestId("secondary-nav").locator('a[href="/settings/firmware"]').click();
    await expect(this.page).toHaveURL(/.*\/settings\/firmware/);
  }

  async navigateToApiKeysSettings() {
    await this.clickNavigationMenuIfMobile();
    await this.clickExpandSettingsIfMobile();
    await this.navigateSettingsIfDesktop();
    await this.page.getByTestId("secondary-nav").locator('a[href="/settings/api-keys"]').click();
    await expect(this.page).toHaveURL(/.*\/settings\/api-keys/);
  }

  async navigateToSchedulesSettings() {
    await this.clickNavigationMenuIfMobile();
    await this.clickExpandSettingsIfMobile();
    await this.navigateSettingsIfDesktop();
    await this.page.getByTestId("secondary-nav").locator('a[href="/settings/schedules"]').click();
    await expect(this.page).toHaveURL(/.*\/settings\/schedules/);
  }

  async clickButton(text: string) {
    await this.page.getByRole("button", { name: text, disabled: false, exact: true }).click();
  }

  async clickUntilNotVisible(text: string) {
    const button = this.page.getByRole("button", { name: text, disabled: false, exact: true });

    await expect(button).toBeVisible();
    await expect(async () => {
      const isVisible = await button.isVisible();
      if (isVisible) {
        await button.click();
        throw new Error("Button still visible, looping until it is not or the time runs out");
      }
    }).toPass({ timeout: DEFAULT_TIMEOUT, intervals: [100] });
  }

  async clickIn(text: string, testId: string) {
    await this.page.getByTestId(testId).getByRole("button", { name: text, disabled: false, exact: true }).click();
  }

  async validateModalIsOpen() {
    await expect(this.page.getByTestId("modal")).toBeVisible();
  }

  async validateModalIsClosed() {
    await expect(this.page.getByTestId("modal")).toBeHidden();
  }

  async clickSaveInModal() {
    await this.clickIn("Save", "modal");
  }

  // Helper method to try an action with timeout and return success/failure
  // Useful in cases where we are not sure in what state the system is at a particular moment, e.g. during cleanup
  async tryAction(action: () => Promise<void>, timeoutMs: number = 3000): Promise<boolean> {
    const originalTimeout = testConfig.actionTimeout;
    this.page.setDefaultTimeout(timeoutMs);
    try {
      await action();
      return true;
    } catch {
      return false;
    } finally {
      this.page.setDefaultTimeout(originalTimeout);
    }
  }
}
