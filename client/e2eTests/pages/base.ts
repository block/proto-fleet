import { expect, Page } from "@playwright/test";

export class BasePage {
  constructor(protected page: Page) {}

  async validateLoggedIn() {
    await expect(this.page.locator(`//*[@data-testid="logout-button"]`)).toBeVisible();
  }

  async logout() {
    await this.page.locator(`//*[@data-testid="logout-button"]`).click();
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
    await this.page.locator(`//*[@data-testid="navigation-menu"]//*[@href='/']`).click();
    await expect(this.page).toHaveURL(/.*\/$/);
  }

  async navigateToMinersPage() {
    await this.page.locator(`//*[@data-testid="navigation-menu"]//*[@href='/miners']`).click();
    await expect(this.page).toHaveURL(/.*\/miners/);
  }

  async navigateToSettingsPage() {
    await this.page.locator(`//*[@data-testid="navigation-menu"]//*[@href='/settings']`).click();
    await expect(this.page).toHaveURL(/.*\/settings/);
  }

  async click(text: string) {
    await this.page.getByRole("button", { name: text }).click();
  }
}
