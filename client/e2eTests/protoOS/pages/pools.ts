import { expect } from "@playwright/test";
import { BasePage } from "./base";

export class PoolsPage extends BasePage {
  async clearAllPoolsViaApi() {
    const response = await this.page.evaluate(async () => {
      const authStorage = window.localStorage.getItem("proto-os-auth");
      if (!authStorage) {
        throw new Error("proto-os-auth is missing from localStorage");
      }

      const parsedStorage = JSON.parse(authStorage) as {
        state?: {
          auth?: {
            authTokens?: {
              accessToken?: {
                value?: string;
              };
            };
          };
        };
      };

      const accessToken = parsedStorage.state?.auth?.authTokens?.accessToken?.value;
      if (!accessToken) {
        throw new Error("Access token is missing from proto-os-auth");
      }

      const request = await fetch("/api/v1/pools", {
        method: "POST",
        headers: {
          Authorization: `Bearer ${accessToken}`,
          "Content-Type": "application/json",
        },
        body: JSON.stringify([]),
      });

      return {
        ok: request.ok,
        status: request.status,
        statusText: request.statusText,
        body: await request.text(),
      };
    });

    expect(
      response.ok,
      `Failed to clear pools: ${response.status} ${response.statusText} ${response.body}`,
    ).toBeTruthy();
  }

  async validateNoPoolsEmptyState() {
    await expect(this.page.getByText("Pools", { exact: true })).toBeVisible();
    await expect(this.page.getByText("Add up to 3 pools for your miner.")).toBeVisible();
    await expect(this.page.getByTestId("add-pool-button")).toBeVisible();
  }

  async validateNoMiningPoolsCalloutHidden() {
    await expect(this.page.getByTestId("callout").getByText("No mining pools configured.")).toBeHidden();
  }

  async validatePoolModalOpened() {
    await expect(this.page.getByTestId("modal")).toBeVisible();
  }

  async inputPoolName(name: string, poolIndex: number = 0) {
    await this.page.getByTestId(`pool-name-${poolIndex}-input`).fill(name);
  }

  async inputPoolUrl(url: string, poolIndex: number = 0) {
    await this.page.getByTestId(`url-${poolIndex}-input`).fill(url);
  }

  async inputPoolUsername(username: string, poolIndex: number = 0) {
    await this.page.getByTestId(`username-${poolIndex}-input`).fill(username);
  }

  async inputPoolPassword(password: string, poolIndex: number = 0) {
    await this.page.getByTestId(`password-${poolIndex}-input`).fill(password);
  }

  async clickTestConnection() {
    await this.page.locator(`//button//*[text()='Test connection']`).click();
  }

  async validateConnectionSuccessful() {
    await expect(
      this.page.locator(`//div[@data-testid='pool-connected-callout' and not(contains(@class,'hidden'))]`),
    ).toBeVisible();
  }

  async clickSave() {
    await this.clickButton("Save");
  }

  async clickAddPool() {
    await this.clickButton("Add pool");
  }

  async clickAddAnotherPool() {
    await this.clickButton("Add another pool");
  }

  async validateUrlValidationError(poolIndex: number, message: string) {
    await expect(this.page.getByTestId(`url-${poolIndex}-input-validation-error`)).toBeVisible();
    await expect(this.page.getByTestId(`url-${poolIndex}-input-validation-error`)).toHaveText(message);
  }

  async validateConnectionFailed() {
    await expect(this.page.getByTestId("pool-not-connected-callout")).toBeVisible();
    await this.validateTextInModal("We couldn't connect with your pool. Review your pool details and try again.");
  }

  async closePoolNotConnectedCallout() {
    await this.page.getByTestId("pool-not-connected-callout").getByRole("button").click();
  }

  async validateSaveButtonDisabled() {
    await expect(this.page.getByTestId("modal").getByRole("button", { name: "Save" })).toBeDisabled();
  }

  async validateSaveButtonEnabled() {
    await expect(this.page.getByTestId("modal").getByRole("button", { name: "Save" })).toBeEnabled();
  }

  async validateCalloutWithText(text: string) {
    await expect(this.page.getByTestId("callout")).toBeVisible();
    await expect(this.page.getByTestId("callout").getByText(text)).toBeVisible();
  }

  async closeCallout() {
    await this.page.getByTestId("callout").getByRole("button").click();
  }

  async closeModal() {
    await this.page.getByTestId("modal").getByLabel("Close dialog").click();
    await this.validateModalIsClosed();
  }

  async clickMiningPoolButton() {
    await this.clickButton("Mining Pool");
  }

  async validatePoolInfoPopoverVisible() {
    await expect(this.page.getByTestId("pool-info-popover")).toBeVisible();
  }

  async validateTitleInPopover(title: string) {
    await expect(
      this.page.getByTestId("pool-info-popover").locator(`//*[contains(@class,'heading')][text()="${title}"]`),
    ).toBeVisible();
  }

  async validateTextInPopover(text: string) {
    await expect(this.page.getByTestId("pool-info-popover").getByText(text)).toBeVisible();
  }

  async validateExactTextInPopover(text: string) {
    await expect(this.page.getByTestId("pool-info-popover").getByText(text, { exact: true })).toBeVisible();
  }

  async clickViewMiningPools() {
    await this.page.getByTestId("pool-info-popover").getByRole("button", { name: "View mining pools" }).click();
  }

  async validatePoolRowCount(expectedCount: number) {
    const poolRows = this.page.getByTestId("pool-row");
    await expect(poolRows).toHaveCount(expectedCount);
  }

  async validatePoolRowDetails(poolIndex: number, poolName: string, poolUrl: string) {
    const poolRows = this.page.getByTestId("pool-row");
    const targetRow = poolRows.nth(poolIndex);

    await expect(targetRow).toBeVisible();
    await expect(targetRow.getByText(poolName)).toBeVisible();
    await expect(targetRow.getByTestId(`pool-${poolIndex}-saved-url`)).toHaveText(poolUrl);
  }
}
