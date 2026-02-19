// NOTE: eslint incorrectly identifies 'use' as react hook
/* eslint-disable react-hooks/rules-of-hooks */
import { test as base } from "@playwright/test";
import { CommonSteps } from "../helpers/commonSteps";
import { AuthenticationPage } from "../pages/authentication";
import { HeaderComponent } from "../pages/components/header";
import { NavigationComponent } from "../pages/components/navigation";
import { SleepWakeDialogsComponent } from "../pages/components/sleepWakeDialog";
import { WakeCalloutComponent } from "../pages/components/wakeCallout";
import { CoolingPage } from "../pages/cooling";
import { DiagnosticsPage } from "../pages/diagnostics";
import { GeneralPage } from "../pages/general";
import { HardwarePage } from "../pages/hardware";
import { HomePage } from "../pages/home";
import { LogsPage } from "../pages/logs";
import { WelcomePage } from "../pages/onboarding";
import { PoolsPage } from "../pages/pools";

type PageFixtures = {
  welcomePage: WelcomePage;
  homePage: HomePage;
  poolsPage: PoolsPage;
  diagnosticsPage: DiagnosticsPage;
  logsPage: LogsPage;
  authenticationPage: AuthenticationPage;
  generalPage: GeneralPage;
  hardwarePage: HardwarePage;
  coolingPage: CoolingPage;
  commonSteps: CommonSteps;
  navigationComponent: NavigationComponent;
  headerComponent: HeaderComponent;
  sleepWakeDialogsComponent: SleepWakeDialogsComponent;
  wakeCalloutComponent: WakeCalloutComponent;
};

export const test = base.extend<PageFixtures>({
  welcomePage: async ({ page, isMobile }, use) => {
    await use(new WelcomePage(page, isMobile));
  },
  homePage: async ({ page, isMobile }, use) => {
    await use(new HomePage(page, isMobile));
  },
  poolsPage: async ({ page, isMobile }, use) => {
    await use(new PoolsPage(page, isMobile));
  },
  diagnosticsPage: async ({ page, isMobile }, use) => {
    await use(new DiagnosticsPage(page, isMobile));
  },
  logsPage: async ({ page, isMobile }, use) => {
    await use(new LogsPage(page, isMobile));
  },
  authenticationPage: async ({ page, isMobile }, use) => {
    await use(new AuthenticationPage(page, isMobile));
  },
  generalPage: async ({ page, isMobile }, use) => {
    await use(new GeneralPage(page, isMobile));
  },
  hardwarePage: async ({ page, isMobile }, use) => {
    await use(new HardwarePage(page, isMobile));
  },
  coolingPage: async ({ page, isMobile }, use) => {
    await use(new CoolingPage(page, isMobile));
  },
  navigationComponent: async ({ page, isMobile }, use) => {
    await use(new NavigationComponent(page, isMobile));
  },
  headerComponent: async ({ page, isMobile }, use) => {
    await use(new HeaderComponent(page, isMobile));
  },
  sleepWakeDialogsComponent: async ({ page, isMobile }, use) => {
    await use(new SleepWakeDialogsComponent(page, isMobile));
  },
  wakeCalloutComponent: async ({ page, isMobile }, use) => {
    await use(new WakeCalloutComponent(page, isMobile));
  },
  commonSteps: async (
    { welcomePage, navigationComponent, headerComponent, sleepWakeDialogsComponent, wakeCalloutComponent },
    use,
  ) => {
    await use(
      new CommonSteps(
        welcomePage,
        navigationComponent,
        headerComponent,
        sleepWakeDialogsComponent,
        wakeCalloutComponent,
      ),
    );
  },
});

export const expect = test.expect;
