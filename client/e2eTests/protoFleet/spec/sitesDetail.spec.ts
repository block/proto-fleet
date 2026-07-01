import { test } from "../fixtures/pageFixtures";
import {
  createSiteDetailScenarioData,
  createSiteDetailSites,
  useSiteDetailHooks,
} from "../helpers/sitesDetailTestSetup";

test.describe("Sites - detail", () => {
  useSiteDetailHooks();

  test("Site detail supports editing details, adding a building, and switching to a sibling site", async ({
    fleetLocationsPage,
  }, testInfo) => {
    const scenario = createSiteDetailScenarioData(testInfo);

    await createSiteDetailSites(fleetLocationsPage, scenario);
    await fleetLocationsPage.openSiteDetail(scenario.siteName);
    await fleetLocationsPage.validateSiteDetailOpened(scenario.siteName);
    await fleetLocationsPage.validateSiteDetailMetrics({ location: "—", buildings: 0 });

    await fleetLocationsPage.editSiteDetailsFromDetail({
      name: scenario.renamedSiteName,
      city: scenario.city,
      powerCapacityMw: scenario.powerCapacityMw,
    });

    await fleetLocationsPage.validateSiteDetailOpened(scenario.renamedSiteName);
    await fleetLocationsPage.validateSiteDetailMetrics({ location: scenario.city, buildings: 0 });

    await fleetLocationsPage.addBuildingFromSiteDetail(scenario.buildingName);

    await fleetLocationsPage.validateSiteDetailMetrics({ location: scenario.city, buildings: 1 });
    await fleetLocationsPage.validateSiteDetailBuildingVisible(scenario.buildingName);
    await fleetLocationsPage.switchSiteDetailBreadcrumbTo(scenario.siblingSiteName);
    await fleetLocationsPage.validateSiteDetailOpened(scenario.siblingSiteName);
    await fleetLocationsPage.validateSiteDetailMetrics({ location: "—", buildings: 0 });

    await fleetLocationsPage.validateSiteRowCounts(scenario.renamedSiteName, {
      buildings: 1,
      racks: 0,
      miners: 0,
    });
    await fleetLocationsPage.validateBuildingRowCounts(scenario.buildingName, {
      siteName: scenario.renamedSiteName,
      racks: 0,
      miners: 0,
    });
  });

  test("Deleting a site from the detail page removes it", async ({ fleetLocationsPage }, testInfo) => {
    const scenario = createSiteDetailScenarioData(testInfo);

    await fleetLocationsPage.createSite(scenario.siteName);
    await fleetLocationsPage.openSiteDetail(scenario.siteName);

    await fleetLocationsPage.deleteSiteFromDetail();
    await fleetLocationsPage.validateSiteNotVisible(scenario.siteName);
  });
});
