import type { Page } from "@playwright/test";
import { DEFAULT_INTERVAL, DEFAULT_TIMEOUT, testConfig } from "../config/test.config";
import { expect, test } from "../fixtures/pageFixtures";

const DEFAULT_POOL_URL = "stratum+tcp://mine.ocean.xyz:3334";
const DEFAULT_POOL_NAME = "PoolNameVisualDefault";
const DEFAULT_POOL_USERNAME = "visual-default-user";

const EMPTY_HOME_SNAPSHOT = ["visual", "home-empty-fleet.png"];
const EMPTY_SITES_SNAPSHOT = ["visual", "fleet-sites-empty.png"];
const EMPTY_BUILDINGS_SNAPSHOT = ["visual", "fleet-buildings-empty.png"];
const EMPTY_RACKS_SNAPSHOT = ["visual", "fleet-racks-empty.png"];
const EMPTY_MINERS_SNAPSHOT = ["visual", "fleet-miners-empty.png"];
const GROUPS_SNAPSHOT = ["visual", "groups-screen.png"];
const ENERGY_SNAPSHOT = ["visual", "energy-screen.png"];
const SETTINGS_POOLS_SNAPSHOT = ["visual", "settings-pools-screen.png"];
const FIND_MINERS_SNAPSHOT = ["visual", "find-miners-screen.png"];
const COMPLETE_SETUP_SNAPSHOT = ["visual", "complete-setup-module.png"];
const SINGLE_MINER_ACTIONS_SNAPSHOT = ["visual", "single-miner-actions-menu.png"];
const BULK_ACTION_BAR_SNAPSHOT = ["visual", "miner-bulk-action-bar.png"];
const BULK_MORE_MENU_SNAPSHOT = ["visual", "miner-bulk-more-menu.png"];

async function authenticateAnyPendingMiners(homePage: {
  tryAction: (action: () => Promise<void>) => Promise<boolean>;
  clickAuthenticateMinersButton: () => Promise<void>;
  validateAuthenticateMinersModalTitle: () => Promise<void>;
  clickShowMinersButton: () => Promise<void>;
  getListOfMinersToAuthenticate: () => Promise<string[]>;
  inputMinerAuthUsername: (username: string) => Promise<void>;
  inputMinerAuthPassword: (password: string) => Promise<void>;
  clickAuthenticateMinersConfirmButton: () => Promise<void>;
  validateModalClosed: () => Promise<void>;
}) {
  const authenticateMinersButtonClicked = await homePage.tryAction(() => homePage.clickAuthenticateMinersButton());
  if (!authenticateMinersButtonClicked) {
    return;
  }

  await homePage.validateAuthenticateMinersModalTitle();
  await homePage.clickShowMinersButton();
  const miners = await homePage.getListOfMinersToAuthenticate();

  if (miners.some((miner) => miner.includes("S17 XP"))) {
    await homePage.inputMinerAuthUsername("root17");
    await homePage.inputMinerAuthPassword("root17");
    await homePage.clickAuthenticateMinersConfirmButton();
  }
  if (miners.some((miner) => miner.includes("S19 XP"))) {
    await homePage.inputMinerAuthUsername("root19");
    await homePage.inputMinerAuthPassword("root19");
    await homePage.clickAuthenticateMinersConfirmButton();
  }
  if (miners.some((miner) => miner.includes("S21 XP"))) {
    await homePage.inputMinerAuthUsername("root21");
    await homePage.inputMinerAuthPassword("root21");
    await homePage.clickAuthenticateMinersConfirmButton();
  }

  await homePage.validateModalClosed();
}

