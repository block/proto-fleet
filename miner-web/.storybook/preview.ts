import type { Preview } from "@storybook/react";
import '../src/index.css';

export const globalTypes = {
  dataThemes: {
    defaultValue: {
      list: [
        { name: "Light", dataTheme: "light" },
      ],
    },
  },
  dataTheme: {
    defaultValue: "light",
  },
};

const preview: Preview = {
  parameters: {
    actions: { argTypesRegex: "^on[A-Z].*" },
    controls: {
      matchers: {
        color: /(background|color)$/i,
        date: /Date$/i,
      },
    },
  },
};

export default preview;
