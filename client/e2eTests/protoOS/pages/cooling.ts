import { expect } from "@playwright/test";
import { BasePage } from "./base";

export class CoolingPage extends BasePage {
  private airCoolingOption() {
    return this.page.getByTestId("cooling-option-air");
  }

  private immersionCoolingOption() {
    return this.page.getByTestId("cooling-option-immersion");
  }

  private airCoolingRadio() {
    return this.airCoolingOption().locator('input[type="radio"]');
  }

  private immersionCoolingRadio() {
    return this.immersionCoolingOption().locator('input[type="radio"]');
  }

  private coolingInfoModal() {
    return this.page.getByTestId("modal");
  }

  async waitForCoolingOptions() {
    await expect(this.airCoolingOption()).toBeVisible();
    await expect(this.immersionCoolingOption()).toBeVisible();
  }

  async validateAirCooledSelected() {
    await this.waitForCoolingOptions();
    await expect(this.airCoolingRadio()).toBeChecked();
  }

  async validateImmersionCooledSelected() {
    await this.waitForCoolingOptions();
    await expect(this.immersionCoolingRadio()).toBeChecked();
  }

  async isAirCooledSelected() {
    await this.waitForCoolingOptions();
    return this.airCoolingRadio().isChecked();
  }

  async isImmersionCooledSelected() {
    await this.waitForCoolingOptions();
    return this.immersionCoolingRadio().isChecked();
  }

  async clickAirCooledOption() {
    await this.waitForCoolingOptions();
    await this.airCoolingOption().click();
  }

  async clickImmersionCooledOption() {
    await this.waitForCoolingOptions();
    await this.immersionCoolingOption().click();
  }

  async clickLearnMoreButton() {
    await this.page.getByTestId("cooling-learn-more-button").click();
  }

  async validateImmersionCoolingModalOpen() {
    await this.validateModalIsOpen();
    await this.validateTitleInModal("Immersion cooling");
    await expect(this.coolingInfoModal().getByRole("button", { name: "Enter sleep mode" })).toBeVisible();
  }

  async validateLearnMoreModalOpen() {
    await this.validateModalIsOpen();
    await this.validateTitleInModal("Immersion cooling");
    await expect(this.coolingInfoModal().getByRole("button", { name: "Enter sleep mode" })).toHaveCount(0);
    await this.validateTextInModal("Prepare your miner for immersion");
  }

  async clickEnterSleepModeInModal() {
    await this.coolingInfoModal().getByRole("button", { name: "Enter sleep mode" }).click();
  }

  async dismissInfoModal() {
    await this.page.keyboard.press("Escape");
    await this.validateModalIsClosed();
  }

  async validateCoolingModeUpdatedTo(mode: "air cooled" | "immersion cooled") {
    await this.validateToastMessage(`Cooling mode updated to ${mode}`);
  }

  async isCoolingInfoModalVisible() {
    return this.coolingInfoModal().isVisible();
  }

  async isWakeCalloutVisible() {
    return this.page.getByTestId("callout").getByRole("button", { name: "Wake up miner" }).isVisible();
  }
}
