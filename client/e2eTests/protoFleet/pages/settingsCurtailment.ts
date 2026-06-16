import { expect, Locator } from "@playwright/test";
import { DEFAULT_INTERVAL, DEFAULT_TIMEOUT } from "../config/test.config";
import { BasePage } from "./base";

export type ResponseProfileFormInput = {
  name: string;
  curtailBatchSize: string;
  curtailBatchIntervalSec: string;
  restoreBatchSize: string;
  restoreBatchIntervalSec: string;
};

export type CurtailmentSourceFormInput = {
  name: string;
  brokerPrimaryHost: string;
  brokerSecondaryHost: string;
  brokerPort: string;
  topic: string;
  username: string;
  password: string;
};

export class SettingsCurtailmentPage extends BasePage {
  async validateCurtailmentPageOpened() {
    await expect(this.page).toHaveURL(/.*\/settings\/curtailment/);
    await expect(this.page.getByTestId("settings-curtailment-page")).toBeVisible();
    await this.validateTitle("Curtailment");
    await this.validateButtonIsVisible("Create profile");
    await this.validateButtonIsVisible("Add source");
  }

  async openCreateResponseProfile() {
    await this.clickButton("Create profile");
    await expect(this.page.getByTestId("full-screen-two-pane-modal")).toBeVisible();
    await expect(this.page.getByText("Create response profile")).toBeVisible();
  }

  async fillResponseProfile(values: ResponseProfileFormInput) {
    await this.page.getByLabel("Profile name").fill(values.name);
    await this.page.getByTestId("response-profile-curtail-batch-size").fill(values.curtailBatchSize);
    await this.page.getByTestId("response-profile-curtail-batch-interval").fill(values.curtailBatchIntervalSec);
    await this.page.getByTestId("response-profile-restore-batch-size").fill(values.restoreBatchSize);
    await this.page.getByTestId("response-profile-restore-batch-interval").fill(values.restoreBatchIntervalSec);
  }

  async saveResponseProfile() {
    await this.clickButton("Save profile");
    await expect(this.page.getByTestId("full-screen-two-pane-modal")).toBeHidden();
  }

  async validateResponseProfileVisible(name: string) {
    const card = this.getResponseProfileCard(name);
    await expect(card).toBeVisible();
    await expect(card.getByText("100% reduction", { exact: true })).toBeVisible();
    await expect(card.getByText("Whole fleet", { exact: true })).toBeVisible();
  }

  async deleteResponseProfilesByPrefix(prefix: string) {
    await this.waitForResponseProfilesToLoad();

    const profileNames = await this.getResponseProfileNamesByPrefix(prefix);
    for (const profileName of profileNames) {
      await this.deleteResponseProfile(profileName);
    }
  }

  async openAddSource() {
    await this.clickButton("Add source");
    await expect(this.page.getByTestId("curtailment-source-modal")).toBeVisible();
    await expect(
      this.page.getByTestId("curtailment-source-modal").getByText("Add source", { exact: true }),
    ).toBeVisible();
  }

  async fillSource(values: CurtailmentSourceFormInput) {
    await this.page.getByLabel("Configuration name").fill(values.name);
    await this.page.getByLabel("Broker host 1").fill(values.brokerPrimaryHost);
    await this.page.getByLabel("Broker host 2").fill(values.brokerSecondaryHost);
    await this.page.getByLabel("Port").fill(values.brokerPort);
    await this.page.getByLabel("Topic").fill(values.topic);
    await this.page.getByLabel("Username").fill(values.username);
    await this.page.locator("#source-password").fill(values.password);
  }

  async saveSource() {
    await this.page.getByTestId("curtailment-source-modal").getByRole("button", { name: "Save", exact: true }).click();
    await expect(this.page.getByTestId("curtailment-source-modal")).toBeHidden();
  }

  async validateSourceVisible(name: string) {
    const row = this.getSourceRow(name);
    await expect(row).toBeVisible();
    await expect(row.getByText("-", { exact: true }).first()).toBeVisible();
  }

  async deleteSourcesByPrefix(prefix: string) {
    await this.waitForSourcesToLoad();

    const sourceNames = await this.getSourceNamesByPrefix(prefix);
    for (const sourceName of sourceNames) {
      await this.disableSource(sourceName);
      await this.deleteSource(sourceName);
    }
  }

  private getResponseProfileCard(name: string): Locator {
    return this.page.getByTestId("response-profile-card").filter({ hasText: name });
  }

