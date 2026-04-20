import { expect } from "@playwright/test";
import { DEFAULT_INTERVAL, DEFAULT_TIMEOUT } from "../config/test.config";
import { BasePage } from "./base";

export class SettingsFirmwarePage extends BasePage {
  async validateFirmwarePageOpened() {
    await expect(this.page).toHaveURL(/.*\/settings\/firmware/);
    await this.validateTitle("Firmware");
  }

  async clickUploadFirmware() {
    await this.clickButton("Upload firmware");
    await this.validateTitleInModal("Upload firmware");
  }

  async uploadFirmwareFile(fileName: string, fileContents: string) {
    await this.page.getByTestId("firmware-file-input").setInputFiles({
      name: fileName,
      mimeType: "application/octet-stream",
      buffer: Buffer.from(fileContents),
    });
  }

  async clickDoneInUploadDialog() {
    await this.clickIn("Done", "modal");
  }

  async validateFirmwareFileVisible(fileName: string) {
    await expect(this.page.getByTestId("list-body").locator("tr").filter({ hasText: fileName })).toBeVisible();
  }

  async deleteAllFirmwareFilesIfAny() {
    const emptyState = this.page.getByText("No firmware files uploaded.", { exact: true });
    const firmwareRows = this.page.getByTestId("list-body").locator("tr");
    const loadingState = this.page.getByText("Loading firmware files...", { exact: true });
    const deleteAllButton = this.page.getByRole("button", { name: "Delete all", exact: true }).first();

    if (await loadingState.isVisible().catch(() => false)) {
      await expect(loadingState).toBeHidden();
    }

    await expect(async () => {
      const emptyVisible = await emptyState.isVisible().catch(() => false);
      const hasRows = (await firmwareRows.count()) > 0;

      expect(emptyVisible || hasRows).toBeTruthy();
    }).toPass({ timeout: DEFAULT_TIMEOUT, intervals: [DEFAULT_INTERVAL] });

    if (await emptyState.isVisible().catch(() => false)) {
      return;
    }

    await expect(deleteAllButton).toBeEnabled();
    await deleteAllButton.click();
    const deleteAllDialog = this.page.getByTestId("delete-all-firmware-dialog");
    await deleteAllDialog.getByRole("button", { name: "Delete all" }).click();
    await expect(deleteAllDialog).toBeHidden();
    await expect(deleteAllButton).toBeDisabled();
    await expect(emptyState).toBeVisible();
  }
}
