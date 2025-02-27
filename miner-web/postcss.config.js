export default {
  plugins: {
    "./scripts/postcssThemeColors.cjs": {
      themePath: "../src/shared/styles/colors.cjs",
    },
    "@tailwindcss/postcss": {},
  }
};

