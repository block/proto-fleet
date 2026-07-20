import { expect } from "@playwright/test";
import { DEFAULT_INTERVAL, DEFAULT_TIMEOUT } from "../config/test.config";
import { BasePage } from "./base";

type HostedMinerMetadata = {
  minerName: string;
  ipAddress?: string;
  firmwareVersion?: string;
  macAddress?: string;
};

const escapeForRegex = (value: string) => value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");

export class SingleMinerPage extends BasePage {
  constructor(page: ConstructorParameters<typeof BasePage>[0], isMobile: boolean = false) {
    super(page, isMobile);
  }

  private closeButtonLabel() {
    return this.page.getByTestId("single-miner-close-button").locator(":scope + span");
  }

  private async ensureNavigationVisible() {
    const navigation = this.page.getByTestId("navigation");
    if (await navigation.isVisible().catch(() => false)) {
      return;
    }

    if (this.isMobile) {
      await this.page.getByTestId("navigation-menu-button").click();
      await expect(navigation).toBeVisible();
    }
  }

  async validateSingleMinerSurfaceOpened() {
    await expect(this.page.getByTestId("single-miner-surface")).toBeVisible();
  }

  async validateCurrentSubRoute(expectedSubRoute: string) {
    const escapedRoute = escapeForRegex(expectedSubRoute);
    await expect.poll(() => new URL(this.page.url()).pathname).toMatch(new RegExp(`^/miners/[^/]+/${escapedRoute}$`));
  }

  async getCurrentMinerIdentifier(): Promise<string> {
    const pathname = new URL(this.page.url()).pathname;
    const match = pathname.match(/^\/miners\/([^/]+)\//);
    if (!match) {
      throw new Error(`Expected an embedded single-miner path, received "${pathname}"`);
    }

    return decodeURIComponent(match[1]);
  }

  async validateCloseButtonLabel(expectedLabel: string) {
    await this.ensureNavigationVisible();
    await expect(this.closeButtonLabel()).toHaveText(expectedLabel);
  }

  async validateHostedMetadata(metadata: HostedMinerMetadata) {
    await this.ensureNavigationVisible();
    await expect(this.page.getByTestId("miner-name-info-item")).toContainText(metadata.minerName);

    if (metadata.ipAddress) {
      await expect(this.page.getByTestId("ip-address-info-item")).toContainText(metadata.ipAddress);
    }

    if (metadata.firmwareVersion) {
      await expect(this.page.getByTestId("version-info-item")).toContainText(metadata.firmwareVersion);
    }

    if (metadata.macAddress) {
      await expect(this.page.getByTestId("mac-address-info-item")).toContainText(metadata.macAddress);
    }
  }

  async navigateToLogs() {
    await this.ensureNavigationVisible();
    await this.page.getByTestId("navigation").getByRole("button", { name: "Logs" }).click();
    await expect(this.page).toHaveURL(/.*\/logs/);
    await this.waitForLogsListToBeReady();
  }

  async waitForLogsListToBeReady() {
    await expect(this.page.getByTestId("log-row").first()).toBeVisible();
  }

  async getSearchableSubstringFromFirstLogRow() {
    const firstRowText = (await this.page.getByTestId("log-row").first().textContent()) ?? "";
    const normalizedText = firstRowText
      .replace(/^\s*\d+\s*/, "")
      .replace(/^\[\d{2}:\d{2}:\d{2}\]\s*/, "")
      .trim();

    const match = normalizedText.match(/[A-Za-z0-9][A-Za-z0-9 .:_/-]{4,}/);
    const query = match?.[0]?.trim().slice(0, 12) ?? normalizedText.slice(0, 12).trim();

    expect(query, "Expected a searchable log substring from the first row").not.toBe("");
    return query;
  }

  async searchLogs(query: string) {
    await this.page.getByLabel("Search").fill(query);
  }

  async validateLogsSearchQuery(expectedQuery: string) {
    await expect(this.page.getByLabel("Search")).toHaveValue(expectedQuery);
  }

  async navigateToAuthenticationSettings() {
    const minerIdentifier = await this.getCurrentMinerIdentifier();
    await this.navigateClientSide(`/miners/${encodeURIComponent(minerIdentifier)}/settings/authentication`);
  }

  async validateAuthenticationSettingsPageOpened() {
    await expect(this.page).toHaveURL(/.*\/settings\/authentication/);
    await this.validateTitle("Update your admin login");
  }

  async validateDirectLoginModalHidden() {
    await expect(this.page.getByTestId("login-form")).toHaveCount(0);
  }

  async navigateClientSide(pathname: string) {
    await this.page.evaluate((nextPath) => {
      window.history.pushState({}, "", nextPath);
      window.dispatchEvent(new PopStateEvent("popstate"));
    }, pathname);
  }

  async clickCloseButton() {
    await expect(async () => {
      await this.ensureNavigationVisible();
      const closeButton = this.page.getByTestId("single-miner-close-button");
      await expect(closeButton).toBeVisible({ timeout: DEFAULT_INTERVAL });
      await closeButton.click({ timeout: DEFAULT_INTERVAL });
    }).toPass({ timeout: DEFAULT_TIMEOUT, intervals: [DEFAULT_INTERVAL] });
  }
}
