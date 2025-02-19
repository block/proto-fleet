const path = require("path");

module.exports = (opts = {}) => {

  const flattenObject = (obj, prefix = "") => {
    return Object.keys(obj).reduce((acc, key) => {
      const newPrefix = prefix ? `${prefix}-${key}` : key;
      if (typeof obj[key] === "object" && obj[key] !== null) {
        return { ...acc, ...flattenObject(obj[key], newPrefix) };
      }
      return { ...acc, [newPrefix]: obj[key] };
    }, {});
  };

  const processThemeVars = (theme, mode) => {
    const flattenedVars = {};

    Object.entries(theme).forEach(([category, values]) => {
      Object.entries(values).forEach(([key, modeValues]) => {
        if (modeValues[mode]) {
          const varName = `--color-${category}-${key}`;
          flattenedVars[varName] = modeValues[mode];
        }
      });
    });

    return flattenedVars;
  };

  return {
    postcssPlugin: "postcss-theme-colors",

    async Once(root) {
      const theme = require(opts.themePath);
      if (!theme) return;

      root.walkAtRules("theme-colors", (rule) => {
        const mode = rule.params.trim();
        const variables = processThemeVars(theme, mode);

        const declaration = Object.entries(variables)
          .map(([name, value]) => `${name}: ${value};`)
          .join("\n");

        rule.replaceWith(`${declaration}`);
      });
    },
  };
};

module.exports.postcss = true;
