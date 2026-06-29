import { test } from "../fixtures/pageFixtures";
import {
  assignRackToBuilding,
  createBuildingsScenarioData,
  createRackWithAssignedMiners,
  createSiteAndBuilding,
  useBuildingsHooks,
  validateBuildingPlacementAcrossTabs,
} from "../helpers/buildingsTestSetup";

test.describe("Buildings", () => {
  useBuildingsHooks();

  test("Create a site, building, rack, and miners flow across fleet tabs", async ({
    page,
    fleetLocationsPage,
    minersPage,
    racksPage,
  }) => {
    const scenario = createBuildingsScenarioData();
    const buildingId = await createSiteAndBuilding(fleetLocationsPage, scenario);
    const { rackId, selectedMinerIps } = await createRackWithAssignedMiners(racksPage, scenario.rackLabel);

    await assignRackToBuilding(page, racksPage, scenario.rackLabel, rackId, scenario.buildingName, buildingId);
    await validateBuildingPlacementAcrossTabs({
      page,
      fleetLocationsPage,
      minersPage,
      racksPage,
      scenario,
      selectedMinerIps,
    });
  });
});
