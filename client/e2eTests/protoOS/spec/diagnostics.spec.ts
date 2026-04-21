/* eslint-disable playwright/expect-expect */
import { expect } from "@playwright/test";
import { test } from "../fixtures/pageFixtures";

type DiagnosticsSection = {
  filterLabel: "Fans" | "Hashboards" | "PSUs" | "Control Board";
  sectionTestIdName: "Fans" | "Hashboards" | "PSU" | "Control Board";
  expectedCardCount: number;
};

const diagnosticsSections: DiagnosticsSection[] = [
  { filterLabel: "Fans", sectionTestIdName: "Fans", expectedCardCount: 6 },
  { filterLabel: "Hashboards", sectionTestIdName: "Hashboards", expectedCardCount: 6 },
  // Note: PSU is the only mismatch: filter button shows "PSUs" but the section uses "PSU".
  { filterLabel: "PSUs", sectionTestIdName: "PSU", expectedCardCount: 3 },
  { filterLabel: "Control Board", sectionTestIdName: "Control Board", expectedCardCount: 1 },
];

test.describe("Diagnostics", () => {
  test.beforeEach(async ({ page, commonSteps }) => {
    await page.goto("/");
    await commonSteps.authenticateAsAdmin();
  });

  test("Diagnostics sections and filters", async ({ commonSteps, diagnosticsPage }) => {
    await commonSteps.navigateToDiagnostics();

    const allSectionTestIdNames = diagnosticsSections.map((s) => s.sectionTestIdName);

    await test.step("Validate all sections are visible", async () => {
      await diagnosticsPage.validateAllSectionsVisible(allSectionTestIdNames);
    });

    await test.step("Validate each filter shows only its section and correct card count", async () => {
      for (const selected of diagnosticsSections) {
        await diagnosticsPage.clickFilterButton(selected.filterLabel);
        await diagnosticsPage.validateOnlySectionVisible(selected.sectionTestIdName, allSectionTestIdNames);
        await diagnosticsPage.validateCardCountInSection(selected.sectionTestIdName, selected.expectedCardCount);
      }
    });
  });

  test("Diagnostics cards show correct modal metrics and metadata", async ({ commonSteps, diagnosticsPage }) => {
    await commonSteps.navigateToDiagnostics();

    const hashRateValuePattern = /\d+(?:\.\d+)?\s*TH\/S/;
    const wattsValuePattern = /\d{1,3}(?:,\d{3})*\s*W/;
    const celsiusValuePattern = /\d+(?:\.\d+)?\s*°C/;
    const efficiencyValuePattern = /\d+(?:\.\d+)?\s*J\/TH/;
    const rpmValuePattern = /\d+\s*RPM/;

    const expectedBySection: Record<
      DiagnosticsSection["sectionTestIdName"],
      {
        metrics: Array<{ label: string | RegExp; valuePattern: RegExp }>;
        metadata: Array<{ label: string }>;
        expectedInfoCardCount: number;
        emptySlotText: RegExp;
        allowExtraMetadataRows?: boolean;
      }
    > = {
      Fans: {
        metrics: [{ label: /\d+(?:\.\d+)?%\s*PWM/, valuePattern: rpmValuePattern }],
        metadata: [],
        expectedInfoCardCount: 4,
        emptySlotText: /No fan detected in this slot/i,
      },
      Hashboards: {
        metrics: [
          { label: "Hashrate", valuePattern: hashRateValuePattern },
          { label: "Power", valuePattern: wattsValuePattern },
          { label: "ASIC Avg Temp", valuePattern: celsiusValuePattern },
          { label: "ASIC High Temp", valuePattern: celsiusValuePattern },
          { label: "Efficiency", valuePattern: efficiencyValuePattern },
        ],
        metadata: [{ label: "Serial Number" }, { label: "Model" }, { label: "ASIC Count" }, { label: "Slot Location" }],
        expectedInfoCardCount: 4,
        emptySlotText: /No hashboard detected in this slot/i,
        allowExtraMetadataRows: true,
      },
      PSU: {
        metrics: [
          { label: "Input Power", valuePattern: wattsValuePattern },
          { label: "Output Power", valuePattern: wattsValuePattern },
          { label: "Average Temp", valuePattern: celsiusValuePattern },
          { label: "Max Temp", valuePattern: celsiusValuePattern },
        ],
        metadata: [{ label: "Serial Number" }, { label: "Model" }, { label: "Firmware Version" }],
        expectedInfoCardCount: 2,
        emptySlotText: /No psu detected in this slot/i,
      },
      "Control Board": {
        metrics: [],
        metadata: [{ label: "Serial Number" }],
        expectedInfoCardCount: 1,
        emptySlotText: /No control board detected in this slot/i,
      },
    };

    for (const section of diagnosticsSections) {
      await test.step(`Validate modal content for ${section.filterLabel} cards`, async () => {
        await diagnosticsPage.clickFilterButton(section.filterLabel);
        await diagnosticsPage.validateCardCountInSection(section.sectionTestIdName, section.expectedCardCount);

        const expected = expectedBySection[section.sectionTestIdName];

        const counts: Record<"info" | "empty", number> = { info: 0, empty: 0 };

        for (let cardIndex = 0; cardIndex < section.expectedCardCount; cardIndex++) {
          const kind = await diagnosticsPage.validateCardInfoOrEmptySlot(
            section.sectionTestIdName,
            cardIndex,
            expected,
          );
          counts[kind] += 1;
        }

        expect(counts.info).toBe(expected.expectedInfoCardCount);
        expect(counts.empty).toBe(section.expectedCardCount - expected.expectedInfoCardCount);
      });
    }
  });
});
