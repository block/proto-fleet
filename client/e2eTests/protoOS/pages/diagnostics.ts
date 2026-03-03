import { expect } from "@playwright/test";
import { DEFAULT_INTERVAL, DEFAULT_TIMEOUT } from "../config/test.config";
import { BasePage } from "./base";

export class DiagnosticsPage extends BasePage {
  async clickFilterButton(filterName: string) {
    await this.page.getByRole("button", { name: filterName }).click();
  }

  async validateTemperaturesInFormat(expectedCount: number, temperaturePattern: RegExp, oppositePattern: RegExp) {
    const page = this.page;
    const textFields = page.locator("div[class*='text-primary']");

    await expect(async () => {
      await expect(textFields.filter({ hasText: temperaturePattern })).toHaveCount(expectedCount);
      await expect(textFields.filter({ hasText: oppositePattern })).toHaveCount(0);
    }).toPass({ timeout: DEFAULT_TIMEOUT, intervals: [DEFAULT_INTERVAL] });
  }
}
