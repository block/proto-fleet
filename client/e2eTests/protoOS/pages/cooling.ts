import { expect } from "@playwright/test";
import { BasePage } from "./base";

export class CoolingPage extends BasePage {
  private airCoolingHeading() {
    return this.page.getByRole("heading", { name: "Air cooled", exact: true });
  }

  private immersionCoolingHeading() {
    return this.page.getByRole("heading", { name: "Immersion cooled", exact: true });
  }

  private airCoolingRadio() {
    return this.page.getByRole("radio").first();
  }

  private immersionCoolingRadio() {
    return this.page.getByRole("radio").nth(1);
  }

  private coolingInfoModal() {
    return this.page.getByTestId("modal");
  }

  async validateAirCooledSelected() {
    await expect(this.airCoolingRadio()).toBeChecked();
  }

  async validateImmersionCooledSelected() {
    await expect(this.immersionCoolingRadio()).toBeChecked();
  }

  async isAirCooledSelected() {
    return this.airCoolingRadio().isChecked();
  }

  async isImmersionCooledSelected() {
    return this.immersionCoolingRadio().isChecked();
  }

  async clickAirCooledOption() {
    await this.airCoolingHeading().click();
  }

  async clickImmersionCooledOption() {
    await this.immersionCoolingHeading().click();
  }

  async clickLearnMoreButton() {
    await this.page.getByRole("button", { name: "Learn more" }).click();
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
