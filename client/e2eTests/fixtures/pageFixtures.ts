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
import { SettingsTeamPage } from "../pages/settingsTeam";

type PageFixtures = {
  authPage: AuthPage;
  homePage: HomePage;
  minersPage: MinersPage;
  addMinersPage: AddMinersPage;
  settingsPage: SettingsPage;
  settingsTeamPage: SettingsTeamPage;
  settingsPoolsPage: SettingsPoolsPage;
  editPoolPage: EditPoolPage;
  newPoolModal: NewPoolModalPage;
};

export const test = base.extend<PageFixtures>({
  authPage: async ({ page }, use) => {
    await use(new AuthPage(page));
  },
  homePage: async ({ page }, use) => {
    await use(new HomePage(page));
  },
  minersPage: async ({ page }, use) => {
    await use(new MinersPage(page));
  },
  addMinersPage: async ({ page }, use) => {
    await use(new AddMinersPage(page));
  },
  settingsPage: async ({ page }, use) => {
    await use(new SettingsPage(page));
  },
  settingsTeamPage: async ({ page }, use) => {
    await use(new SettingsTeamPage(page));
  },
  settingsPoolsPage: async ({ page }, use) => {
    await use(new SettingsPoolsPage(page));
  },
  editPoolPage: async ({ page }, use) => {
    await use(new EditPoolPage(page));
  },
  newPoolModal: async ({ page }, use) => {
    await use(new NewPoolModalPage(page));
  },
});

export const expect = test.expect;
