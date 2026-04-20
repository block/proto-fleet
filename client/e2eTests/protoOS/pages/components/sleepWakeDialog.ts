import { expect } from "@playwright/test";
import { BasePage } from "../base";

export class SleepWakeDialogsComponent extends BasePage {
  async clickEnterSleepMode() {
    const dialog = this.page.getByTestId("warn-sleep-dialog");
    await dialog.getByRole("button", { name: "Enter sleep mode" }).click();
  }

  async validateEnteringSleepDialog() {
    const dialog = this.page.getByTestId("entering-sleep-dialog");
    await expect(dialog).toBeVisible();
    await expect(dialog).toContainText("Entering sleep mode");
    await expect(dialog).toBeHidden();
  }

  async clickWakeMinerInDialog() {
    const dialog = this.page.getByTestId("warn-wake-up-dialog");
    await dialog.getByRole("button", { name: "Wake up miner" }).click();
  }

  async validateWakingDialog() {
    const dialog = this.page.getByTestId("waking-dialog");
    await expect(dialog).toBeVisible();
    await expect(dialog).toContainText("Waking up miner");
    await expect(dialog).toBeHidden();
  }

  async validateMinerAsleepModal() {
    await this.validateModalIsOpen();
    await this.validateTitleInModal("Miner is asleep");
    await this.validateTextInModal("Done");
  }

  async clickWakeMinerInModal() {
    const modal = this.page.getByTestId("modal");
    await modal.getByRole("button", { name: "Wake miner" }).click();
  }
}
