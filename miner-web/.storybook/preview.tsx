import React from "react";
import type { Preview } from "@storybook/react";
import { useDarkMode } from "storybook-dark-mode";

import "../src/index.css";

export const decorators = [
  (Story) => {
    const isDark = useDarkMode();

    React.useEffect(() => {
      const body = document.querySelector("body");
      body?.setAttribute("data-theme", isDark ? "dark" : "light");
    }, [isDark]);

    return <Story />;
  },
];

const preview: Preview = {
  parameters: {
    actions: { argTypesRegex: "^on[A-Z].*" },
    controls: {
      matchers: {
        color: /(background|color)$/i,
        date: /Date$/i,
      },
    },
    darkMode: {
      current: "light",
    },
  },
};

export default preview;