  private async waitForResponseProfilesToLoad() {
    const cards = this.page.getByTestId("response-profile-card");
    const emptyState = this.page.getByText("No response profiles configured", { exact: true });

    await expect(this.page.getByRole("button", { name: "Create profile", exact: true })).toBeVisible();
    await this.waitForLoadedSection(cards, emptyState, "curtailment-response-profiles-loading");
    await this.waitForStableCount(cards, emptyState);
  }

  private async getResponseProfileNamesByPrefix(prefix: string): Promise<string[]> {
    const cards = await this.page.getByTestId("response-profile-card").all();
    const profileNames: string[] = [];

    for (const card of cards) {
      const name = (await card.getByTestId("response-profile-name").textContent())?.trim();
      if (name?.startsWith(prefix)) {
        profileNames.push(name);
      }
    }

    return profileNames;
  }

  private async deleteResponseProfile(name: string) {
    const card = this.getResponseProfileCard(name);
    await expect(card).toBeVisible();
    await card.getByRole("button", { name: "Edit", exact: true }).click();
    await expect(this.page.getByText("Edit response profile")).toBeVisible();
    await this.clickResponseProfileDelete();
    await expect(this.page.getByTestId("full-screen-two-pane-modal")).toBeHidden();
    await expect(card).toHaveCount(0);
  }

  private async clickResponseProfileDelete() {
    if (this.isMobile) {
      await this.page.getByTestId("overflow-menu-trigger").click();
      await this.page.getByRole("button", { name: "Delete", exact: true }).click();
      return;
    }

    await this.clickButton("Delete");
  }

  private getSourceRow(name: string): Locator {
    return this.page.getByTestId("list-row").filter({
      has: this.page.getByTestId("name").getByText(name, { exact: true }),
    });
  }

  private async waitForSourcesToLoad() {
    const rows = this.page.getByTestId("list-row");
    const emptyState = this.page.getByText("No sources configured", { exact: true });

    await expect(this.page.getByRole("button", { name: "Add source", exact: true })).toBeVisible();
    await this.waitForLoadedSection(rows, emptyState, "curtailment-sources-loading");
    await this.waitForStableCount(rows, emptyState);
  }

  private async getSourceNamesByPrefix(prefix: string): Promise<string[]> {
    const rows = await this.page.getByTestId("list-row").all();
    const sourceNames: string[] = [];

    for (const row of rows) {
      const name = (await row.getByTestId("name").textContent())?.trim();
      if (name?.startsWith(prefix)) {
        sourceNames.push(name);
      }
    }

    return sourceNames;
  }

  private async deleteSource(name: string) {
    const row = this.getSourceRow(name);
    await expect(row).toBeVisible();
    await row.click();
    await expect(
      this.page.getByTestId("curtailment-source-modal").getByText("Edit source", { exact: true }),
    ).toBeVisible();
    await this.page
      .getByTestId("curtailment-source-modal")
      .getByRole("button", { name: "Delete", exact: true })
      .click();
    await expect(this.page.getByTestId("curtailment-source-modal")).toBeHidden();
    await expect(row).toHaveCount(0);
  }

  private async disableSource(name: string) {
    const row = this.getSourceRow(name);
    await expect(row).toBeVisible();

    const enabledSwitch = row.locator('input[type="checkbox"]');
    if (!(await enabledSwitch.isChecked())) {
      return;
    }

    await row.locator('input[type="checkbox"] + span').click();
    await expect(enabledSwitch).not.toBeChecked();
  }

  private async waitForStableCount(items: Locator, emptyState: Locator) {
    if (await emptyState.isVisible().catch(() => false)) {
      return;
    }

    await expect(async () => {
      const itemCount = await items.count();
      await new Promise((resolve) => setTimeout(resolve, DEFAULT_INTERVAL));
      const itemCountAfterDelay = await items.count();
      // eslint-disable-next-line playwright/prefer-to-have-count -- intentionally non-retrying: verifies count has stabilized
      expect(itemCountAfterDelay).toBe(itemCount);
    }).toPass({ timeout: DEFAULT_TIMEOUT, intervals: [DEFAULT_INTERVAL] });
  }

  private async waitForLoadedSection(items: Locator, emptyState: Locator, loadingTestId: string) {
    await expect(this.page.getByTestId(loadingTestId)).toBeHidden();

    await expect(async () => {
      const itemCount = await items.count();
      const isEmpty = await emptyState.isVisible().catch(() => false);

      expect(itemCount > 0 || isEmpty).toBe(true);
    }).toPass({ timeout: DEFAULT_TIMEOUT, intervals: [DEFAULT_INTERVAL] });
  }
}
