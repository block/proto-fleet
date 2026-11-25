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

  currentReadings() {
    return this.page.getByRole("region", { name: "Current readings", exact: true });
  }

  currentReading(label: string) {
    return this.currentReadings()
      .getByTestId("stats-item")
      .filter({
        has: this.page.getByRole("heading", { name: label, exact: true }),
      });
  }

  timeRanges() {
    return this.page.getByRole("group", { name: "Time range", exact: true });
  }

  timeRange(label: string) {
    return this.timeRanges().getByRole("button", { name: label, exact: true });
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
    if (this.isMobile) await expect(navigation).toBeHidden();
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

  async clearAssetFilter(label: "Model" | "Site" | "Container" | "Assignment") {
    await this.page.getByRole("button", { name: `Clear ${label} filter`, exact: true }).click();
  }

  private async selectFilter(key: string, id: string, heading: string) {
    await this.page.getByRole("button", { name: "Add Filter", exact: true }).click();
    await this.page.getByTestId(`nested-dropdown-filter-row-${key}`).click();
    await this.page.getByTestId(`filter-option-${id}`).click();
    if (this.isMobile) await this.page.keyboard.press("Escape");
    else await this.page.getByRole("heading", { name: heading, exact: true }).click();
    await expect(this.page.getByTestId("nested-dropdown-filter-popover")).toBeHidden();
  }

  async expectActivity() {
    await expect(this.page.getByRole("heading", { name: "Activity", exact: true })).toBeVisible();
    await expect(this.page.getByText("Created pool configuration", { exact: true })).toBeVisible();
    await expect(this.page.getByRole("button", { name: "Export activity CSV", exact: true })).toBeEnabled();
  }

  async loadMoreActivity() {
    await this.requestMoreActivity();
    await expect(this.page.getByText("Recalled mining assets", { exact: true })).toBeVisible();
    await expect(this.page.getByText("2 assets", { exact: true })).toBeVisible();
  }

  async requestMoreActivity() {
    await this.page.getByRole("button", { name: "Load more", exact: true }).click();
  }

  async openPoolActivityDetails() {
    await this.page.getByText("Created pool configuration", { exact: true }).click();
    await expect(this.page.getByRole("dialog", { name: "Created pool configuration", exact: true })).toBeVisible();
  }

  async expectActivityCleared() {
    await expect(this.page.getByText("Created pool configuration", { exact: true })).toHaveCount(0);
    await expect(this.page.getByRole("button", { name: "Export activity CSV", exact: true })).toHaveCount(0);
    await expect(this.page.getByRole("textbox", { name: "Search activity", exact: true })).toHaveCount(0);
    await expect(this.page.getByRole("button", { name: "Search activity", exact: true })).toHaveCount(0);
  }

  async expectMinerActivity(visible: boolean) {
    if (visible) await expect(this.page.getByText("Rebooted miners", { exact: true })).toBeVisible();
    else await expect(this.page.getByText("Rebooted miners", { exact: true })).toHaveCount(0);
  }

  async openMinerActivityDetails() {
    await this.page.getByText("Rebooted miners", { exact: true }).click();
    await expect(this.page.getByRole("dialog", { name: "Rebooted miners", exact: true })).toBeVisible();
  }

  async expectMinerActivityDetailsCleared() {
    await expect(this.page.getByRole("dialog", { name: "Rebooted miners", exact: true })).toHaveCount(0);
  }

  async expectRenamedActivity() {
    await expect(this.page.getByText("Renamed account", { exact: true })).toBeVisible();
    await expect(this.page.getByText("Recalled mining assets", { exact: true })).toHaveCount(0);
    await expect(this.page.getByText("Created pool configuration", { exact: true })).toHaveCount(0);
  }

  async openActivityDetails() {
    await this.page.getByText("Recalled mining assets", { exact: true }).click();
    const detail = this.page.getByRole("dialog", { name: "Recalled mining assets", exact: true });
    await expect(detail).toBeVisible();
    await expect(detail.getByText("Completed", { exact: true })).toBeVisible();
    await this.page.keyboard.press("Escape");
    await expect(detail).toHaveCount(0);
  }

  async searchActivity(text: string) {
    // An empty search collapses behind a toggle button with the same name.
    const input = this.page.getByRole("textbox", { name: "Search activity", exact: true });
    const toggle = this.page.getByRole("button", { name: "Search activity", exact: true });
    await expect(input.or(toggle)).toBeVisible();
    if (await toggle.isVisible()) await toggle.click();
    await input.fill(text);
  }

  async exportActivity() {
    const pending = this.page.waitForEvent("download");
    await this.page.getByRole("button", { name: "Export activity CSV", exact: true }).click();
    const download = await pending;
    const stream = await download.createReadStream();
    let csv = "";
    for await (const chunk of stream) csv += chunk.toString();
    return csv;
  }

  async expectFilteredActivity() {
    await expect(this.page.getByText("Created pool configuration", { exact: true })).toHaveCount(0);
    await expect(this.page.getByText("Recalled mining assets", { exact: true })).toBeVisible();
    await expect(this.page.getByRole("textbox", { name: "Search activity", exact: true })).toBeFocused();
  }

  async expectNavigation(accountId: string) {
    const navigation = await this.navigation();
    for (const label of ["Home", "Fleet", "Activity", "Reports"])
      await expect(navigation.getByRole("link", { name: label, exact: true })).toBeVisible();
    if (this.isMobile)
      await expect(navigation.getByRole("button", { name: "Settings menu toggle", exact: true })).toBeVisible();
    else await expect(navigation.getByRole("link", { name: "Settings", exact: true })).toBeVisible();
    for (const label of ["Maintenance", "Billing"])
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

  async createClient(name: string, accountId: string) {
    await this.page.goto("/accounts");
    await this.page.getByRole("button", { name: "Create client", exact: true }).click();
    const dialog = this.page.getByRole("dialog", { name: "Create client", exact: true });
    await dialog.getByRole("textbox", { name: "Client name", exact: true }).fill(name);
    await dialog.getByRole("button", { name: "Create client", exact: true }).click();
    await expect(this.page).toHaveURL(new RegExp(`/accounts/${accountId}/manage/assets$`));
    await expect(this.page.getByRole("heading", { name, exact: true })).toBeVisible();
    await expect(this.page.getByRole("dialog")).toHaveCount(0);
    await expect(this.page.getByText("No mining assets", { exact: true })).toBeVisible();
  }

  allocationPicker() {
    return this.page.getByRole("dialog", { name: "Select assets", exact: true });
  }

  async openAllocationPicker() {
    await this.page.getByRole("button", { name: "Allocate assets", exact: true }).click();
    await expect(this.allocationPicker()).toBeVisible();
  }

  async cancelAllocationPicker() {
    await this.openAllocationPicker();
    const picker = this.allocationPicker();
    await picker.getByRole("checkbox", { name: "Select Allocation miner", exact: true }).check();
    await picker.getByRole("button", { name: "Close dialog", exact: true }).click();
    await expect(picker).toHaveCount(0);
  }

  async allocateClientMiner(accountId: string, clientName: string, screenshotPath: string) {
    const originalUrl = this.page.url();
    await this.openAllocationPicker();
    const picker = this.allocationPicker();
    const miner = picker
      .getByTestId("list-row")
      .filter({ has: this.page.getByText("Allocation miner", { exact: true }) });
    const other = picker.getByTestId("list-row").filter({ has: this.page.getByText("Other miner", { exact: true }) });
    await expect(miner).toBeVisible();
    await picker.getByRole("button", { name: "Add Filter", exact: true }).click();
    await this.page.getByTestId("nested-dropdown-filter-row-model").click();
    await this.page.getByTestId("filter-option-S21").click();
    if (this.isMobile) await this.page.keyboard.press("Escape");
    else await picker.getByText("0 assets selected", { exact: true }).click();
    await expect(this.page).toHaveURL(originalUrl);
    await expect(miner.getByRole("checkbox")).not.toBeChecked();
    await miner.getByRole("checkbox").check();
    await expect(other.getByRole("checkbox")).not.toBeChecked();
    const screenshot = await this.page.screenshot({ path: screenshotPath, animations: "disabled" });
    await picker.getByRole("button", { name: "Allocate selected", exact: true }).click();
    const dialog = this.page.getByRole("dialog", { name: "Allocate 1 asset?", exact: true });
    await expect(dialog).toBeVisible();
    await expect(dialog.getByText(`Allocate the selected assets to ${clientName}.`, { exact: true })).toBeVisible();
    await dialog.getByRole("button", { name: "Allocate 1 asset", exact: true }).focus();
    await this.page.keyboard.press("Tab");
    await expect(dialog.getByRole("button", { name: "Cancel", exact: true })).toBeFocused();
    await dialog.getByRole("button", { name: "Allocate 1 asset", exact: true }).click();
    await expect(picker).toHaveCount(0);
    await expect(this.page).toHaveURL(originalUrl);
    expect(new URL(originalUrl).pathname).toBe(`/accounts/${accountId}/manage/assets`);
    await expect(
      this.page.getByTestId("toast").filter({ hasText: `Allocated 1 asset to ${clientName}.` }),
    ).toBeVisible();
    await expect(this.page.getByRole("button", { name: "Allocate assets", exact: true })).toBeFocused();
    await expect(this.page.getByText("Allocation miner", { exact: true })).toBeVisible();
    return screenshot;
  }

  async inviteClientAdministrator(accountId: string, name: string) {
    await this.manageClientSection(accountId, "Team", "team");
    await this.page.getByRole("button", { name: "Invite team member", exact: true }).click();
    const dialog = this.page.getByRole("dialog", { name: "Create invitation", exact: true });
    await expect(dialog.getByText(`Invite someone to ${name}.`, { exact: true })).toBeVisible();
    await expect(dialog.getByRole("button", { name: "Role", exact: true })).toContainText("Account administrator");
    await dialog.getByRole("button", { name: "Create invitation", exact: true }).click();
    const share = this.page.getByRole("dialog", { name: "Share invitation", exact: true });
    await expect(share.getByRole("textbox", { name: "Invitation code", exact: true })).toHaveValue("K7MP-4X9R");
    await share.getByRole("button", { name: "Done", exact: true }).click();
    await expect(share).toHaveCount(0);
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
    await expect(this.page.getByRole("dialog", { name: `Viewing as ${name}`, exact: true })).toBeVisible();
    await expect(this.page.getByRole("heading", { name: "Overview", exact: true })).toBeVisible();
  }
}
