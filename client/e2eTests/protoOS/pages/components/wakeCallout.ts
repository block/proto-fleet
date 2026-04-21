import { expect } from "@playwright/test";
import { BasePage } from "../base";

export class WakeCalloutComponent extends BasePage {
  async validateWakeCallout() {
    const callout = this.page.getByTestId("callout");
    await expect(callout.getByText("This miner is asleep and is not hashing.")).toBeVisible();
    await expect(callout.getByRole("button", { name: "Wake up miner" })).toBeVisible();
  }

  async validateWakeCalloutNotVisible() {
    const callout = this.page.getByTestId("callout");
    await expect(callout.getByRole("button", { name: "Wake up miner" })).toBeHidden();
  }

  async clickWakeMinerInCallout() {
    const callout = this.page.getByTestId("callout");
    await callout.getByRole("button", { name: "Wake up miner" }).click();
  }
}
