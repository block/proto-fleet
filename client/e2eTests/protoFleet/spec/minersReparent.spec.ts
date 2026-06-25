import { type Page, type Response as PlaywrightResponse } from "@playwright/test";
import { test } from "../fixtures/pageFixtures";
import { type CommonSteps } from "../helpers/commonSteps";
import { generateRandomText } from "../helpers/testDataHelper";
import { type MinersPage } from "../pages/miners";
import { type RacksPage } from "../pages/racks";
import { PairingStatus } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";

type SiteManagementMode = "fleet";

type PlacementRefSnapshot = {
  id?: string;
  label?: string;
};

type VisibleMinerSnapshot = {
  deviceIdentifier: string;
  ipAddress: string;
  pairingStatus: PairingStatus;
  rackPosition: string;
  placement?: {
    site?: PlacementRefSnapshot;
    building?: PlacementRefSnapshot;
    rack?: PlacementRefSnapshot;
  };
};

type VisibleMinerCandidate = Pick<
  VisibleMinerSnapshot,
  "ipAddress" | "deviceIdentifier" | "rackPosition" | "placement"
>;

const LIST_MINERS_RESPONSE = "ListMinerStateSnapshots";
const ASSIGN_DEVICES_TO_SITE = "AssignDevicesToSite";
const ASSIGN_DEVICES_TO_RACK = "AssignDevicesToRack";
const ASSIGN_RACKS_TO_BUILDING = "AssignRacksToBuilding";
const ASSIGN_RACKS_TO_SITE = "AssignRacksToSite";
const TEMP_ZONE = "ReparentAutomationZone";
const ACTIVE_SITE_STORAGE_KEY = "proto-fleet-multi-site";

function getListRowByName(page: Page, name: string) {
  return page
    .getByTestId("list-row")
    .filter({ has: page.getByTestId("name").getByText(name, { exact: true }) })
    .first();
}

