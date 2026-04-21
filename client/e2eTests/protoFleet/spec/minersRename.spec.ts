import { testConfig } from "../config/test.config";
import { expect, test } from "../fixtures/pageFixtures";
import { CommonSteps } from "../helpers/commonSteps";
import { generateRandomText } from "../helpers/testDataHelper";
import { AuthPage } from "../pages/auth";
import { MinersPage } from "../pages/miners";

const BULK_RENAME_PROPERTIES = [
  "custom",
  "fixed-mac-address",
  "fixed-serial-number",
  "fixed-model",
  "fixed-manufacturer",
] as const;

const COUNTER_SCALE = {
  MIN: 1,
  MAX: 6,
  DEFAULT: 2,
} as const;

const COUNTER_START = {
  DEFAULT: 1,
  SINGLE_DIGIT: 5,
  DOUBLE_DIGIT: 56,
  TRIPLE_DIGIT: 567,
} as const;

const CHARACTER_COUNT = {
  MIN: 1,
  MAX: 6,
} as const;

const SEPARATORS_THAT_CHANGE_NAME = [
  { id: "dash", value: "-" },
  { id: "underscore", value: "_" },
  { id: "none", value: "" },
] as const;

const BULK_RENAME_COUNTER_PREVIEW = String(COUNTER_START.DEFAULT).padStart(COUNTER_SCALE.DEFAULT, "0");

