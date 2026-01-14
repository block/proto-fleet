import { BasePage } from "./base";

export class EditPoolPage extends BasePage {
  async clickAddDefaultMiningPool() {
    await this.clickIn("Add pool", "default-pool");
  }

  async clickAddBackupPoolOne() {
    await this.clickIn("Add pool", "backup-pool-1");
  }

  async clickPoolRowByName(name: string) {
    await this.page.getByText(name).click();
  }

  async clickSavePoolChoice() {
    await this.clickIn("Save", "modal");
  }

  async clickAddNewPool() {
    await this.clickIn("Add new pool", "modal");
  }

  async clickAssignToXMiners(count: number | Promise<number>) {
    const minerCount = await Promise.resolve(count);
    const buttonText = `Assign to ${minerCount} miner${minerCount === 1 ? "" : "s"}`;
    await this.click(buttonText);
  }
}
