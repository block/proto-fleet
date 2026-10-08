import { expect, test } from "../fixtures/pageFixtures";

test.describe("Add miners scan completion", () => {
  test.use({ storageState: { cookies: [], origins: [] } });

  test("waits for discovery to finish before reading a late container and miner summary", async ({
    page,
    addMinersPage,
  }) => {
    // Mobile keeps the scanning action in its closed overflow menu. The partial
    // summary is the visible indication that discovery has not finished yet.
    await page.setContent(`
      <div id="summary">Scanning… 1 miner found on your network</div>
      <div data-testid="found-miners-list">Proto Rig</div>
      <div hidden><button>Scanning</button></div>
      <button>Continue with 1 miners</button>
    `);

    // Keep discovery incomplete through a short negative-assertion budget.
    // An early return would let selection miss a container arriving afterward.
    await expect(page.getByTestId("found-miners-list")).toBeVisible();
    await expect(page.getByRole("button", { name: "Continue with 1 miners", exact: true })).toBeVisible();
    let partialScanError: unknown;
    try {
      await addMinersPage.waitForNetworkScanToFinish(750);
    } catch (error) {
      partialScanError = error;
    }
    expect(partialScanError).toBeInstanceOf(Error);
    expect((partialScanError as Error).message).toContain("toBe");

    await page.locator("#summary").evaluate((summary) => {
      summary.textContent = "1 container and 1 miner found on your network";
    });
    await page.getByRole("button", { name: "Continue with 1 miners", exact: true }).evaluate((button) => {
      button.textContent = "Continue with 2 miners";
    });
    await page.getByRole("button", { name: "Scanning", exact: true, includeHidden: true }).evaluate((button) => {
      button.textContent = "Rescan network";
      button.parentElement!.hidden = false;
    });

    await addMinersPage.waitForNetworkScanToFinish();
    expect(await addMinersPage.getFoundMinersCount()).toBe(1);
    await expect(page.getByRole("button", { name: "Continue with 2 miners", exact: true })).toBeVisible();
  });
});

test.describe("Proto Fleet - Add Miners Validation", () => {
  test.beforeEach(async ({ page, commonSteps }) => {
    await page.goto("/");
    await commonSteps.loginAsAdmin();
  });

  test(
    "Back to editing button closes dialog and returns to form",
    { tag: "@smoke" },
    async ({ minersPage, addMinersPage }) => {
      await test.step("Navigate to add miners flow", async () => {
        await minersPage.navigateToMinersPage();
        await minersPage.clickAddMinersButton();
      });

      await test.step("Enter mix of valid and invalid entries", async () => {
        await addMinersPage.inputMinerIp("192.168.1.1, 999.999.999.999");
        await addMinersPage.clickFindMinersByIp();
      });

      await test.step("Validate error dialog is shown", async () => {
        await addMinersPage.validateValidationErrorDialogIsVisible();
      });

      await test.step("Click back to editing", async () => {
        await addMinersPage.clickBackToEditing();
      });

      await test.step("Validate dialog is closed and form is still visible", async () => {
        await addMinersPage.validateValidationErrorDialogIsClosed();
        // Verify the textarea is still accessible with the original value
        const textarea = addMinersPage["page"].locator("#ipAddresses");
        await expect(textarea).toBeVisible();
      });

      await test.step("Validate error message appears on textarea", async () => {
        await addMinersPage.validateTextareaErrorContains("Check the format of the following and retry");
        await addMinersPage.validateTextareaErrorContains("999.999.999.999");
      });
    },
  );

  test(
    "Continue anyway button proceeds with valid entries only",
    { tag: "@smoke" },
    async ({ minersPage, addMinersPage }) => {
      await test.step("Navigate to add miners flow", async () => {
        await minersPage.navigateToMinersPage();
        await minersPage.clickAddMinersButton();
      });

      await test.step("Enter mix of valid and invalid entries", async () => {
        await addMinersPage.inputMinerIp("192.168.1.1, 999.999.999.999");
        await addMinersPage.clickFindMinersByIp();
      });

      await test.step("Validate error dialog is shown", async () => {
        await addMinersPage.validateValidationErrorDialogIsVisible();
      });

      await test.step("Click continue anyway", async () => {
        await addMinersPage.clickContinueAnyway();
      });

      await test.step("Validate dialog is closed and discovery proceeds", async () => {
        await addMinersPage.validateValidationErrorDialogIsClosed();
        await addMinersPage.waitForFoundMinersList();
      });
    },
  );

  test("Shows multiple error categories in dialog", async ({ minersPage, addMinersPage }) => {
    await test.step("Navigate to add miners flow", async () => {
      await minersPage.navigateToMinersPage();
      await minersPage.clickAddMinersButton();
    });

    await test.step("Enter multiple types of invalid entries", async () => {
      await addMinersPage.inputMinerIp("999.999.999.999, 192.168.1.100-50, 192.168.1.0/33");
      await addMinersPage.clickFindMinersByIp();
    });

    await test.step("Validate all error categories are shown", async () => {
      await addMinersPage.validateValidationErrorDialogIsVisible();
      await addMinersPage.validateInvalidIpAddressesInDialog(["999.999.999.999"]);
      await addMinersPage.validateInvalidIpRangesInDialog(["192.168.1.100-50"]);
      await addMinersPage.validateInvalidSubnetsInDialog(["192.168.1.0/33"]);
    });
  });

  test("Hides Continue anyway when all entries are invalid", async ({ minersPage, addMinersPage }) => {
    await test.step("Navigate to add miners flow", async () => {
      await minersPage.navigateToMinersPage();
      await minersPage.clickAddMinersButton();
    });

    await test.step("Enter only invalid entries", async () => {
      await addMinersPage.inputMinerIp("999.999.999.999, 256.1.1.1");
      await addMinersPage.clickFindMinersByIp();
    });

    await test.step("Validate dialog shows only Back to editing button", async () => {
      await addMinersPage.validateValidationErrorDialogIsVisible();
      await addMinersPage.validateContinueAnywayButtonNotVisible();
    });

    await test.step("Back to editing works correctly", async () => {
      await addMinersPage.clickBackToEditing();
      await addMinersPage.validateValidationErrorDialogIsClosed();
    });
  });
});
