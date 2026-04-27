import { expect, test } from "../fixtures/pageFixtures";

test.describe("Proto Fleet - Add Miners Validation", () => {
  test.beforeEach(async ({ page, commonSteps }) => {
    await page.goto("/");
    await commonSteps.loginAsAdmin();
  });

  test("Shows validation error dialog for invalid IP addresses", async ({ minersPage, addMinersPage }) => {
    await test.step("Navigate to add miners flow", async () => {
      await minersPage.navigateToMinersPage();
      await minersPage.clickAddMinersButton();
    });

    await test.step("Enter invalid IP addresses", async () => {
      await addMinersPage.inputMinerIp("999.999.999.999, 256.1.1.1");
      await addMinersPage.clickFindMinersByIp();
    });

    await test.step("Validate error dialog is shown with invalid entries", async () => {
      await addMinersPage.validateValidationErrorDialogIsVisible();
      await addMinersPage.validateInvalidIpAddressesInDialog(["999.999.999.999", "256.1.1.1"]);
    });
  });

  test("Shows validation error dialog for invalid IP ranges", async ({ minersPage, addMinersPage }) => {
    await test.step("Navigate to add miners flow", async () => {
      await minersPage.navigateToMinersPage();
      await minersPage.clickAddMinersButton();
    });

    await test.step("Enter invalid IP range (end before start)", async () => {
      await addMinersPage.inputMinerIp("192.168.1.100-50");
      await addMinersPage.clickFindMinersByIp();
    });

    await test.step("Validate error dialog is shown with invalid range", async () => {
      await addMinersPage.validateValidationErrorDialogIsVisible();
      await addMinersPage.validateInvalidIpRangesInDialog(["192.168.1.100-50"]);
    });
  });

  test("Shows validation error dialog for invalid subnets", async ({ minersPage, addMinersPage }) => {
    await test.step("Navigate to add miners flow", async () => {
      await minersPage.navigateToMinersPage();
      await minersPage.clickAddMinersButton();
    });

    await test.step("Enter invalid subnet (mask > 32)", async () => {
      await addMinersPage.inputMinerIp("192.168.1.0/33");
      await addMinersPage.clickFindMinersByIp();
    });

    await test.step("Validate error dialog is shown with invalid subnet", async () => {
      await addMinersPage.validateValidationErrorDialogIsVisible();
      await addMinersPage.validateInvalidSubnetsInDialog(["192.168.1.0/33"]);
    });
  });

  test("Back to editing button closes dialog and returns to form", async ({ minersPage, addMinersPage }) => {
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
  });

  test("Continue anyway button proceeds with valid entries only", async ({ minersPage, addMinersPage, page }) => {
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
      // The pairing step should now be active (either loading or showing results)
      const findingMinersTitle = page.getByText("Finding miners on your network");
      const foundMinersSection = page.getByText(/\d+ miners found/);
      const noMinersFound = page.getByText(/No miners found/);

      // Wait for either the loading state, results, or no miners found
      await expect(findingMinersTitle.or(foundMinersSection).or(noMinersFound)).toBeVisible({ timeout: 10000 });
    });
  });

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

  test("Shows Continue anyway when mix of valid and invalid entries", async ({ minersPage, addMinersPage }) => {
    await test.step("Navigate to add miners flow", async () => {
      await minersPage.navigateToMinersPage();
      await minersPage.clickAddMinersButton();
    });

    await test.step("Enter mix of valid and invalid entries", async () => {
      await addMinersPage.inputMinerIp("192.168.1.1, 999.999.999.999");
      await addMinersPage.clickFindMinersByIp();
    });

    await test.step("Validate dialog shows both buttons", async () => {
      await addMinersPage.validateValidationErrorDialogIsVisible();
      await addMinersPage.validateContinueAnywayButtonVisible();
    });
  });
});
