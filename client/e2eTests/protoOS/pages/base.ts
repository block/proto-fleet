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
    const titleLocator = this.page.locator(`//*[contains(@class,'heading')][text()="${expectedTitle}"]`);
    await expect(titleLocator).toBeVisible();
  }

  async validateTitleInModal(expectedTitle: string) {
    const titleLocator = this.page.locator(
      `//*[@data-testid='modal']//*[contains(@class,'heading')][text()="${expectedTitle}"]`,
    );
    await expect(titleLocator).toBeVisible();
  }

  async validateTitleNotVisible(expectedTitle: string) {
    const titleLocator = this.page.locator(`//*[contains(@class,'heading')][text()="${expectedTitle}"]`);
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

  async click(text: string) {
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

  async clickNavigationMenuIfMobile() {
    if (this.isMobile) {
      await this.page.getByTestId("navigation-menu-button").click();
    }
  }

  async clickNavigationItem(itemName: string) {
    await this.page.getByTestId("navigation").getByRole("button", { name: itemName }).click();
  }

  async navigateToHome() {
    await this.clickNavigationMenuIfMobile();
    await this.clickNavigationItem("Home");
    await expect(this.page).toHaveURL(/.*\/hashrate/);
    await this.validateTitle("Home");
  }

  async navigateToDiagnostics() {
    await this.clickNavigationMenuIfMobile();
    await this.clickNavigationItem("Diagnostics");
    await expect(this.page).toHaveURL(/.*\/diagnostics/);
    await this.validateTitle("Diagnostics");
  }

  async navigateToLogs() {
    await this.clickNavigationMenuIfMobile();
    await this.clickNavigationItem("Logs");
    await expect(this.page).toHaveURL(/.*\/logs/);
  }

  async navigateToAuthenticationSettings() {
    await this.clickNavigationMenuIfMobile();
    await this.clickNavigationItem("Settings");
    await this.clickNavigationItem("Authentication");
    await expect(this.page).toHaveURL(/.*\/settings\/authentication/);
    await this.validateTitle("Update your admin login");
  }

  async navigateToGeneralSettings() {
    await this.clickNavigationMenuIfMobile();
    await this.clickNavigationItem("Settings");
    await this.clickNavigationItem("General");
    await expect(this.page).toHaveURL(/.*\/settings\/general/);
    await this.validateTitle("General");
  }

  async navigateToPoolsSettings() {
    await this.clickNavigationMenuIfMobile();
    await this.clickNavigationItem("Settings");
    await this.clickNavigationItem("Pools");
    await expect(this.page).toHaveURL(/.*\/settings\/mining-pools/);
  }

  async navigateToHardwareSettings() {
    await this.clickNavigationMenuIfMobile();
    await this.clickNavigationItem("Settings");
    await this.clickNavigationItem("Hardware");
    await expect(this.page).toHaveURL(/.*\/settings\/hardware/);
    await this.validateTitle("Hardware");
  }

  async navigateToCoolingSettings() {
    await this.clickNavigationMenuIfMobile();
    await this.clickNavigationItem("Settings");
    await this.clickNavigationItem("Cooling");
    await expect(this.page).toHaveURL(/.*\/settings\/cooling/);
    await this.validateTitle("Cooling");
  }
}
