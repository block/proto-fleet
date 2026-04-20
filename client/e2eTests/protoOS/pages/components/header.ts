import { expect } from "@playwright/test";
import { BasePage } from "../base";

export class HeaderComponent extends BasePage {
  async clickPowerButton() {
    await this.page.getByTestId("power-button").click();
  }

  async clickPowerPopoverButton(buttonText: string) {
    const popover = this.page.getByTestId("power-popover");
    await popover.getByRole("button", { name: buttonText }).click();
  }

  async clickMinerStatusButton(status: string = "Sleeping") {
    const header = this.page.getByTestId("page-header");
    await header.getByRole("button", { name: status }).click();
  }

  async validateMinerStatus(status: string) {
    const header = this.page.getByTestId("page-header");
    await expect(header.getByRole("button", { name: status })).toBeVisible();
  }
}
