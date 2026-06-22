import { type Page, type Response as PlaywrightResponse } from "@playwright/test";
import { test } from "../fixtures/pageFixtures";
import { type CommonSteps } from "../helpers/commonSteps";
import { generateRandomText } from "../helpers/testDataHelper";
import { type MinersPage } from "../pages/miners";
import { type RacksPage } from "../pages/racks";
import { PairingStatus } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";

type SiteManagementMode = "fleet" | "legacy";

type VisibleMinerSnapshot = {
  deviceIdentifier: string;
  ipAddress: string;
  name: string;
  pairingStatus: PairingStatus;
  rackLabel: string;
  rackPosition: string;
  siteId?: string;
  siteLabel: string;
};

const LIST_MINERS_RESPONSE = "ListMinerStateSnapshots";
const ASSIGN_DEVICES_TO_SITE = "AssignDevicesToSite";
const ASSIGN_DEVICES_TO_RACK = "AssignDevicesToRack";
const ASSIGN_RACKS_TO_BUILDING = "AssignRacksToBuilding";
const TEMP_ZONE = "ReparentAutomationZone";
const ACTIVE_SITE_STORAGE_KEY = "proto-fleet-multi-site";

function getListRowByName(page: Page, name: string) {
  return page
    .getByTestId("list-row")
    .filter({ has: page.getByTestId("name").getByText(name, { exact: true }) })
    .first();
}

function getLegacySiteRowByName(page: Page, name: string) {
  return page
    .locator('button[data-testid^="sites-all-table-row-"]')
    .filter({ has: page.getByText(name, { exact: true }) })
    .first();
}

function getLegacyBuildingRowByName(page: Page, name: string) {
  return page
    .locator('[data-testid^="site-settings-building-row-"]')
    .filter({ has: page.getByText(name, { exact: true }) })
    .first();
}

async function resetActiveSiteSelection(page: Page) {
  await page.evaluate(
    ({ storageKey }) => {
      localStorage.setItem(
        storageKey,
        JSON.stringify({
          state: {
            ui: {
              activeSite: { kind: "all" },
            },
          },
          version: 0,
        }),
      );
    },
    { storageKey: ACTIVE_SITE_STORAGE_KEY },
  );
}

