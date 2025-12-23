import { BasePage } from "./base";

export class EditPoolPage extends BasePage {
  async clickAddDefaultMiningPool() {
    await this.clickIn("Add pool", "default-pool");
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
