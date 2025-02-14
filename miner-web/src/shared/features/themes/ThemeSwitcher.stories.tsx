import { action } from "@storybook/addon-actions";

import ThemeSwitcherComponent from "./ThemeSwitcher";

export const ThemeSwitcher = () => {
  return <ThemeSwitcherComponent onClickDone={action("Done clicked")} />;
};

export default {
  title: "Components (Shared)/Theme Switcher",
};