async function detectSiteManagementMode(page: Page): Promise<SiteManagementMode> {
  await resetActiveSiteSelection(page);
  await page.goto("/fleet/racks");
  await test.expect(page).toHaveURL(/\/fleet\/racks(?:[?#].*)?$/);

  const sitesTab = page.getByRole("tab", { name: "Sites", exact: true });
  return (await sitesTab.isVisible().catch(() => false)) ? "fleet" : "legacy";
}

async function openSitesManagementPage(page: Page, mode: SiteManagementMode) {
  await resetActiveSiteSelection(page);

  if (mode === "fleet") {
    await page.goto("/fleet/sites");
    await test.expect(page).toHaveURL(/\/fleet\/sites(?:[?#].*)?$/);
    await test.expect(page.getByTestId("fleet-sites-page")).toBeVisible();
    return;
  }

  await page.goto("/settings/sites");
  await test.expect(page).toHaveURL(/\/settings\/sites(?:[?#].*)?$/);
  await test.expect(page.getByTestId("settings-sites-page")).toBeVisible();
}

async function openBuildingsManagementPage(page: Page, mode: SiteManagementMode) {
  if (mode !== "fleet") {
    throw new Error("Legacy ProtoFleet does not expose a standalone buildings management page");
  }

  await resetActiveSiteSelection(page);
  await page.goto("/fleet/buildings");
  await test.expect(page).toHaveURL(/\/fleet\/buildings(?:[?#].*)?$/);
  await test.expect(page.getByTestId("fleet-buildings-page")).toBeVisible();
}

async function openLegacySiteSettings(page: Page, siteName: string) {
  await openSitesManagementPage(page, "legacy");

  const row = getLegacySiteRowByName(page, siteName);
  await test.expect(row).toBeVisible();
  await row.click();

  await test.expect(page.getByTestId("site-settings-single-view")).toBeVisible();
  await test.expect(page.getByText(siteName, { exact: true })).toBeVisible();
}

async function clickManageSiteEditDetails(page: Page) {
  const editDetailsButton = page.locator('[data-testid="manage-site-modal-edit-details"]:visible');
  if (await editDetailsButton.isVisible().catch(() => false)) {
    await editDetailsButton.click();
    return;
  }

  const overflowTrigger = page.getByTestId("full-screen-two-pane-modal").getByTestId("overflow-menu-trigger");
  await test.expect(overflowTrigger).toBeVisible();
  await overflowTrigger.click();
  await page.locator("div.fixed.inset-0.z-60").getByText("Edit details", { exact: true }).click();
}

async function getScopeIdFromRowName(page: Page, name: string, scope: "site" | "building" | "rack"): Promise<bigint> {
  const row = getListRowByName(page, name);
  await test.expect(row).toBeVisible();

  const trigger = row.locator('button[data-testid$="-actions-trigger"]').first();
  await test.expect(trigger).toBeVisible();

  const testId = await trigger.getAttribute("data-testid");
  const pattern =
    scope === "rack"
      ? /^rack-list-row-(\d+)-actions-trigger$/
      : new RegExp(`^${scope}-list-row-(\\d+)-actions-trigger$`);
  const capturedId = testId?.match(pattern)?.[1];
  if (!capturedId) {
    throw new Error(`Could not parse ${scope} id from row action trigger: ${testId ?? "missing test id"}`);
  }

  return BigInt(capturedId);
}

async function getLegacySiteIdFromName(page: Page, name: string): Promise<bigint> {
  const row = getLegacySiteRowByName(page, name);
  await test.expect(row).toBeVisible();

  const testId = await row.getAttribute("data-testid");
  const capturedId = testId?.match(/^sites-all-table-row-(\d+)$/)?.[1];
  if (!capturedId) {
    throw new Error(`Could not parse site id from settings sites row: ${testId ?? "missing test id"}`);
  }

  return BigInt(capturedId);
}

async function getLegacyBuildingIdFromName(page: Page, name: string): Promise<bigint> {
  const row = getLegacyBuildingRowByName(page, name);
  await test.expect(row).toBeVisible();

  const testId = await row.getAttribute("data-testid");
  const capturedId = testId?.match(/^site-settings-building-row-(\d+)$/)?.[1];
  if (!capturedId) {
    throw new Error(`Could not parse building id from site settings row: ${testId ?? "missing test id"}`);
  }

  return BigInt(capturedId);
}

async function openRowActions(page: Page, name: string) {
  const row = getListRowByName(page, name);
  await test.expect(row).toBeVisible();
  await row.locator('button[data-testid$="-actions-trigger"]').first().click();
}

async function clickRowAction(page: Page, label: string) {
  await page.getByText(label, { exact: true }).click();
}

async function selectParentPickerTarget(page: Page, label: string) {
  await page.getByText(label, { exact: true }).click();
  await page.getByTestId("modal").getByRole("button", { name: "Save", exact: true }).click();
}

async function continueDialogIfVisible(page: Page, title: string) {
  const dialog = page.getByText(title, { exact: true });
  if (await dialog.isVisible().catch(() => false)) {
    await page.getByRole("button", { name: "Continue", exact: true }).click();
  }
}

async function loadVisibleMiners({
  page,
  commonSteps,
}: {
  page: Page;
  commonSteps: CommonSteps;
}): Promise<VisibleMinerSnapshot[]> {
  const responsePromise = new Promise<VisibleMinerSnapshot[]>((resolve, reject) => {
    const timeoutId = setTimeout(() => {
      page.off("response", handleResponse);
      resolve([]);
    }, 10000);

    const handleResponse = async (response: PlaywrightResponse) => {
      if (response.request().method() !== "POST" || !response.url().includes(LIST_MINERS_RESPONSE)) {
        return;
      }

      try {
        const body = (await response.json()) as {
          miners?: VisibleMinerSnapshot[];
          snapshots?: VisibleMinerSnapshot[];
        };
        const snapshots = body.snapshots ?? body.miners ?? [];
        if (snapshots.length === 0) {
          return;
        }

        clearTimeout(timeoutId);
        page.off("response", handleResponse);
        resolve(snapshots);
      } catch (error) {
        clearTimeout(timeoutId);
        page.off("response", handleResponse);
        reject(error);
      }
    };

    page.on("response", handleResponse);
  });

  await commonSteps.goToMinersPage();

  return await responsePromise;
}

function pickUnrackedPairedMiner(miners: VisibleMinerSnapshot[]): VisibleMinerSnapshot {
  const candidate =
    miners.find((miner) => miner.pairingStatus === PairingStatus.PAIRED && miner.rackLabel === "") ??
    miners.find((miner) => miner.rackLabel === "") ??
    miners.find((miner) => miner.pairingStatus === PairingStatus.PAIRED) ??
    miners[0];

  if (!candidate) {
    throw new Error("Expected at least one visible miner for reparent coverage");
  }

  return candidate;
}

async function createSite(page: Page, name: string, mode: SiteManagementMode): Promise<bigint> {
  await openSitesManagementPage(page, mode);

  if (mode === "fleet") {
    const addSiteButton = page.getByTestId("fleet-sites-add");
    await test.expect(addSiteButton).toBeVisible();
    await addSiteButton.click();
    await page.getByTestId("site-settings-name-input").fill(name);
    await page.getByTestId("site-settings-modal-continue").click();
    const saveSiteButton = page.locator('[data-testid="manage-site-modal-save"]:visible');
    await test.expect(saveSiteButton).toBeVisible();
    await saveSiteButton.click();

    await test.expect(page.getByTestId("toaster-container").getByText(`Site "${name}" created`)).toBeVisible();
    return await getScopeIdFromRowName(page, name, "site");
  }

  await page.getByTestId("sites-page-header-add").click();
  await page.getByTestId("site-settings-name-input").fill(name);
  await page.getByTestId("site-settings-modal-continue").click();
  const saveSiteButton = page.locator('[data-testid="manage-site-modal-save"]:visible');
  await test.expect(saveSiteButton).toBeVisible();
  await saveSiteButton.click();

  return await getLegacySiteIdFromName(page, name);
}

async function deleteSite(page: Page, name: string, mode: SiteManagementMode) {
  if (mode === "fleet") {
    await openSitesManagementPage(page, mode);
    await openRowActions(page, name);
    await clickRowAction(page, "Edit site");
    await clickManageSiteEditDetails(page);
    await page.locator('[data-testid="site-settings-modal-delete"]:visible').click();
    await page.getByTestId("site-delete-dialog-confirm").click();
    await test.expect(page.getByTestId("toaster-container").getByText(`Site "${name}" deleted`)).toBeVisible();
    return;
  }

  await openLegacySiteSettings(page, name);
  await page.getByTestId("site-settings-manage").click();
  await clickManageSiteEditDetails(page);
  await page.locator('[data-testid="site-settings-modal-delete"]:visible').click();
  await page.getByTestId("site-delete-dialog-confirm").click();
  await openSitesManagementPage(page, "legacy");
  await test
    .expect(
      page
        .locator('button[data-testid^="sites-all-table-row-"]')
        .filter({ has: page.getByText(name, { exact: true }) }),
    )
    .toHaveCount(0);
}

async function createBuilding(
  page: Page,
  siteName: string,
  buildingName: string,
  mode: SiteManagementMode,
): Promise<bigint> {
  if (mode === "fleet") {
    await openBuildingsManagementPage(page, mode);

    const addBuildingButton = page.getByTestId("fleet-buildings-add");
    await test.expect(addBuildingButton).toBeVisible();
    await addBuildingButton.click();
    await page.getByTestId("building-settings-site-select").click();
    await page.getByRole("option", { name: siteName, exact: true }).click();
    await page.getByTestId("building-settings-name-input").fill(buildingName);
    await page.getByTestId("building-settings-modal-save").click();

    await test
      .expect(page.getByTestId("toaster-container").getByText(`Building "${buildingName}" created`))
      .toBeVisible();
    return await getScopeIdFromRowName(page, buildingName, "building");
  }

  await openLegacySiteSettings(page, siteName);
  await page.getByTestId("site-settings-add-building").click();
  await test.expect(page.getByTestId("building-settings-modal")).toBeVisible();
  await page.getByTestId("building-settings-name-input").fill(buildingName);
  await page.getByTestId("building-settings-modal-save").click();

  return await getLegacyBuildingIdFromName(page, buildingName);
}

async function deleteBuilding(page: Page, siteName: string, name: string, mode: SiteManagementMode) {
  if (mode === "fleet") {
    await openBuildingsManagementPage(page, mode);
    await openRowActions(page, name);
    await clickRowAction(page, "Edit building");
    await page.getByTestId("manage-building-delete").click();
    await page.getByTestId("building-delete-dialog-confirm").click();
    await test.expect(page.getByTestId("toaster-container").getByText(`Building "${name}" deleted`)).toBeVisible();
    return;
  }

  await openLegacySiteSettings(page, siteName);
  const row = getLegacyBuildingRowByName(page, name);
  await test.expect(row).toBeVisible();
  await row.click();
  await page.getByTestId("building-settings-modal-delete").click();
  await page.getByTestId("building-delete-dialog-confirm").click();
  await test
    .expect(
      page.locator('[data-testid^="site-settings-building-row-"]').filter({
        has: page.getByText(name, { exact: true }),
      }),
    )
    .toHaveCount(0);
}

async function createRack({
  page,
  racksPage,
  label,
  zone,
}: {
  page: Page;
  racksPage: RacksPage;
  label: string;
  zone: string;
}): Promise<bigint> {
  await racksPage.navigateToRacksPage();
  await racksPage.clickAddRackButton();
  await racksPage.inputZone(zone);
  await racksPage.inputRackLabel(label);
  await racksPage.clickContinueFromRackSettings();
  await racksPage.clickSaveRack();
  await racksPage.validateRackToast(label);
  await racksPage.clickViewList();
  await racksPage.waitForRackListToLoad({ allowEmpty: false });
  return await getScopeIdFromRowName(page, label, "rack");
}

async function assignRackToSite(page: Page, rackLabel: string, siteName: string) {
  await page.goto("/fleet/racks");
  await page.getByRole("button", { name: "View list", exact: true }).click();
  await openRowActions(page, rackLabel);
  await clickRowAction(page, "Add to site");
  await selectParentPickerTarget(page, siteName);
}

async function assignRackToBuilding(page: Page, rackLabel: string, buildingName: string) {
  await page.goto("/fleet/racks");
  await page.getByRole("button", { name: "View list", exact: true }).click();
  await openRowActions(page, rackLabel);
  await clickRowAction(page, "Add to building");
  await selectParentPickerTarget(page, buildingName);
}

async function moveSingleMinerToSite({
  page,
  minersPage,
  minerIp,
  siteName,
}: {
  page: Page;
  minersPage: MinersPage;
  minerIp: string;
  siteName: string;
}) {
  await minersPage.clickMinerCheckbox(minerIp);
  await minersPage.validateActionBarMinerCount(1);
  await minersPage.clickActionsMenuButton();
  await page.getByTestId("add-to-site-popover-button").click();
  await selectParentPickerTarget(page, siteName);
  await continueDialogIfVisible(page, "Move miners between sites?");
}

async function moveSingleMinerToRack({
  page,
  minersPage,
  minerIp,
  rackLabel,
}: {
  page: Page;
  minersPage: MinersPage;
  minerIp: string;
  rackLabel: string;
}) {
  await minersPage.clickMinerCheckbox(minerIp);
  await minersPage.validateActionBarMinerCount(1);
  await minersPage.clickActionsMenuButton();
  await page.getByTestId("add-to-rack-popover-button").click();
  await selectParentPickerTarget(page, rackLabel);
}

async function assertSiteMinerCount({
  page,
  siteName,
  mode,
  expectedCount,
}: {
  page: Page;
  siteName: string;
  mode: SiteManagementMode;
  expectedCount: number;
}) {
  await openSitesManagementPage(page, mode);

  if (mode === "fleet") {
    const row = getListRowByName(page, siteName);
    await test.expect(row.getByTestId("miners")).toHaveText(String(expectedCount));
    return;
  }

  const row = getLegacySiteRowByName(page, siteName);
  await test.expect(row).toContainText(`${expectedCount} miners`);
}

async function assertRackPlacementForMode({
  row,
  mode,
  siteName,
  buildingName,
}: {
  row: ReturnType<typeof getListRowByName>;
  mode: SiteManagementMode;
  siteName: string;
  buildingName: string;
}) {
  await test.expect(row).toBeVisible();

  if (mode !== "fleet") {
    return;
  }

  await test.expect(row.getByTestId("site")).toHaveText(siteName);
  await test.expect(row.getByTestId("building")).toHaveText(buildingName);
}

async function assertRackMembershipForMode({
  row,
  mode,
  targetSiteName,
}: {
  row: ReturnType<typeof getListRowByName>;
  mode: SiteManagementMode;
  targetSiteName: string;
}) {
  await test.expect(row).toBeVisible();
  await test.expect(row.getByTestId("miners")).toHaveText("1");

  if (mode !== "fleet") {
    return;
  }

  await test.expect(row.getByTestId("site")).toHaveText(targetSiteName);
}

async function restoreMinerSiteIfNeeded({
  page,
  minersPage,
  minerIp,
  originalSiteLabel,
}: {
  page: Page;
  minersPage: MinersPage;
  minerIp: string;
  originalSiteLabel?: string;
}) {
  if (!originalSiteLabel) {
    return;
  }

  await page.goto("/fleet/miners");
  await minersPage.waitForMinersListToLoad();
  await moveSingleMinerToSite({ page, minersPage, minerIp, siteName: originalSiteLabel });
}

async function restoreMinerRackSlotIfNeeded({
  page,
  racksPage,
  minerIp,
  originalRackLabel,
  originalRackPosition,
}: {
  page: Page;
  racksPage: RacksPage;
  minerIp: string;
  originalRackLabel?: string;
  originalRackPosition?: string;
}) {
  if (!originalRackLabel || !originalRackPosition) {
    return;
  }

  const slotNumber = Number(originalRackPosition);
  if (Number.isNaN(slotNumber)) {
    throw new Error(`Could not parse original rack position "${originalRackPosition}"`);
  }

  await page.goto("/fleet/racks");
  await racksPage.clickViewList();
  await racksPage.waitForRackListToLoad({ allowEmpty: false });
  await racksPage.openRackFromList(originalRackLabel);
  await racksPage.clickRackOverviewEmptySlot(slotNumber);
  await racksPage.assignSearchMinerByIpAddress(minerIp);
  await racksPage.validateRackOverviewAssignedSlots([slotNumber]);
}

async function restoreMinerRackIfNeeded({
  page,
  minersPage,
  minerIp,
  originalRackLabel,
}: {
  page: Page;
  minersPage: MinersPage;
  minerIp: string;
  originalRackLabel?: string;
}) {
  if (!originalRackLabel) {
    return;
  }

  await page.goto("/fleet/miners");
  await minersPage.waitForMinersListToLoad();
  await moveSingleMinerToRack({ page, minersPage, minerIp, rackLabel: originalRackLabel });
}

async function restoreMinerPlacementIfNeeded({
  page,
  minersPage,
  racksPage,
  minerIp,
  originalSiteLabel,
  originalRackLabel,
  originalRackPosition,
}: {
  page: Page;
  minersPage: MinersPage;
  racksPage: RacksPage;
  minerIp: string;
  originalSiteLabel?: string;
  originalRackLabel?: string;
  originalRackPosition?: string;
}) {
  if (!originalRackLabel) {
    await restoreMinerSiteIfNeeded({ page, minersPage, minerIp, originalSiteLabel });
    return;
  }

  await restoreMinerSiteIfNeeded({ page, minersPage, minerIp, originalSiteLabel });

  if (originalRackPosition) {
    await restoreMinerRackSlotIfNeeded({
      page,
      racksPage,
      minerIp,
      originalRackLabel,
      originalRackPosition,
    });
    return;
  }

  await restoreMinerRackIfNeeded({ page, minersPage, minerIp, originalRackLabel });
}

async function deleteRackIfCreated({
  racksPage,
  created,
  rackLabel,
}: {
  racksPage: RacksPage;
  created: boolean;
  rackLabel: string;
}) {
  if (!created) {
    return;
  }

  await racksPage.navigateToRacksPage();
  await racksPage.tryAction(() => racksPage.clickViewList());
  if (!(await racksPage.tryAction(() => racksPage.openRackFromList(rackLabel)))) {
    return;
  }

  await racksPage.clickEditRack();
  await racksPage.clickDeleteRack();
  await racksPage.clickDeleteConfirm();
  await racksPage.tryAction(() => racksPage.validateRackDeletedToast());
}

async function deleteBuildingIfCreated({
  page,
  created,
  siteName,
  buildingName,
  mode,
}: {
  page: Page;
  created: boolean;
  siteName: string;
  buildingName: string;
  mode: SiteManagementMode;
}) {
  if (!created) {
    return;
  }

  await deleteBuilding(page, siteName, buildingName, mode);
}

async function deleteSiteIfCreated({
  page,
  created,
  siteName,
  mode,
}: {
  page: Page;
  created: boolean;
  siteName: string;
  mode: SiteManagementMode;
}) {
  if (!created) {
    return;
  }

  await deleteSite(page, siteName, mode);
}

test.describe("Miners reparent", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/");
  });

  test("Move a rack to a building from the Racks tab", async ({ page, commonSteps, racksPage }) => {
    await commonSteps.loginAsAdmin();

    const siteManagementMode = await detectSiteManagementMode(page);
    const siteName = generateRandomText("reparent_site");
    const buildingName = generateRandomText("reparent_building");
    const rackLabel = generateRandomText("reparent_rack");

    let createdSite = false;
    let createdBuilding = false;
    let createdRack = false;

    try {
      await createSite(page, siteName, siteManagementMode);
      createdSite = true;
      const buildingId = await createBuilding(page, siteName, buildingName, siteManagementMode);
      createdBuilding = true;
      const rackId = await createRack({ page, racksPage, label: rackLabel, zone: TEMP_ZONE });
      createdRack = true;

      const requestPromise = page.waitForRequest(new RegExp(ASSIGN_RACKS_TO_BUILDING));
      const responsePromise = page.waitForResponse(new RegExp(ASSIGN_RACKS_TO_BUILDING));

      await test.step("Move the rack into the target building", async () => {
        await assignRackToBuilding(page, rackLabel, buildingName);
      });

      await test.step("Validate the rack move request and resulting row placement", async () => {
        const request = await requestPromise;
        const response = await responsePromise;
        const body = request.postDataJSON() as {
          targetBuildingId?: string;
          racks: Array<{ rackId?: string }>;
        };

        test.expect(String(body.targetBuildingId)).toBe(buildingId.toString());
        test.expect(body.racks).toHaveLength(1);
        test.expect(String(body.racks[0]?.rackId)).toBe(rackId.toString());
        test.expect(response.status()).toBe(200);

        await racksPage.navigateToRacksPage();
        await racksPage.clickViewList();
        await racksPage.waitForRackListToLoad({ allowEmpty: false });
        const row = getListRowByName(page, rackLabel);
        await assertRackPlacementForMode({ row, mode: siteManagementMode, siteName, buildingName });
      });
    } finally {
      await deleteRackIfCreated({ racksPage, created: createdRack, rackLabel });
      await deleteBuildingIfCreated({
        page,
        created: createdBuilding,
        siteName,
        buildingName,
        mode: siteManagementMode,
      });
      await deleteSiteIfCreated({ page, created: createdSite, siteName, mode: siteManagementMode });
    }
  });

  test("Assign a single miner to a site from the Miners tab", async ({ page, commonSteps, minersPage, racksPage }) => {
    await commonSteps.loginAsAdmin();

    const siteManagementMode = await detectSiteManagementMode(page);
    const targetSiteName = generateRandomText("miner_site");
    let createdSite = false;
    let minerIp = "";
    let originalSiteLabel: string | undefined;
    let originalRackLabel: string | undefined;
    let originalRackPosition: string | undefined;

    try {
      const snapshots = await loadVisibleMiners({ page, commonSteps });
      const miner = pickUnrackedPairedMiner(snapshots);
      minerIp = miner.ipAddress;
      originalSiteLabel = miner.siteLabel || undefined;
      originalRackLabel = miner.rackLabel || undefined;
      originalRackPosition = miner.rackPosition || undefined;

      const targetSiteId = await createSite(page, targetSiteName, siteManagementMode);
      createdSite = true;

      const requestPromise = page.waitForRequest(new RegExp(ASSIGN_DEVICES_TO_SITE));
      const responsePromise = page.waitForResponse(new RegExp(ASSIGN_DEVICES_TO_SITE));

      await page.goto("/fleet/miners");
      await minersPage.waitForMinersListToLoad();

      await test.step("Move one miner into the target site", async () => {
        await moveSingleMinerToSite({ page, minersPage, minerIp: miner.ipAddress, siteName: targetSiteName });
      });

      await test.step("Validate the request and the target site miner count", async () => {
        const request = await requestPromise;
        const response = await responsePromise;
        const body = request.postDataJSON() as {
          targetSiteId?: string;
          deviceIdentifiers?: string[];
        };

        test.expect(String(body.targetSiteId)).toBe(targetSiteId.toString());
        test.expect(body.deviceIdentifiers).toEqual([miner.deviceIdentifier]);
        test.expect(response.status()).toBe(200);

        await assertSiteMinerCount({
          page,
          siteName: targetSiteName,
          mode: siteManagementMode,
          expectedCount: 1,
        });
      });
    } finally {
      await restoreMinerPlacementIfNeeded({
        page,
        minersPage,
        racksPage,
        minerIp,
        originalSiteLabel,
        originalRackLabel,
        originalRackPosition,
      });
      await deleteSiteIfCreated({ page, created: createdSite, siteName: targetSiteName, mode: siteManagementMode });
    }
  });

  test("Assign a single miner to a rack from the Miners tab", async ({ page, commonSteps, minersPage, racksPage }) => {
    await commonSteps.loginAsAdmin();

    const siteManagementMode = await detectSiteManagementMode(page);
    const targetSiteName = generateRandomText("rack_site");
    const rackLabel = generateRandomText("miner_rack");

    let createdSite = false;
    let createdRack = false;
    let minerIp = "";
    let originalSiteLabel: string | undefined;
    let originalRackLabel: string | undefined;
    let originalRackPosition: string | undefined;

    try {
      const snapshots = await loadVisibleMiners({ page, commonSteps });
      const miner = pickUnrackedPairedMiner(snapshots);
      minerIp = miner.ipAddress;
      originalSiteLabel = miner.siteLabel || undefined;
      originalRackLabel = miner.rackLabel || undefined;
      originalRackPosition = miner.rackPosition || undefined;

      await createSite(page, targetSiteName, siteManagementMode);
      createdSite = true;
      const rackId = await createRack({ page, racksPage, label: rackLabel, zone: TEMP_ZONE });
      createdRack = true;
      await assignRackToSite(page, rackLabel, targetSiteName);

      const requestPromise = page.waitForRequest(new RegExp(ASSIGN_DEVICES_TO_RACK));
      const responsePromise = page.waitForResponse(new RegExp(ASSIGN_DEVICES_TO_RACK));

      await page.goto("/fleet/miners");
      await minersPage.waitForMinersListToLoad();

      await test.step("Move one miner into the target rack", async () => {
        await moveSingleMinerToRack({ page, minersPage, minerIp: miner.ipAddress, rackLabel });
      });

      await test.step("Validate the request and target rack miner count", async () => {
        const request = await requestPromise;
        const response = await responsePromise;
        const body = request.postDataJSON() as {
          targetRackId?: string;
          deviceSelector?: {
            deviceList?: {
              deviceIdentifiers?: string[];
            };
          };
        };

        test.expect(String(body.targetRackId)).toBe(rackId.toString());
        test.expect(body.deviceSelector?.deviceList?.deviceIdentifiers).toEqual([miner.deviceIdentifier]);
        test.expect(response.status()).toBe(200);

        await racksPage.navigateToRacksPage();
        await racksPage.clickViewList();
        await racksPage.waitForRackListToLoad({ allowEmpty: false });
        const row = getListRowByName(page, rackLabel);
        await assertRackMembershipForMode({ row, mode: siteManagementMode, targetSiteName });
      });
    } finally {
      await restoreMinerPlacementIfNeeded({
        page,
        minersPage,
        racksPage,
        minerIp,
        originalSiteLabel,
        originalRackLabel,
        originalRackPosition,
      });
      await deleteRackIfCreated({ racksPage, created: createdRack, rackLabel });
      await deleteSiteIfCreated({ page, created: createdSite, siteName: targetSiteName, mode: siteManagementMode });
    }
  });
});
