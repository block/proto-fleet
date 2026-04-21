import { expect } from "@playwright/test";
import { DEFAULT_INTERVAL, DEFAULT_TIMEOUT } from "../config/test.config";
import { BasePage } from "./base";

export class SettingsApiKeysPage extends BasePage {
  async waitForApiKeysListToLoad() {
    const rows = this.page.getByTestId("list-body").getByTestId("list-row");
    const emptyState = this.page.getByText(
      "No API keys yet. Create your first key to enable programmatic access to the Fleet API.",
    );

    await expect(this.page.getByText("Loading API keys...")).toBeHidden();
    await expect(this.page.getByRole("button", { name: "Create API key" })).toBeVisible();

    await expect(async () => {
      if (await emptyState.isVisible().catch(() => false)) {
        return;
      }

      const rowCount = await rows.count();
      if (rowCount === 0) {
        throw new Error("API keys list is still loading");
      }

      expect(rowCount).toBeGreaterThan(0);
      await new Promise((resolve) => setTimeout(resolve, DEFAULT_INTERVAL));

      if (await emptyState.isVisible().catch(() => false)) {
        return;
      }

      const rowCountAfterDelay = await rows.count();
      // eslint-disable-next-line playwright/prefer-to-have-count -- intentionally non-retrying: verifies count has stabilized
      expect(rowCountAfterDelay).toBe(rowCount);
    }).toPass({ timeout: DEFAULT_TIMEOUT, intervals: [DEFAULT_INTERVAL] });
  }

  async validateApiKeysPageOpened() {
    await expect(this.page).toHaveURL(/.*\/settings\/api-keys/);
    await this.validateTitle("API Keys");
    await this.validateButtonIsVisible("Create API key");
  }

  async clickCreateApiKey() {
    await this.clickButton("Create API key");
  }

  async inputApiKeyName(name: string) {
    await this.page.locator("#api-key-name").fill(name);
  }

  async clickCreateInModal() {
    await this.page.getByTestId("modal").getByRole("button", { name: "Create", exact: true }).click();
  }

  async validateApiKeyNameRequired() {
    await this.validateTextInModal("Name is required");
  }

  async openExpirationDatePicker() {
    const trigger = this.page.getByTestId("api-key-expires-trigger");

    if ((await trigger.getAttribute("aria-expanded")) !== "true") {
      await trigger.click();
    }

    await expect(trigger).toHaveAttribute("aria-expanded", "true");
  }

  async validateExpirationDayDisabled(day: number) {
    await expect(this.page.getByTestId(`api-key-expires-calendar-day-${day}`)).toBeDisabled();
  }

  async selectExpirationDate(date: Date) {
    const today = new Date();
    const monthDelta = (date.getFullYear() - today.getFullYear()) * 12 + (date.getMonth() - today.getMonth());
    const calendar = this.page.getByTestId("api-key-expires-calendar");

    await this.openExpirationDatePicker();
    await expect(calendar).toBeVisible();

    for (let i = 0; i < Math.max(monthDelta, 0); i += 1) {
      await this.page.getByTestId("api-key-expires-calendar-next-month").click();
    }

    for (let i = 0; i < Math.max(-monthDelta, 0); i += 1) {
      await this.page.getByTestId("api-key-expires-calendar-prev-month").click();
    }

    await expect(calendar).toBeVisible();
    await this.page.getByTestId(`api-key-expires-calendar-day-${date.getDate()}`).click();
  }

  async validateApiKeyCreated() {
    await expect(this.page.getByText("API key created")).toBeVisible();
    await expect(this.page.getByTestId("api-key-value")).not.toHaveText("");
  }

  async clickDone() {
    await this.clickButton("Done");
  }

  async validateApiKeyVisible(name: string) {
    await expect(this.getApiKeyRow(name)).toBeVisible();
  }

  async validateApiKeyHasNoExpiration(name: string) {
    await expect(this.getApiKeyRow(name).getByTestId("expiresAt")).toHaveText("Never");
  }

  async validateApiKeyHasExpiration(name: string) {
    await expect(this.getApiKeyRow(name).getByTestId("expiresAt")).not.toHaveText("Never");
  }

  async clickRevokeApiKey(name: string) {
    await this.getApiKeyRow(name).getByRole("button", { name: "Revoke", exact: true }).click();
  }

  async confirmRevokeApiKey() {
    await this.clickButton("Revoke key");
  }

  async validateApiKeyNotVisible(name: string) {
    await expect(this.getApiKeyRow(name)).toHaveCount(0);
  }

  async deleteApiKeysByPrefix(prefix: string) {
    await this.waitForApiKeysListToLoad();

    const rows = await this.page.getByTestId("list-body").getByTestId("list-row").all();
    const keyNames: string[] = [];

    for (const row of rows) {
      const name = (await row.getByTestId("name").textContent())?.trim();
      if (name?.startsWith(prefix)) {
        keyNames.push(name);
      }
    }

    for (const keyName of keyNames) {
      await this.clickRevokeApiKey(keyName);
      await this.confirmRevokeApiKey();
      await this.validateApiKeyNotVisible(keyName);
    }
  }

  private getApiKeyRow(name: string) {
    return this.page
      .getByTestId("list-body")
      .getByTestId("list-row")
      .filter({
        has: this.page.getByTestId("name").getByText(name, { exact: true }),
      });
  }
}