async function clearAncillaryFleetState({
  groupsPage,
  racksPage,
  fleetLocationsPage,
}: {
  groupsPage: {
    navigateToGroupsPage: () => Promise<void>;
    listSavedGroupNames: () => Promise<string[]>;
    deleteSavedGroupIfVisible: (groupName: string) => Promise<void>;
  };
  racksPage: {
    navigateToRacksPage: () => Promise<void>;
    clickViewList: () => Promise<void>;
    listRackNames: () => Promise<string[]>;
    deleteRackByLabelIfVisible: (label: string) => Promise<void>;
    tryAction: (action: () => Promise<void>, timeout?: number) => Promise<boolean>;
  };
  fleetLocationsPage: {
    listBuildingNames: () => Promise<string[]>;
    deleteBuildingByNameIfVisible: (name: string) => Promise<void>;
    listSiteNames: () => Promise<string[]>;
    deleteSiteByNameIfVisible: (name: string) => Promise<void>;
  };
}) {
  await groupsPage.navigateToGroupsPage();
  for (const groupName of await groupsPage.listSavedGroupNames()) {
    await groupsPage.deleteSavedGroupIfVisible(groupName);
  }

  await racksPage.navigateToRacksPage();
  await racksPage.tryAction(() => racksPage.clickViewList(), 2000);
  for (const rackName of await racksPage.listRackNames()) {
    await racksPage.deleteRackByLabelIfVisible(rackName);
  }

  for (const buildingName of await fleetLocationsPage.listBuildingNames()) {
    await fleetLocationsPage.deleteBuildingByNameIfVisible(buildingName);
  }

  for (const siteName of await fleetLocationsPage.listSiteNames()) {
    await fleetLocationsPage.deleteSiteByNameIfVisible(siteName);
  }
}

async function addAnyDiscoveredMiners({
  page,
  authPage,
  minersPage,
  addMinersPage,
}: {
  page: Page;
  authPage: { clickGetStarted: () => Promise<void> };
  minersPage: {
    tryAction: (action: () => Promise<void>, timeout?: number) => Promise<boolean>;
    clickAddMinersButton: () => Promise<void>;
    waitForMinersTitle: () => Promise<void>;
    waitForMinersListToLoad: () => Promise<void>;
  };
  addMinersPage: {
    clickFindMinersInNetwork: () => Promise<void>;
    waitForFoundMinersList: () => Promise<void>;
    getFoundMinersCount: () => Promise<number>;
    clickContinueWithSelectedMiners: () => Promise<void>;
    closeAddMinersFlowIfOpen: () => Promise<void>;
  };
}) {
  const addMinersButtonClicked = await minersPage.tryAction(() => minersPage.clickAddMinersButton());
  if (!addMinersButtonClicked) {
    await authPage.clickGetStarted();
  }

  await addMinersPage.clickFindMinersInNetwork();
  await addMinersPage.waitForFoundMinersList();
  await expect
    .poll(
      async () => {
        const scanningVisible = await page
          .getByRole("button", { name: "Scanning", exact: true })
          .isVisible()
          .catch(() => false);
        if (scanningVisible) {
          return "scanning";
        }

        const noMinersVisible = await page
          .getByText("No miners found", { exact: true })
          .isVisible()
          .catch(() => false);
        if (noMinersVisible) {
          return "none";
        }

        const foundMinersVisible = await page
          .getByText(/\d+ miners found on your network/)
          .isVisible()
          .catch(() => false);
        if (foundMinersVisible) {
          return "found";
        }

        return "pending";
      },
      { timeout: DEFAULT_TIMEOUT, intervals: [DEFAULT_INTERVAL] },
    )
    .toMatch(/found|none/);
  const foundMinerCount = await addMinersPage.getFoundMinersCount();

  if (foundMinerCount === 0) {
    await addMinersPage.closeAddMinersFlowIfOpen();
    return;
  }

  await addMinersPage.clickContinueWithSelectedMiners();
  await minersPage.waitForMinersTitle();
  await minersPage.waitForMinersListToLoad();
}

