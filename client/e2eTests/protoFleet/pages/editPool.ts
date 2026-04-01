import { expect } from "@playwright/test";
import { BasePage } from "./base";

export class EditPoolPage extends BasePage {
  async clickAddPoolButton() {
    await this.page.getByTestId("add-pool-button").click();
  }

  async clickAddAnotherPoolButton() {
    await this.page.getByTestId("add-another-pool-button").click();
  }

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
    await this.clickSaveInModal();
  }

  async clickAddNewPool() {
    await this.clickIn("Add new pool", "modal");
  }

  async clickAssignToXMiners(count: number | Promise<number>) {
    const minerCount = await Promise.resolve(count);
    const buttonText = `Assign to ${minerCount} miner${minerCount === 1 ? "" : "s"}`;
    await this.clickButton(buttonText);
  }

  async getPoolNameByIndex(index: number): Promise<string> {
    const poolRow = this.page.getByTestId(`pool-row-${index}`);
    return await poolRow.getByTestId("pool-name").innerText();
  }

  async getPoolUrlByIndex(index: number): Promise<string> {
    const poolRow = this.page.getByTestId(`pool-row-${index}`);
    return await poolRow.getByTestId("pool-url").innerText();
  }

  async validatePoolCount(count: number) {
    const poolRows = this.page.getByTestId(/^pool-row-\d+$/);
    await expect(poolRows).toHaveCount(count);
  }

  async validatePoolByIndex(index: number, name: string, url: string) {
    const poolRow = this.page.getByTestId(`pool-row-${index}`);
    await expect(poolRow.getByTestId("pool-name")).toHaveText(name);
    await expect(poolRow.getByTestId("pool-url")).toHaveText(url);
  }

  async reorderPoolByDragging(fromIndex: number, toIndex: number) {
    const sourceHandle = this.page.getByTestId(`pool-row-${fromIndex}`).getByTestId("reorder-handle");
    const targetHandle = this.page.getByTestId(`pool-row-${toIndex}`).getByTestId("reorder-handle");
    await sourceHandle.dragTo(targetHandle, { steps: 20 });
  }

  async removeAllPools() {
    const poolRows = this.page.getByTestId(/^pool-row-\d+$/);
    const poolCount = await poolRows.count();

    for (let i = 0; i < poolCount; i++) {
      const firstRow = poolRows.first();
      await firstRow.getByRole("button", { name: "Pool actions", exact: true }).click();
      await this.clickButton("Remove");
      await expect(poolRows).toHaveCount(poolCount - 1 - i);
    }
    await expect(poolRows).toHaveCount(0);
  }
}
