import { test } from "../fixtures/pageFixtures";
import {
  createBuildingsScenarioData,
  createSiteAndBuilding,
  setupRackAssignedToBuilding,
  useBuildingsHooks,
} from "../helpers/buildingsTestSetup";

test.describe("Proto Fleet - Fleet filters", () => {
  useBuildingsHooks();

  test("Buildings site filter scopes the list and clears cleanly", async ({ page, fleetLocationsPage }) => {
    const primary = createBuildingsScenarioData();
    const secondary = createBuildingsScenarioData();

    await createSiteAndBuilding(fleetLocationsPage, primary);
    await createSiteAndBuilding(fleetLocationsPage, secondary);

    const primarySiteId = await fleetLocationsPage.getSiteIdByName(primary.siteName);
    const secondarySiteId = await fleetLocationsPage.getSiteIdByName(secondary.siteName);

    await test.step("Apply the first site filter from the buildings tab", async () => {
      await fleetLocationsPage.navigateToBuildingsPage();
      await fleetLocationsPage.applySiteFilter([primary.siteName]);

      await fleetLocationsPage.validateActiveFilterSummary("site", primary.siteName);
      await fleetLocationsPage.validateCurrentBuildingRowCounts(primary.buildingName, {
        siteName: primary.siteName,
        racks: 0,
        miners: 0,
      });
      await fleetLocationsPage.validateCurrentBuildingNotVisible(secondary.buildingName);

      test.expect(new URL(page.url()).searchParams.getAll("site")).toEqual([primarySiteId.toString()]);
    });

    await test.step("Switch the filter to the second site", async () => {
      await fleetLocationsPage.applySiteFilter([secondary.siteName]);

      await fleetLocationsPage.validateActiveFilterSummary("site", secondary.siteName);
      await fleetLocationsPage.validateCurrentBuildingRowCounts(secondary.buildingName, {
        siteName: secondary.siteName,
        racks: 0,
        miners: 0,
      });
      await fleetLocationsPage.validateCurrentBuildingNotVisible(primary.buildingName);

      test.expect(new URL(page.url()).searchParams.getAll("site")).toEqual([secondarySiteId.toString()]);
    });

    await test.step("Clear the site filter and show both buildings again", async () => {
      await fleetLocationsPage.clearActiveFilter("site");

      await fleetLocationsPage.validateActiveFilterNotVisible("Sites");
      await fleetLocationsPage.validateCurrentBuildingVisible(primary.buildingName);
      await fleetLocationsPage.validateCurrentBuildingVisible(secondary.buildingName);

      test.expect(new URL(page.url()).searchParams.getAll("site")).toEqual([]);
    });
  });

  test("Racks site and building filters can reach no results and then clear cleanly", async ({
    page,
    fleetLocationsPage,
    racksPage,
  }) => {
    const primary = createBuildingsScenarioData();
    const secondary = createBuildingsScenarioData();

    await setupRackAssignedToBuilding(page, fleetLocationsPage, racksPage, primary);
    const { buildingId: secondaryBuildingId } = await setupRackAssignedToBuilding(
      page,
      fleetLocationsPage,
      racksPage,
      secondary,
    );

    const primarySiteId = await fleetLocationsPage.getSiteIdByName(primary.siteName);

    await test.step("Apply a site filter and confirm only the matching rack remains", async () => {
      await racksPage.navigateToRacksPage();
      await racksPage.clickViewList();
      await racksPage.waitForRackListToLoad({ allowEmpty: false });

      await racksPage.applySiteFilter([primary.siteName]);
      await racksPage.waitForRackListToLoad({ allowEmpty: false });

      await racksPage.validateActiveFilterSummary("site", primary.siteName);
      await racksPage.validateRackPlacementRow(primary.rackLabel, primary.siteName, primary.buildingName);
      await racksPage.validateRackNotVisible(secondary.rackLabel);

      test.expect(new URL(page.url()).searchParams.getAll("site")).toEqual([primarySiteId.toString()]);
    });

    await test.step("Add a mismatched building filter to force the no-results state", async () => {
      await racksPage.applyBuildingFilter([secondary.buildingName]);
      await racksPage.waitForRackListToLoad();

      await racksPage.validateActiveFilterSummary("building", secondary.buildingName);
      await racksPage.validateNoResultsEmptyState();

      const searchParams = new URL(page.url()).searchParams;
      test.expect(searchParams.getAll("site")).toEqual([primarySiteId.toString()]);
      test.expect(searchParams.getAll("building")).toEqual([secondaryBuildingId.toString()]);
    });

    await test.step("Clear all filters and restore the full rack list", async () => {
      await racksPage.clickClearAllFilters();
      await racksPage.waitForRackListToLoad({ allowEmpty: false });

      await racksPage.validateActiveFilterNotVisible("Sites");
      await racksPage.validateActiveFilterNotVisible("Buildings");
      await racksPage.validateRackPlacementRow(primary.rackLabel, primary.siteName, primary.buildingName);
      await racksPage.validateRackPlacementRow(secondary.rackLabel, secondary.siteName, secondary.buildingName);

      const searchParams = new URL(page.url()).searchParams;
      test.expect(searchParams.getAll("site")).toEqual([]);
      test.expect(searchParams.getAll("building")).toEqual([]);
    });
  });
});
