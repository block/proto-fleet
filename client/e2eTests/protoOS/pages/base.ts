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
    await expect(this.page.getByTestId("power-button")).toBeVisible();
  }

  async validateTitle(expectedTitle: string) {
    const titleLocator = this.page.getByText(expectedTitle, { exact: true });
    await expect(titleLocator).toBeVisible();
  }

  async validateTitleInModal(expectedTitle: string) {
    const titleLocator = this.page.getByTestId("modal").getByText(expectedTitle, { exact: true });
    await expect(titleLocator).toBeVisible();
  }

  async validateTitleNotVisible(expectedTitle: string) {
    const titleLocator = this.page.getByText(expectedTitle, { exact: true });
    await expect(titleLocator).toBeHidden();
  }

  async validateTextIsVisible(text: string) {
    await expect(this.page.getByText(text)).toBeVisible();
  }

  async validateTextInModal(text: string) {
    await expect(this.page.getByTestId("modal").getByText(text)).toBeVisible();
  }

  async validateTextNotInModal(text: string) {
    await expect(this.page.getByTestId("modal").getByText(text)).toBeHidden();
  }

  async validateToastMessage(message: string) {
    await expect(this.page.getByTestId("toast").getByText(message)).toBeVisible();
  }

  async inputLoginPassword(password: string) {
    await this.page.getByTestId("password").fill(password);
  }

  async clickLoginButton() {
    await this.page.getByTestId("login-button").click();
  }

  async clickButton(text: string) {
    await this.page.getByRole("button", { name: text, disabled: false }).click();
  }

  async clickIn(text: string, testId: string) {
    await this.page.getByTestId(testId).getByRole("button", { name: text, disabled: false }).click();
  }

  async validateModalIsOpen() {
    await expect(this.page.getByTestId("modal")).toBeVisible();
  }

  async validateModalIsClosed() {
    await expect(this.page.getByTestId("modal")).toBeHidden();
  }

  async validateButtonIsVisible(text: string) {
    await expect(this.page.getByRole("button", { name: text })).toBeVisible();
  }
}
