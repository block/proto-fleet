export default {
  plugins: {
    "postcss-simple-vars": {},
    "./scripts/postcssThemeColors.cjs": {
      themePath: "../src/shared/styles/colors.cjs",
    },
    "@tailwindcss/postcss": {},
  }
};

