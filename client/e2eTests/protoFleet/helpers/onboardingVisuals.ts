import type { Locator, Page, TestInfo } from "@playwright/test";
import { expect } from "@playwright/test";
import fs from "fs/promises";
import path from "path";
import { testConfig } from "../config/test.config";
import type { AddMinersPage } from "../pages/addMiners";
import type { AuthPage } from "../pages/auth";
import type { EnergyPage } from "../pages/energy";
import type { FleetLocationsPage } from "../pages/fleetLocations";
import type { GroupsPage } from "../pages/groups";
import type { HomePage } from "../pages/home";
import type { MinersPage } from "../pages/miners";
import type { RacksPage } from "../pages/racks";
import type { SettingsPage } from "../pages/settings";
import type { SettingsPoolsPage } from "../pages/settingsPools";
import type { CommonSteps } from "./commonSteps";

const OVERWRITE_VISUAL_SNAPSHOTS = process.env.PROTOFLEET_VISUAL_OVERWRITE === "1";
const DEFAULT_VISUAL_OPTIONS = { animations: "disabled" as const, scale: "css" as const };
const FIXED_VISUAL_DATE = "2026-01-15T12:00:00Z";

function delay(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

export const VISUAL_SNAPSHOTS = {
  signUpForm: ["visual", "sign-up-form.png"],
  emptyHome: ["visual", "home-empty-fleet.png"],
  emptySites: ["visual", "fleet-sites-empty.png"],
  emptyBuildings: ["visual", "fleet-buildings-empty.png"],
  emptyRacks: ["visual", "fleet-racks-empty.png"],
  emptyMiners: ["visual", "fleet-miners-empty.png"],
  groups: ["visual", "groups-screen.png"],
  energy: ["visual", "energy-screen.png"],
  settingsPools: ["visual", "settings-pools-screen.png"],
  navigationMenu: ["visual", "navigation-menu.png"],
  findMiners: ["visual", "find-miners-screen.png"],
  completeSetup: ["visual", "complete-setup-module.png"],
  singleMinerActions: ["visual", "single-miner-actions-menu.png"],
  bulkActionBar: ["visual", "miner-bulk-action-bar.png"],
  bulkMoreMenu: ["visual", "miner-bulk-more-menu.png"],
} as const;

export class VisualSnapshotHelper {
  constructor(private readonly testInfo: TestInfo) {}

  async capturePage(page: Page, snapshotName: readonly string[], options: { maxDiffPixels?: number } = {}) {
    if (OVERWRITE_VISUAL_SNAPSHOTS) {
      const snapshotPath = this.testInfo.snapshotPath(...snapshotName);
      await fs.mkdir(path.dirname(snapshotPath), { recursive: true });
      await page.screenshot({ path: snapshotPath, ...DEFAULT_VISUAL_OPTIONS });
      return;
    }

    await expect(page).toHaveScreenshot(snapshotName, { ...DEFAULT_VISUAL_OPTIONS, ...options });
  }

  async captureLocator(locator: Locator, snapshotName: readonly string[], options: { maxDiffPixels?: number } = {}) {
    if (OVERWRITE_VISUAL_SNAPSHOTS) {
      const snapshotPath = this.testInfo.snapshotPath(...snapshotName);
      await fs.mkdir(path.dirname(snapshotPath), { recursive: true });
      await locator.screenshot({ path: snapshotPath, ...DEFAULT_VISUAL_OPTIONS });
      return;
    }

    await expect(locator).toHaveScreenshot(snapshotName, { ...DEFAULT_VISUAL_OPTIONS, ...options });
  }
}

type OnboardingVisualDependencies = {
  page: Page;
  addMinersPage: AddMinersPage;
  authPage: AuthPage;
  commonSteps: CommonSteps;
  energyPage: EnergyPage;
  fleetLocationsPage: FleetLocationsPage;
  groupsPage: GroupsPage;
  homePage: HomePage;
  minersPage: MinersPage;
  racksPage: RacksPage;
  settingsPage: SettingsPage;
  settingsPoolsPage: SettingsPoolsPage;
  snapshots: VisualSnapshotHelper;
};

export class OnboardingVisualHelper {
  constructor(private readonly deps: OnboardingVisualDependencies) {}

  async openSignUpPage() {
    const { page, authPage } = this.deps;
    await page.clock.setFixedTime(FIXED_VISUAL_DATE);
    await page.goto("/welcome");
    await expect(page).toHaveURL(/\/welcome(?:[?#].*)?$/);
    await authPage.validateCreateCredentialsPrompt();
  }

  async captureSignUpForm() {
    const { authPage, snapshots } = this.deps;
    await snapshots.captureLocator(authPage.getCreateCredentialsForm(), VISUAL_SNAPSHOTS.signUpForm);
  }

  async signUpAsNewAdmin() {
    const { authPage } = this.deps;
    await authPage.inputUsername(testConfig.users.admin.username);
    await authPage.inputPassword(testConfig.users.admin.password);
    await authPage.clickContinue();
    await authPage.validateLoggedIn();
  }

  async loginAsAdmin() {
    const { page, commonSteps } = this.deps;
    await page.goto("/");
    await commonSteps.loginAsAdmin({ forceReauth: true });
    await this.clearCompleteSetupDismissals();
  }

  async captureEmptyStateScreens() {
    const {
      page,
      fleetLocationsPage,
      groupsPage,
      energyPage,
      minersPage,
      racksPage,
      settingsPage,
      settingsPoolsPage,
      snapshots,
    } = this.deps;

    await expect(page).toHaveURL(/\/onboarding\/miners(?:[?#].*)?$/);
    await minersPage.validateTextIsVisible("Let's set up your fleet.");
    await snapshots.capturePage(page, VISUAL_SNAPSHOTS.emptyHome);

    await fleetLocationsPage.navigateToSitesPage();
    await fleetLocationsPage.validateTextIsVisible("No sites yet");
    await snapshots.capturePage(page, VISUAL_SNAPSHOTS.emptySites);

    await fleetLocationsPage.navigateToBuildingsPage();
    await fleetLocationsPage.validateTextIsVisible("No buildings yet");
    await snapshots.capturePage(page, VISUAL_SNAPSHOTS.emptyBuildings);

    await racksPage.navigateToRacksPage();
    await racksPage.validateRacksPageOpened();
    await racksPage.waitForRackListToLoad();
    await racksPage.validateTextIsVisible("You haven't set up any racks");
    await snapshots.capturePage(page, VISUAL_SNAPSHOTS.emptyRacks);

    await minersPage.navigateToMinersPage();
    await minersPage.validateMinersPageOpened();
    await minersPage.validateTextIsVisible("You haven't paired any miners");
    await snapshots.capturePage(page, VISUAL_SNAPSHOTS.emptyMiners);

    await groupsPage.navigateToGroupsPage();
    await groupsPage.waitForSavedGroupsListToLoad();
    await groupsPage.validateTextIsVisible("Organize your miners into groups.");
    await snapshots.capturePage(page, VISUAL_SNAPSHOTS.groups);

    await energyPage.navigateToEnergyPage();
    await energyPage.validateEnergyPageOpened();
    await snapshots.capturePage(page, VISUAL_SNAPSHOTS.energy);

    await settingsPage.navigateToMiningPoolsSettings();
    await settingsPoolsPage.validateMiningPoolsPageOpened();
    await settingsPoolsPage.validateTextIsVisible("Add a pool to start assigning your miners.");
    await snapshots.capturePage(page, VISUAL_SNAPSHOTS.settingsPools);
  }

  async captureNavigationMenu() {
    const { minersPage, page, snapshots } = this.deps;
    await minersPage.clickNavigationMenuIfMobile();
    const navigationMenu = page.getByRole("navigation", { name: "Main" });
    await expect(navigationMenu).toBeVisible();
    await snapshots.captureLocator(navigationMenu, VISUAL_SNAPSHOTS.navigationMenu);
  }

  async openFindMinersFromMinersPage() {
    const { addMinersPage, minersPage, page } = this.deps;
    await minersPage.navigateToMinersPage();
    await minersPage.validateMinersPageOpened();
    await minersPage.waitForMinersPageContentToLoad();
    const openedViaGetStarted = await minersPage.tryAction(() => minersPage.clickGetStarted(), 2_000);
    if (!openedViaGetStarted) {
      await minersPage.clickAddMinersButton();
    }
    await addMinersPage.validateAddMinersFlowOpened();
    await expect(page.getByTestId("section-import-foreman")).toBeVisible();
  }

  async captureFindMinersScreen() {
    const { page, snapshots } = this.deps;
    await snapshots.capturePage(page, VISUAL_SNAPSHOTS.findMiners);
  }

  async findAndContinueWithMiners(expectedMinerCount: number) {
    const { addMinersPage } = this.deps;
    await addMinersPage.clickFindMinersInNetwork();
    await addMinersPage.waitForExpectedNetworkMinerCount(expectedMinerCount);
    await addMinersPage.clickContinueWithXMiners(expectedMinerCount);
    await this.waitForPairingToFinish(expectedMinerCount);
    await this.waitForCompleteSetupModuleReady();
  }

  async captureCompleteSetupModule() {
    const { homePage, snapshots } = this.deps;
    const module = homePage.getCompleteSetupModule();
    await this.waitForCompleteSetupModuleReady();
    await snapshots.captureLocator(module, VISUAL_SNAPSHOTS.completeSetup, { maxDiffPixels: 5000 });
  }

  async openSingleProtoRigActionsMenu() {
    const { minersPage } = this.deps;
    await this.goToReadyMinersPage();
    await minersPage.openSingleMinerActionsForFirstProtoRig();
  }

  async captureSingleMinerActionsMenu() {
    const { minersPage, snapshots } = this.deps;
    await snapshots.captureLocator(minersPage.getSingleMinerActionsPopover(), VISUAL_SNAPSHOTS.singleMinerActions);
  }

  async selectProtoRigMiners(count: number) {
    const { minersPage } = this.deps;
    await minersPage.dismissSingleMinerActionsPopoverIfVisible();
    await minersPage.selectProtoRigMiners(count);
    await expect(minersPage.getActionBar()).toBeVisible();
  }

  async captureBulkActionBar() {
    const { minersPage, snapshots } = this.deps;
    await snapshots.captureLocator(minersPage.getActionBar(), VISUAL_SNAPSHOTS.bulkActionBar);
  }

  async openBulkActionsMenu() {
    const { minersPage } = this.deps;
    await minersPage.clickBulkActionsMoreButton();
  }

  async captureBulkActionsMenu() {
    const { minersPage, snapshots } = this.deps;
    await snapshots.captureLocator(minersPage.getBulkActionsPopover(), VISUAL_SNAPSHOTS.bulkMoreMenu);
  }

  private async waitForCompleteSetupModuleReady() {
    const { homePage, page } = this.deps;
    const module = homePage.getCompleteSetupModule();
    const authenticateCard = homePage.getCompleteSetupCard("Authenticate miners");
    const configurePoolsCard = homePage.getCompleteSetupCard("Configure pools");

    await expect(page).toHaveURL(/\/(dashboard|fleet\/miners)(?:[?#].*)?$/);
    await expect(async () => {
      await expect(module).toBeVisible({ timeout: 2_000 });
      await expect(authenticateCard).toBeVisible({ timeout: 2_000 });
      await expect(configurePoolsCard).toBeVisible({ timeout: 2_000 });
      await expect(homePage.getCompleteSetupButton("Authenticate")).toBeVisible({ timeout: 2_000 });
      await expect(homePage.getCompleteSetupButton("Configure")).toBeVisible({ timeout: 2_000 });
    }).toPass({ timeout: testConfig.testTimeout, intervals: [250, 500, 1_000] });

    await this.waitForCompleteSetupLayoutToSettle(module, authenticateCard, configurePoolsCard);
  }

  private async clearCompleteSetupDismissals() {
    const { page } = this.deps;

    await page.evaluate(() => {
      localStorage.removeItem("completeSetupDismissed");
      localStorage.removeItem("configurePoolDismissed");
    });
  }

  private async goToReadyMinersPage() {
    const { minersPage } = this.deps;
    await minersPage.navigateToMinersPage();
    await minersPage.validateMinersPageOpened();
    await minersPage.waitForMinersListToLoad();
  }

  private async waitForPairingToFinish(expectedMinerCount: number) {
    const { addMinersPage, minersPage } = this.deps;
    await expect(async () => {
      await addMinersPage.validateAddMinersFlowClosed();
      await minersPage.validateMinersPageOpened();
      await minersPage.validateMinersAdded(expectedMinerCount);
    }).toPass({ timeout: testConfig.testTimeout });
  }

  private async completeSetupCardsDoNotOverlap(firstCard: Locator, secondCard: Locator): Promise<boolean> {
    const [firstBox, secondBox] = await Promise.all([firstCard.boundingBox(), secondCard.boundingBox()]);
    if (!firstBox || !secondBox) {
      return false;
    }

    const sameRow = Math.abs(firstBox.y - secondBox.y) <= 4;
    if (sameRow) {
      return true;
    }

    return firstBox.y + firstBox.height <= secondBox.y + 1 || secondBox.y + secondBox.height <= firstBox.y + 1;
  }

  private async waitForCompleteSetupLayoutToSettle(
    module: Locator,
    authenticateCard: Locator,
    configurePoolsCard: Locator,
  ) {
    const stableSamples = 3;
    const intervalMs = 100;
    const maxWaitMs = 2_000;

    let previousSignature = await this.getLayoutSignature(module, authenticateCard, configurePoolsCard);
    let stableCount = 0;
    const deadline = Date.now() + maxWaitMs;

    while (Date.now() < deadline) {
      await delay(intervalMs);
      const nextSignature = await this.getLayoutSignature(module, authenticateCard, configurePoolsCard);
      const cardsDoNotOverlap = await this.completeSetupCardsDoNotOverlap(authenticateCard, configurePoolsCard);

      if (this.layoutSignaturesMatch(previousSignature, nextSignature) && cardsDoNotOverlap) {
        stableCount += 1;
        if (stableCount >= stableSamples) {
          return;
        }
      } else {
        stableCount = 0;
      }

      previousSignature = nextSignature;
    }
  }

  private async getLayoutSignature(...locators: Locator[]) {
    const boxes = await Promise.all(locators.map(async (locator) => locator.boundingBox()));

    if (boxes.some((box) => !box)) {
      throw new Error("Expected complete setup layout boxes to be available while waiting for layout to settle.");
    }

    return boxes.map((box) => ({
      x: Number(box!.x.toFixed(2)),
      y: Number(box!.y.toFixed(2)),
      width: Number(box!.width.toFixed(2)),
      height: Number(box!.height.toFixed(2)),
    }));
  }

  private layoutSignaturesMatch(
    previousSignature: Array<{ x: number; y: number; width: number; height: number }>,
    nextSignature: Array<{ x: number; y: number; width: number; height: number }>,
  ) {
    return previousSignature.every((previousBox, index) => {
      const nextBox = nextSignature[index];
      return (
        Math.abs(previousBox.x - nextBox.x) <= 0.5 &&
        Math.abs(previousBox.y - nextBox.y) <= 0.5 &&
        Math.abs(previousBox.width - nextBox.width) <= 0.5 &&
        Math.abs(previousBox.height - nextBox.height) <= 0.5
      );
    });
  }
}
