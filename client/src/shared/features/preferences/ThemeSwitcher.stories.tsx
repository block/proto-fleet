import { useState } from "react";
import { action } from "storybook/actions";

import ThemeSwitcherComponent from "./ThemeSwitcher";

export const ThemeSwitcher = () => {
  const [theme, setTheme] = useState<"dark" | "light" | "system">("system");

  return <ThemeSwitcherComponent onClickDone={action("Done clicked")} theme={theme} setTheme={setTheme} />;
};

export default {
  title: "Shared/Theme Switcher",
};
