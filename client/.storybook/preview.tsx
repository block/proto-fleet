import React, { ComponentType } from "react";
import { MemoryRouter } from "react-router-dom";
import type { Preview } from "@storybook/react-vite";
import "../src/shared/styles/index.css";

import { spyOn } from "storybook/test";

export const beforeEach = () => {
  spyOn(console, "log").mockName("console.log");
  spyOn(console, "warn").mockName("console.warn");
};

export const decorators = [
  (Story: ComponentType) => {
    return (
      <MemoryRouter>
        <Story />
      </MemoryRouter>
    );
  },
];

const preview: Preview = {
  parameters: {
    actions: { argTypesRegex: "^on[A-Z].*" },
    layout: "fullscreen",
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
