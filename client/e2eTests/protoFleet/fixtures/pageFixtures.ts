// NOTE: eslint incorrectly identifies 'use' as react hook
/* eslint-disable react-hooks/rules-of-hooks */
import { test as base } from "@playwright/test";
import { CommonSteps } from "../helpers/commonSteps";
import { AddMinersPage } from "../pages/addMiners";
import { AuthPage } from "../pages/auth";
import { LoginModalComponent } from "../pages/components/loginModal";
import { EditPoolPage } from "../pages/editPool";
import { GroupsPage } from "../pages/groups";
import { HomePage } from "../pages/home";
import { MinersPage } from "../pages/miners";
import { NewPoolModalPage } from "../pages/newPoolModal";
import { RacksPage } from "../pages/racks";
import { SettingsPage } from "../pages/settings";
import { SettingsApiKeysPage } from "../pages/settingsApiKeys";
import { SettingsFirmwarePage } from "../pages/settingsFirmware";
import { SettingsPoolsPage } from "../pages/settingsPools";
import { SettingsSchedulesPage } from "../pages/settingsSchedules";
import { SettingsSecurityPage } from "../pages/settingsSecurity";
import { SettingsTeamPage } from "../pages/settingsTeam";

type PageFixtures = {
  authPage: AuthPage;
  homePage: HomePage;
  minersPage: MinersPage;
  groupsPage: GroupsPage;
  racksPage: RacksPage;
  addMinersPage: AddMinersPage;
  settingsPage: SettingsPage;
  settingsFirmwarePage: SettingsFirmwarePage;
  settingsApiKeysPage: SettingsApiKeysPage;
  settingsSchedulesPage: SettingsSchedulesPage;
  settingsSecurityPage: SettingsSecurityPage;
  settingsTeamPage: SettingsTeamPage;
  settingsPoolsPage: SettingsPoolsPage;
  editPoolPage: EditPoolPage;
  newPoolModal: NewPoolModalPage;
  loginModal: LoginModalComponent;
  commonSteps: CommonSteps;
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
  groupsPage: async ({ page, isMobile }, use) => {
    await use(new GroupsPage(page, isMobile));
  },
  racksPage: async ({ page, isMobile }, use) => {
    await use(new RacksPage(page, isMobile));
  },
  addMinersPage: async ({ page, isMobile }, use) => {
    await use(new AddMinersPage(page, isMobile));
  },
  settingsPage: async ({ page, isMobile }, use) => {
    await use(new SettingsPage(page, isMobile));
  },
  settingsFirmwarePage: async ({ page, isMobile }, use) => {
    await use(new SettingsFirmwarePage(page, isMobile));
  },
  settingsApiKeysPage: async ({ page, isMobile }, use) => {
    await use(new SettingsApiKeysPage(page, isMobile));
  },
  settingsSchedulesPage: async ({ page, isMobile }, use) => {
    await use(new SettingsSchedulesPage(page, isMobile));
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
  loginModal: async ({ page, isMobile }, use) => {
    await use(new LoginModalComponent(page, isMobile));
  },
  commonSteps: async ({ authPage, minersPage }, use) => {
    await use(new CommonSteps(authPage, minersPage));
  },
});

export const expect = test.expect;
