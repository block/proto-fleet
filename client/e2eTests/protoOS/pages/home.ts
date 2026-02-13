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
    await expect(tooltip.locator("[class*='text-primary']").filter({ hasText: expectedValuePattern })).toHaveCount(5);
    await expect(tooltip.getByText("Hashboards")).toBeVisible();
  }

  async validateChartTooltipSummaryOnly(expectedValuePattern: RegExp) {
    const tooltip = this.page.locator(".recharts-tooltip-wrapper");
    await expect(tooltip).toBeVisible();
    await expect(tooltip.getByText("Summary")).toBeVisible();
    await expect(tooltip.locator("[class*='text-primary']").filter({ hasText: expectedValuePattern })).toBeVisible();
  }
}