function getMinerRowByIp(page: Page, ipAddress: string) {
  return page
    .getByTestId("list-row")
    .filter({ has: page.getByText(ipAddress, { exact: true }) })
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

async function selectAllSitesIfNeeded(page: Page) {
  const sitePickerTrigger = page.getByTestId("site-picker-trigger");
  if (!(await sitePickerTrigger.isVisible().catch(() => false))) {
    await test.expect(page.getByTestId("fleet-sites-page")).toBeVisible();
    return;
  }

  const currentLabel = (await sitePickerTrigger.textContent())?.trim();
  if (currentLabel === "All sites") {
    return;
  }

  await sitePickerTrigger.click();
  const allSitesOption = page.getByTestId("site-picker-option-all");
  await test.expect(allSitesOption).toBeVisible();
  await allSitesOption.click();
  await test.expect(sitePickerTrigger).toContainText("All sites");
}

async function clickAddSiteButton(page: Page) {
  const headerAddSiteButton = page.getByTestId("fleet-sites-add");
  if (await headerAddSiteButton.isVisible().catch(() => false)) {
    await headerAddSiteButton.click();
    return;
  }

  const emptyStateAddSiteButton = page.getByRole("button", { name: "Add a site", exact: true });
  await test.expect(emptyStateAddSiteButton).toBeVisible();
  await emptyStateAddSiteButton.click();
}

async function clickAddBuildingButton(page: Page) {
  const headerAddBuildingButton = page.getByTestId("fleet-buildings-add");
  if (await headerAddBuildingButton.isVisible().catch(() => false)) {
    await headerAddBuildingButton.click();
    return;
  }

  const emptyStateAddBuildingButton = page.getByRole("button", { name: "Add building", exact: true });
  await test.expect(emptyStateAddBuildingButton).toBeVisible();
  await emptyStateAddBuildingButton.click();
}

async function detectSiteManagementMode(page: Page): Promise<SiteManagementMode> {
  await resetActiveSiteSelection(page);
  await openSitesManagementPage(page, "fleet");
  return "fleet";
}

async function openSitesManagementPage(page: Page, mode: SiteManagementMode) {
  void mode;
  await page.goto("/fleet/sites");
  await test.expect(page).toHaveURL(/\/fleet\/sites(?:[?#].*)?$/);
  await test.expect(page.getByTestId("fleet-sites-redirecting")).toHaveCount(0);
  await test.expect(page.getByTestId("fleet-sites-page")).toBeVisible();
  await selectAllSitesIfNeeded(page);

  const sitePickerTrigger = page.getByTestId("site-picker-trigger");
  if (await sitePickerTrigger.isVisible().catch(() => false)) {
    await page.goto("/fleet/sites");
    await test.expect(page).toHaveURL(/\/fleet\/sites(?:[?#].*)?$/);
    await test.expect(page.getByTestId("fleet-sites-redirecting")).toHaveCount(0);
    await test.expect(page.getByTestId("fleet-sites-page")).toBeVisible();
    await test.expect(sitePickerTrigger).toContainText("All sites");
  }
}

async function openBuildingsManagementPage(page: Page, mode: SiteManagementMode) {
  await openSitesManagementPage(page, mode);
  await page.goto("/fleet/buildings");
  await test.expect(page).toHaveURL(/\/fleet\/buildings(?:[?#].*)?$/);
  await test.expect(page.getByTestId("fleet-buildings-page")).toBeVisible();
}

async function openFullScreenOverflowMenu(page: Page) {
  const overflowTrigger = page.getByTestId("full-screen-two-pane-modal").getByTestId("overflow-menu-trigger");
  await test.expect(overflowTrigger).toBeVisible();
  await overflowTrigger.click();
  return page.locator("div.fixed.inset-0.z-60");
}

async function clickManageSiteDelete(page: Page) {
  const manageSiteDeleteButton = page.locator('[data-testid="manage-site-modal-delete"]:visible');
  if (await manageSiteDeleteButton.isVisible().catch(() => false)) {
    await manageSiteDeleteButton.click();
    return;
  }

  const siteSettingsDeleteButton = page.locator('[data-testid="site-settings-modal-delete"]:visible');
  if (await siteSettingsDeleteButton.isVisible().catch(() => false)) {
    await siteSettingsDeleteButton.click();
    return;
  }

  const overflowMenu = await openFullScreenOverflowMenu(page);
  const deleteSiteAction = overflowMenu.getByText("Delete site", { exact: true });
  if (await deleteSiteAction.isVisible().catch(() => false)) {
    await deleteSiteAction.click();
    return;
  }

  await overflowMenu.getByText("Site settings", { exact: true }).click();
  await test.expect(siteSettingsDeleteButton).toBeVisible();
  await siteSettingsDeleteButton.click();
}

async function clickManageBuildingDelete(page: Page) {
  const deleteButton = page.locator('[data-testid="manage-building-delete"]:visible');
  if (await deleteButton.isVisible().catch(() => false)) {
    await deleteButton.click();
    return;
  }

  const overflowMenu = await openFullScreenOverflowMenu(page);
  await overflowMenu.getByText("Delete building", { exact: true }).click();
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

async function openRowActions(page: Page, name: string) {
  const row = getListRowByName(page, name);
  await test.expect(row).toBeVisible();
  await row.locator('button[data-testid$="-actions-trigger"]').first().click();
}

async function clickRowAction(page: Page, label: string) {
  await page.getByText(label, { exact: true }).click();
}

async function selectParentPickerTarget(page: Page, label: string) {
  const modal = page.getByTestId("modal");
  await modal.getByText(label, { exact: true }).click();
  await modal.getByRole("button", { name: "Save", exact: true }).click();
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

function isBlankListCell(value: string | null | undefined): boolean {
  const normalized = value?.trim();
  return normalized === undefined || normalized === "" || normalized === "—";
}

function normalizePlacementLabel(value: string | null | undefined): string | undefined {
  return isBlankListCell(value) ? undefined : value?.trim();
}

async function rowHasBlankBuildingAndRack(row: ReturnType<typeof getMinerRowByIp>): Promise<boolean> {
  const buildingText = await row
    .getByTestId("building")
    .textContent()
    .catch(() => null);
  const rackText = await row
    .getByTestId("rack")
    .textContent()
    .catch(() => null);
  return isBlankListCell(buildingText) && isBlankListCell(rackText);
}

async function hasAuthenticationRequiredIssue(row: ReturnType<typeof getMinerRowByIp>): Promise<boolean> {
  return (
    (await row
      .getByRole("button", { name: "Authentication required", exact: true })
      .count()
      .catch(() => 0)) > 0
  );
}

async function pickUnrackedPairedMiner(page: Page, miners: VisibleMinerSnapshot[]): Promise<VisibleMinerCandidate> {
  const rows = page.getByTestId("list-body").locator("tr");
  const rowCount = await rows.count();

  for (let index = 0; index < rowCount; index++) {
    const row = rows.nth(index);
    const ipAddress = normalizePlacementLabel(
      await row
        .getByTestId("ipAddress")
        .textContent()
        .catch(() => null),
    );
    if (!ipAddress) {
      continue;
    }

    if (!(await rowHasBlankBuildingAndRack(getMinerRowByIp(page, ipAddress)))) {
      continue;
    }
    if (await hasAuthenticationRequiredIssue(getMinerRowByIp(page, ipAddress))) {
      continue;
    }

    const miner = miners.find((snapshot) => snapshot.ipAddress === ipAddress);
    if (miner) {
      return miner;
    }

    return { ipAddress };
  }

  throw new Error("Expected at least one visible miner without building or rack placement for reparent coverage");
}

async function createSite(page: Page, name: string, mode: SiteManagementMode): Promise<bigint> {
  await openSitesManagementPage(page, mode);
  await clickAddSiteButton(page);
  await page.getByTestId("site-settings-name-input").fill(name);
  await page.getByTestId("site-settings-modal-continue").click();
  const saveSiteButton = page.locator('[data-testid="manage-site-modal-save"]:visible');
  await test.expect(saveSiteButton).toBeVisible();
  await saveSiteButton.click();

  const row = getListRowByName(page, name);
  await test.expect(row).toBeVisible();
  return await getScopeIdFromRowName(page, name, "site");
}

async function deleteSite(page: Page, name: string, mode: SiteManagementMode) {
  void mode;
  await openSitesManagementPage(page, "fleet");
  await openRowActions(page, name);
  await clickRowAction(page, "Edit site");
  await clickManageSiteDelete(page);
  const confirmDeleteButton = page.getByTestId("site-delete-dialog-confirm");
  await test.expect(confirmDeleteButton).toBeVisible();
  await confirmDeleteButton.click({ trial: true });
  await confirmDeleteButton.click();
  await test.expect(getListRowByName(page, name)).toHaveCount(0);
}

async function createBuilding(
  page: Page,
  siteName: string,
  buildingName: string,
  mode: SiteManagementMode,
): Promise<bigint> {
  void mode;
  await openBuildingsManagementPage(page, "fleet");
  await clickAddBuildingButton(page);
  await page.getByTestId("building-settings-site-select").click();
  await page.getByRole("option", { name: siteName, exact: true }).click();
  await page.getByTestId("building-settings-name-input").fill(buildingName);
  await page.getByTestId("building-settings-modal-save").click();

  const row = getListRowByName(page, buildingName);
  await test.expect(row).toBeVisible();
  return await getScopeIdFromRowName(page, buildingName, "building");
}

async function deleteBuilding(page: Page, siteName: string, name: string, mode: SiteManagementMode) {
  void siteName;
  void mode;
  await openBuildingsManagementPage(page, "fleet");
  await openRowActions(page, name);
  await clickRowAction(page, "Edit building");
  await clickManageBuildingDelete(page);
  const confirmDeleteButton = page.getByTestId("building-delete-dialog-confirm");
  await test.expect(confirmDeleteButton).toBeVisible();
  await confirmDeleteButton.click({ trial: true });
  await confirmDeleteButton.click();
  await test.expect(getListRowByName(page, name)).toHaveCount(0);
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
  await racksPage.enableCustomRackLayout();
  await racksPage.inputColumns(2);
  await racksPage.inputRows(2);
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
  const row = getListRowByName(page, siteName);
  await test.expect(row.getByTestId("miners")).toHaveText(String(expectedCount));
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
  void mode;
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
  void mode;
  await test.expect(row.getByTestId("site")).toHaveText(targetSiteName);
}

function expectSingleDeviceIdentifier(actual: string[] | undefined, expected?: string) {
  if (expected) {
    test.expect(actual).toEqual([expected]);
    return;
  }

  test.expect(actual).toHaveLength(1);
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
    await page.addInitScript(
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
      const miner = await pickUnrackedPairedMiner(page, snapshots);
      const minerRow = getMinerRowByIp(page, miner.ipAddress);
      minerIp = miner.ipAddress;
      originalSiteLabel = normalizePlacementLabel(
        await minerRow
          .getByTestId("site")
          .textContent()
          .catch(() => null),
      );
      originalRackLabel = miner.placement?.rack?.label || undefined;
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
        expectSingleDeviceIdentifier(body.deviceIdentifiers, miner.deviceIdentifier);
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
      const miner = await pickUnrackedPairedMiner(page, snapshots);
      const minerRow = getMinerRowByIp(page, miner.ipAddress);
      minerIp = miner.ipAddress;
      originalSiteLabel = normalizePlacementLabel(
        await minerRow
          .getByTestId("site")
          .textContent()
          .catch(() => null),
      );
      originalRackLabel = miner.placement?.rack?.label || undefined;
      originalRackPosition = miner.rackPosition || undefined;

      await createSite(page, targetSiteName, siteManagementMode);
      createdSite = true;
      const rackId = await createRack({ page, racksPage, label: rackLabel, zone: TEMP_ZONE });
      createdRack = true;
      const rackSiteRequestPromise = page.waitForRequest(new RegExp(ASSIGN_RACKS_TO_SITE));
      const rackSiteResponsePromise = page.waitForResponse(new RegExp(ASSIGN_RACKS_TO_SITE));
      await assignRackToSite(page, rackLabel, targetSiteName);
      await rackSiteRequestPromise;
      await rackSiteResponsePromise;

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
        expectSingleDeviceIdentifier(body.deviceSelector?.deviceList?.deviceIdentifiers, miner.deviceIdentifier);
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
