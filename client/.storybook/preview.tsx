import React, { ComponentType } from "react";
import type { Preview } from "@storybook/react-vite";
import "../src/shared/styles/index.css";

export const decorators = [
  (Story: ComponentType) => {
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
    options: {
      storySort: {
        order: ["Foundation", "Shared", "ProtoOS", "Proto Fleet", "*"],
      },
    },
  },
};

export default preview;
