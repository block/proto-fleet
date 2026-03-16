import { testConfig } from "../config/test.config";
import { expect, test } from "../fixtures/pageFixtures";
import { CommonSteps } from "../helpers/commonSteps";
import { AuthPage } from "../pages/auth";
import { MinersPage } from "../pages/miners";

const BULK_RENAME_PROPERTIES = [
  "custom",
  "fixed-mac-address",
  "fixed-serial-number",
  "fixed-worker-name",
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

test.describe("Miners Rename", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/");
  });

  test.afterAll(async ({ browser }, testInfo) => {
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

      const minerCount = await minersPage.getMinersCount();

      await minersPage.clickSelectAllCheckbox();
      await minersPage.validateActionBarMinerCount(minerCount);

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
    } catch (error) {
      console.warn("Cleanup failed:", error instanceof Error ? error.message : String(error));
    } finally {
      await context.close();
    }
  });

  test("Validate bulk rename functionality", async ({ minersPage, commonSteps }) => {
    await commonSteps.loginAsAdmin();
    await commonSteps.goToMinersPage();

    const minerCount = await minersPage.getMinersCount();

    await test.step("Select all miners and open bulk rename", async () => {
      await minersPage.clickSelectAllCheckbox();
      await minersPage.clickActionsMenuButton();
      await minersPage.clickRenameButton();
      await minersPage.validateBulkRenamePageOpened();

      await minersPage.ensureBulkRenamePropertyFirst("custom");
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

      await expect
        .poll(
          async () => {
            const names = await minersPage.getMinerNames();
            return names.some((name) => name.split(".").length === BULK_RENAME_PROPERTIES.length);
          },
          { message: "Waiting for miner names to update with new format" },
        )
        .toBe(true);
    });

    await test.step("Validate renamed miner names", async () => {
      const names = await minersPage.getMinerNames();
      expect(names).toHaveLength(minerCount);

      const expectedSegmentCount = BULK_RENAME_PROPERTIES.length;
      const counters: number[] = [];

      for (const name of names) {
        const segments = name.split(".");
        expect(segments, `Name should have ${expectedSegmentCount} segments`).toHaveLength(expectedSegmentCount);

        // Validate no empty segments
        const emptySegmentIndices = segments.map((s, i) => (s.trim() === "" ? i : -1)).filter((i) => i >= 0);
        expect(
          emptySegmentIndices,
          `Name "${name}" contains empty segments at positions: ${emptySegmentIndices.join(", ")}`,
        ).toHaveLength(0);

        // 'custom' is reordered to be the first module; by default it outputs just the counter.
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
    const previewSegments = minerName.split(".");
    expect(previewSegments, `Expected miner name to be dot-separated but got: "${minerName}"`).toHaveLength(
      BULK_RENAME_PROPERTIES.length,
    );

    await test.step("Open bulk rename for a single miner", async () => {
      await minersPage.clickMinerCheckboxByIndex(0);
      await minersPage.clickActionsMenuButton();
      await minersPage.clickRenameButton();
      await minersPage.validateBulkRenamePageOpened();
      await minersPage.ensureBulkRenamePropertyFirst("custom");

      for (const propertyId of BULK_RENAME_PROPERTIES) {
        await minersPage.toggleBulkRenameProperty(propertyId, true);
      }
      await minersPage.setCustomBulkRenameCounterScale(COUNTER_SCALE.DEFAULT);
    });

    await test.step("Validate period separator shows unchanged placeholder", async () => {
      await minersPage.selectBulkRenameSeparator("period");
      await minersPage.validateBulkRenamePreviewUnchangedPlaceholder();
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
      const fixedProperties = BULK_RENAME_PROPERTIES.filter((p) => p !== "custom");

      for (const propertyId of fixedProperties) {
        const segmentIndex = BULK_RENAME_PROPERTIES.indexOf(propertyId);
        const fullValue = previewSegments[segmentIndex];

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
});
