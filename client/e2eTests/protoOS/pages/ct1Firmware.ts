import { expect, type Page } from "@playwright/test";
import { firmwareTestImage } from "../../../src/protoOS/api/adapters/mdkV2/firmwareTestFixtures";

export class CT1FirmwarePage {
  constructor(readonly page: Page) {}
  async open() {
    await this.page.goto("/settings/general");
    await this.page.getByRole("button", { name: "Upload hashboard firmware", exact: true }).click();
  }
  dialog() {
    return this.page.getByRole("dialog");
  }
  async choose(hardware = "f1") {
    await this.dialog()
      .getByLabel("Hashboard firmware file")
      .setInputFiles({
        name: "f1-app-prod.bin",
        mimeType: "application/octet-stream",
        buffer: Buffer.from(firmwareTestImage("prod", hardware)),
      });
  }
  async install() {
    await this.choose();
    await expect(this.dialog().getByText("Version 9.0.0 · prod · 18 hashboards")).toBeVisible();
    await this.dialog().getByRole("button", { name: "Upload and install" }).click();
  }
  async reopenStatus() {
    await this.page.reload();
    await this.page.getByRole("button", { name: "Hashboard update status", exact: true }).click();
  }
}
