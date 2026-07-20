import { expect } from "@playwright/test";
import { DEFAULT_INTERVAL, DEFAULT_TIMEOUT } from "../../../protoFleet/config/test.config";
import { BasePage } from "../base";

export class HeaderComponent extends BasePage {
  async clickPowerButton() {
    await this.page.getByTestId("power-button").click();
  }

  async clickPowerPopoverButton(buttonText: string) {
    await expect(async () => {
      const button = this.page.getByTestId("power-popover").getByRole("button", { name: buttonText });
      await expect(button).toBeVisible({ timeout: DEFAULT_INTERVAL });
      await button.click({ timeout: DEFAULT_INTERVAL });
    }).toPass({ timeout: DEFAULT_TIMEOUT, intervals: [DEFAULT_INTERVAL] });
  }

  async validateWarnRebootDialog() {
    const dialog = this.page.getByTestId("warn-reboot-dialog");
    await expect(dialog).toBeVisible();
    await expect(dialog).toContainText("Reboot miner?");
  }

  async clickRebootMinerInDialog() {
    const dialog = this.page.getByTestId("warn-reboot-dialog");
    await dialog.getByRole("button", { name: "Reboot miner" }).click();
  }

  async clickMinerStatusButton(status: string = "Sleeping") {
    const header = this.page.getByTestId("page-header");
    await header.getByRole("button", { name: status }).click();
  }

  async validateMinerStatus(status: string) {
    const header = this.page.getByTestId("page-header");
    await expect(header.getByRole("button", { name: status })).toBeVisible();
  }

  async openGlobalActionsMenu() {
    await this.page.getByTestId("global-actions-widget").click();
    await expect(this.page.getByTestId("global-actions-popover")).toBeVisible();
  }

  async clickBlinkLeds() {
    await this.page.getByTestId("global-action-blink-leds").click();
  }

  async clickDownloadLogs() {
    await this.page.getByTestId("global-action-download-logs").click();
  }

  async validateGlobalActionsMenuClosed() {
    await expect(this.page.getByTestId("global-actions-popover")).toHaveCount(0);
  }

  async openPowerTargetPopover() {
    await this.page.getByTestId("power-target-widget").click();
    await expect(this.page.getByTestId("power-target-popover")).toBeVisible();
  }

  async clickCustomPowerTargetMode() {
    await this.page.getByTestId("power-target-mode-custom").click();
  }

  async inputCustomPowerTargetKw(valueKw: number) {
    await this.page.getByTestId("power-target-input").fill(String(valueKw));
  }

  async clickApplyPowerTarget() {
    await this.page.getByTestId("power-target-apply-button").click();
  }

  async validatePowerTargetWidgetText(expectedText: string) {
    await expect(this.page.getByTestId("power-target-widget")).toContainText(expectedText);
  }

  async validateFirmwareStatusWidgetText(expectedText: string | RegExp) {
    await expect(this.page.getByTestId("firmware-status-widget")).toHaveText(expectedText);
  }

  async validateFirmwareStatusWidgetHidden() {
    await expect(this.page.getByTestId("firmware-status-widget")).toBeHidden();
  }

  async openFirmwareStatusModal() {
    await this.page.getByTestId("firmware-status-widget").click();
    await expect(this.page.getByTestId("firmware-status-modal")).toBeVisible();
  }

  async validateFirmwareStatusModalTitle(expectedTitle: string) {
    await expect(this.page.getByTestId("firmware-status-modal")).toContainText(expectedTitle);
  }

  async validateFirmwareStatusModalVersionLabel(label: "Current Version:" | "New Version:", expectedValue: string) {
    await expect(this.page.getByTestId("firmware-status-modal").getByText(`${label} ${expectedValue}`)).toBeVisible();
  }

  async clickFirmwareStatusModalInstallButton() {
    await this.page.getByTestId("firmware-status-modal").getByRole("button", { name: "Install" }).click();
  }

  async clickFirmwareStatusModalRebootButton() {
    await this.page.getByTestId("firmware-status-modal-reboot-button").click();
  }
}
