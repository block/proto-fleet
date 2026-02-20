import { expect } from "@playwright/test";
import { BasePage } from "./base";

export class HomePage extends BasePage {
  async clickTab(tabName: string) {
    await this.page.getByTestId(`tab-${tabName}`).click();
  }

  async validateTabHeading(tabName: string, expectedHeading: string) {
    const tab = this.page.getByTestId(`tab-${tabName}`);
    await expect(tab.locator('[class*="heading"]').first()).toHaveText(expectedHeading);
  }

  async validateTabValue(tabName: string, valuePattern: RegExp) {
    const tab = this.page.getByTestId(`tab-${tabName}`);
    const valueLocator = tab.locator('[class*="heading"]').nth(1);
    await expect(valueLocator).toHaveText(valuePattern);
  }

  async validateStatsCount(expectedCount: number) {
    const statsItems = this.page.getByTestId("stats-item");
    await expect(statsItems).toHaveCount(expectedCount);
  }

  async validateStatItem(index: number, expectedLabel: string, valuePattern: RegExp) {
    const statItem = this.page.getByTestId("stats-item").nth(index);
    await expect(statItem.locator('[class*="heading"]').first()).toHaveText(expectedLabel);
    const valueLocator = statItem.locator('[class*="heading"]').nth(1);
    await expect(valueLocator).toHaveText(valuePattern);
  }

  async hoverOverChart() {
    const chart = this.page.getByTestId("line-chart");
    await chart.scrollIntoViewIfNeeded();
    if (this.isMobile) {
      await chart.click();
    } else {
      await chart.hover();
    }
  }

  async validateChartTooltipWithHashboards(expectedValuePattern: RegExp) {
    const tooltip = this.page.locator(".recharts-tooltip-wrapper");
    await expect(tooltip).toBeVisible();
    await expect(tooltip.getByText("Summary")).toBeVisible();
    await expect(tooltip.locator("div[class*='text-primary']").filter({ hasText: expectedValuePattern })).toHaveCount(
      5,
    );
    await expect(tooltip.getByText("Hashboards")).toBeVisible();
  }

  async getFilterButtonBackgroundColors() {
    const buttons = this.page.locator('[data-testid^="chart-filter-hashboard-"]');
    await expect(buttons).toHaveCount(4);

    const colors: string[] = [];
    for (let i = 0; i < 4; i++) {
      const btn = buttons.nth(i);
      const colorEl = btn.locator('[style*="background"]');
      const style = await colorEl.getAttribute("style");
      const match = style?.match(/rgba?\(.+\)/);
      if (match) {
        colors.push(match[0]);
      } else {
        throw new Error(`No background color found for button ${i + 1}`);
      }
    }
    console.warn("Colors: ", colors);
    return colors;
  }

  async validateFilterButtonBorder(testId: string, shouldBeActive: boolean) {
    const btn = this.page.getByTestId(testId);
    const classList = await btn.getAttribute("class");
    if (shouldBeActive) {
      expect(classList).toContain("border-core-primary-fill");
    } else {
      expect(classList).toContain("border-transparent");
    }
  }

  async validateAllFilterButtonBorder(expectedHashboards: string[]) {
    await this.validateFilterButtonBorder("chart-filter-summary", expectedHashboards.includes("S"));
    const allHashboards = ["1", "2", "3", "4"];
    const allActive = allHashboards.every((h) => expectedHashboards.includes(h));
    await this.validateFilterButtonBorder("chart-filter-all-hashboards", allActive);
    for (const h of allHashboards) {
      await this.validateFilterButtonBorder(`chart-filter-hashboard-${h}`, expectedHashboards.includes(h));
    }
  }

  async validateValueInTooltip(expectedHashboards: string[], backgroundColors: string[]) {
    const expectedValuePattern = /(\d+,)?\d+\.\d\sTH\/(S|s)/;
    const tooltip = this.page.locator(".recharts-tooltip-wrapper");
    await expect(tooltip).toBeVisible();

    // Summary
    if (expectedHashboards.includes("S")) {
      const summary = tooltip.getByText("Summary");
      await expect(summary).toBeVisible();
      const summaryValue = summary.locator("xpath=following-sibling::*[1]");
      await expect(summaryValue).toHaveText(expectedValuePattern);
    } else {
      await expect(tooltip.getByText("Summary")).toBeHidden();
    }

    // Hashboards
    const hashboardNumbers = expectedHashboards.filter((h) => ["1", "2", "3", "4"].includes(h));
    if (hashboardNumbers.length > 0) {
      const hashboardsLabel = tooltip.getByText("Hashboards");
      await expect(hashboardsLabel).toBeVisible();
      const hashboardElements = tooltip.locator("//div[text()='Hashboards']/following-sibling::*");
      for (const [i, h] of hashboardNumbers.entries()) {
        const hashboard = hashboardElements.nth(i);
        await expect(hashboard.locator(`//*[text()='${h}']`)).toBeVisible();
        await expect(hashboard).toHaveText(expectedValuePattern);
        const colorIndex = parseInt(h) - 1;
        const expectedColor = backgroundColors[colorIndex];
        console.warn(`Checking hashboard ${h} with expected color ${expectedColor}`);
        await expect(hashboard.locator(`//*[contains(@style, '${expectedColor}')]`)).toBeVisible();
      }
    } else {
      await expect(tooltip.getByText("Hashboards")).toBeHidden();
    }
  }

  async validateFilteredChart(expectedHashboards: string[]) {
    const backgroundColors = await this.getFilterButtonBackgroundColors();
    await this.validateAllFilterButtonBorder(expectedHashboards);
    await this.hoverOverChart();
    if (!expectedHashboards.length) {
      await this.validateValueInTooltip(["S", "1", "2", "3", "4"], backgroundColors);
    } else {
      await this.validateValueInTooltip(expectedHashboards, backgroundColors);
    }
  }

  async validateTemperatureInFormat(temperaturePattern: RegExp) {
    await this.validateTabValue("temperature", temperaturePattern);
  }

  async validateWarnSleepDialog() {
    const dialog = this.page.getByTestId("warn-sleep-dialog");
    await expect(dialog).toBeVisible();
    await expect(dialog).toContainText("Enter sleep mode?");
  }

  async validateWarnWakeUpDialog() {
    const dialog = this.page.getByTestId("warn-wake-up-dialog");
    await expect(dialog).toBeVisible();
    await expect(dialog).toContainText("Wake up miner?");
  }
}
