import { expect, type Page } from "@playwright/test";

type RpcResult<T> = { body: T; status: number };

export interface MaintenanceTicketJson {
  id: string;
  ticketNumber: string;
  component: string;
  diagnosis: string;
}

export class MaintenancePage {
  constructor(private page: Page) {}

  async open() {
    await this.page.goto("/maintenance");
    await expect(this.page.getByRole("heading", { name: "Maintenance" })).toBeVisible();
  }

  async openQueue() {
    await this.page.getByRole("button", { name: "Queue", exact: true }).click();
  }

  async openHistory() {
    await this.page.getByRole("button", { name: "History", exact: true }).click();
  }

  async openInventory() {
    await this.page.getByRole("button", { name: "Inventory", exact: true }).click();
  }

  async expectTicket(ticketNumber: string) {
    await expect(this.page.getByText(ticketNumber, { exact: true })).toBeVisible();
  }

  async rpc<T>(procedure: string, body: Record<string, unknown> = {}): Promise<RpcResult<T>> {
    return await this.page.evaluate(
      async ({ procedure, body }) => {
        const response = await fetch(`/api-proxy/${procedure}`, {
          method: "POST",
          credentials: "include",
          headers: { "Connect-Protocol-Version": "1", "Content-Type": "application/json" },
          body: JSON.stringify(body),
        });
        const text = await response.text();
        return { body: text ? JSON.parse(text) : {}, status: response.status };
      },
      { procedure, body },
    );
  }

  async ensurePlacedMiner(): Promise<{ deviceIdentifier: string; site: { id: string; name: string } }> {
    const response = await this.rpc<{
      miners?: Array<{ deviceIdentifier: string; placement?: { site?: { id: string; label: string } } }>;
    }>("fleetmanagement.v1.FleetManagementService/ListMinerStateSnapshots", { pageSize: 50 });
    expect(response.status).toBe(200);
    const placed = response.body.miners?.find((item) => item.placement?.site);
    if (placed?.placement?.site) {
      return {
        deviceIdentifier: placed.deviceIdentifier,
        site: { id: placed.placement.site.id, name: placed.placement.site.label },
      };
    }
    const miner = response.body.miners?.[0];
    if (!miner) throw new Error("Maintenance E2E requires at least one miner");
    const sites = await this.rpc<{ sites?: Array<{ site?: { id: string; name: string } }> }>(
      "sites.v1.SiteService/ListSites",
      { pageSize: 1 },
    );
    let site = sites.body.sites?.[0]?.site;
    if (!site) {
      const created = await this.rpc<{ site?: { id: string; name: string } }>("sites.v1.SiteService/CreateSite", {
        name: `Maintenance E2E site ${Date.now()}`,
        locationCity: "Austin",
        locationState: "TX",
        timezone: "America/Chicago",
        country: "US",
      });
      expect(created.status).toBe(200);
      site = created.body.site;
    }
    if (!site) throw new Error("Unable to create Maintenance E2E site");
    const rack = await this.rpc("device_set.v1.DeviceSetService/SaveRack", {
      label: `Maintenance E2E ${Date.now()}`,
      rackInfo: {
        rows: 1,
        columns: 1,
        orderIndex: "RACK_ORDER_INDEX_BOTTOM_LEFT",
        coolingType: "RACK_COOLING_TYPE_AIR",
        siteId: site.id,
      },
      deviceSelector: { deviceList: { deviceIdentifiers: [miner.deviceIdentifier] } },
    });
    expect(rack.status, JSON.stringify(rack.body)).toBe(200);
    return { deviceIdentifier: miner.deviceIdentifier, site };
  }

  async importPartThroughUI(csv: string, partName: string) {
    await this.openInventory();
    await this.page.getByRole("button", { name: "Import CSV" }).click();
    await this.page.getByLabel("Inventory CSV").setInputFiles({
      name: `${partName}.csv`,
      mimeType: "text/csv",
      buffer: Buffer.from(csv),
    });
    const confirm = this.page.getByRole("button", { name: "Import 1 part" });
    await expect(confirm).toBeEnabled();
    await confirm.click();
    await expect(this.page.getByText(partName, { exact: true })).toBeVisible();
    await this.openQueue();
  }

  async createMinerTicketThroughUI(siteName: string, diagnosis: string): Promise<MaintenanceTicketJson> {
    await this.page.getByRole("button", { name: "Create ticket" }).click();
    await this.page.getByRole("button", { name: "Select miner" }).click();
    await this.page.getByRole("row").filter({ hasText: siteName }).getByRole("radio").first().check();
    await this.page.getByRole("button", { name: "Use selected miner" }).click();
    await this.page.getByRole("button", { name: "Component", exact: true }).click();
    await this.page.getByText("Fan", { exact: true }).click();
    await this.page.getByRole("textbox", { name: "Issue description" }).fill(diagnosis);
    await this.page.getByRole("button", { name: "Create ticket", exact: true }).last().click();
    await expect(this.page.getByText(diagnosis, { exact: false }).first()).toBeVisible();

    const listed = await this.rpc<{ tickets: Array<{ ticket?: MaintenanceTicketJson }> }>(
      "maintenance.v1.MaintenanceService/ListRepairTickets",
      { filter: { searchQuery: diagnosis }, pageSize: 100 },
    );
    expect(listed.status).toBe(200);
    const ticket = listed.body.tickets.find((item) => item.ticket?.diagnosis === diagnosis)?.ticket;
    if (!ticket) throw new Error(`Unable to find created maintenance ticket for ${diagnosis}`);
    return ticket;
  }

  async assignStartAndCommentThroughUI(ticket: MaintenanceTicketJson, comment: string) {
    await this.page.getByRole("row").filter({ hasText: ticket.ticketNumber }).click();
    await expect(this.page.getByText(ticket.ticketNumber, { exact: true })).toBeVisible();
    const assign = this.page.getByRole("button", { name: "Assign" });
    if (!(await assign.isVisible())) await this.page.getByRole("button", { name: "More actions" }).click();
    await assign.click();
    await this.page.getByRole("button", { name: "admin", exact: true }).click();
    await expect(this.page.getByText("admin", { exact: true }).last()).toBeVisible();
    await this.page.getByRole("button", { name: "Update status" }).click();
    await this.page.getByRole("button", { name: "In Progress", exact: true }).click();
    await expect(this.page.getByText("in progress", { exact: true })).toBeVisible();
    await this.page.getByRole("button", { name: "Add comment" }).click();
    await this.page.getByRole("textbox", { name: "Add a comment" }).fill(comment);
    await this.page.getByRole("button", { name: "Post" }).click();
    await expect(this.page.getByText(comment, { exact: true })).toBeVisible();
  }

  async completeTicketThroughUI(partName: string) {
    await this.page.getByRole("button", { name: "Complete repair" }).first().click();
    const quantity = this.page.getByRole("spinbutton", { name: `${partName} quantity` });
    await expect(quantity).toBeVisible();
    await quantity.fill("1");
    await this.page.getByRole("button", { name: "Complete repair" }).last().click();
    await expect(this.page.getByRole("table", { name: "Ticket resolution" })).toBeVisible();
  }
}
