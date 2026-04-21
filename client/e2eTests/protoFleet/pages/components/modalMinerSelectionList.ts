import { expect, type Locator } from "@playwright/test";
import { DEFAULT_INTERVAL, DEFAULT_TIMEOUT } from "../../config/test.config";

export class ModalMinerSelectionList {
  constructor(private readonly root: Locator) {}

  private get rows(): Locator {
    return this.root.getByTestId("list-row");
  }

  async waitForListToLoad({ allowEmpty = false }: { allowEmpty?: boolean } = {}) {
    if (!allowEmpty) {
      await expect(this.rows).not.toHaveCount(0);
    }

    await expect(async () => {
      const rowCount = await this.rows.count();
      await new Promise((resolve) => setTimeout(resolve, DEFAULT_INTERVAL));
      const rowCountAfterDelay = await this.rows.count();
      // eslint-disable-next-line playwright/prefer-to-have-count -- intentionally non-retrying: verifies count has stabilized
      expect(rowCountAfterDelay).toBe(rowCount);
    }).toPass({ timeout: DEFAULT_TIMEOUT, intervals: [DEFAULT_INTERVAL] });
  }

  async getRowCount(): Promise<number> {
    return await this.rows.count();
  }

  async getSelectableRowIndexes(count: number): Promise<number[]> {
    const rowCount = await this.rows.count();
    const indexes: number[] = [];

    for (let i = 0; i < rowCount; i++) {
      const input = this.rows.nth(i).getByTestId("checkbox").locator("input").first();
      if (!(await input.isDisabled())) {
        indexes.push(i);
      }

      if (indexes.length === count) {
        break;
      }
    }

    return indexes;
  }

  async clickSelectAllCheckbox() {
    await this.root.getByTestId("select-all-checkbox").locator('input[type="checkbox"]').click();
  }

  async selectRowsByIndex(indexes: number[]) {
    for (const index of indexes) {
      const row = this.rows.nth(index);
      await row.scrollIntoViewIfNeeded();
      await row.getByTestId("checkbox").locator("input").first().click();
    }
  }

  async getCellTextByIndex(index: number, cellTestId: string): Promise<string> {
    return (await this.rows.nth(index).getByTestId(cellTestId).innerText()).trim();
  }

  async getVisibleCellTexts(cellTestId: string): Promise<string[]> {
    const cells = this.rows.getByTestId(cellTestId);
    const count = await cells.count();
    const values: string[] = [];

    for (let i = 0; i < count; i++) {
      values.push((await cells.nth(i).innerText()).trim());
    }

    return values;
  }

  async selectRowByCellText(cellTestId: string, text: string) {
    const rowCount = await this.rows.count();

    for (let i = 0; i < rowCount; i++) {
      const row = this.rows.nth(i);
      if ((await row.getByTestId(cellTestId).innerText()).trim() !== text) {
        continue;
      }

      await row.scrollIntoViewIfNeeded();
      await row.getByTestId("checkbox").locator("input").first().click();
      return;
    }

    throw new Error(`Could not find a list row with ${cellTestId}="${text}"`);
  }

  async validateCellTextByIndex(index: number, cellTestId: string, expectedText: string) {
    await expect(this.rows.nth(index).getByTestId(cellTestId)).toHaveText(expectedText);
  }
}
