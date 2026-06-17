import { test } from "../fixtures/pageFixtures";
import { formatPowerFilterSummary, getFirstVisibleIpv4MinerIp, toSubnet24 } from "../helpers/minersFiltersViews";
import { generateRandomText } from "../helpers/testDataHelper";

test.describe("Proto Fleet - Miners filters and saved views", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/");
  });

  test("Numeric and subnet filters persist through reload and clear cleanly", async ({
    minersPage,
    commonSteps,
    page,
  }) => {
    let initialMinerCount = 0;
    let filteredMinerIp = "";
    let targetSubnet = "";
    let powerMin: number | undefined;
    let powerMax: number | undefined;

    await commonSteps.loginAsAdmin();
    await commonSteps.goToMinersPage();

    await test.step("Capture a target miner and its filter values", async () => {
      initialMinerCount = await minersPage.getMinersCount();
      filteredMinerIp = await getFirstVisibleIpv4MinerIp(minersPage);
      targetSubnet = toSubnet24(filteredMinerIp);

      powerMin = 2;
      powerMax = undefined;

      test.expect(initialMinerCount).toBeGreaterThan(0);
    });

    await test.step("Apply subnet and power filters", async () => {
      await minersPage.applySubnetFilter([targetSubnet]);
      await minersPage.waitForMinersListToLoad();
      await minersPage.applyPowerFilter(powerMin, powerMax);
      await minersPage.waitForMinersListToLoad();
    });

    await test.step("Validate filtered results, chips, and URL", async () => {
      filteredMinerIp = await minersPage.getMinerIpAddressByIndex(0);
      await minersPage.validateActiveFilterSummary("subnet", targetSubnet);
      await minersPage.validateActiveFilterSummary("power", formatPowerFilterSummary(powerMin, powerMax));
      await minersPage.validateMinerInList(filteredMinerIp);
      test.expect(await minersPage.getMinersCount()).toBeGreaterThan(0);

      const searchParams = new URL(page.url()).searchParams;
      test.expect(searchParams.getAll("subnet")).toEqual([targetSubnet]);
      test.expect(searchParams.get("power_min")).toBe(String(powerMin));
      test.expect(searchParams.get("power_max")).toBeNull();
    });

    await test.step("Reload and validate the filters persist", async () => {
      await minersPage.reloadPage();
      await minersPage.waitForMinersTitle();
      await minersPage.waitForMinersListToLoad();

      await minersPage.validateActiveFilterSummary("subnet", targetSubnet);
      await minersPage.validateActiveFilterSummary("power", formatPowerFilterSummary(powerMin, powerMax));
      await minersPage.validateMinerInList(filteredMinerIp);
      test.expect(await minersPage.getMinersCount()).toBeGreaterThan(0);
    });

    await test.step("Apply a strict power filter and validate the empty state", async () => {
      await minersPage.applyPowerFilter(50, 50);
      await minersPage.validateNoResultsEmptyState();
    });

    await test.step("Clear the filters and validate the full list returns", async () => {
      await minersPage.clickClearAllFilters();
      await minersPage.waitForMinersListToLoad();

      test.expect(await minersPage.getMinersCount()).toBe(initialMinerCount);

      const searchParams = new URL(page.url()).searchParams;
      test.expect(searchParams.getAll("subnet")).toEqual([]);
      test.expect(searchParams.get("power_min")).toBeNull();
      test.expect(searchParams.get("power_max")).toBeNull();
    });
  });

  test("Saved view can be created and reset back to its saved filters", async ({ minersPage, commonSteps, page }) => {
    const viewName = generateRandomText("miners_view");
    let firstMinerIp = "";
    let firstMinerSubnet = "";
    const dirtyPowerMin = 2;
    const dirtyPowerMax = undefined;

    await commonSteps.loginAsAdmin();
    await commonSteps.goToMinersPage();

    await test.step("Capture a miner and save a view for its subnet", async () => {
      firstMinerIp = await getFirstVisibleIpv4MinerIp(minersPage);
      firstMinerSubnet = toSubnet24(firstMinerIp);

      await minersPage.applySubnetFilter([firstMinerSubnet]);
      await minersPage.waitForMinersListToLoad();
      firstMinerIp = await minersPage.getMinerIpAddressByIndex(0);
      await minersPage.clickNewSavedViewButton();
      await minersPage.validateViewModalOpened("New view");
      await minersPage.inputViewName(viewName);
      await minersPage.saveNewView();
    });

    await test.step("Validate the new view is active", async () => {
      await minersPage.validateViewTabVisible(viewName);
      await minersPage.validateViewTabActive(viewName);
      await minersPage.validateActiveFilterSummary("subnet", firstMinerSubnet);
      await minersPage.validateMinerInList(firstMinerIp);
    });

    await test.step("Change the live filters so the view becomes dirty", async () => {
      await minersPage.applyPowerFilter(dirtyPowerMin, dirtyPowerMax);
      await minersPage.waitForMinersListToLoad();

      await minersPage.validateActiveFilterSummary("subnet", firstMinerSubnet);
      await minersPage.validateActiveFilterSummary("power", formatPowerFilterSummary(dirtyPowerMin, dirtyPowerMax));
      test.expect(await minersPage.getMinersCount()).toBeGreaterThan(0);
    });

    await test.step("Reset the view back to the saved filters", async () => {
      await minersPage.clickResetViewAction(viewName);
      await minersPage.waitForMinersListToLoad();

      await minersPage.validateViewTabActive(viewName);
      await minersPage.validateActiveFilterSummary("subnet", firstMinerSubnet);
      await minersPage.validateActiveFilterNotVisible("Power");
      await minersPage.validateMinerInList(firstMinerIp);
      test.expect(new URL(page.url()).searchParams.getAll("subnet")).toEqual([firstMinerSubnet]);
      test.expect(new URL(page.url()).searchParams.get("power_min")).toBeNull();
      test.expect(new URL(page.url()).searchParams.get("power_max")).toBeNull();
    });
  });

  test("Saved view can be updated after the filters change", async ({ minersPage, commonSteps, page }) => {
    const viewName = generateRandomText("miners_view");
    let firstMinerIp = "";
    let firstMinerSubnet = "";
    let updatedMinerIp = "";
    const updatedPowerMin = 2;
    const updatedPowerMax = undefined;

    await commonSteps.loginAsAdmin();
    await commonSteps.goToMinersPage();

    await test.step("Create a saved view from the first miner subnet", async () => {
      firstMinerIp = await getFirstVisibleIpv4MinerIp(minersPage);
      firstMinerSubnet = toSubnet24(firstMinerIp);

      await minersPage.applySubnetFilter([firstMinerSubnet]);
      await minersPage.waitForMinersListToLoad();
      firstMinerIp = await minersPage.getMinerIpAddressByIndex(0);
      await minersPage.clickNewSavedViewButton();
      await minersPage.validateViewModalOpened("New view");
      await minersPage.inputViewName(viewName);
      await minersPage.saveNewView();
    });

    await test.step("Change the active filters by adding a power filter", async () => {
      await minersPage.applyPowerFilter(updatedPowerMin, updatedPowerMax);
      await minersPage.waitForMinersListToLoad();
      updatedMinerIp = await minersPage.getMinerIpAddressByIndex(0);

      await minersPage.validateActiveFilterSummary("subnet", firstMinerSubnet);
      await minersPage.validateActiveFilterSummary("power", formatPowerFilterSummary(updatedPowerMin, updatedPowerMax));
      await minersPage.validateMinerInList(updatedMinerIp);
    });

    await test.step("Update the saved view to include the power filter", async () => {
      await minersPage.clickUpdateViewAction(viewName);
      await minersPage.validateViewModalOpened("Update view");
      await minersPage.updateSavedView();
    });

    await test.step("Reload the page and wait for the miners view to stabilize", async () => {
      await minersPage.reloadPage();
      await minersPage.waitForMinersTitle();
      await minersPage.waitForMinersListToLoad();
    });

    await test.step("Clear the active view before reopening it", async () => {
      await minersPage.clickClearActiveView();
      await minersPage.waitForMinersListToLoad();
    });

    await test.step("Reopen the updated saved view", async () => {
      await minersPage.clickViewTab(viewName);
      await minersPage.waitForMinersListToLoad();
    });

    await test.step("Validate the updated view now restores the new filters", async () => {
      await minersPage.validateViewTabActive(viewName);
      await minersPage.validateActiveFilterSummary("subnet", firstMinerSubnet);
      await minersPage.validateActiveFilterSummary("power", formatPowerFilterSummary(updatedPowerMin, updatedPowerMax));
      await minersPage.validateMinerInList(updatedMinerIp);
      test.expect(new URL(page.url()).searchParams.getAll("subnet")).toEqual([firstMinerSubnet]);
      test.expect(new URL(page.url()).searchParams.get("power_min")).toBe(String(updatedPowerMin));
      test.expect(new URL(page.url()).searchParams.get("power_max")).toBeNull();
    });
  });

  test("Saved view can be renamed and persists after reload", async ({ minersPage, commonSteps, page }) => {
    const originalViewName = generateRandomText("miners_view");
    const renamedViewName = generateRandomText("renamed_view");
    let firstMinerIp = "";
    let firstMinerSubnet = "";
    let activeViewId = "";

    await commonSteps.loginAsAdmin();
    await commonSteps.goToMinersPage();

    await test.step("Create a saved view from the first miner subnet", async () => {
      firstMinerIp = await getFirstVisibleIpv4MinerIp(minersPage);
      firstMinerSubnet = toSubnet24(firstMinerIp);

      await minersPage.applySubnetFilter([firstMinerSubnet]);
      await minersPage.waitForMinersListToLoad();
      firstMinerIp = await minersPage.getMinerIpAddressByIndex(0);
      await minersPage.clickNewSavedViewButton();
      await minersPage.validateViewModalOpened("New view");
      await minersPage.inputViewName(originalViewName);
      await minersPage.saveNewView();
    });

    await test.step("Rename the saved view", async () => {
      await minersPage.clickRenameViewAction(originalViewName);
      await minersPage.validateViewModalOpened("Rename view");
      await minersPage.inputViewName(renamedViewName);
      await minersPage.confirmRenameView();
    });

    await test.step("Validate the renamed view is active and the old name is gone", async () => {
      await minersPage.validateViewTabVisible(renamedViewName);
      await minersPage.validateViewTabActive(renamedViewName);
      await minersPage.validateViewTabNotVisible(originalViewName);
      await minersPage.validateActiveFilterSummary("subnet", firstMinerSubnet);
      await minersPage.validateMinerInList(firstMinerIp);

      const searchParams = new URL(page.url()).searchParams;
      activeViewId = searchParams.get("view") ?? "";
      test.expect(activeViewId).not.toBe("");
      test.expect(searchParams.getAll("subnet")).toEqual([firstMinerSubnet]);
    });

    await test.step("Reload and validate the renamed view persists", async () => {
      await minersPage.reloadPage();
      await minersPage.waitForMinersTitle();
      await minersPage.waitForMinersListToLoad();

      await minersPage.validateViewTabVisible(renamedViewName);
      await minersPage.validateViewTabActive(renamedViewName);
      await minersPage.validateViewTabNotVisible(originalViewName);
      await minersPage.validateActiveFilterSummary("subnet", firstMinerSubnet);
      await minersPage.validateMinerInList(firstMinerIp);

      const searchParams = new URL(page.url()).searchParams;
      test.expect(searchParams.get("view")).toBe(activeViewId);
      test.expect(searchParams.getAll("subnet")).toEqual([firstMinerSubnet]);
    });
  });

  test("Active saved view can be deleted and clears the URL state", async ({ minersPage, commonSteps, page }) => {
    const viewName = generateRandomText("miners_view");
    let firstMinerSubnet = "";

    await commonSteps.loginAsAdmin();
    await commonSteps.goToMinersPage();

    await test.step("Create a saved view from the first miner subnet", async () => {
      const firstMinerIp = await getFirstVisibleIpv4MinerIp(minersPage);
      firstMinerSubnet = toSubnet24(firstMinerIp);

      await minersPage.applySubnetFilter([firstMinerSubnet]);
      await minersPage.waitForMinersListToLoad();
      await minersPage.clickNewSavedViewButton();
      await minersPage.validateViewModalOpened("New view");
      await minersPage.inputViewName(viewName);
      await minersPage.saveNewView();

      const searchParams = new URL(page.url()).searchParams;
      test.expect(searchParams.get("view")).not.toBeNull();
      test.expect(searchParams.getAll("subnet")).toEqual([firstMinerSubnet]);
    });

    await test.step("Delete the active saved view", async () => {
      await minersPage.clickDeleteViewAction(viewName);
      await minersPage.validateDeleteViewDialogOpened(viewName);
      await minersPage.confirmDeleteView();
    });

    await test.step("Validate the deleted active view clears only the view param and keeps the live filters", async () => {
      await minersPage.validateViewTabNotVisible(viewName);

      const searchParams = new URL(page.url()).searchParams;
      test.expect(searchParams.get("view")).toBeNull();
      test.expect(searchParams.getAll("subnet")).toEqual([firstMinerSubnet]);
    });

    await test.step("Reload and validate the deleted active view stays gone", async () => {
      await minersPage.reloadPage();
      await minersPage.waitForMinersTitle();
      await minersPage.waitForMinersListToLoad();

      await minersPage.validateViewTabNotVisible(viewName);
      await minersPage.validateActiveFilterSummary("subnet", firstMinerSubnet);
    });
  });
});
