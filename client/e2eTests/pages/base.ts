import { expect, Page } from "@playwright/test";

export class BasePage {
  constructor(protected page: Page) {}

  async reloadPage() {
    await this.page.reload();
  }

  async validateLoggedIn() {
    await expect(this.page.getByTestId("logout-button")).toBeVisible();
  }

  async logout() {
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

  async navigateToHomePage() {
    await this.page.getByTestId("navigation-menu").locator('a[href="/"]').click();
    await expect(this.page).toHaveURL(/.*\/$/);
  }

  async navigateToMinersPage() {
    await this.page.getByTestId("navigation-menu").locator('a[href="/miners"]').click();
    await expect(this.page).toHaveURL(/.*\/miners/);
  }

  async navigateToSettingsPage() {
    await this.page.getByTestId("navigation-menu").locator('a[href="/settings"]').click();
    await expect(this.page).toHaveURL(/.*\/settings/);
  }

  async click(text: string) {
    await this.page.getByRole("button", { name: text, disabled: false }).click();
  }

  async clickIn(text: string, testId: string) {
    await this.page.getByTestId(testId).getByRole("button", { name: text, disabled: false }).click();
  }
}
