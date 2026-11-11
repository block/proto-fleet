import { expect, type Page } from "@playwright/test";
import { firmwareTestImage } from "../../../src/protoOS/api/adapters/mdkV2/firmwareTestFixtures";

// Just over the controller's 1 MiB chunk limit, so the simulator's size and digest check covers a second chunk.
const imageBytes = 1024 * 1024 + 4096;

export class CT1FirmwarePage {
  constructor(readonly page: Page) {}
  /** Present only while no update is tracked; a tracked update renames the entry point to its status. */
  uploadButton() {
    return this.page.getByRole("button", { name: "Upload hashboard firmware", exact: true });
  }
  async open() {
    await this.page.goto("/settings/general");
    await this.uploadButton().click();
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
        buffer: Buffer.from(firmwareTestImage("prod", hardware, imageBytes)),
      });
  }
  /** Chooses a valid image and waits for the confirmation summary, without starting the update. */
  async select() {
    await this.choose();
    await expect(this.dialog().getByText("Version 9.0.0 · prod · 18 hashboards")).toBeVisible();
  }
  async confirm() {
    await this.dialog().getByRole("button", { name: "Upload and install" }).click();
  }
  async install() {
    await this.select();
    await this.confirm();
  }
  async reopenStatus() {
    await this.page.reload();
    await this.page.getByRole("button", { name: "Hashboard update status", exact: true }).click();
  }
}
