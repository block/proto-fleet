import { testConfig } from "../config/test.config";
import { test } from "../fixtures/pageFixtures";
import { OnboardingVisualHelper, VisualSnapshotHelper } from "../helpers/onboardingVisuals";

test.describe("Proto Fleet - Visual coverage @visual", () => {
  // eslint-disable-next-line playwright/no-skipped-test
  test.skip(testConfig.target === "real", "Visual snapshots are only supported against the fake E2E environment.");
  test.use({ storageState: { cookies: [], origins: [] } });

  test("Test1: capture the signup form and empty-state visuals", async ({
    page,
    addMinersPage,
    authPage,
    commonSteps,
    energyPage,
    fleetLocationsPage,
    groupsPage,
    homePage,
    minersPage,
    racksPage,
    settingsPage,
    settingsPoolsPage,
  }, testInfo) => {
    const visuals = new OnboardingVisualHelper({
      page,
      addMinersPage,
      authPage,
      commonSteps,
      energyPage,
      fleetLocationsPage,
      groupsPage,
      homePage,
      minersPage,
      racksPage,
      settingsPage,
      settingsPoolsPage,
      snapshots: new VisualSnapshotHelper(testInfo),
    });

    await test.step("Capture the sign up form", async () => {
      await visuals.openSignUpPage();
      await visuals.captureSignUpForm();
    });

    await test.step("Sign up as a new admin", async () => {
      await visuals.signUpAsNewAdmin();
    });

    await test.step("Capture the empty-state screens", async () => {
      await visuals.captureEmptyStateScreens();
    });
  });

  test("Test2: capture find-miners and complete-setup visuals", async ({
    page,
    addMinersPage,
    authPage,
    commonSteps,
    energyPage,
    fleetLocationsPage,
    groupsPage,
    homePage,
    minersPage,
    racksPage,
    settingsPage,
    settingsPoolsPage,
  }, testInfo) => {
    const visuals = new OnboardingVisualHelper({
      page,
      addMinersPage,
      authPage,
      commonSteps,
      energyPage,
      fleetLocationsPage,
      groupsPage,
      homePage,
      minersPage,
      racksPage,
      settingsPage,
      settingsPoolsPage,
      snapshots: new VisualSnapshotHelper(testInfo),
    });

    await test.step("Log in", async () => {
      await visuals.loginAsAdmin();
    });

    await test.step("Open Get started from the Fleet miners tab and capture the whole page", async () => {
      await visuals.openFindMinersFromMinersPage();
      await visuals.captureFindMinersScreen();
    });

    await test.step("Find miners, continue with 14 miners, and capture complete setup", async () => {
      await visuals.findAndContinueWithMiners(14);
      await visuals.captureCompleteSetupModule();
    });
  });

  test("Test3: capture single-miner and bulk action visuals", async ({
    page,
    addMinersPage,
    authPage,
    commonSteps,
    energyPage,
    fleetLocationsPage,
    groupsPage,
    homePage,
    minersPage,
    racksPage,
    settingsPage,
    settingsPoolsPage,
  }, testInfo) => {
    const visuals = new OnboardingVisualHelper({
      page,
      addMinersPage,
      authPage,
      commonSteps,
      energyPage,
      fleetLocationsPage,
      groupsPage,
      homePage,
      minersPage,
      racksPage,
      settingsPage,
      settingsPoolsPage,
      snapshots: new VisualSnapshotHelper(testInfo),
    });

    await test.step("Log in", async () => {
      await visuals.loginAsAdmin();
    });

    await test.step("Open the first Proto Rig action menu and capture it", async () => {
      await visuals.openSingleProtoRigActionsMenu();
      await visuals.captureSingleMinerActionsMenu();
    });

    await test.step("Select two Proto Rig miners and capture the bulk action bar", async () => {
      await visuals.selectProtoRigMiners(2);
      await visuals.captureBulkActionBar();
    });

    await test.step("Open More or Actions and capture the detailed actions menu", async () => {
      await visuals.openBulkActionsMenu();
      await visuals.captureBulkActionsMenu();
    });
  });
});
