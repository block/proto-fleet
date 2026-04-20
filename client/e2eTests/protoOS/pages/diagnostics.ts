import { expect } from "@playwright/test";
import { DEFAULT_INTERVAL, DEFAULT_TIMEOUT } from "../config/test.config";
import { BasePage } from "./base";

export class DiagnosticsPage extends BasePage {
  async clickFilterButton(filterName: string) {
    await this.page.getByTestId("segmented-control").getByRole("button", { name: filterName }).click();
  }

  private section(sectionTestIdName: string) {
    return this.page.getByTestId(`component-section-${sectionTestIdName}`);
  }

  async validateAllSectionsVisible(sectionTestIdNames: string[]) {
    for (const sectionTestIdName of sectionTestIdNames) {
      await expect(this.section(sectionTestIdName)).toBeVisible();
    }
  }

  async validateOnlySectionVisible(selectedSectionTestIdName: string, allSectionTestIdNames: string[]) {
    await expect(this.section(selectedSectionTestIdName)).toBeVisible();

    for (const sectionTestIdName of allSectionTestIdNames) {
      if (sectionTestIdName === selectedSectionTestIdName) continue;
      await expect(this.section(sectionTestIdName)).toBeHidden();
    }
  }

  async validateCardCountInSection(sectionTestIdName: string, expectedCardCount: number) {
    await expect(this.section(sectionTestIdName).getByTestId("card")).toHaveCount(expectedCardCount);
  }

  private cardInSection(sectionTestIdName: string, index: number) {
    return this.section(sectionTestIdName).getByTestId("card").nth(index);
  }

  async cardHasMoreInfoButton(sectionTestIdName: string, cardIndex: number): Promise<boolean> {
    const card = this.cardInSection(sectionTestIdName, cardIndex);
    return (await card.getByRole("button", { name: "More info" }).count()) > 0;
  }

  async validateEmptySlotCard(sectionTestIdName: string, cardIndex: number, expectedText: string | RegExp) {
    const card = this.cardInSection(sectionTestIdName, cardIndex);
    await expect(card.getByText(expectedText)).toBeVisible();
    await expect(card.getByRole("button", { name: "More info" })).toHaveCount(0);
  }

  async validateCardInfoOrEmptySlot(
    sectionTestIdName: string,
    cardIndex: number,
    expected: {
      metrics: Array<{ label: string | RegExp; valuePattern: RegExp }>;
      metadata: Array<{ label: string }>;
      emptySlotText: RegExp;
      allowExtraMetadataRows?: boolean;
    },
  ): Promise<"info" | "empty"> {
    const hasMoreInfo = await this.cardHasMoreInfoButton(sectionTestIdName, cardIndex);

    if (!hasMoreInfo) {
      await this.validateEmptySlotCard(sectionTestIdName, cardIndex, expected.emptySlotText);
      return "empty";
    }

    await this.openMoreInfoForCard(sectionTestIdName, cardIndex);
    await this.validateStatusModalMetrics(expected.metrics);
    await this.validateStatusModalMetadataRows(expected.metadata, { allowExtraRows: expected.allowExtraMetadataRows });
    await this.closeStatusModal();
    return "info";
  }

  async openMoreInfoForCard(sectionTestIdName: string, cardIndex: number) {
    const card = this.cardInSection(sectionTestIdName, cardIndex);
    await card.getByRole("button", { name: "More info" }).click();
    await this.validateModalIsOpen();
  }

  async closeStatusModal() {
    await this.clickIn("Done", "modal");
    await this.validateModalIsClosed();
  }

  async validateStatusModalMetrics(expected: Array<{ label: string | RegExp; valuePattern: RegExp }>) {
    const metrics = this.page.getByTestId("status-modal-metric");
    await expect(metrics).toHaveCount(expected.length);

    for (const { label, valuePattern } of expected) {
      const labelLocator =
        typeof label === "string"
          ? this.page.getByTestId("status-modal-metric-label").getByText(label, { exact: true })
          : this.page.getByTestId("status-modal-metric-label").getByText(label);

      const metric = metrics.filter({
        has: labelLocator,
      });
      await expect(metric).toHaveCount(1);

      await expect(metric.first().getByTestId("status-modal-metric-label")).toHaveText(label);
      await expect(metric.first().getByTestId("status-modal-metric-value")).toHaveText(valuePattern);
    }
  }

  async validateStatusModalMetadataRows(expected: Array<{ label: string }>, options?: { allowExtraRows?: boolean }) {
    const rows = this.page.getByTestId("status-modal-metadata-row");

    if (options?.allowExtraRows) {
      await expect(async () => {
        const count = await rows.count();
        expect(count).toBeGreaterThanOrEqual(expected.length);
      }).toPass({ timeout: DEFAULT_TIMEOUT, intervals: [DEFAULT_INTERVAL] });
    } else {
      await expect(rows).toHaveCount(expected.length);
    }

    for (const { label } of expected) {
      const row = rows.filter({
        has: this.page.getByTestId("status-modal-metadata-label").getByText(label, { exact: true }),
      });
      await expect(row).toHaveCount(1);

      await expect(row.first().getByTestId("status-modal-metadata-label")).toHaveText(label);
      await expect(row.first().getByTestId("status-modal-metadata-value")).toHaveText(/\S+/);
    }
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
