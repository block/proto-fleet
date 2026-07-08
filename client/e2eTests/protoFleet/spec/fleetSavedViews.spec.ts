import { test } from "../fixtures/pageFixtures";
import {
  AUTOMATION_BUILDINGS_ZONE,
  createBuildingsScenarioData,
  setupRackAssignedToBuilding,
  useBuildingsHooks,
} from "../helpers/buildingsTestSetup";
import { generateRandomText } from "../helpers/testDataHelper";

test.describe("Proto Fleet - Fleet saved views", () => {
  useBuildingsHooks();

  test("Buildings saved view restores the site-filtered fleet view", async ({
    page,
    fleetLocationsPage,
    racksPage,
  }) => {
    const scenario = createBuildingsScenarioData();
    const viewName = generateRandomText("buildings_view");

    await setupRackAssignedToBuilding(page, fleetLocationsPage, racksPage, scenario);

    let siteId = 0n;

    await test.step("Open the buildings tab from the site row and save the filtered view", async () => {
      siteId = await fleetLocationsPage.openBuildingsForSite(scenario.siteName);

      await fleetLocationsPage.validateCurrentBuildingRowCounts(scenario.buildingName, {
        siteName: scenario.siteName,
        racks: 1,
        miners: 2,
      });

      let searchParams = new URL(page.url()).searchParams;
      test.expect(searchParams.getAll("site")).toEqual([siteId.toString()]);

      await fleetLocationsPage.clickNewSavedViewButton();
      await fleetLocationsPage.validateViewModalOpened("New view");
      await fleetLocationsPage.inputViewName(viewName);
      await fleetLocationsPage.saveNewView();
      await fleetLocationsPage.validateViewTabActive(viewName);
    });

    await test.step("Clear the site filter so the saved view becomes dirty", async () => {
      await fleetLocationsPage.clearActiveFilter("site");

      const searchParams = new URL(page.url()).searchParams;
      test.expect(searchParams.getAll("site")).toEqual([]);
      test.expect(searchParams.get("view")).not.toBeNull();
    });

    await test.step("Reset and then delete the saved view", async () => {
      await fleetLocationsPage.clickResetViewAction(viewName);
      await fleetLocationsPage.validateViewTabActive(viewName);
      await fleetLocationsPage.validateCurrentBuildingRowCounts(scenario.buildingName, {
        siteName: scenario.siteName,
        racks: 1,
        miners: 2,
      });

      let searchParams = new URL(page.url()).searchParams;
      test.expect(searchParams.getAll("site")).toEqual([siteId.toString()]);

      await fleetLocationsPage.clickDeleteViewAction(viewName);
      await fleetLocationsPage.validateDeleteViewDialogOpened(viewName);
      await fleetLocationsPage.confirmDeleteView();
      await fleetLocationsPage.validateViewTabNotVisible(viewName);

      searchParams = new URL(page.url()).searchParams;
      test.expect(searchParams.get("view")).toBeNull();
      test.expect(searchParams.getAll("site")).toEqual([siteId.toString()]);
    });
  });

  test("Racks saved view restores the building filter and display mode", async ({
    page,
    fleetLocationsPage,
    racksPage,
  }) => {
    const scenario = createBuildingsScenarioData();
    const viewName = generateRandomText("racks_view");

    const { buildingId } = await setupRackAssignedToBuilding(page, fleetLocationsPage, racksPage, scenario);

    await test.step("Open the racks tab from the building row and save a grid view", async () => {
      const openedBuildingId = await fleetLocationsPage.openRacksForBuilding(scenario.buildingName);
      test.expect(openedBuildingId).toBe(buildingId);

      await racksPage.waitForRackListToLoad({ allowEmpty: false });
      await racksPage.clickViewGrid();
      await racksPage.validateRackCardVisible(scenario.rackLabel, AUTOMATION_BUILDINGS_ZONE);

      let searchParams = new URL(page.url()).searchParams;
      test.expect(searchParams.getAll("building")).toEqual([buildingId.toString()]);
      test.expect(searchParams.get("display")).toBe("grid");

      await racksPage.clickNewSavedViewButton();
      await racksPage.validateViewModalOpened("New view");
      await racksPage.inputViewName(viewName);
      await racksPage.saveNewView();
      await racksPage.validateViewTabActive(viewName);
    });

    await test.step("Change to list view and clear the building filter", async () => {
      await racksPage.clickViewList();
      await racksPage.waitForRackListToLoad({ allowEmpty: false });
      await racksPage.clearActiveFilter("building");
      await racksPage.waitForRackListToLoad({ allowEmpty: false });

      await racksPage.validateRackPlacementRow(scenario.rackLabel, scenario.siteName, scenario.buildingName);

      const searchParams = new URL(page.url()).searchParams;
      test.expect(searchParams.getAll("building")).toEqual([]);
      test.expect(searchParams.get("display")).toBe("list");
      test.expect(searchParams.get("view")).not.toBeNull();
    });

    await test.step("Reset the saved view back to the building-scoped grid state", async () => {
      await racksPage.clickResetViewAction(viewName);
      await racksPage.validateViewTabActive(viewName);
      await racksPage.validateRackCardVisible(scenario.rackLabel, AUTOMATION_BUILDINGS_ZONE);

      const searchParams = new URL(page.url()).searchParams;
      test.expect(searchParams.getAll("building")).toEqual([buildingId.toString()]);
      test.expect(searchParams.get("display")).toBe("grid");
    });

    await test.step("Clear the active view, then reopen it from the saved views menu", async () => {
      await racksPage.reloadPage();
      await racksPage.validateRackCardVisible(scenario.rackLabel, AUTOMATION_BUILDINGS_ZONE);
      await racksPage.clickViewList();
      await racksPage.waitForRackListToLoad({ allowEmpty: false });
      await racksPage.clearActiveFilter("building");
      await racksPage.waitForRackListToLoad({ allowEmpty: false });
      await racksPage.clickClearActiveView();
      await racksPage.clickViewTab(viewName);

      await racksPage.validateViewTabActive(viewName);
      await racksPage.validateRackCardVisible(scenario.rackLabel, AUTOMATION_BUILDINGS_ZONE);

      const searchParams = new URL(page.url()).searchParams;
      test.expect(searchParams.getAll("building")).toEqual([buildingId.toString()]);
      test.expect(searchParams.get("display")).toBe("grid");
      test.expect(searchParams.get("view")).not.toBeNull();
    });

    await test.step("Delete the saved view to leave the fleet state clean", async () => {
      await racksPage.clickDeleteViewAction(viewName);
      await racksPage.validateDeleteViewDialogOpened(viewName);
      await racksPage.confirmDeleteView();
      await racksPage.validateViewTabNotVisible(viewName);

      const searchParams = new URL(page.url()).searchParams;
      test.expect(searchParams.getAll("building")).toEqual([buildingId.toString()]);
      test.expect(searchParams.get("display")).toBe("grid");
      test.expect(searchParams.get("view")).toBeNull();
    });
  });
});
