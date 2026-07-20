import { expect } from "@playwright/test";
import { NavigationComponent } from "../../protoOS/pages/components/navigation";
import { LogsPage } from "../../protoOS/pages/logs";
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
  private readonly navigation: NavigationComponent;
  private readonly logsPage: LogsPage;

  constructor(page: ConstructorParameters<typeof BasePage>[0], isMobile: boolean = false) {
    super(page, isMobile);
    this.navigation = new NavigationComponent(page, isMobile);
    this.logsPage = new LogsPage(page, isMobile);
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
    await this.navigation.navigateToLogs();
    await this.logsPage.waitForLogsListToBeReady();
  }

  async waitForLogsListToBeReady() {
    await this.logsPage.waitForLogsListToBeReady();
  }

  async getSearchableSubstringFromFirstLogRow() {
    return await this.logsPage.getSearchableSubstringFromFirstRow();
  }

  async searchLogs(query: string) {
    await this.logsPage.searchLogs(query);
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
