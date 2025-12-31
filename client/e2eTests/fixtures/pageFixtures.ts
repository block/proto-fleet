// NOTE: eslint incorrectly identifies 'use' as react hook
/* eslint-disable react-hooks/rules-of-hooks */
import { test as base } from "@playwright/test";
import { AddMinersPage } from "../pages/addMiners";
import { AuthPage } from "../pages/auth";
import { EditPoolPage } from "../pages/editPool";
import { HomePage } from "../pages/home";
import { MinersPage } from "../pages/miners";
import { NewPoolModalPage } from "../pages/newPoolModal";
import { SettingsPage } from "../pages/settings";
import { SettingsPoolsPage } from "../pages/settingsPools";
import { SettingsSecurityPage } from "../pages/settingsSecurity";
import { SettingsTeamPage } from "../pages/settingsTeam";

type PageFixtures = {
  authPage: AuthPage;
  homePage: HomePage;
  minersPage: MinersPage;
  addMinersPage: AddMinersPage;
  settingsPage: SettingsPage;
  settingsSecurityPage: SettingsSecurityPage;
  settingsTeamPage: SettingsTeamPage;
  settingsPoolsPage: SettingsPoolsPage;
  editPoolPage: EditPoolPage;
  newPoolModal: NewPoolModalPage;
};

export const test = base.extend<PageFixtures>({
  authPage: async ({ page, isMobile }, use) => {
    await use(new AuthPage(page, isMobile));
  },
  homePage: async ({ page, isMobile }, use) => {
    await use(new HomePage(page, isMobile));
  },
  minersPage: async ({ page, isMobile }, use) => {
    await use(new MinersPage(page, isMobile));
  },
  addMinersPage: async ({ page, isMobile }, use) => {
    await use(new AddMinersPage(page, isMobile));
  },
  settingsPage: async ({ page, isMobile }, use) => {
    await use(new SettingsPage(page, isMobile));
  },
  settingsSecurityPage: async ({ page, isMobile }, use) => {
    await use(new SettingsSecurityPage(page, isMobile));
  },
  settingsTeamPage: async ({ page, isMobile }, use) => {
    await use(new SettingsTeamPage(page, isMobile));
  },
  settingsPoolsPage: async ({ page, isMobile }, use) => {
    await use(new SettingsPoolsPage(page, isMobile));
  },
  editPoolPage: async ({ page, isMobile }, use) => {
    await use(new EditPoolPage(page, isMobile));
  },
  newPoolModal: async ({ page, isMobile }, use) => {
    await use(new NewPoolModalPage(page, isMobile));
  },
});

export const expect = test.expect;
