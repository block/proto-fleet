import { test } from "../fixtures/pageFixtures";
import { generateRandomText } from "../helpers/testDataHelper";

function getSubnet29(ip: string) {
  const [first, second, third, fourth] = ip.split(".");
  const networkBase = Math.floor(Number(fourth) / 8) * 8;
  return `${first}.${second}.${third}.${networkBase}/29`;
}

function findDistinctSubnet29Pair(ips: string[]) {
  const seen = new Map<string, string>();

  for (const ip of ips) {
    const subnet = getSubnet29(ip);
    if (!seen.has(subnet)) {
      seen.set(subnet, ip);
    }

    if (seen.size >= 2) {
      const entries = [...seen.entries()];
      return {
        firstSubnet: entries[0][0],
        firstMinerIp: entries[0][1],
        secondSubnet: entries[1][0],
        secondMinerIp: entries[1][1],
      };
    }
  }

  throw new Error(`Could not find two visible miners in distinct /29 subnets. Visible IPs: ${ips.join(", ")}`);
}

async function getVisibleMinerIps(minersPage: {
  getMinersCount(): Promise<number>;
  getMinerIpAddressByIndex(index: number): Promise<string>;
}) {
  const minerCount = await minersPage.getMinersCount();
  const ips: string[] = [];

  for (let index = 0; index < minerCount; index++) {
    ips.push(await minersPage.getMinerIpAddressByIndex(index));
  }

  return ips;
}

function formatPowerRangeSummary(min: number, max: number) {
  return `${min} kW - ${max} kW`;
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
    let powerMin = 0;
    let powerMax = 0;

    await commonSteps.loginAsAdmin();
    await commonSteps.goToMinersPage();

    await test.step("Capture a target miner and its filter values", async () => {
      initialMinerCount = await minersPage.getMinersCount();
      targetSubnet = getSubnet29(await minersPage.getMinerIpAddressByIndex(0));

      powerMin = 0;
      powerMax = 50;

      test.expect(initialMinerCount).toBeGreaterThan(1);
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
      await minersPage.validateActiveFilterSummary("power", formatPowerRangeSummary(powerMin, powerMax));
      await minersPage.validateMinerInList(filteredMinerIp);
      test.expect(await minersPage.getMinersCount()).toBeGreaterThan(0);
      test.expect(await minersPage.getMinersCount()).toBeLessThan(initialMinerCount);

      const searchParams = new URL(page.url()).searchParams;
      test.expect(searchParams.getAll("subnet")).toEqual([targetSubnet]);
      test.expect(searchParams.get("power_min")).toBe(String(powerMin));
      test.expect(searchParams.get("power_max")).toBe(String(powerMax));
    });

    await test.step("Reload and validate the filters persist", async () => {
      await minersPage.reloadPage();
      await minersPage.waitForMinersTitle();
      await minersPage.waitForMinersListToLoad();

      await minersPage.validateActiveFilterSummary("subnet", targetSubnet);
      await minersPage.validateActiveFilterSummary("power", formatPowerRangeSummary(powerMin, powerMax));
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
    let secondMinerIp = "";
    let firstMinerSubnet = "";
    let secondMinerSubnet = "";

    await commonSteps.loginAsAdmin();
    await commonSteps.goToMinersPage();

    await test.step("Capture two miners and save a view for the first subnet", async () => {
      const visibleIps = await getVisibleMinerIps(minersPage);
      const subnetPair = findDistinctSubnet29Pair(visibleIps);
      firstMinerIp = subnetPair.firstMinerIp;
      secondMinerIp = subnetPair.secondMinerIp;
      firstMinerSubnet = subnetPair.firstSubnet;
      secondMinerSubnet = subnetPair.secondSubnet;

      test.expect(secondMinerIp).not.toBe(firstMinerIp);

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
      await minersPage.clearActiveFilter("subnet");
      await minersPage.waitForMinersListToLoad();
      await minersPage.applySubnetFilter([secondMinerSubnet]);
      await minersPage.waitForMinersListToLoad();
      secondMinerIp = await minersPage.getMinerIpAddressByIndex(0);

      await minersPage.validateActiveFilterSummary("subnet", secondMinerSubnet);
      await minersPage.validateMinerInList(secondMinerIp);
    });

    await test.step("Reset the view back to the saved filters", async () => {
      await minersPage.clickResetViewAction(viewName);
      await minersPage.waitForMinersListToLoad();

      await minersPage.validateViewTabActive(viewName);
      await minersPage.validateActiveFilterSummary("subnet", firstMinerSubnet);
      await minersPage.validateMinerInList(firstMinerIp);
      test.expect(new URL(page.url()).searchParams.getAll("subnet")).toEqual([firstMinerSubnet]);
    });
  });

  test("Saved view can be updated after the filters change", async ({ minersPage, commonSteps, page }) => {
    const viewName = generateRandomText("miners_view");
    let firstMinerIp = "";
    let secondMinerIp = "";
    let secondMinerSubnet = "";

    await commonSteps.loginAsAdmin();
    await commonSteps.goToMinersPage();

    await test.step("Create a saved view from the first miner subnet", async () => {
      const visibleIps = await getVisibleMinerIps(minersPage);
      const subnetPair = findDistinctSubnet29Pair(visibleIps);
      firstMinerIp = subnetPair.firstMinerIp;
      secondMinerIp = subnetPair.secondMinerIp;
      const firstMinerSubnet = subnetPair.firstSubnet;
      secondMinerSubnet = subnetPair.secondSubnet;

      test.expect(secondMinerIp).not.toBe(firstMinerIp);

      await minersPage.applySubnetFilter([firstMinerSubnet]);
      await minersPage.waitForMinersListToLoad();
      firstMinerIp = await minersPage.getMinerIpAddressByIndex(0);
      await minersPage.clickNewSavedViewButton();
      await minersPage.validateViewModalOpened("New view");
      await minersPage.inputViewName(viewName);
      await minersPage.saveNewView();
    });

    await test.step("Change the active filters to the second miner subnet", async () => {
      await minersPage.clearActiveFilter("subnet");
      await minersPage.waitForMinersListToLoad();
      await minersPage.applySubnetFilter([secondMinerSubnet]);
      await minersPage.waitForMinersListToLoad();
      secondMinerIp = await minersPage.getMinerIpAddressByIndex(0);

      await minersPage.validateActiveFilterSummary("subnet", secondMinerSubnet);
      await minersPage.validateMinerInList(secondMinerIp);
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
      await minersPage.validateActiveFilterSummary("subnet", secondMinerSubnet);
      await minersPage.validateMinerInList(secondMinerIp);
      test.expect(new URL(page.url()).searchParams.getAll("subnet")).toEqual([secondMinerSubnet]);
    });
  });
});