async function restoreFleetBaseline({
  page,
  authPage,
  homePage,
  minersPage,
  addMinersPage,
  settingsPage,
  settingsPoolsPage,
  editPoolPage,
  newPoolModal,
  loginModal,
}: {
  page: Page;
  authPage: { clickGetStarted: () => Promise<void> };
  homePage: {
    tryAction: (action: () => Promise<void>) => Promise<boolean>;
    clickAuthenticateMinersButton: () => Promise<void>;
    validateAuthenticateMinersModalTitle: () => Promise<void>;
    clickShowMinersButton: () => Promise<void>;
    getListOfMinersToAuthenticate: () => Promise<string[]>;
    inputMinerAuthUsername: (username: string) => Promise<void>;
    inputMinerAuthPassword: (password: string) => Promise<void>;
    clickAuthenticateMinersConfirmButton: () => Promise<void>;
    validateModalClosed: () => Promise<void>;
  };
  minersPage: {
    tryAction: (action: () => Promise<void>, timeout?: number) => Promise<boolean>;
    navigateToMinersPage: () => Promise<void>;
    clickAddMinersButton: () => Promise<void>;
    waitForMinersTitle: () => Promise<void>;
    waitForMinersListToLoad: () => Promise<void>;
    getMinersCount: () => Promise<number>;
    clickSelectAllCheckbox: () => Promise<void>;
    getSelectedMinersCount: () => Promise<number>;
    clickActionsMenuButton: () => Promise<void>;
    clickEditMiningPoolButton: () => Promise<void>;
    validateNoActionableMinerWithIssue: (issue: string, expectedCount?: number) => Promise<void>;
  };
  addMinersPage: {
    clickFindMinersInNetwork: () => Promise<void>;
    waitForFoundMinersList: () => Promise<void>;
    getFoundMinersCount: () => Promise<number>;
    clickContinueWithSelectedMiners: () => Promise<void>;
    closeAddMinersFlowIfOpen: () => Promise<void>;
  };
  settingsPage: { navigateToMiningPoolsSettings: () => Promise<void> };
  settingsPoolsPage: { validateMiningPoolsPageOpened: () => Promise<void>; deleteAllPools: () => Promise<void> };
  editPoolPage: {
    clickAddPoolButton: () => Promise<void>;
    clickAddNewPool: () => Promise<void>;
    clickAssignToXMiners: (count: number | Promise<number>) => Promise<void>;
    validateTextInToastGroup: (text: string) => Promise<void>;
  };
  newPoolModal: {
    inputPoolName: (name: string) => Promise<void>;
    inputPoolUrl: (url: string) => Promise<void>;
    inputPoolUsername: (username: string) => Promise<void>;
    clickSaveNewPool: () => Promise<void>;
  };
  loginModal: { loginAsAdmin: () => Promise<void> };
}) {
  await addMinersPage.closeAddMinersFlowIfOpen();
  await minersPage.navigateToMinersPage();
  await minersPage.waitForMinersTitle();
  await addAnyDiscoveredMiners({ page, authPage, minersPage, addMinersPage });
  await authenticateAnyPendingMiners(homePage);

  await settingsPage.navigateToMiningPoolsSettings();
  await settingsPoolsPage.validateMiningPoolsPageOpened();
  await settingsPoolsPage.deleteAllPools();

  await minersPage.navigateToMinersPage();
  await minersPage.waitForMinersTitle();
  const minerCount = await minersPage.getMinersCount();
  if (minerCount === 0) {
    return;
  }

  await minersPage.clickSelectAllCheckbox();
  const selectedMinerCount = await minersPage.getSelectedMinersCount();
  await minersPage.clickActionsMenuButton();
  await minersPage.clickEditMiningPoolButton();
  await loginModal.loginAsAdmin();

  await editPoolPage.clickAddPoolButton();
  await editPoolPage.clickAddNewPool();
  await newPoolModal.inputPoolName(DEFAULT_POOL_NAME);
  await newPoolModal.inputPoolUrl(DEFAULT_POOL_URL);
  await newPoolModal.inputPoolUsername(DEFAULT_POOL_USERNAME);
  await newPoolModal.clickSaveNewPool();
  await editPoolPage.clickAssignToXMiners(selectedMinerCount);
  await editPoolPage.validateTextInToastGroup("Assigned pools");
  await minersPage.validateNoActionableMinerWithIssue("Pool required", selectedMinerCount);
}

