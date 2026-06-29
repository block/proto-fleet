import { test } from "../fixtures/pageFixtures";
import {
  assignRackToBuilding,
  createBuildingsScenarioData,
  createRackWithAssignedMiners,
  createSiteAndBuilding,
  removeRackFromBuilding,
  useBuildingsHooks,
  validateBuildingPlacementAcrossTabs,
  validateRackAndMinerPlacementAcrossTabs,
  validateSiteAndBuildingCounts,
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

  test("Move a rack between buildings and then unassign it", async ({
    page,
    fleetLocationsPage,
    minersPage,
    racksPage,
  }) => {
    const siteName = createBuildingsScenarioData().siteName;
    const buildingA = createBuildingsScenarioData().buildingName;
    const buildingB = createBuildingsScenarioData().buildingName;
    const rackLabel = createBuildingsScenarioData().rackLabel;

    await fleetLocationsPage.createSite(siteName);
    const buildingAId = await fleetLocationsPage.createBuilding(siteName, buildingA);
    const buildingBId = await fleetLocationsPage.createBuilding(siteName, buildingB);
    const { rackId, selectedMinerIps } = await createRackWithAssignedMiners(racksPage, rackLabel);

    await assignRackToBuilding(page, racksPage, rackLabel, rackId, buildingA, buildingAId);
    await assignRackToBuilding(page, racksPage, rackLabel, rackId, buildingB, buildingBId);

    await validateSiteAndBuildingCounts(fleetLocationsPage, {
      siteName,
      siteCounts: {
        buildings: 2,
        racks: 1,
        miners: 2,
      },
      buildings: [
        { buildingName: buildingA, racks: 0, miners: 0 },
        { buildingName: buildingB, racks: 1, miners: 2 },
      ],
    });
    await validateRackAndMinerPlacementAcrossTabs({
      page,
      minersPage,
      racksPage,
      siteName,
      buildingName: buildingB,
      rackLabel,
      selectedMinerIps,
    });

    await removeRackFromBuilding(page, fleetLocationsPage, buildingB, rackId);

    await validateSiteAndBuildingCounts(fleetLocationsPage, {
      siteName,
      siteCounts: {
        buildings: 2,
        racks: 1,
        miners: 2,
      },
      buildings: [
        { buildingName: buildingA, racks: 0, miners: 0 },
        { buildingName: buildingB, racks: 0, miners: 0 },
      ],
    });
    await validateRackAndMinerPlacementAcrossTabs({
      page,
      minersPage,
      racksPage,
      siteName,
      rackLabel,
      selectedMinerIps,
    });
  });

  test("Rename a building and propagate the new name across fleet tabs", async ({
    page,
    fleetLocationsPage,
    minersPage,
    racksPage,
  }) => {
    const scenario = createBuildingsScenarioData();
    const renamedBuilding = createBuildingsScenarioData().buildingName;
    const buildingId = await createSiteAndBuilding(fleetLocationsPage, scenario);
    const { rackId, selectedMinerIps } = await createRackWithAssignedMiners(racksPage, scenario.rackLabel);

    await assignRackToBuilding(page, racksPage, scenario.rackLabel, rackId, scenario.buildingName, buildingId);
    await fleetLocationsPage.renameBuilding(scenario.buildingName, renamedBuilding);

    await validateSiteAndBuildingCounts(fleetLocationsPage, {
      siteName: scenario.siteName,
      siteCounts: {
        buildings: 1,
        racks: 1,
        miners: 2,
      },
      buildings: [{ buildingName: renamedBuilding, racks: 1, miners: 2 }],
    });
    await validateRackAndMinerPlacementAcrossTabs({
      page,
      minersPage,
      racksPage,
      siteName: scenario.siteName,
      buildingName: renamedBuilding,
      rackLabel: scenario.rackLabel,
      selectedMinerIps,
    });
  });

  test("Delete a building with an assigned rack and keep the rack on the site", async ({
    page,
    fleetLocationsPage,
    minersPage,
    racksPage,
  }) => {
    const scenario = createBuildingsScenarioData();
    const buildingId = await createSiteAndBuilding(fleetLocationsPage, scenario);
    const { rackId, selectedMinerIps } = await createRackWithAssignedMiners(racksPage, scenario.rackLabel);

    await assignRackToBuilding(page, racksPage, scenario.rackLabel, rackId, scenario.buildingName, buildingId);
    await fleetLocationsPage.deleteBuilding(scenario.buildingName);

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
