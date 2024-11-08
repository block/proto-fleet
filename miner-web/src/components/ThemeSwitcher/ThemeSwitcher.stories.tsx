import { action } from "@storybook/addon-actions";

import ThemeSwitcherComponent from ".";

export const ThemeSwitcher = () => {
  return <ThemeSwitcherComponent onClickDone={action("Done clicked")} />;
};

export default {
  title: "Components/Theme Switcher",
};
