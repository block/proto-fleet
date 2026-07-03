import { test } from "../fixtures/pageFixtures";
import {
  createBuildingDetailScenarioData,
  setupBuildingDetailDeletionScenario,
  setupBuildingDetailScenario,
  useBuildingDetailHooks,
  validateBuildingDetailScenarioAcrossTabs,
} from "../helpers/buildingDetailTestSetup";
import { validateRackAndMinerPlacementAcrossTabs, validateSiteAndBuildingCounts } from "../helpers/buildingsTestSetup";

test.describe("Buildings - detail", () => {
  useBuildingDetailHooks();

  test("Building detail supports editing details, opening scoped racks and miners, and switching to a sibling building", async ({
    page,
    fleetLocationsPage,
    minersPage,
    racksPage,
  }, testInfo) => {
    const scenario = createBuildingDetailScenarioData(testInfo);
    const { selectedMinerIps } = await setupBuildingDetailScenario(page, fleetLocationsPage, racksPage, scenario);

    await fleetLocationsPage.openBuildingDetail(scenario.buildingName);
    await fleetLocationsPage.validateBuildingDetailOpened(scenario.buildingName);
    await fleetLocationsPage.validateBuildingDetailMetrics({ totalMiners: 2 });

    await fleetLocationsPage.editBuildingDetailsFromDetail({
      name: scenario.renamedBuildingName,
      powerCapacityMw: scenario.powerCapacityMw,
    });

    await fleetLocationsPage.validateBuildingDetailOpened(scenario.renamedBuildingName);
    await fleetLocationsPage.validateBuildingDetailMetrics({ totalMiners: 2 });

    await validateBuildingDetailScenarioAcrossTabs({
      page,
      fleetLocationsPage,
      minersPage,
      racksPage,
      scenario: {
        siteName: scenario.siteName,
        buildingName: scenario.renamedBuildingName,
        siblingBuildingName: scenario.siblingBuildingName,
        rackLabel: scenario.rackLabel,
      },
      selectedMinerIps,
    });

    await fleetLocationsPage.openBuildingDetail(scenario.renamedBuildingName);
    await fleetLocationsPage.switchBuildingDetailBreadcrumbTo(scenario.siblingBuildingName);
    await fleetLocationsPage.validateBuildingDetailOpened(scenario.siblingBuildingName);
    await fleetLocationsPage.validateBuildingDetailMetrics({ minersOnline: "0 / 0" });
  });

  test("Deleting a building from the detail page keeps the rack on the site", async ({
    page,
    fleetLocationsPage,
    minersPage,
    racksPage,
  }, testInfo) => {
    const scenario = createBuildingDetailScenarioData(testInfo);
    const { selectedMinerIps } = await setupBuildingDetailDeletionScenario(
      page,
      fleetLocationsPage,
      racksPage,
      scenario,
    );

    await fleetLocationsPage.openBuildingDetail(scenario.buildingName);
    await fleetLocationsPage.validateBuildingDetailOpened(scenario.buildingName);
    await fleetLocationsPage.deleteBuildingFromDetail();

    await fleetLocationsPage.validateBuildingNotVisible(scenario.buildingName);
    await validateSiteAndBuildingCounts(fleetLocationsPage, {
      siteName: scenario.siteName,
      siteCounts: {
        buildings: 0,
        racks: 1,
        miners: 2,
      },
      buildings: [],
    });
    await validateRackAndMinerPlacementAcrossTabs({
      page,
      minersPage,
      racksPage,
      siteName: scenario.siteName,
      rackLabel: scenario.rackLabel,
      selectedMinerIps,
    });
  });
});
