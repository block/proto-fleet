import { test } from "../fixtures/pageFixtures";

test.describe("Temperature unit switching", () => {
  test.beforeEach(async ({ page, commonSteps }) => {
    await page.goto("/");
    await commonSteps.authenticateAsAdmin();
  });

  test("Switch between Fahrenheit and Celsius", async ({ homePage, diagnosticsPage, generalPage, commonSteps }) => {
    const fahrenheitPattern = /\d+\.\d\s°F/;
    const celsiusPattern = /\d+\.\d\s°C/;
    await commonSteps.navigateToGeneralSettings();

    await test.step("Change temperature to Fahrenheit", async () => {
      await generalPage.clickTemperatureButton();
      await generalPage.selectFahrenheit();
      await generalPage.clickDoneButton();
      await generalPage.validateTemperatureFormatFahrenheit();
    });

    await commonSteps.navigateToHome();

    await test.step("Validate temperature in Fahrenheit on Home", async () => {
      await homePage.clickTab("temperature");
      await homePage.validateTemperatureInFormat(fahrenheitPattern);
      await homePage.validateStatItem(0, "Average", fahrenheitPattern);
      await homePage.validateStatItem(1, "Highest", fahrenheitPattern);
      await homePage.validateStatItem(2, "Lowest", fahrenheitPattern);
      await homePage.hoverOverChart();
      await homePage.validateChartTooltipWithHashboards(fahrenheitPattern);
    });

    await commonSteps.navigateToDiagnostics();

    await test.step("Validate temperature in Fahrenheit on Diagnostics - Hashboards", async () => {
      await diagnosticsPage.clickFilterButton("Hashboards");
      await diagnosticsPage.validateTemperaturesInFormat(8, fahrenheitPattern, celsiusPattern);
    });

    await test.step("Validate temperature in Fahrenheit on Diagnostics - PSUs", async () => {
      await diagnosticsPage.clickFilterButton("PSUs");
      await diagnosticsPage.validateTemperaturesInFormat(4, fahrenheitPattern, celsiusPattern);
    });

    await commonSteps.navigateToGeneralSettings();

    await test.step("Change temperature back to Celsius", async () => {
      await generalPage.clickTemperatureButton();
      await generalPage.selectCelsius();
      await generalPage.clickDoneButton();
      await generalPage.validateTemperatureFormatCelsius();
    });

    await commonSteps.navigateToHome();

    await test.step("Validate temperature in Celsius on Home", async () => {
      await homePage.clickTab("temperature");
      await homePage.validateTemperatureInFormat(celsiusPattern);
      await homePage.validateStatItem(0, "Average", celsiusPattern);
      await homePage.validateStatItem(1, "Highest", celsiusPattern);
      await homePage.validateStatItem(2, "Lowest", celsiusPattern);
      await homePage.hoverOverChart();
      await homePage.validateChartTooltipWithHashboards(celsiusPattern);
    });

    await commonSteps.navigateToDiagnostics();

    await test.step("Validate temperature in Celsius on Diagnostics - Hashboards", async () => {
      await diagnosticsPage.clickFilterButton("Hashboards");
      await diagnosticsPage.validateTemperaturesInFormat(8, celsiusPattern, fahrenheitPattern);
    });

    await test.step("Validate temperature in Celsius on Diagnostics - PSUs", async () => {
      await diagnosticsPage.clickFilterButton("PSUs");
      await diagnosticsPage.validateTemperaturesInFormat(4, celsiusPattern, fahrenheitPattern);
    });
  });
});
