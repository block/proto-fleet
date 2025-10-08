import { action } from "storybook/actions";

import ThemeSwitcherComponent from "./ThemeSwitcher";

export const ThemeSwitcher = () => {
  return <ThemeSwitcherComponent onClickDone={action("Done clicked")} />;
};

export default {
  title: "Shared/Theme Switcher",
};
