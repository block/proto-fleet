import { expect, test } from "@playwright/test";
import {
  clientA,
  clientB,
  hostAccount,
  mockClientWorkspace,
  newClient,
  subclient,
} from "../helpers/clientWorkspaceMocks";
import { ClientWorkspacePage } from "../pages/clientWorkspace";

test.describe("Client workspace and read-only host preview", () => {
  test.use({ storageState: { cookies: [], origins: [] } });

  for (const preview of [false, true]) {
    test(`${preview ? "host preview" : "actual client administrator"} shares dashboard and account navigation`, async ({
      page,
      context,
      isMobile,
    }) => {
      const mock = await mockClientWorkspace(context, preview);
      const workspace = new ClientWorkspacePage(page, isMobile);
      await workspace.open(clientA);
      await expect(workspace.fleetSize().getByText("4 assets", { exact: true })).toBeVisible();
      await expect(page.getByRole("heading", { name: "Healthy", exact: true })).toBeVisible();
      await expect(page.getByText("Performance", { exact: true })).toBeVisible();
      const current = workspace.currentReadings();
      const power = workspace.currentReading("Power");
      await expect(power.getByText("9.0 kW", { exact: true })).toBeVisible();
      await expect(power.getByText("3 of 4 assets reporting", { exact: true })).toBeVisible();
      const efficiency = workspace.currentReading("Efficiency");
      await expect(efficiency.getByText("—", { exact: true })).toBeVisible();
      await expect(current.getByText("52.0 °C", { exact: true })).toBeVisible();
      await expect(current.getByText("97.0%", { exact: true })).toBeVisible();
      await expect(workspace.performanceReading("Hashrate").getByText("400.0", { exact: true })).toBeVisible();
      await expect(workspace.performanceReading("Power").getByText("9.0", { exact: true })).toBeVisible();
      await expect(workspace.performanceReading("Efficiency").getByText("N/A", { exact: true })).toBeVisible();
      for (const range of ["1h", "24h"]) await expect(workspace.timeRange(range)).toBeVisible();
      for (const range of ["7d", "30d", "90d", "1y"]) await expect(workspace.timeRange(range)).toHaveCount(0);
      await expect(page.getByText("Viewing as Customer A", { exact: true })).toHaveCount(preview ? 1 : 0);
      await expect(page.getByText("Actions are unavailable in this read-only preview.", { exact: true })).toHaveCount(
        0,
      );
      await expect(page.getByRole("dialog", { name: "Viewing as Customer A", exact: true })).toHaveCount(
        preview ? 1 : 0,
      );
      await expect(page.getByText("Read-only", { exact: true })).toHaveCount(preview ? 1 : 0);
      await expect(workspace.fleetSize()).toHaveCSS("opacity", "1");
      await workspace.expectNavigation(clientA);

      await test.step("Read account activity with shared filtering, pagination and details", async () => {
        await workspace.navigate("Activity", clientA, "activity");
        await workspace.expectActivity();
        await test.info().attach("Account Activity", {
          body: await page.screenshot({ path: test.info().outputPath("account-activity.png") }),
          contentType: "image/png",
        });
        await workspace.loadMoreActivity();
        await workspace.openActivityDetails();
        await workspace.searchActivity("Recalled");
        await expect
          .poll(() => mock.calls.filter((call) => call.method === "ListAccountActivities").slice(-1)[0]?.body.filter)
          .toMatchObject({ searchText: "Recalled" });
        await workspace.expectFilteredActivity();
        const csv = await workspace.exportActivity();
        expect(csv).toContain("Recalled mining assets");
        expect(csv).not.toContain("Created pool configuration");
        const activityCalls = mock.calls.filter((call) => call.method === "ListAccountActivities");
        expect(activityCalls.some((call) => call.body.pageToken === "next")).toBe(true);
        expect(activityCalls.every((call) => call.account === clientA && call.body.accountId === clientA)).toBe(true);
        expect(
          mock.calls.some((call) =>
            ["ListActivities", "ListAlerts", "GetCommandBatchDeviceResults"].includes(call.method),
          ),
        ).toBe(false);
        await workspace.navigate("Home", clientA, "overview");
      });

      await test.step("Keep allocation, site and duration filters in the parent's workspace", async () => {
        await workspace.selectReportFilter("allocation", subclient);
        await expect
          .poll(
            () => mock.calls.filter((call) => call.method === "GetAccountDashboard").slice(-1)[0]?.body.ownerAccountId,
          )
          .toBe(subclient);
        await workspace.selectReportFilter("site", "7");
        await expect
          .poll(() => mock.calls.filter((call) => call.method === "GetAccountDashboard").slice(-1)[0]?.body.siteId)
          .toBe("7");
        await workspace.timeRange("24h").click();
        await expect(workspace.timeRange("24h")).toHaveAttribute("aria-pressed", "true");
        await expect(page).toHaveURL(new RegExp(`/accounts/${clientA}/overview$`));
      });

      await test.step("Use the same tools with preview mutations visibly disabled", async () => {
        await workspace.navigate("Fleet", clientA, "assets");
        await expect(page.getByRole("heading", { name: "Fleet", exact: true })).toBeVisible();
        await expect(
          page
            .getByRole("navigation", { name: "Fleet sections", exact: true })
            .getByRole("button", { name: "Miners", exact: true }),
        ).toHaveAttribute("aria-current", "page");
        await expect(page.getByRole("button", { name: "Add Filter", exact: true })).toBeVisible();
        await expect(page.getByText("1 mining asset", { exact: true })).toBeVisible();
        await expect(page.getByText("Customer A miner", { exact: true })).toBeVisible();
        await workspace.selectAssetFilter("model", "S21");
        await workspace.selectAssetFilter("site", "7");
        await expect
          .poll(() => mock.calls.filter((call) => call.method === "ListAccountAssets").slice(-1)[0]?.body)
          .toMatchObject({ model: "S21", siteId: "7" });
        await expect(page.getByTestId("active-filter-model")).toContainText("S21");
        await expect(page.getByTestId("active-filter-site")).toContainText("A allocated site");
        await expect(page.getByText("Customer A miner", { exact: true })).toBeVisible();
        await workspace.clearAssetFilter("Model");
        await workspace.selectAssetFilter("model", "S19");
        await expect(page.getByText("No matching assets", { exact: true })).toBeVisible();
        await workspace.clearAssetFilter("Model");
        await expect(page.getByText("Customer A miner", { exact: true })).toBeVisible();
        await workspace.navigate("Pools", clientA, "pools");
        await expect(page.getByText("Add and manage the pools for your account.", { exact: true })).toBeVisible();
        await expect(page.getByText("No pools yet", { exact: true })).toBeVisible();
        await expect(page.getByRole("button", { name: "Add pool", exact: true })).toBeEnabled({ enabled: !preview });
        await expect(page.getByText("Stored pool profile", { exact: true })).toHaveCount(0);
        await workspace.navigate("Team", clientA, "team");
        await expect(page.getByRole("button", { name: "Invite team member", exact: true })).toBeEnabled({
          enabled: !preview,
        });
        await workspace.navigate("Access", clientA, "access");
        await expect(page.getByRole("button", { name: "Rename account", exact: true })).toBeEnabled({
          enabled: !preview,
        });
        await expect(page.getByRole("region", { name: "Account details", exact: true })).toBeVisible();
        await expect(page.getByRole("heading", { name: "Your permissions", exact: true })).toBeVisible();
        await expect(page.getByRole("button", { name: "Grant host or parent access", exact: true })).toBeEnabled({
          enabled: !preview,
        });
      });

      await test.step("Persist personal preferences and apply temperature units to current readings", async () => {
        await workspace.navigate("Preferences", clientA, "preferences");
        await workspace.setTemperatureUnit("F");
        await workspace.setTheme("dark");
        await expect(page.locator("body")).toHaveAttribute("data-theme", "dark");
        await page.reload();
        await expect(page.getByTestId("temperature-button")).toHaveText("Fahrenheit");
        await expect(page.getByTestId("theme-button")).toHaveText("Dark");
        await expect(page.locator("body")).toHaveAttribute("data-theme", "dark");
        await workspace.navigate("Home", clientA, "overview");
        await expect(workspace.currentReading("Temperature").getByText("125.6 °F", { exact: true })).toBeVisible();
        await expect(workspace.performanceReading("Temperature")).toContainText("All assets within optimal range");
      });

      const scopedCalls = mock.calls.filter((call) => call.body.accountId && call.body.accountId !== hostAccount);
      for (const call of mock.calls.filter((call) => call.body.accountId === hostAccount)) {
        expect(call.method).toBe("ListChildren");
        expect(call.account).toBe(hostAccount);
      }
      expect(scopedCalls.length).toBeGreaterThan(0);
      for (const call of scopedCalls) {
        expect(call.body.accountId).toBe(clientA);
        expect(call.account).toBe(clientA);
      }
      expect(mock.unexpected).toEqual([]);
    });
  }

  test("uptime surfaces missing module observations alongside healthy miners", async ({ page, context, isMobile }) => {
    const mock = await mockClientWorkspace(context, false, { missingModuleUptime: true });
    const workspace = new ClientWorkspacePage(page, isMobile);
    await workspace.open(clientA);
    const uptime = workspace.performanceReading("Uptime");
    await expect(uptime).toContainText("1 of 4 assets reporting");
    await expect(uptime).not.toContainText("All assets hashing");
    await uptime.scrollIntoViewIfNeeded();
    await test.info().attach("Incomplete uptime reporting", {
      body: await page.screenshot({ path: test.info().outputPath("uptime-coverage.png"), animations: "disabled" }),
      contentType: "image/png",
    });
    expect(mock.unexpected).toEqual([]);
  });

  test("create a client and explicitly invite its team after Fleet allocation", async ({ page, context, isMobile }) => {
    const mock = await mockClientWorkspace(context, true, { fleet: true });
    const workspace = new ClientWorkspacePage(page, isMobile);

    await test.step("Create a client without sending an invitation", async () => {
      await workspace.createClient("New client", newClient);
      await test.info().attach("Client setup", {
        body: await page.screenshot({ path: test.info().outputPath("client-setup.png"), animations: "disabled" }),
        contentType: "image/png",
      });
      expect(mock.calls.filter((call) => call.method === "CreateAccount")).toMatchObject([
        { account: hostAccount, body: { accountId: hostAccount, name: "New client", capabilities: "31" } },
      ]);
      expect(mock.calls.filter((call) => call.method === "CreateInvitation")).toHaveLength(0);
    });

    await test.step("Allocate only the selected Fleet miner to the original client", async () => {
      const screenshot = await workspace.allocateFleetMiner(
        newClient,
        "New client",
        test.info().outputPath("fleet-allocation.png"),
      );
      await test.info().attach("Fleet allocation", { body: screenshot, contentType: "image/png" });
      expect(mock.calls.filter((call) => call.method === "AssignAccountAssets")).toMatchObject([
        {
          account: hostAccount,
          body: {
            accountId: hostAccount,
            childAccountId: newClient,
            intent: "ASSET_ASSIGNMENT_INTENT_ASSIGN",
            assets: [{ kind: "MINING_ASSET_KIND_MINER", id: "42", expectedRevision: "7" }],
          },
        },
      ]);
      const assignment = mock.calls.find((call) => call.method === "AssignAccountAssets")!;
      expect(assignment.body.assets).toHaveLength(1);
      expect(mock.calls.filter((call) => call.method === "CreateInvitation")).toHaveLength(0);
    });

    await test.step("The allocated client name in Fleet opens the same management view", async () => {
      await page.goto("/fleet/miners");
      const client = page.getByRole("link", { name: "New client", exact: true });
      await expect(client).toHaveAttribute("href", `/accounts/${newClient}/manage/assets`);
      await client.click();
      await expect(page).toHaveURL(new RegExp(`/accounts/${newClient}/manage/assets$`));
      await expect(page.getByRole("dialog")).toHaveCount(0);
    });

    await test.step("Create the first invitation from Team", async () => {
      await workspace.inviteClientAdministrator(newClient, "New client");
      expect(mock.calls.filter((call) => call.method === "CreateInvitation")).toMatchObject([
        {
          account: newClient,
          body: { accountId: newClient, role: "ACCOUNT_ROLE_ADMINISTRATOR", expiresInSeconds: 86400 },
        },
      ]);
    });

    expect(mock.unexpected).toEqual([]);
  });

  test("create a client and explicitly invite its team without allocating assets", async ({
    page,
    context,
    isMobile,
  }) => {
    const mock = await mockClientWorkspace(context, true);
    const workspace = new ClientWorkspacePage(page, isMobile);
    await workspace.createClient("New client", newClient);
    expect(mock.calls.filter((call) => call.method === "CreateInvitation")).toHaveLength(0);
    await workspace.inviteClientAdministrator(newClient, "New client");
    expect(mock.calls.filter((call) => call.method === "AssignAccountAssets")).toHaveLength(0);
    expect(mock.calls.filter((call) => call.method === "CreateInvitation")).toMatchObject([
      {
        account: newClient,
        body: { accountId: newClient, role: "ACCOUNT_ROLE_ADMINISTRATOR", expiresInSeconds: 86400 },
      },
    ]);
    expect(mock.unexpected).toEqual([]);
  });

  test("client rows open host management with visible team, settings and permission actions", async ({
    page,
    context,
    isMobile,
  }) => {
    const mock = await mockClientWorkspace(context, true);
    const workspace = new ClientWorkspacePage(page, isMobile);
    await workspace.openClientManagement(clientA, "Customer A");
    await expect(page.getByRole("link", { name: "Allocate assets", exact: true })).toBeVisible();
    await expect
      .poll(() =>
        mock.calls.some(
          (call) =>
            call.method === "ListAccountAssets" && call.account === hostAccount && call.body.ownerAccountId === clientA,
        ),
      )
      .toBe(true);
    await expect(page.getByRole("link", { name: "Invite team member", exact: true })).toHaveCount(0);
    await workspace.manageClientSection(clientA, "Team", "team");
    await page.getByRole("button", { name: "Invite team member", exact: true }).click();
    const invitation = page.getByRole("dialog", { name: "Create invitation", exact: true });
    await expect(invitation.getByText("Invite someone to Customer A.", { exact: true })).toBeVisible();
    await page.keyboard.press("Escape");
    await expect(invitation).toHaveCount(0);
    await workspace.manageClientSection(clientA, "Client settings", "settings");
    await expect(page.getByRole("button", { name: "Rename account", exact: true })).toBeEnabled();
    await workspace.manageClientSection(clientA, "Permissions", "permissions");
    const permissions = page.getByRole("region", { name: "Client permissions", exact: true });
    await expect(page.getByRole("button", { name: "Manage permissions", exact: true })).toHaveCount(0);
    await expect(permissions.getByRole("checkbox", { name: "View account data", exact: true })).toBeDisabled();
    await expect(permissions.getByRole("button", { name: "Save permissions", exact: true })).toHaveCount(0);
    await permissions.getByRole("checkbox", { name: "Select stored pool profile", exact: true }).uncheck();
    await expect.poll(() => mock.calls.filter((call) => call.method === "ConfigureAccount").length).toBe(1);
    await expect(permissions.getByRole("checkbox", { name: "Select stored pool profile", exact: true })).toBeEnabled();
    await permissions.getByRole("button", { name: "Suspend account", exact: true }).click();
    const suspend = page.getByRole("dialog", { name: "Suspend Customer A?", exact: true });
    await expect(suspend).toBeVisible();
    expect(mock.calls.filter((call) => call.method === "ConfigureAccount")).toHaveLength(1);
    await suspend.getByRole("button", { name: "Cancel", exact: true }).click();
    await expect(suspend).toHaveCount(0);
    await permissions.getByRole("button", { name: "Suspend account", exact: true }).click();
    await suspend.getByRole("button", { name: "Suspend account", exact: true }).click();
    await permissions.getByRole("button", { name: "Resume account", exact: true }).click();
    await page
      .getByRole("dialog", { name: "Resume Customer A?", exact: true })
      .getByRole("button", { name: "Resume account", exact: true })
      .click();
    await expect(permissions.getByRole("button", { name: "Suspend account", exact: true })).toBeVisible();
    const updates = mock.calls.filter((call) => call.method === "ConfigureAccount");
    expect(updates.map((call) => call.body.suspended ?? false)).toEqual([false, true, false]);
    for (const call of updates) {
      expect(call.account).toBe(hostAccount);
      expect(call.body).toMatchObject({ accountId: hostAccount, childAccountId: clientA, capabilities: "27" });
    }

    await workspace.previewFromAccountSettings();
    await expect(page.getByRole("dialog", { name: "Viewing as Customer A", exact: true })).toBeVisible();
    await workspace.closePreview();
    await expect(page).toHaveURL(new RegExp(`/accounts/${clientA}/manage/permissions$`));
    await expect(
      permissions.getByRole("checkbox", { name: "Select stored pool profile", exact: true }),
    ).not.toBeChecked();
    await expect(permissions.getByRole("checkbox", { name: "Select stored pool profile", exact: true })).toBeEnabled();
    expect(mock.unexpected).toEqual([]);
  });

  test("Close and Escape return to the same host view while nested dialogs dismiss independently", async ({
    page,
    context,
    isMobile,
  }) => {
    const mock = await mockClientWorkspace(context, true);
    const workspace = new ClientWorkspacePage(page, isMobile);
    const hostPath = `/accounts/${clientA}/manage/settings?sort=kept#permissions`;
    await page.goto(hostPath);
    await expect(page.getByRole("button", { name: "Rename account", exact: true })).toBeEnabled();
    await workspace.previewFromAccountSettings();
    await workspace.navigate("Fleet", clientA, "assets");
    await page.getByRole("button", { name: "Manage columns", exact: true }).click();
    await expect(page.getByRole("dialog", { name: "Manage columns", exact: true })).toBeVisible();
    await page.keyboard.press("Escape");
    await expect(page.getByRole("dialog", { name: "Manage columns", exact: true })).toHaveCount(0);
    await expect(page.getByRole("dialog", { name: "Viewing as Customer A", exact: true })).toBeVisible();
    await workspace.navigate("Preferences", clientA, "preferences");
    for (const [triggerId, label] of [
      ["theme-button", "Theme"],
      ["temperature-button", "Temperature"],
    ]) {
      const trigger = page.getByTestId(triggerId);
      await trigger.click();
      const dialog = page.getByRole("dialog", { name: label, exact: true });
      const close = dialog.getByRole("button", { name: "Close dialog", exact: true });
      await expect(close).toBeFocused();
      await page.keyboard.press("Shift+Tab");
      await expect(dialog.getByRole("radio").last()).toBeFocused();
      await page.keyboard.press("Tab");
      await expect(close).toBeFocused();
      await page.keyboard.press("Escape");
      await expect(dialog).toHaveCount(0);
      await expect(trigger).toBeFocused();
      await expect(page.getByRole("dialog", { name: "Viewing as Customer A", exact: true })).toBeVisible();
    }
    await page.keyboard.press("Escape");
    await expect(page).toHaveURL(new URL(hostPath, page.url()).href);
    await expect(page.getByRole("button", { name: "Rename account", exact: true })).toBeEnabled();
    await workspace.previewFromAccountSettings();
    await workspace.navigate("Access", clientA, "access");
    await workspace.closePreview();
    await expect(page).toHaveURL(new URL(hostPath, page.url()).href);
    await expect(page.getByRole("button", { name: "Rename account", exact: true })).toBeEnabled();
    expect(mock.unexpected).toEqual([]);
  });

  test("preview tabs keep independent accounts and exiting one preserves the other's scope", async ({
    context,
    page,
    isMobile,
  }) => {
    const mock = await mockClientWorkspace(context, true);
    const other = await context.newPage();
    const firstWorkspace = new ClientWorkspacePage(page, isMobile);
    const secondWorkspace = new ClientWorkspacePage(other, isMobile);
    await firstWorkspace.open(clientA);
    await secondWorkspace.open(clientB);
    await expect(page.getByText("Viewing as Customer A", { exact: true })).toBeVisible();
    await expect(other.getByText("Viewing as Customer B", { exact: true })).toBeVisible();
    await expect(firstWorkspace.fleetSize().getByText("4 assets", { exact: true })).toBeVisible();
    await expect(secondWorkspace.fleetSize().getByText("2 assets", { exact: true })).toBeVisible();
    await page.getByRole("button", { name: "Close client preview", exact: true }).click();
    await expect(page).toHaveURL(/\/accounts$/);
    await expect(page.getByRole("button", { name: "Create client", exact: true })).toBeVisible();
    await expect(other.getByText("Viewing as Customer B", { exact: true })).toBeVisible();
    await secondWorkspace.navigate("Access", clientB, "access");
    await expect(other.getByRole("button", { name: "Rename account", exact: true })).toBeDisabled();
    await secondWorkspace.navigate("Home", clientB, "overview");
    await expect(secondWorkspace.fleetSize().getByText("2 assets", { exact: true })).toBeVisible();
    for (const call of mock.calls.filter(
      (call) => call.page === other && call.body.accountId && call.body.accountId !== hostAccount,
    )) {
      expect(call.account).toBe(clientB);
      expect(call.body.accountId).toBe(clientB);
    }
    const auth = await other.evaluate(() => JSON.parse(localStorage.getItem("proto-fleet-auth")!).state.auth);
    expect(auth.username).toBe(mock.username);
    expect(auth.permissions).toEqual(["account:manage"]);
    expect(mock.unexpected).toEqual([]);
  });

  test("a pending preview response cannot restore the previous client after exit", async ({
    context,
    page,
    isMobile,
  }) => {
    const mock = await mockClientWorkspace(context, true);
    const held = mock.pauseDashboard(clientA);
    const workspace = new ClientWorkspacePage(page, isMobile);
    try {
      await workspace.open(clientA);
      await held.pending;
      await page.getByRole("button", { name: "Close client preview", exact: true }).click();
      await expect(page).toHaveURL(/\/accounts$/);
      await workspace.viewClientFromManagement("Customer B");
      await expect(page.getByText("Viewing as Customer B", { exact: true })).toBeVisible();
      await expect(workspace.fleetSize().getByText("2 assets", { exact: true })).toBeVisible();
      held.release();
      await workspace.navigate("Access", clientB, "access");
      await workspace.navigate("Home", clientB, "overview");
      await expect(workspace.fleetSize().getByText("2 assets", { exact: true })).toBeVisible();
      await expect(workspace.fleetSize().getByText("4 assets", { exact: true })).toHaveCount(0);
      await expect(page.getByText("A allocated site", { exact: true })).toHaveCount(0);
      expect(mock.unexpected).toEqual([]);
    } finally {
      held.release();
    }
  });

  test("pending Activity cannot restore the previous account after preview exit", async ({
    context,
    page,
    isMobile,
  }) => {
    const mock = await mockClientWorkspace(context, true);
    const workspace = new ClientWorkspacePage(page, isMobile);
    await workspace.open(clientA);
    await workspace.navigate("Activity", clientA, "activity");
    await workspace.expectActivity();
    const held = mock.pauseActivity(clientA);
    try {
      await workspace.requestMoreActivity();
      await held.pending;
      await workspace.closePreview();
      await workspace.viewClientFromManagement("Customer B");
      await workspace.navigate("Activity", clientB, "activity");
      await workspace.expectRenamedActivity();
      held.release();
      await held.finished;
      await workspace.expectRenamedActivity();
      expect(mock.unexpected).toEqual([]);
    } finally {
      held.release();
    }
  });

  test("narrowed Activity authority withdraws accumulated rows and open details", async ({
    context,
    page,
    isMobile,
  }) => {
    const mock = await mockClientWorkspace(context, true);
    mock.showMinerActivity();
    const workspace = new ClientWorkspacePage(page, isMobile);
    await workspace.open(clientA);
    await workspace.navigate("Activity", clientA, "activity");
    await workspace.expectMinerActivity(true);
    await workspace.loadMoreActivity();
    await workspace.openMinerActivityDetails();
    mock.narrowActivityAuthority();
    await page.evaluate(() => window.dispatchEvent(new Event("focus")));
    await workspace.expectMinerActivity(false);
    await workspace.expectMinerActivityDetailsCleared();
    await expect(page.getByText("Recalled mining assets", { exact: true })).toBeVisible();
    expect(mock.unexpected).toEqual([]);
  });

  test("changing Activity filters cancels the old export and allows a fresh download", async ({
    context,
    page,
    isMobile,
  }) => {
    const mock = await mockClientWorkspace(context, false);
    const workspace = new ClientWorkspacePage(page, isMobile);
    await workspace.open(clientA);
    await workspace.navigate("Activity", clientA, "activity");
    await workspace.expectActivity();
    const held = mock.pauseActivityExport(clientA);
    const downloads: string[] = [];
    page.on("download", (download) => downloads.push(download.suggestedFilename()));
    const cancelled = page.waitForEvent("requestfailed", {
      predicate: (request) => request.url().endsWith("/ExportAccountActivities"),
    });
    try {
      await page.getByRole("button", { name: "Export activity CSV", exact: true }).click();
      await held.pending;
      await workspace.searchActivity("Recalled");
      await workspace.expectFilteredActivity();
      await cancelled;
      held.release();
      await held.finished;
      const csv = await workspace.exportActivity();
      expect(csv).toContain("Recalled mining assets");
      expect(csv).not.toContain("Created pool configuration");
      expect(downloads).toHaveLength(1);
      expect(mock.unexpected).toEqual([]);
    } finally {
      held.release();
    }
  });

  test("access removal clears Activity rows, filters and details despite a pending page", async ({
    context,
    page,
    isMobile,
  }) => {
    const mock = await mockClientWorkspace(context, true);
    const workspace = new ClientWorkspacePage(page, isMobile);
    await workspace.open(clientA);
    await workspace.navigate("Activity", clientA, "activity");
    await workspace.expectActivity();
    const held = mock.pauseActivity(clientA);
    try {
      await workspace.requestMoreActivity();
      await held.pending;
      await workspace.openPoolActivityDetails();
      mock.denyAccount();
      await page.evaluate(() => window.dispatchEvent(new Event("focus")));
      await expect(page.getByText(/Account access removed/)).toBeVisible();
      await workspace.expectActivityCleared();
      held.release();
      await held.finished;
      await workspace.expectActivityCleared();
      expect(mock.unexpected).toEqual([]);
    } finally {
      held.release();
    }
  });

  test("live access removal clears already displayed account data", async ({ context, page, isMobile }) => {
    const mock = await mockClientWorkspace(context, true);
    const workspace = new ClientWorkspacePage(page, isMobile);
    await workspace.open(clientA);
    await expect(workspace.fleetSize().getByText("4 assets", { exact: true })).toBeVisible();
    mock.denyAccount();
    await page.evaluate(() => window.dispatchEvent(new Event("focus")));
    await expect(page.getByText(/Account access removed/)).toBeVisible();
    await expect(workspace.fleetSize().getByText("4 assets", { exact: true })).toHaveCount(0);
    await expect(workspace.timeRanges()).toHaveCount(0);
    expect(mock.unexpected).toEqual([]);
  });

  test("a response started before access removal cannot restore cleared readings", async ({
    context,
    page,
    isMobile,
  }) => {
    const mock = await mockClientWorkspace(context, true);
    const workspace = new ClientWorkspacePage(page, isMobile);
    await workspace.open(clientA);
    await expect(workspace.fleetSize().getByText("4 assets", { exact: true })).toBeVisible();
    const held = mock.pauseDashboard(clientA);
    try {
      await page.evaluate(() => window.dispatchEvent(new Event("focus")));
      await held.pending;
      mock.denyAccount();
      await page.evaluate(() => window.dispatchEvent(new Event("focus")));
      await expect(page.getByText(/Account access removed/)).toBeVisible();
      held.release();
      await held.finished;
      await expect(workspace.fleetSize().getByText("4 assets", { exact: true })).toHaveCount(0);
      await expect(page.getByRole("region", { name: "Performance", exact: true })).toHaveCount(0);
      await expect(page.getByText("A allocated site", { exact: true })).toHaveCount(0);
      expect(mock.unexpected).toEqual([]);
    } finally {
      held.release();
    }
  });

  test("retained dashboard history does not keep current details visible after recall", async ({
    context,
    page,
    isMobile,
  }) => {
    const mock = await mockClientWorkspace(context, false);
    const workspace = new ClientWorkspacePage(page, isMobile);
    await workspace.open(clientA);
    await workspace.navigate("Fleet", clientA, "assets");
    const asset = page.getByRole("link", { name: "Customer A miner", exact: true });
    await expect(asset).toHaveAttribute("href", `/accounts/${clientA}/assets/miner/42`);
    await asset.click();
    await expect(page.getByText("Current allocated mining asset", { exact: true })).toBeVisible();
    mock.recallAsset();
    await page.evaluate(() => window.dispatchEvent(new Event("focus")));
    await expect(page.getByText(/Asset is no longer allocated/)).toBeVisible();
    await expect(page.getByText("Current allocated mining asset", { exact: true })).toHaveCount(0);
    await workspace.navigate("Home", clientA, "overview");
    await expect(page.getByRole("region", { name: "Performance", exact: true })).toBeVisible();
    await expect(page.getByText(/Charts include retained readings from established allocation periods/)).toBeVisible();
    await expect(workspace.performanceReading("Hashrate").getByText("400.0", { exact: true })).toBeVisible();
    const detail = mock.calls.filter((call) => call.method === "GetAccountAssetDetail");
    expect(detail.length).toBeGreaterThan(0);
    for (const call of detail) {
      expect(call.account).toBe(clientA);
      expect(call.body).toMatchObject({ accountId: clientA, asset: { kind: "MINING_ASSET_KIND_MINER", id: "42" } });
    }
    expect(mock.unexpected).toEqual([]);
  });

  test("unknown allocation history stays unavailable while current readings remain visible", async ({
    context,
    page,
    isMobile,
  }) => {
    const mock = await mockClientWorkspace(context, false);
    mock.withoutHistoricalReadings();
    const workspace = new ClientWorkspacePage(page, isMobile);
    await workspace.open(clientA);
    await expect(workspace.fleetSize().getByText("4 assets", { exact: true })).toBeVisible();
    await expect(
      page.getByText(
        "No authorized historical readings in this time range. Unknown allocation periods and missing telemetry are unavailable.",
        { exact: true },
      ),
    ).toBeVisible();
    for (const label of ["Hashrate", "Uptime", "Temperature", "Power", "Efficiency"]) {
      await expect(workspace.performanceReading(label).getByText("No data", { exact: true })).toBeVisible();
    }
    expect(mock.unexpected).toEqual([]);
  });
});
