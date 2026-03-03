import { expect } from "@playwright/test";
import { BasePage } from "./base";

export class HomePage extends BasePage {
  async validateCompleteSetupTitle() {
    await this.validateTitle("Complete setup");
  }

  async validateHomePageOpened() {
    await expect(this.page).toHaveURL(/.*\/$/);
  }

  async clickAuthenticateMinersButton() {
    await this.clickButton("Authenticate");
  }

  async validateAuthenticateMinersModalTitle() {
    await this.validateTitleInModal("Authenticate miners");
  }

  async inputMinerAuthUsername(username: string) {
    await this.page.locator(`//input[@id='username']`).fill(username);
  }

  async inputMinerAuthPassword(password: string) {
    await this.page.locator(`//input[@id='password']`).fill(password);
  }

  async clickAuthenticateMinersConfirmButton() {
    await this.page.getByTestId("modal").getByRole("button", { name: "Authenticate" }).click();
  }

  async validateCompleteSetupTitleNotVisible() {
    await this.validateTitleNotVisible("Complete setup");
  }

  async validateAuthenticateMinersButtonNotVisible() {
    await expect(this.page.getByRole("button", { name: "Authenticate" })).toBeHidden();
  }

  async clickControlBoardsLink() {
    await this.page.getByRole("link", { name: "Control Boards" }).click();
  }

  async clickFansLink() {
    await this.page.getByRole("link", { name: "Fans" }).click();
  }

  async clickHashboardsLink() {
    await this.page.getByRole("link", { name: "Hashboards" }).click();
  }

  async clickPowerSuppliesLink() {
    await this.page.getByRole("link", { name: "Power supplies" }).click();
  }

  async getListOfMinersToAuthenticate(): Promise<string[]> {
    return this.page.getByTestId("modal").getByTestId("model").allTextContents();
  }

  async clickShowMinersButton() {
    await this.page.getByTestId("modal").getByRole("button", { name: "Show miners" }).click();
  }

  async validateCalloutInModal(text: string) {
    await expect(this.page.getByTestId("modal").locator("[data-testid*='callout']").getByText(text)).toBeVisible();
  }

  async validateNoCalloutInModal() {
    await expect(this.page.getByTestId("modal").locator("[data-testid*='callout']")).toBeHidden();
  }

  async clickCalloutButton() {
    await this.page.getByTestId("modal").locator("[data-testid*='callout']").getByRole("button").click();
  }

  async getMinerRowByModel(model: string) {
    return this.page
      .getByTestId("modal")
      .locator("tr")
      .filter({ has: this.page.getByTestId("model").getByText(model) });
  }

  async clickMinerAuthCheckbox(model: string) {
    const row = await this.getMinerRowByModel(model);
    await row.locator('input[type="checkbox"]').click();
  }

  async inputMinerRowUsername(model: string, username: string) {
    const row = await this.getMinerRowByModel(model);
    await row.getByTestId("username").locator("input").fill(username);
  }

  async inputMinerRowPassword(model: string, password: string) {
    const row = await this.getMinerRowByModel(model);
    await row.getByTestId("password").locator("input").fill(password);
  }

  async validateModalClosed() {
    await expect(this.page.getByTestId("modal")).toBeHidden();
  }
}
