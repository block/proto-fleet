import { test } from "../fixtures/pageFixtures";
import { IssueIcon } from "../helpers/testDataHelper";

test.describe("Miner Issues Tests", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/");
  });

  test("mock ErrorQueryService with custom errors", async ({ page, minersPage, commonSteps }) => {
    const errorControlBoard = "COMPONENT_TYPE_CONTROL_BOARD";
    const errorHashBoard = "COMPONENT_TYPE_HASH_BOARD";
    const errorPsu = "COMPONENT_TYPE_PSU";
    const errorFan = "COMPONENT_TYPE_FAN";
    const date = "2026-01-01T12:00:00.203124Z";
    let testMiners: Array<{ deviceIdentifier: string; ipAddress: string; name: string }> = [];

    await commonSteps.loginAsAdmin();

    await test.step("Capture miner data from ListMinerStateSnapshots", async () => {
      const expectedMinerCount = 5;
      const responsePromise = page.waitForResponse(async (response) => {
        if (!response.url().includes("ListMinerStateSnapshots")) return false;
        const data = await response.json();
        return Array.isArray(data.miners) && data.miners.length >= expectedMinerCount;
      });
      await commonSteps.goToMinersPage();
      const response = await responsePromise;
      const responseData = await response.json();

      testMiners = responseData.miners.map((miner: { deviceIdentifier: string; ipAddress: string; name: string }) => ({
        deviceIdentifier: miner.deviceIdentifier,
        ipAddress: miner.ipAddress,
        name: miner.name,
      }));
    });

    await test.step("Setup error mock for ErrorQueryService", async () => {
      const mockErrorData = {
        devices: {
          items: [
            {
              deviceIdentifier: testMiners[0].deviceIdentifier,
              errors: [
                {
                  errorId: "test-error-1",
                  summary: errorControlBoard,
                  lastSeenAt: date,
                  deviceIdentifier: testMiners[0].deviceIdentifier,
                  componentType: errorControlBoard,
                },
              ],
            },
            {
              deviceIdentifier: testMiners[1].deviceIdentifier,
              errors: [
                {
                  errorId: "test-error-2",
                  summary: errorHashBoard,
                  lastSeenAt: date,
                  deviceIdentifier: testMiners[1].deviceIdentifier,
                  componentType: errorHashBoard,
                },
              ],
            },
            {
              deviceIdentifier: testMiners[2].deviceIdentifier,
              errors: [
                {
                  errorId: "test-error-3",
                  summary: errorPsu,
                  lastSeenAt: date,
                  deviceIdentifier: testMiners[2].deviceIdentifier,
                  componentType: errorPsu,
                },
              ],
            },
            {
              deviceIdentifier: testMiners[3].deviceIdentifier,
              errors: [
                {
                  errorId: "test-error-4",
                  summary: errorFan,
                  lastSeenAt: date,
                  deviceIdentifier: testMiners[3].deviceIdentifier,
                  componentType: errorFan,
                },
              ],
            },
            {
              deviceIdentifier: testMiners[4].deviceIdentifier,
              errors: [
                {
                  errorId: "test-error-5a",
                  summary: errorControlBoard,
                  lastSeenAt: date,
                  deviceIdentifier: testMiners[4].deviceIdentifier,
                  componentType: errorControlBoard,
                },
                {
                  errorId: "test-error-5b",
                  summary: errorHashBoard,
                  lastSeenAt: date,
                  deviceIdentifier: testMiners[4].deviceIdentifier,
                  componentType: errorHashBoard,
                },
                {
                  errorId: "test-error-5c",
                  summary: errorPsu,
                  lastSeenAt: date,
                  deviceIdentifier: testMiners[4].deviceIdentifier,
                  componentType: errorPsu,
                },
                {
                  errorId: "test-error-5d",
                  summary: errorFan,
                  lastSeenAt: date,
                  deviceIdentifier: testMiners[4].deviceIdentifier,
                  componentType: errorFan,
                },
              ],
            },
          ],
        },
      };

      await page.route(/ErrorQueryService\/Query/, async (route) => {
        return route.fulfill({
          status: 200,
          contentType: "application/json",
          body: JSON.stringify(mockErrorData),
        });
      });

      await minersPage.reloadPage();
      await minersPage.validateMinersPageOpened();
    });

    await test.step("Validate first miner icons and status modal - CONTROL BOARD failure", async () => {
      const ip = testMiners[0].ipAddress;
      const name = testMiners[0].name;
      await minersPage.validateMinerIcon(ip, "issues", IssueIcon.CONTROL_BOARD);
      await minersPage.validateMinerIcon(ip, "name", IssueIcon.GENERAL_ALERT);
      await minersPage.clickMinerElementAndExpectModal(ip, "issues", name);
      await minersPage.validateTitleInModal("Control board failure");
      await minersPage.validateErrorInModal(errorControlBoard, IssueIcon.CONTROL_BOARD);
      await minersPage.clickCloseStatusModal();
    });

    await test.step("Validate second miner icons and status modal - HASHBOARD failure", async () => {
      const ip = testMiners[1].ipAddress;
      const name = testMiners[1].name;
      await minersPage.validateMinerIcon(ip, "issues", IssueIcon.HASH_BOARD);
      await minersPage.validateMinerIcon(ip, "name", IssueIcon.GENERAL_ALERT);
      await minersPage.clickMinerElementAndExpectModal(ip, "issues", name);
      await minersPage.validateTitleInModal("Hashboard failure");
      await minersPage.validateErrorInModal(errorHashBoard, IssueIcon.HASH_BOARD);
      await minersPage.clickCloseStatusModal();
    });

    await test.step("Validate third miner icons and status modal - PSU failure", async () => {
      const ip = testMiners[2].ipAddress;
      const name = testMiners[2].name;
      await minersPage.validateMinerIcon(ip, "issues", IssueIcon.PSU);
      await minersPage.validateMinerIcon(ip, "name", IssueIcon.GENERAL_ALERT);
      await minersPage.clickMinerElementAndExpectModal(ip, "issues", name);
      await minersPage.validateTitleInModal("PSU failure");
      await minersPage.validateErrorInModal(errorPsu, IssueIcon.PSU);
      await minersPage.clickCloseStatusModal();
    });

    await test.step("Validate fourth miner icons and status modal - FAN failure", async () => {
      const ip = testMiners[3].ipAddress;
      const name = testMiners[3].name;
      await minersPage.validateMinerIcon(ip, "issues", IssueIcon.FAN);
      await minersPage.validateMinerIcon(ip, "name", IssueIcon.GENERAL_ALERT);

      await minersPage.clickMinerElementAndExpectModal(ip, "issues", name);
      await minersPage.validateTitleInModal("Fan failure");
      await minersPage.validateErrorInModal(errorFan, IssueIcon.FAN);
      await minersPage.clickCloseStatusModal();
    });

    await test.step("Validate fifth miner icons and status modal - Multiple failures", async () => {
      const ip = testMiners[4].ipAddress;
      const name = testMiners[4].name;
      await minersPage.validateMinerIcon(ip, "issues", IssueIcon.GENERAL_ALERT);
      await minersPage.validateMinerIcon(ip, "name", IssueIcon.GENERAL_ALERT);
      await minersPage.clickMinerElementAndExpectModal(ip, "issues", name);
      await minersPage.validateTitleInModal("Multiple failures");
      await minersPage.validateErrorInModal(errorControlBoard, IssueIcon.CONTROL_BOARD);
      await minersPage.validateErrorInModal(errorHashBoard, IssueIcon.HASH_BOARD);
      await minersPage.validateErrorInModal(errorPsu, IssueIcon.PSU);
      await minersPage.validateErrorInModal(errorFan, IssueIcon.FAN);
      await minersPage.clickCloseStatusModal();
    });

    const firstMinerIp = testMiners[0].ipAddress;
    const firstMinerName = testMiners[0].name;

    await test.step("Validate modal can be opened from alert icon", async () => {
      // From general alert icon
      await minersPage.clickMinerElementAndExpectModal(firstMinerIp, "alert-icon", firstMinerName);
      await minersPage.clickCloseStatusModal();
    });

    await test.step("Validate modal can be opened from status column", async () => {
      // From status column
      await minersPage.clickMinerElementAndExpectModal(firstMinerIp, "status", firstMinerName);
      await minersPage.clickCloseStatusModal();
    });

    await test.step("Validate modal can be opened from issues column", async () => {
      // From issues column
      await minersPage.clickMinerElementAndExpectModal(firstMinerIp, "issues", firstMinerName);
      await minersPage.clickCloseStatusModal();
    });
  });
});
