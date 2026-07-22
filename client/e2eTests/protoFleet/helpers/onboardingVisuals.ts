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
    await page.goto("/auth");
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

  async openFindMinersFromMinersPage() {
    const { addMinersPage, minersPage, page } = this.deps;
    await minersPage.navigateToMinersPage();
    await minersPage.validateMinersPageOpened();
    await minersPage.clickGetStarted();
    await addMinersPage.validateAddMinersFlowOpened();
    await expect(page.getByTestId("section-import-foreman")).toBeVisible();
  }

  async captureFindMinersScreen() {
    const { page, snapshots } = this.deps;
    await snapshots.capturePage(page, VISUAL_SNAPSHOTS.findMiners);
  }

  async findAndContinueWithMiners(expectedMinerCount: number) {
    const { addMinersPage, homePage, minersPage } = this.deps;
    await addMinersPage.clickFindMinersInNetwork();
    await addMinersPage.waitForExpectedNetworkMinerCount(expectedMinerCount);
    await addMinersPage.clickContinueWithXMiners(expectedMinerCount);
    await minersPage.validateMinersAdded(expectedMinerCount);
    await homePage.navigateToHomePage();
    await homePage.validateHomePageOpened();
  }

  async captureCompleteSetupModule() {
    const { homePage, snapshots } = this.deps;
    const module = homePage.getCompleteSetupModule();
    await homePage.validateDashboardSectionVisible("Your fleet");
    await expect(module).toBeVisible();
    await expect(module.getByText("Configure pools", { exact: true })).toBeVisible();
    await expect(module.getByRole("button", { name: "Configure", exact: true })).toBeVisible();
    await expect(module.getByText("Authenticate miners", { exact: true })).toBeVisible();
    await expect(module.getByRole("button", { name: "Authenticate", exact: true })).toBeVisible();
    await snapshots.captureLocator(module, VISUAL_SNAPSHOTS.completeSetup);
  }

  async openSingleProtoRigActionsMenu() {
    const { minersPage } = this.deps;
    await minersPage.navigateToMinersPage();
    await minersPage.validateMinersPageOpened();
    await minersPage.waitForMinersListToLoad();
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
}
