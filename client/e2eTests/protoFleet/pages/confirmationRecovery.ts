import { expect } from "@playwright/test";
import { BasePage } from "./base";

export class ConfirmationRecoveryPage extends BasePage {
  async retry(password: string) {
    await this.page.getByRole("button", { name: "Review update", exact: true }).click();
    const recovery = this.page.getByRole("dialog", { name: "Resolve update confirmation", exact: true });
    await expect(recovery.getByLabel("Your password", { exact: true })).toBeEmpty();
    await recovery.getByLabel("Your password", { exact: true }).fill(password);
    await recovery.getByRole("button", { name: "Retry confirmation", exact: true }).click();
    await expect(recovery).toBeHidden();
  }

  async validateOutcome(expired: boolean, screenshotPath: string) {
    if (expired) {
      const terminal = this.page.getByRole("dialog", { name: "Update not submitted", exact: true });
      await expect(terminal).toContainText("expired without being accepted");
      await expect(terminal.getByRole("button", { name: "Retry confirmation" })).toHaveCount(0);
      await expect(terminal).toHaveCSS("opacity", "1");
      await expect(terminal.locator("..")).toHaveCSS("opacity", "1");
      await this.page.screenshot({ path: screenshotPath });
      await terminal.getByRole("button", { name: "Close", exact: true }).click();
    } else {
      await this.page.getByRole("button", { name: "View details", exact: true }).click();
      await expect(this.page.getByRole("dialog", { name: "Pool configuration", exact: true })).toContainText(
        "Verified",
      );
    }
  }
}
