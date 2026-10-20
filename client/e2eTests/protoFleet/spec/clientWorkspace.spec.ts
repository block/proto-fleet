import { expect, test } from "@playwright/test";
import { clientA, clientB, hostAccount, mockClientWorkspace, subclient } from "../helpers/clientWorkspaceMocks";
import { ClientWorkspacePage } from "../pages/clientWorkspace";

test.describe("Client workspace and read-only host preview", () => {
  test.use({ storageState: { cookies: [], origins: [] } });

  for (const preview of [false, true]) {
    test(`${preview ? "host preview" : "actual client administrator"} shares dashboard and account navigation`, async ({
      page,
      context,
      isMobile,
    }, testInfo) => {
      const mock = await mockClientWorkspace(context, preview);
      const workspace = new ClientWorkspacePage(page, isMobile);
      await workspace.open(clientA);
      await expect(workspace.fleetSize().getByText("4 assets", { exact: true })).toBeVisible();
      await expect(page.getByText("Healthy", { exact: true })).toBeVisible();
      await expect(page.getByText("Performance", { exact: true })).toBeVisible();
      const current = page.getByRole("region", { name: "Current readings", exact: true });
      const power = current
        .getByTestId("stats-item")
        .filter({ has: page.getByRole("heading", { name: "Power", exact: true }) });
      await expect(power.getByText("9.0 kW", { exact: true })).toBeVisible();
      await expect(power.getByText("3 of 4 assets reporting", { exact: true })).toBeVisible();
      const efficiency = current
        .getByTestId("stats-item")
        .filter({ has: page.getByRole("heading", { name: "Efficiency", exact: true }) });
      await expect(efficiency.getByText("—", { exact: true })).toBeVisible();
      await expect(current.getByText("52.0 °C", { exact: true })).toBeVisible();
      await expect(current.getByText("97.0%", { exact: true })).toBeVisible();
      await expect(workspace.performanceReading("Hashrate").getByText("400.0", { exact: true })).toBeVisible();
      await expect(workspace.performanceReading("Power").getByText("9.0", { exact: true })).toBeVisible();
      await expect(workspace.performanceReading("Efficiency").getByText("N/A", { exact: true })).toBeVisible();
      const ranges = page.getByRole("group", { name: "Time range" });
      for (const range of ["1h", "24h", "7d", "30d", "90d", "1y"])
        await expect(ranges.getByRole("button", { name: range, exact: true })).toBeVisible();
      await expect(page.getByText("Viewing as Customer A", { exact: true })).toHaveCount(preview ? 1 : 0);
      await expect(page.getByText("Actions are unavailable in this read-only preview.", { exact: true })).toHaveCount(
        0,
      );
      await expect(page.getByRole("dialog", { name: "Viewing as Customer A", exact: true })).toHaveCount(
        preview ? 1 : 0,
      );
      await expect(page.getByText("Read-only", { exact: true })).toHaveCount(preview ? 1 : 0);
      await expect(workspace.fleetSize().getByText("4 assets", { exact: true }).locator("..")).toHaveCSS(
        "opacity",
        "1",
      );
      await page.screenshot({
        path: `/tmp/issue236-${preview ? "preview" : "client"}-${testInfo.project.name}.png`,
        fullPage: true,
        animations: "disabled",
      });
      await workspace.expectNavigation(clientA);

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
        await ranges.getByRole("button", { name: "7d", exact: true }).click();
        await expect(ranges.getByRole("button", { name: "7d", exact: true })).toHaveAttribute("aria-pressed", "true");
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
        await page.getByRole("button", { name: "Clear Model filter", exact: true }).click();
        await workspace.selectAssetFilter("model", "S19");
        await expect(page.getByText("No matching assets", { exact: true })).toBeVisible();
        await page.getByRole("button", { name: "Clear Model filter", exact: true }).click();
        await expect(page.getByText("Customer A miner", { exact: true })).toBeVisible();
        await workspace.navigate("Pools", clientA, "pools");
        await expect(page.getByText("Add and manage the pools for your account.", { exact: true })).toBeVisible();
        await expect(page.getByText("No pools yet", { exact: true })).toBeVisible();
        await expect(page.getByRole("button", { name: "Add pool", exact: true })).toBeEnabled({ enabled: !preview });
        await expect(page.getByText("Stored pool profile", { exact: true })).toHaveCount(0);
        await workspace.navigate("Team", clientA, "team");
        await expect(page.getByRole("button", { name: "Create invitation", exact: true })).toBeEnabled({
          enabled: !preview,
        });
        await workspace.navigate("Access", clientA, "access");
        await expect(page.getByRole("button", { name: "Rename account", exact: true })).toBeEnabled({
          enabled: !preview,
        });
        await expect(page.getByRole("region", { name: "Account details", exact: true })).toBeVisible();
        await expect(page.getByRole("heading", { name: "Your permissions", exact: true })).toBeVisible();
        await expect(page.getByRole("button", { name: "Grant ancestor access", exact: true })).toBeEnabled({
          enabled: !preview,
        });
      });

      await test.step("Persist personal preferences and apply units to current and historical temperatures", async () => {
        await workspace.navigate("Preferences", clientA, "preferences");
        await workspace.setTemperatureUnit("F");
        await workspace.setTheme("dark");
        await expect(page.locator("body")).toHaveAttribute("data-theme", "dark");
        await page.reload();
        await expect(page.getByTestId("temperature-button")).toHaveText("Fahrenheit");
        await expect(page.getByTestId("theme-button")).toHaveText("Dark");
        await expect(page.locator("body")).toHaveAttribute("data-theme", "dark");
        await workspace.navigate("Home", clientA, "overview");
        await expect(
          page.getByRole("region", { name: "Current readings", exact: true }).getByText("125.6 °F", { exact: true }),
        ).toBeVisible();
        await expect(workspace.performanceReading("Temperature").getByText("125.6", { exact: true })).toBeVisible();
        await expect(workspace.performanceReading("Temperature")).toContainText("°F");
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

  test("Close and Escape return to the same host view while nested dialogs dismiss independently", async ({
    page,
    context,
    isMobile,
  }) => {
    const mock = await mockClientWorkspace(context, true);
    const workspace = new ClientWorkspacePage(page, isMobile);
    const hostPath = `/accounts/${clientA}/manage/access?sort=kept#permissions`;
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

  test("live access removal clears already displayed account data", async ({ context, page, isMobile }) => {
    const mock = await mockClientWorkspace(context, true);
    const workspace = new ClientWorkspacePage(page, isMobile);
    await workspace.open(clientA);
    await expect(workspace.fleetSize().getByText("4 assets", { exact: true })).toBeVisible();
    mock.denyAccount();
    await page.evaluate(() => window.dispatchEvent(new Event("focus")));
    await expect(page.getByText(/Account access removed/)).toBeVisible();
    await expect(workspace.fleetSize().getByText("4 assets", { exact: true })).toHaveCount(0);
    await expect(page.getByRole("group", { name: "Time range" })).toHaveCount(0);
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
      await page.getByRole("button", { name: "Refresh", exact: true }).click();
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
    await expect(
      page.getByRole("region", { name: "Performance", exact: true }).getByText("No data", { exact: true }),
    ).toHaveCount(3);
    expect(mock.unexpected).toEqual([]);
  });
});
