import { test } from "../fixtures/pageFixtures";
import { generateRandomText } from "../helpers/testDataHelper";

function parseIpv4(ip: string) {
  const normalizedIp = ip.trim();
  const octets = normalizedIp.split(".");

  if (octets.length !== 4) {
    return null;
  }

  const numericOctets = octets.map((octet) => Number(octet));
  const isValidIpv4 = numericOctets.every((octet) => Number.isInteger(octet) && octet >= 0 && octet <= 255);

  if (!isValidIpv4) {
    return null;
  }

  return normalizedIp;
}

async function getFirstVisibleIpv4MinerIp(minersPage: {
  getMinersCount(): Promise<number>;
  getMinerIpAddressByIndex(index: number): Promise<string>;
}) {
  const minerCount = await minersPage.getMinersCount();

  for (let index = 0; index < minerCount; index++) {
    const ipAddress = await minersPage.getMinerIpAddressByIndex(index);
    const parsedIp = parseIpv4(ipAddress);

    if (parsedIp !== null) {
      return parsedIp;
    }
  }

  throw new Error("Subnet filter coverage requires at least one visible IPv4 miner.");
}

function toSubnet24(ip: string) {
  const parsedIp = parseIpv4(ip);
  if (parsedIp === null) {
    throw new Error(`Expected a valid IPv4 address, got "${ip}".`);
  }

  const [first, second, third] = parsedIp.split(".");
  return `${first}.${second}.${third}.0/24`;
}

function formatPowerFilterSummary(min: number | undefined, max: number | undefined) {
  if (min !== undefined && max !== undefined) {
    return `${min} kW - ${max} kW`;
  }

  if (min !== undefined) {
    return `≥ ${min} kW`;
  }

  if (max !== undefined) {
    return `≤ ${max} kW`;
  }

  return "";
}

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

    await test.step("Drive the filtered empty state and clear filters", async () => {
      await minersPage.applyPowerFilter(50, 50);
      await minersPage.validateNoResultsEmptyState();
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

    await test.step("Update the saved view to the new subnet", async () => {
      await minersPage.clickUpdateViewAction(viewName);
      await minersPage.validateViewModalOpened("Update view");
      await minersPage.updateSavedView();
    });

    await test.step("Reload, leave the view, and reopen it", async () => {
      await minersPage.reloadPage();
      await minersPage.waitForMinersTitle();
      await minersPage.waitForMinersListToLoad();

      await minersPage.clickViewTab("All miners");
      await minersPage.waitForMinersListToLoad();
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

  test("Saved view can be renamed and persists after reload", async ({ minersPage, commonSteps }) => {
    const originalViewName = generateRandomText("miners_view");
    const renamedViewName = generateRandomText("renamed_view");
    let firstMinerIp = "";
    let firstMinerSubnet = "";

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
      await minersPage.validateViewModalOpened("Update view");
      await minersPage.inputViewName(renamedViewName);
      await minersPage.updateSavedView();
    });

    await test.step("Validate the renamed view is active and the old name is gone", async () => {
      await minersPage.validateViewTabVisible(renamedViewName);
      await minersPage.validateViewTabActive(renamedViewName);
      await minersPage.validateViewTabNotVisible(originalViewName);
      await minersPage.validateActiveFilterSummary("subnet", firstMinerSubnet);
      await minersPage.validateMinerInList(firstMinerIp);
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
    });
  });

  test("Saved view can be deleted from the views bar", async ({ minersPage, commonSteps, page }) => {
    const viewName = generateRandomText("miners_view");

    await commonSteps.loginAsAdmin();
    await commonSteps.goToMinersPage();

    await test.step("Create a saved view from the first miner subnet", async () => {
      const firstMinerIp = await getFirstVisibleIpv4MinerIp(minersPage);
      const firstMinerSubnet = toSubnet24(firstMinerIp);

      await minersPage.applySubnetFilter([firstMinerSubnet]);
      await minersPage.waitForMinersListToLoad();
      await minersPage.clickNewSavedViewButton();
      await minersPage.validateViewModalOpened("New view");
      await minersPage.inputViewName(viewName);
      await minersPage.saveNewView();
    });

    await test.step("Return to All miners so the saved view becomes inactive", async () => {
      await minersPage.clickViewTab("All miners");
      await minersPage.waitForMinersListToLoad();
      await minersPage.validateViewTabActive("All miners");
    });

    await test.step("Delete the saved view", async () => {
      await minersPage.clickDeleteViewAction(viewName);
      await minersPage.validateDeleteViewDialogOpened(viewName);
      await minersPage.confirmDeleteView();
    });

    await test.step("Validate the deleted view is gone and the URL stayed clean", async () => {
      await minersPage.validateViewTabNotVisible(viewName);
      await minersPage.validateViewTabActive("All miners");

      const searchParams = new URL(page.url()).searchParams;
      test.expect(searchParams.get("view")).toBeNull();
      test.expect(searchParams.getAll("subnet")).toEqual([]);
    });

    await test.step("Reload and validate the deleted view stays gone", async () => {
      await minersPage.reloadPage();
      await minersPage.waitForMinersTitle();
      await minersPage.waitForMinersListToLoad();

      await minersPage.validateViewTabNotVisible(viewName);
      await minersPage.validateViewTabActive("All miners");
      await minersPage.validateActiveFilterNotVisible("Subnet");
    });
  });
});
