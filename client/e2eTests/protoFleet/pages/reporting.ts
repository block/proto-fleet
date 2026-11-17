import { expect, type Page } from "@playwright/test";
export class ReportingPage {
  constructor(private readonly page: Page) {}
  async open(accountId: string) {
    await this.page.goto(`/accounts/${accountId}/reports`);
    await expect(this.page.getByRole("heading", { name: "Reports", exact: true })).toBeVisible();
  }
  async openHost(accountId: string, reportId: string) {
    await this.page.goto(`/accounts/reports?account=${accountId}&view=reports&report=${reportId}`);
    await expect(this.page.getByRole("region", { name: "Host report detail", exact: true })).toBeVisible();
  }
  async downloadSources() {
    const detail = this.page.getByRole("region", { name: "Host report detail", exact: true });
    await detail.getByRole("button", { name: "Report actions", exact: true }).click();
    const downloading = this.page.waitForEvent("download");
    await this.page.getByText("Download source inputs", { exact: true }).click();
    return downloading;
  }
  detail() {
    return this.page.getByRole("region", { name: "Selected report", exact: true });
  }
  totals() {
    return this.detail().getByRole("region", { name: "Report results", exact: true });
  }
  row(name: string) {
    return this.page.getByRole("row").filter({ has: this.page.getByRole("cell", { name, exact: true }) });
  }
  async view(name = "Monthly service report") {
    await this.row(name).click();
  }
  async previous() {
    await this.detail().getByText("Publication history", { exact: true }).click();
    await this.detail()
      .getByRole("region", { name: "Publication history", exact: true })
      .getByRole("row")
      .filter({ has: this.page.getByRole("cell", { name: "Replaced", exact: true }) })
      .click();
  }
  async latest() {
    await this.detail().getByText("Publication history", { exact: true }).click();
    await this.detail()
      .getByRole("region", { name: "Publication history", exact: true })
      .getByRole("row")
      .filter({ has: this.page.getByRole("cell", { name: "Current publication", exact: true }) })
      .click();
  }
  async downloadHTML() {
    const downloading = this.page.waitForEvent("download");
    await this.detail().getByRole("button", { name: "Download report HTML", exact: true }).click();
    return downloading;
  }
  async refresh() {
    await this.page.reload();
  }
}

export class ReportingInputsPage {
  constructor(private readonly page: Page) {}
  async open(accountId: string) {
    await this.page.goto(`/accounts/reports?account=${accountId}&view=settings`);
    await expect(this.page.getByRole("region", { name: "Reporting setup", exact: true })).toBeVisible();
  }
  facts() {
    return this.page.getByRole("region", { name: "Reporting facts", exact: true });
  }
  async next() {
    await this.facts().getByRole("button", { name: "Next page", exact: true }).click();
  }
  async edit(id: string) {
    await this.facts()
      .getByRole("button", { name: `Edit fact ${id}`, exact: true })
      .click();
    return this.page.getByRole("dialog", { name: "Edit fact", exact: true });
  }
  async remove(id: string) {
    await this.facts()
      .getByRole("button", { name: `Delete fact ${id}`, exact: true })
      .click();
    return this.page.getByRole("dialog", { name: "Delete fact", exact: true });
  }
  async importRows(count: number) {
    await this.facts().getByRole("button", { name: "Import facts", exact: true }).click();
    const form = this.page.getByRole("dialog", { name: "Add or update reporting facts", exact: true });
    await form.getByLabel("Facts file (CSV or JSON)", { exact: true }).setInputFiles({
      name: "coverage.json",
      mimeType: "application/json",
      buffer: Buffer.from(JSON.stringify(Array.from({ length: count }, (_, i) => ({ id: `import-${i}` })))),
    });
    await form.getByLabel("Import evidence reference", { exact: true }).fill("Reviewed imported evidence");
    await form.getByRole("button", { name: "Add or update rows", exact: true }).click();
    return form;
  }
}