test.describe("Proto Fleet - Visual coverage @visual", () => {
  test.describe.configure({ mode: "serial" });

  test.beforeEach(
    async ({
      page,
      authPage,
      homePage,
      minersPage,
      addMinersPage,
      settingsPage,
      settingsPoolsPage,
      editPoolPage,
      newPoolModal,
      loginModal,
    }) => {
      await page.goto("/");
      await authPage.completeInitialSetupOrLogin(testConfig.users.admin.username, testConfig.users.admin.password);
      await restoreFleetBaseline({
        page,
        authPage,
        homePage,
        minersPage,
        addMinersPage,
        settingsPage,
        settingsPoolsPage,
        editPoolPage,
        newPoolModal,
        loginModal,
      });
    },
  );

  test.afterEach(
    async ({
      page,
      authPage,
      homePage,
      minersPage,
      addMinersPage,
      settingsPage,
      settingsPoolsPage,
      editPoolPage,
      newPoolModal,
      loginModal,
    }) => {
      await restoreFleetBaseline({
        page,
        authPage,
        homePage,
        minersPage,
        addMinersPage,
        settingsPage,
        settingsPoolsPage,
        editPoolPage,
        newPoolModal,
        loginModal,
      });
    },
  );

  test("captures empty-state and setup-focused visuals", async ({
    page,
    commonSteps,
    homePage,
    minersPage,
    addMinersPage,
    groupsPage,
    energyPage,
    settingsPage,
    settingsPoolsPage,
    fleetLocationsPage,
    racksPage,
  }) => {
    await test.step("Capture the complete-setup module on a partially configured fleet", async () => {
      await commonSteps.goToMinersPage();

      const authenticatedAntminerRow = page
        .getByTestId("list-body")
        .locator("tr")
        .filter({ has: page.getByText(/Antminer S(17|19|21) XP/) })
        .filter({ has: page.locator('input[type="checkbox"]:not([disabled])') })
        .first();
      await expect(authenticatedAntminerRow).toBeVisible();

      const minerIp = (await authenticatedAntminerRow.getByTestId("ipAddress").innerText()).trim();
      await minersPage.clickMinerThreeDotsButton(minerIp);
      await minersPage.clickUnpairButton();
      await minersPage.clickUnpairConfirm();
      await minersPage.validateMinerNotPresent(minerIp);

      await minersPage.clickAddMinersButton();
      await addMinersPage.clickFindMinersInNetwork();
      await addMinersPage.waitForNetworkScanToFinish();
      expect(await addMinersPage.getSelectedMinersCount()).toBe(1);
      await addMinersPage.clickContinueWithSelectedMiners();
      await minersPage.waitForMinersTitle();
      await minersPage.waitForMinersListToLoad();

      await homePage.navigateToHomePage();
      await homePage.validateHomePageOpened();
      await expect(homePage.getCompleteSetupModule()).toBeVisible();
      await expect(homePage.getCompleteSetupModule()).toHaveScreenshot(COMPLETE_SETUP_SNAPSHOT, {
        animations: "disabled",
        scale: "css",
      });
    });

    await test.step("Clear local fleet organization state before empty-state captures", async () => {
      await clearAncillaryFleetState({ groupsPage, racksPage, fleetLocationsPage });
    });

    await test.step("Unpair every miner to reach the empty fleet state", async () => {
      await commonSteps.goToMinersPage();
      await minersPage.clickSelectAllCheckbox();
      const selectedMinerCount = await minersPage.getSelectedMinersCount();
      await minersPage.clickActionsMenuButton();
      await minersPage.clickUnpairButton();
      await minersPage.clickUnpairConfirm();
      await minersPage.validateAmountOfMiners(0);
      await minersPage.validateTextIsVisible("You haven't paired any miners");
      expect(selectedMinerCount).toBeGreaterThan(0);
    });

    await test.step("Capture the empty home screen with navigation", async () => {
      await homePage.navigateToHomePage();
      await homePage.validateHomePageOpened();
      await homePage.validateTextIsVisible("Let's set up your fleet.");
      await expect(page).toHaveScreenshot(EMPTY_HOME_SNAPSHOT, {
        animations: "disabled",
        scale: "css",
      });
    });

    await test.step("Capture empty fleet location and miners screens", async () => {
      await fleetLocationsPage.navigateToSitesPage();
      await fleetLocationsPage.validateSitesPageOpened();
      await page.getByText("No sites yet", { exact: true }).waitFor();
      await expect(page).toHaveScreenshot(EMPTY_SITES_SNAPSHOT, {
        animations: "disabled",
        scale: "css",
      });

      await fleetLocationsPage.navigateToBuildingsPage();
      await expect(page.getByTestId("fleet-buildings-page")).toBeVisible();
      await page.getByText("No buildings yet", { exact: true }).waitFor();
      await expect(page).toHaveScreenshot(EMPTY_BUILDINGS_SNAPSHOT, {
        animations: "disabled",
        scale: "css",
      });

      await racksPage.navigateToRacksPage();
      await racksPage.validateRacksPageOpened();
      await racksPage.waitForRackListToLoad();
      await page.getByText("You haven't set up any racks", { exact: true }).waitFor();
      await expect(page).toHaveScreenshot(EMPTY_RACKS_SNAPSHOT, {
        animations: "disabled",
        scale: "css",
      });

      await minersPage.navigateToMinersPage();
      await minersPage.waitForMinersTitle();
      await minersPage.validateTextIsVisible("You haven't paired any miners");
      await expect(page).toHaveScreenshot(EMPTY_MINERS_SNAPSHOT, {
        animations: "disabled",
        scale: "css",
      });
    });

    await test.step("Capture empty and critical supporting screens", async () => {
      await groupsPage.navigateToGroupsPage();
      await page.getByText("Organize your miners into groups.", { exact: true }).waitFor();
      await expect(page).toHaveScreenshot(GROUPS_SNAPSHOT, {
        animations: "disabled",
        scale: "css",
      });

      await energyPage.navigateToEnergyPage();
      await energyPage.validateEnergyPageOpened();
      await expect(page).toHaveScreenshot(ENERGY_SNAPSHOT, {
        animations: "disabled",
        scale: "css",
      });

      await settingsPage.navigateToMiningPoolsSettings();
      await settingsPoolsPage.validateMiningPoolsPageOpened();
      await expect(page).toHaveScreenshot(SETTINGS_POOLS_SNAPSHOT, {
        animations: "disabled",
        scale: "css",
      });
    });

    await test.step("Capture the find-miners screen launched from Get started", async () => {
      await minersPage.navigateToMinersPage();
      await minersPage.clickGetStarted();
      await addMinersPage.validateAddMinersFlowOpened();
      await expect(page).toHaveScreenshot(FIND_MINERS_SNAPSHOT, {
        animations: "disabled",
        scale: "css",
      });
    });
  });

  test("captures populated miner action menus", async ({ commonSteps, minersPage }) => {
    await commonSteps.goToMinersPage();

    await test.step("Capture a single-miner action menu", async () => {
      await minersPage.openSingleMinerActionsForAuthenticatedMinerWithAction("mining-pool-popover-button");
      await expect(minersPage.getSingleMinerActionsPopover()).toBeVisible();
      await expect(minersPage.getSingleMinerActionsPopover()).toHaveScreenshot(SINGLE_MINER_ACTIONS_SNAPSHOT, {
        animations: "disabled",
        scale: "css",
      });
      await minersPage.dismissSingleMinerActionsPopoverIfVisible();
    });

    await test.step("Capture the bulk action bar and the More menu", async () => {
      const firstMinerIp = await minersPage.getAuthenticatedMinerIpAddressByIndex(0);
      const secondMinerIp = await minersPage.getAuthenticatedMinerIpAddressByIndex(1);

      await minersPage.clickMinerCheckbox(firstMinerIp);
      await minersPage.clickMinerCheckbox(secondMinerIp);
      await minersPage.validateActionBarMinerCount(2);

      await expect(minersPage.getActionBar()).toHaveScreenshot(BULK_ACTION_BAR_SNAPSHOT, {
        animations: "disabled",
        maxDiffPixels: 30,
        scale: "css",
      });

      await minersPage.clickActionsMenuButton();
      await expect(minersPage.getBulkActionsPopover()).toBeVisible();
      await expect(minersPage.getBulkActionsPopover()).toHaveScreenshot(BULK_MORE_MENU_SNAPSHOT, {
        animations: "disabled",
        scale: "css",
      });
    });
  });
});
