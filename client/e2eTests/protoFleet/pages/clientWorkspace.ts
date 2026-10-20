import { expect, type Page } from "@playwright/test";

export class ClientWorkspacePage {
  constructor(
    private readonly page: Page,
    private readonly isMobile: boolean,
  ) {}

  async open(accountId: string) {
    await this.page.goto(`/accounts/${accountId}/overview`);
    await expect(this.page.getByRole("heading", { name: "Overview", exact: true })).toBeVisible();
  }

  fleetSize() {
    return this.page.getByTestId("stats-item").filter({
      has: this.page.getByRole("heading", { name: "Your fleet", exact: true }),
    });
  }

  performanceReading(label: string) {
    return this.page
      .getByRole("region", { name: "Performance", exact: true })
      .getByTestId("stats-item")
      .filter({
        has: this.page.getByRole("heading", { name: label, exact: true }),
      });
  }

  async navigation() {
    if (this.isMobile) await this.page.getByRole("button", { name: "Open navigation menu", exact: true }).click();
    return this.page.getByRole("navigation", { name: "Main", exact: true }).getByRole("list");
  }

  async navigate(label: string, accountId: string, section: string) {
    const navigation = await this.navigation();
    await expect(
      this.page.getByRole("navigation", { name: "Main", exact: true }).getByTestId("navigation-home-link"),
    ).toHaveAttribute("href", `/accounts/${accountId}/overview`);
    const isSettingsPage = ["pools", "team", "access", "children", "preferences"].includes(section);
    let destination = navigation;
    if (isSettingsPage) {
      if (this.isMobile) {
        const toggle = navigation.getByRole("button", { name: "Settings menu toggle", exact: true });
        if ((await toggle.getAttribute("aria-expanded")) !== "true") await toggle.click();
      } else {
        await navigation.getByRole("link", { name: "Settings", exact: true }).click();
        destination = this.page.getByRole("navigation", { name: "Settings", exact: true });
      }
    }
    const link = destination.getByRole("link", { name: label, exact: true });
    await expect(link).toHaveAttribute("href", `/accounts/${accountId}/${section}`);
    await link.click();
    await expect(this.page).toHaveURL(new RegExp(`/accounts/${accountId}/${section}$`));
  }

  async setTemperatureUnit(unit: "C" | "F") {
    await this.page.getByTestId("temperature-button").click();
    await this.page.getByTestId(unit === "C" ? "celsius-option" : "fahrenheit-option").click();
    await this.page.getByRole("button", { name: "Done", exact: true }).click();
    await expect(this.page.getByTestId("temperature-button")).toHaveText(unit === "C" ? "Celsius" : "Fahrenheit");
  }

  async setTheme(theme: "system" | "light" | "dark") {
    await this.page.getByTestId("theme-button").click();
    await this.page.getByTestId(`theme-${theme}-option`).click();
    await this.page.getByRole("button", { name: "Done", exact: true }).click();
    await expect(this.page.getByTestId("theme-button")).toHaveText(new RegExp(theme, "i"));
  }

  async selectReportFilter(key: "allocation" | "site", id: string) {
    await this.selectFilter(key, id, "Overview");
  }

  async selectAssetFilter(key: "model" | "site" | "container" | "assignment", id: string) {
    await this.selectFilter(key, id, "Fleet");
  }

  private async selectFilter(key: string, id: string, heading: string) {
    await this.page.getByRole("button", { name: "Add Filter", exact: true }).click();
    await this.page.getByTestId(`nested-dropdown-filter-row-${key}`).click();
    await this.page.getByTestId(`filter-option-${id}`).click();
    if (this.isMobile) await this.page.keyboard.press("Escape");
    else await this.page.getByRole("heading", { name: heading, exact: true }).click();
    await expect(this.page.getByTestId("nested-dropdown-filter-popover")).toBeHidden();
  }

  async expectNavigation(accountId: string) {
    const navigation = await this.navigation();
    for (const label of ["Home", "Fleet"])
      await expect(navigation.getByRole("link", { name: label, exact: true })).toBeVisible();
    if (this.isMobile)
      await expect(navigation.getByRole("button", { name: "Settings menu toggle", exact: true })).toBeVisible();
    else await expect(navigation.getByRole("link", { name: "Settings", exact: true })).toBeVisible();
    for (const label of ["Maintenance", "Activity", "Reports", "Billing"])
      await expect(navigation.getByRole("link", { name: label, exact: true })).toHaveCount(0);
    await expect(
      this.page
        .getByRole("navigation", { name: "Main", exact: true })
        .getByRole("button", { name: "Log out", exact: true }),
    ).toBeInViewport();
    await navigation.getByRole("link", { name: "Home", exact: true }).click();
    await expect(this.page).toHaveURL(new RegExp(`/accounts/${accountId}/overview$`));
  }

  async openClientManagement(accountId: string, name: string) {
    await this.page.goto("/accounts");
    await this.page.getByRole("link", { name, exact: true }).click();
    await expect(this.page).toHaveURL(new RegExp(`/accounts/${accountId}/manage/assets$`));
    await expect(this.page.getByRole("heading", { name, exact: true })).toBeVisible();
    await expect(this.page.getByRole("dialog")).toHaveCount(0);
  }

  async manageClientSection(accountId: string, label: string, section: string) {
    await this.page
      .getByRole("navigation", { name: "Client sections", exact: true })
      .getByRole("button", { name: label, exact: true })
      .click();
    await expect(this.page).toHaveURL(new RegExp(`/accounts/${accountId}/manage/${section}$`));
  }

  async previewFromAccountSettings() {
    await this.page.getByRole("link", { name: "View as client", exact: true }).click();
    await expect(this.page.getByRole("button", { name: "Close client preview", exact: true })).toBeVisible();
  }

  async closePreview() {
    await this.page.getByRole("button", { name: "Close client preview", exact: true }).click();
  }

  async viewClientFromManagement(name: string) {
    const row = this.page.getByTestId("list-row").filter({ has: this.page.getByRole("link", { name, exact: true }) });
    await row.getByRole("button", { name: "Row actions", exact: true }).click();
    await this.page.getByRole("button", { name: "View as client", exact: true }).click();
  }
}
