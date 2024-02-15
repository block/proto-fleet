const config = {
  stories: ["../src/**/*.stories.@(js|jsx|mjs|ts|tsx)"],
  addons: [
    "storybook-addon-data-theme-switcher",
    "@storybook/addon-actions"
  ],
  framework: {
    name: "@storybook/react-vite",
    options: {},
  },
};
export default config;