test.describe("Miners Rename", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/");
  });

  test.afterEach("CLEANUP: Rename back to just model names", async ({ browser }, testInfo) => {
    // CLEANUP: Rename back to just model names
    const isMobile = testInfo.project.use?.isMobile ?? false;
    const context = await browser.newContext({ baseURL: testConfig.baseUrl });
    const page = await context.newPage();
    await page.goto("/");

    try {
      const authPage = new AuthPage(page, isMobile);
      const minersPage = new MinersPage(page, isMobile);
      const commonSteps = new CommonSteps(authPage, minersPage);

      await commonSteps.loginAsAdmin();
      await commonSteps.goToMinersPage();

      await minersPage.clickSelectAllCheckbox();
      await minersPage.clickActionsMenuButton();
      await minersPage.clickRenameButton();
      await minersPage.validateBulkRenamePageOpened();

      for (const propertyId of BULK_RENAME_PROPERTIES) {
        await minersPage.toggleBulkRenameProperty(propertyId, propertyId === "fixed-model");
      }

      await minersPage.clickBulkRenameSave();
      await minersPage.confirmBulkRenameWarningsIfPresent();
      await minersPage.waitForMinersTitle();
      await minersPage.waitForMinersListToLoad();
    } finally {
      await context.close();
    }
  });

  test("Validate bulk rename functionality", async ({ minersPage, commonSteps }) => {
    await commonSteps.loginAsAdmin();
    await commonSteps.goToMinersPage();
    await minersPage.setBulkRenamePropertyOrder(BULK_RENAME_PROPERTIES);

    const minerCount = await minersPage.getMinersCount();

    await test.step("Select all miners and open bulk rename", async () => {
      await minersPage.clickSelectAllCheckbox();
      await minersPage.clickActionsMenuButton();
      await minersPage.clickRenameButton();
      await minersPage.validateBulkRenamePageOpened();
      expect((await minersPage.getBulkRenamePropertyOrder())[0]).toBe("custom");
    });

    await test.step("Enable all rename properties", async () => {
      for (const propertyId of BULK_RENAME_PROPERTIES) {
        await minersPage.toggleBulkRenameProperty(propertyId, true);
      }

      await minersPage.setCustomBulkRenameCounterScale(COUNTER_SCALE.DEFAULT);
    });

    await test.step("Select period separator", async () => {
      await minersPage.selectBulkRenameSeparator("period");
    });

    await test.step("Apply rename and wait for names update", async () => {
      await minersPage.clickBulkRenameSave();
      await minersPage.waitForMinersTitle();
      await minersPage.waitForMinersListToLoad();

      const expectedMinSegmentCount = BULK_RENAME_PROPERTIES.length - 1;
      const expectedMaxSegmentCount = BULK_RENAME_PROPERTIES.length;

      await expect
        .poll(
          async () => {
            const names = await minersPage.getMinerNames();
            return names.every((name) => {
              const segments = name.split(".");
              return (
                /^\d+$/.test(segments[0] ?? "") &&
                segments.length >= expectedMinSegmentCount &&
                segments.length <= expectedMaxSegmentCount
              );
            });
          },
          { message: "Waiting for miner names to update with new format" },
        )
        .toBe(true);
    });

    await test.step("Validate renamed miner names", async () => {
      const names = await minersPage.getMinerNames();
      expect(names).toHaveLength(minerCount);

      const expectedMinSegmentCount = BULK_RENAME_PROPERTIES.length - 1;
      const expectedMaxSegmentCount = BULK_RENAME_PROPERTIES.length;
      const counters: number[] = [];

      for (const name of names) {
        const segments = name.split(".");
        expect(
          segments.length,
          `Name should have between ${expectedMinSegmentCount} and ${expectedMaxSegmentCount} segments`,
        ).toBeGreaterThanOrEqual(expectedMinSegmentCount);
        expect(
          segments.length,
          `Name should have between ${expectedMinSegmentCount} and ${expectedMaxSegmentCount} segments`,
        ).toBeLessThanOrEqual(expectedMaxSegmentCount);

        // Validate no empty segments
        const emptySegmentIndices = segments.map((s, i) => (s.trim() === "" ? i : -1)).filter((i) => i >= 0);
        expect(
          emptySegmentIndices,
          `Name "${name}" contains empty segments at positions: ${emptySegmentIndices.join(", ")}`,
        ).toHaveLength(0);

        const counterSegment = segments[0];
        expect(
          /^\d+$/.test(counterSegment),
          `First segment should be numeric counter (validates 'custom' is first), got: "${counterSegment}" in "${name}"`,
        ).toBe(true);

        const counter = parseInt(counterSegment, 10);
        expect(counter, `Counter should be positive, got: ${counter}`).toBeGreaterThan(0);
        counters.push(counter);
      }

      const sortedCounters = [...counters].sort((a, b) => a - b);
      const expectedSequence = Array.from({ length: minerCount }, (_, i) => i + 1);
      expect(sortedCounters, "Counters should be sequential from 1 to N").toEqual(expectedSequence);
    });
  });

  test("Configure each miner rename property", async ({ minersPage, commonSteps }) => {
    await commonSteps.loginAsAdmin();
    await commonSteps.goToMinersPage();

    const minerCount = await minersPage.getMinersCount();
    expect(minerCount, "At least one miner must be available").toBeGreaterThan(0);
    const minerName = await minersPage.getMinerNameByIndex(0);
    const fixedProperties = BULK_RENAME_PROPERTIES.filter((p) => p !== "custom");
    const fixedPropertyValues = new Map<(typeof fixedProperties)[number], string>();
    let propertyOrder: string[] = [];

    await test.step("Open bulk rename for a single miner", async () => {
      await minersPage.clickMinerCheckboxByIndex(0);
      await minersPage.clickActionsMenuButton();
      await minersPage.clickRenameButton();
      await minersPage.validateBulkRenamePageOpened();

      for (const propertyId of BULK_RENAME_PROPERTIES) {
        await minersPage.toggleBulkRenameProperty(propertyId, true);
      }
      await minersPage.setCustomBulkRenameCounterScale(COUNTER_SCALE.DEFAULT);
      propertyOrder = await minersPage.getBulkRenamePropertyOrder();
    });

    await test.step("Capture fixed property preview values", async () => {
      for (const propertyId of fixedProperties) {
        await minersPage.clickBulkRenamePropertyOptions(propertyId);
        fixedPropertyValues.set(propertyId, await minersPage.getFixedValuePreviewText());
        await minersPage.dismissRenameOptionsModal();
      }
    });

    const previewSegments = propertyOrder
      .map((propertyId) => {
        if (propertyId === "custom") {
          return BULK_RENAME_COUNTER_PREVIEW;
        }

        return fixedPropertyValues.get(propertyId as (typeof fixedProperties)[number]) ?? "";
      })
      .filter((segment) => segment.trim() !== "");

    await test.step("Validate period separator preview behavior", async () => {
      await minersPage.selectBulkRenameSeparator("period");
      const expectedPeriodPreviewName = previewSegments.join(".");
      await minersPage.validateBulkRenamePreviewState(expectedPeriodPreviewName, minerName);
    });

    await test.step("Validate other separators update the new name", async () => {
      for (const separator of SEPARATORS_THAT_CHANGE_NAME) {
        await minersPage.selectBulkRenameSeparator(separator.id);
        const expectedPreviewName = previewSegments.join(separator.value);
        await minersPage.waitForBulkRenamePreviewName(expectedPreviewName);
      }
    });

    await test.step("Toggle all properties off except custom", async () => {
      for (const propertyId of BULK_RENAME_PROPERTIES) {
        await minersPage.toggleBulkRenameProperty(propertyId, propertyId === "custom");
      }
    });

    await test.step("Validate custom property options preview behavior", async () => {
      await minersPage.clickBulkRenamePropertyOptions("custom");

      // Make the initial expectations deterministic.
      await minersPage.selectCustomPropertyType("string-and-counter");
      await minersPage.fillCustomPropertyCounterStart(COUNTER_START.DEFAULT);
      await minersPage.clickCustomPropertyCounterScale(COUNTER_SCALE.DEFAULT);

      await minersPage.fillCustomPropertyPrefix("pre");
      await minersPage.validateCustomPropertyPreviewText("pre01");

      await minersPage.fillCustomPropertyPrefix("");
      await minersPage.fillCustomPropertySuffix("suf");
      await minersPage.validateCustomPropertyPreviewText("01suf");

      await minersPage.fillCustomPropertyPrefix("pre");
      await minersPage.validateCustomPropertyPreviewText("pre01suf");

      await minersPage.fillCustomPropertyCounterStart("");
      await minersPage.validateCustomPropertySaveDisabled();

      await minersPage.fillCustomPropertyCounterStart(COUNTER_START.SINGLE_DIGIT);
      await minersPage.validateCustomPropertyPreviewText("pre05suf");

      await minersPage.fillCustomPropertyCounterStart(COUNTER_START.DOUBLE_DIGIT);
      await minersPage.validateCustomPropertyPreviewText("pre56suf");

      await minersPage.fillCustomPropertyCounterStart(COUNTER_START.TRIPLE_DIGIT);
      await minersPage.validateCustomPropertyPreviewText("pre567suf");

      await minersPage.fillCustomPropertyCounterStart(COUNTER_START.SINGLE_DIGIT);

      for (let scale = COUNTER_SCALE.MIN; scale <= COUNTER_SCALE.MAX; scale++) {
        await minersPage.clickCustomPropertyCounterScale(scale);
        const paddedCounterValue = String(COUNTER_START.SINGLE_DIGIT).padStart(scale, "0");
        await minersPage.validateCustomPropertyPreviewText(`pre${paddedCounterValue}suf`);
      }

      await minersPage.clickCustomPropertyCounterScale(COUNTER_SCALE.MIN);
      await minersPage.selectCustomPropertyType("counter-only");
      await minersPage.validateCustomPropertyPreviewText(String(COUNTER_START.SINGLE_DIGIT));

      await minersPage.selectCustomPropertyType("string-only");
      await minersPage.fillCustomPropertyStringValue("sometext");
      await minersPage.validateCustomPropertyPreviewText("sometext");

      await minersPage.dismissRenameOptionsModal();
      await minersPage.toggleBulkRenameProperty("custom", false);
    });

    await test.step("Validate fixed property options preview behavior", async () => {
      for (const propertyId of fixedProperties) {
        const fullValue = fixedPropertyValues.get(propertyId) ?? "";

        await minersPage.toggleBulkRenameProperty(propertyId, true);
        await minersPage.clickBulkRenamePropertyOptions(propertyId);

        await minersPage.validateFixedValuePreviewText(fullValue);

        // String section options only render when character count is not "All".
        await minersPage.clickFixedValueCharacterCountOption(CHARACTER_COUNT.MIN);

        await minersPage.clickFixedValueStringSectionOption("first");
        for (let count = CHARACTER_COUNT.MIN; count <= CHARACTER_COUNT.MAX; count++) {
          await minersPage.clickFixedValueCharacterCountOption(count);
          await minersPage.validateFixedValuePreviewText(fullValue.slice(0, count));
        }

        await minersPage.clickFixedValueStringSectionOption("last");
        for (let count = CHARACTER_COUNT.MIN; count <= CHARACTER_COUNT.MAX; count++) {
          await minersPage.clickFixedValueCharacterCountOption(count);
          await minersPage.validateFixedValuePreviewText(fullValue.slice(-count));
        }

        await minersPage.dismissRenameOptionsModal();
        await minersPage.toggleBulkRenameProperty(propertyId, false);
      }
    });
  });

  test("RENAME a single miner", async ({ minersPage, page, commonSteps }) => {
    await commonSteps.loginAsAdmin();
    await commonSteps.goToMinersPage();

    const requestPromise = page.waitForRequest(/RenameMiners/);
    const responsePromise = page.waitForResponse(/RenameMiners/);

    const newName = generateRandomText("Renamed Miner E2E");
    let minerIp: string;

    await test.step("Select first miner and rename it", async () => {
      minerIp = await minersPage.getMinerIpAddressByIndex(0);
      await minersPage.clickMinerThreeDotsButton(minerIp);
      await minersPage.clickRenameButton();
      await minersPage.fillRenameInput(newName);
      await minersPage.clickRenameSave();
    });

    await test.step("Validate update process", async () => {
      await minersPage.validateTextInToastGroup("Miner renamed");
    });

    await test.step("Validate 'RenameMiners' API request", async () => {
      const request = await requestPromise;
      const response = await responsePromise;
      const requestBody = request.postDataJSON();
      test.expect(request.method()).toBe("POST");
      test.expect(requestBody).toHaveProperty("deviceSelector");
      test.expect(requestBody.deviceSelector).toHaveProperty("includeDevices");
      test.expect(requestBody.deviceSelector.includeDevices).toHaveProperty("deviceIdentifiers");
      test.expect(requestBody.deviceSelector.includeDevices.deviceIdentifiers).toHaveLength(1);
      test.expect(response.status()).toBe(200);
    });

    await test.step("Validate name updated in miner list", async () => {
      await minersPage.validateMinerName(minerIp, newName);
    });
  });

  test("BULK RENAME multiple miners", async ({ minersPage, page, commonSteps }, testInfo) => {
    // eslint-disable-next-line playwright/no-skipped-test
    test.skip(testInfo.project.use?.isMobile === true, "Desktop-only bulk rename flow");
    await commonSteps.loginAsAdmin();
    await commonSteps.goToMinersPage();

    const requestPromise = page.waitForRequest(/RenameMiners/);
    const responsePromise = page.waitForResponse(/RenameMiners/);

    let minerIp1: string;
    let minerIp2: string;

    await test.step("Select two rig miners and open bulk rename", async () => {
      await minersPage.filterRigMiners();
      minerIp1 = await minersPage.getMinerIpAddressByIndex(0);
      minerIp2 = await minersPage.getMinerIpAddressByIndex(1);
      await minersPage.clickMinerCheckbox(minerIp1);
      await minersPage.validateActionBarMinerCount(1);
      await minersPage.clickMinerCheckbox(minerIp2);
      await minersPage.validateActionBarMinerCount(2);
      await minersPage.clickActionsMenuButton();
      await minersPage.clickRenameButton();
      await minersPage.validateBulkRenamePageOpened();
    });

    await test.step("Enable MAC address and validate preview updates", async () => {
      await minersPage.clickBulkRenamePropertyToggle("fixed-mac-address");
      await test.expect(page.getByTestId("bulk-rename-desktop-preview")).toContainText(/([0-9a-f]{2}:){2}/i);
    });

    await test.step("Save the bulk rename", async () => {
      await minersPage.clickBulkRenameSave();
    });

    await test.step("Validate update process", async () => {
      await minersPage.validateTextInToastGroup("Renamed 2 miners");
    });

    await test.step("Validate 'RenameMiners' API request", async () => {
      const request = await requestPromise;
      const response = await responsePromise;
      const requestBody = request.postDataJSON();
      test.expect(request.method()).toBe("POST");
      test.expect(requestBody.deviceSelector.includeDevices.deviceIdentifiers).toHaveLength(2);
      test.expect(requestBody.nameConfig.properties).toHaveLength(1);
      test.expect(requestBody.nameConfig.separator).toBe("-");
      test.expect(response.status()).toBe(200);
    });
  });

  test("BULK RENAME mobile layout", async ({ minersPage, page, commonSteps }, testInfo) => {
    // eslint-disable-next-line playwright/no-skipped-test
    test.skip(testInfo.project.use?.isMobile !== true, "Mobile-only bulk rename layout");
    await commonSteps.loginAsAdmin();
    await commonSteps.goToMinersPage();

    await test.step("Open bulk rename from selected miners", async () => {
      await minersPage.filterRigMiners();
      const minerIp1 = await minersPage.getMinerIpAddressByIndex(0);
      const minerIp2 = await minersPage.getMinerIpAddressByIndex(1);
      await minersPage.clickMinerCheckbox(minerIp1);
      await minersPage.clickMinerCheckbox(minerIp2);
      await minersPage.clickActionsMenuButton();
      await minersPage.clickRenameButton();
      await minersPage.validateBulkRenamePageOpened();
    });

    await test.step("Validate mobile preview and fixed-value options sheet", async () => {
      await test.expect(page.getByTestId("bulk-rename-mobile-preview")).toBeVisible();
      await minersPage.clickBulkRenamePropertyToggle("fixed-mac-address");
      await minersPage.clickBulkRenamePropertyOptions("fixed-mac-address");
      await minersPage.validateTextIsVisible("Number of characters");
      await test.expect(page.getByTestId("fixed-value-options-save-button-mobile")).toBeVisible();
      await page.getByTestId("fixed-value-options-save-button-mobile").click();
      await minersPage.validateBulkRenamePageOpened();
    });
  });
});
