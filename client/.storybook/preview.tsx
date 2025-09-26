import React from "react";
import type { Preview } from "@storybook/react-vite";

import { PreferencesProvider } from "../src/shared/features/preferences";

import "../src/shared/styles/index.css";

export const decorators = [
  (Story) => {
    return (
      <PreferencesProvider>
        <Story />
      </PreferencesProvider>
    );
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
