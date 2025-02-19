/***
 * This file defines the colors in our tailwind theme
 * They get preprocessed /scripts/postcssThemeColors.js
 * which then sets each of these colors as theme variables 
 */

const black_100 =  "0 0 0"; // #000000 
const blue_100 =  "0 150 209"; // #2690C7 
const gray_90 =  "16 16 16"; // #101010 
const gray_80 =  "32 32 32"; // #202020 
const gray_70 =  "48 48 48"; // #303030 
const gray_60 =  "80 80 80"; // #505050 
const gray_20 =  "192 192 192"; // #C0C0C0 
const gray_10 =  "224 224 224"; // #E0E0E0 
const gray_5 =  "242 242 242"; // #F2F2F2 
const green_100 =  "56 166 0"; // #38A600 
const light_green_100 =  "157 211 26"; // #9DD31A 
const indigo_100 =  "120 62 237";  // #783EED 
const orange_100 =  "254 124 0"; // #FE7C00 
const red_100 =  "250 43 55"; // #FA2B37 
const white_100 =  "255 255 255"; // #FFFFFF 
const yellow_100 =  "253 138 0"; // #FD8A00 

const text_accent =  "51 18 0"; // #331200 
const text_accent_contrast =  "249 115 42"; // #F9732A 
const text_critical =  "116 20 13"; // #74140D 
const text_info =  "1 83 119"; // #015377 
const text_success =  "6 63 37"; // #063F25 
const text_warning =  "135 73 0"; // #874900 
const text_indigo =  "55 28 109"; // #371C6D 

module.exports = {
  text: {
    primary: {
      light: `rgb(${black_100} / 90%)`,
      dark: `rgb(${white_100} / 90%)`,
    },
    "primary-70": {
      light: `rgb(${black_100} / 70%)`,
      dark: `rgb(${white_100} / 70%)`,
    },
    "primary-50": {
      light: `rgb(${black_100} / 50%)`,
      dark: `rgb(${white_100} / 50%)`,
    },
    "primary-30": {
      light: `rgb(${black_100} / 30%)`,
      dark: `rgb(${white_100} / 30%)`,
    },
    contrast: {
      light: `rgb(${white_100})`,
      dark: `rgb(${black_100})`,
    },
    "contrast-70": {
      light: `rgb(${white_100} / 70%)`,
      dark: `rgb(${black_100} / 80%)`,
    },
    emphasis: {
      light: `rgb(${orange_100})`,
      dark: `rgb(${orange_100})`,
    },
    accent: {
      light: `rgb(${orange_100} / 80%)`,
      dark: `rgb(${orange_100} / 80%)`,
    },
    success: {
      light: `rgb(${light_green_100})`,
      dark: `rgb(${light_green_100})`,
    },
    warning: {
      light: `rgb(${yellow_100})`,
      dark: `rgb(${yellow_100})`,
    },
    critical: {
      light: `rgb(${red_100})`,
      dark: `rgb(${red_100})`,
    },
    "base-static": {
      light: `rgb(${black_100} / 90%)`,
      dark: `rgb(${black_100} / 90%)`,
    },
    "base-contrast-static": {
      light: `rgb(${white_100})`,
      dark: `rgb(${white_100})`,
    },
  },
  surface: {
    default: {
      light: `rgb(${white_100} / 2%)`,
      dark: `rgb(${black_100} / 2%)`,
    },
    base: {
      light: `rgb(${white_100})`,
      dark: `rgb(${gray_90})`,
    },
    "elevated-base": {
      light: white_100,
      dark: `rgb(${gray_80})`,
    },
    20: {
      light: `rgb(${gray_20})`,
      dark: `rgb(${gray_80})`,
    },
    10: {
      light: `rgb(${gray_10})`,
      dark: `rgb(${gray_70})`,
    },
    5: {
      light: `rgb(${gray_5})`,
      dark: `rgb(${gray_60})`,
    },
    overlay: {
      light: `rgb(${black_100} / 5%)`,
      dark: `rgb(${black_100} / 5%)`,
    },
  },
  border: {
    primary: {
      light: `rgb(${black_100} / 90%)`,
      dark: `rgb(${black_100} / 90%)`,
    },
    20: {
      light: `rgb(${black_100} / 20%)`,
      dark: `rgb(${black_100} / 20%)`,
    },
    10: {
      light: `rgb(${black_100} / 10%)`,
      dark: `rgb(${black_100} / 10%)`,
    },
    5: {
      light: `rgb(${black_100} / 5%)`,
      dark: `rgb(${black_100} / 5%)`,
    },
  },
  core: {
    "primary-fill": {
      light: `rgb(${black_100} / 90%)`,
      dark: `rgb(${white_100} / 90%)`,
    },
    "primary-80": {
      light: `rgb(${black_100} / 80%)`,
      dark: `rgb(${white_100} / 80%)`,
    },
    "primary-50": {
      light: `rgb(${black_100} / 50%)`,
      dark: `rgb(${white_100} / 50%)`,
    },
    "primary-20": {
      light: `rgb(${black_100} / 20%)`,
      dark: `rgb(${white_100} / 20%)`,
    },
    "primary-10": {
      light: `rgb(${black_100} / 10%)`,
      dark: `rgb(${white_100} / 10%)`,
    },
    "primary-5": {
      light: `rgb(${black_100} / 5%)`,
      dark: `rgb(${white_100} / 5%)`,
    },
    "accent-fill": {
      light: `rgb(${orange_100})`,
      dark: `rgb(${orange_100})`,
    },
    "accent-text": {
      light: `rgb(${text_accent})`,
      dark: `rgb(${text_accent_contrast})`,
    },
    "accent-80": {
      light: `rgb(${orange_100} / 80%)`,
      dark: `rgb(${orange_100} / 80%)`,
    },
    "accent-50": {
      light: `rgb(${orange_100} / 50%)`,
      dark: `rgb(${orange_100} / 50%)`,
    },
    "accent-20": {
      light: `rgb(${orange_100} / 20%)`,
      dark: `rgb(${orange_100} / 20%)`,
    },
    "accent-10": {
      light: `rgb(${orange_100} / 10%)`,
      dark: `rgb(${orange_100} / 10%)`,
    },
    "indigo-fill": {
      light: `rgb(${indigo_100})`,
      dark: `rgb(${indigo_100})`,
    },
    "indigo-text": {
      light: `rgb(${text_indigo})`,
      dark: `rgb(${text_indigo})`,
    },
    "indigo-80": {
      light: `rgb(${indigo_100} / 80%)`,
      dark: `rgb(${indigo_100} / 80%)`,
    },
    "indigo-50": {
      light: `rgb(${indigo_100} / 50%)`,
      dark: `rgb(${indigo_100} / 50%)`,
    },
    "indigo-20": {
      light: `rgb(${indigo_100} / 20%)`,
      dark: `rgb(${indigo_100} / 20%)`,
    },
    "indigo-10": {
      light: `rgb(${indigo_100} / 10%)`,
      dark: `rgb(${indigo_100} / 10%)`,
    },
  },
  intent: {
    "info-fill": {
      light: `rgb(${blue_100})`,
      dark: `rgb(${blue_100})`,
    },
    "info-text": {
      light: `rgb(${text_info})`,
      dark: `rgb(${blue_100})`,
    },
    "info-80": {
      light: `rgb(${blue_100} / 80%)`,
      dark: `rgb(${blue_100} / 90%)`,
    },
    "info-50": {
      light: `rgb(${blue_100} / 50%)`,
      dark: `rgb(${blue_100} / 60%)`,
    },
    "info-20": {
      light: `rgb(${blue_100} / 20%)`,
      dark: `rgb(${blue_100} / 30%)`,
    },
    "info-10": {
      light: `rgb(${blue_100} / 10%)`,
      dark: `rgb(${blue_100} / 20%)`,
    },
    "success-fill": {
      light: `rgb(${green_100})`,
      dark: `rgb(${green_100})`,
    },
    "success-text": {
      light: `rgb(${text_success})`,
      dark: `rgb(${green_100})`,
    },
    "success-80": {
      light: `rgb(${green_100} / 80%)`,
      dark: `rgb(${green_100} / 90%)`,
    },
    "success-50": {
      light: `rgb(${green_100} / 50%)`,
      dark: `rgb(${green_100} / 60%)`,
    },
    "success-20": {
      light: `rgb(${green_100} / 20%)`,
      dark: `rgb(${green_100} / 30%)`,
    },
    "success-10": {
      light: `rgb(${green_100} / 10%)`,
      dark: `rgb(${green_100} / 20%)`,
    },
    "warning-fill": {
      light: `rgb(${yellow_100})`,
      dark: `rgb(${yellow_100})`,
    },
    "warning-text": {
      light: `rgb(${text_warning})`,
      dark: `rgb(${yellow_100})`,
    },
    "warning-80": {
      light: `rgb(${yellow_100} / 80%)`,
      dark: `rgb(${yellow_100} / 90%)`,
    },
    "warning-50": {
      light: `rgb(${yellow_100} / 50%)`,
      dark: `rgb(${yellow_100} / 60%)`,
    },
    "warning-20": {
      light: `rgb(${yellow_100} / 20%)`,
      dark: `rgb(${yellow_100} / 30%)`,
    },
    "warning-10": {
      light: `rgb(${yellow_100} / 10%)`,
      dark: `rgb(${yellow_100} / 20%)`,
    },
    "critical-fill": {
      light: `rgb(${red_100})`,
      dark: `rgb(${red_100})`,
    },
    "critical-text": {
      light: `rgb(${text_critical})`,
      dark: `rgb(${red_100})`,
    },
    "critical-80": {
      light: `rgb(${red_100} / 80%)`,
      dark: `rgb(${red_100} / 90%)`,
    },
    "critical-50": {
      light: `rgb(${red_100} / 50%)`,
      dark: `rgb(${red_100} / 60%)`,
    },
    "critical-20": {
      light: `rgb(${red_100} / 20%)`,
      dark: `rgb(${red_100} / 30%)`,
    },
    "critical-10": {
      light: `rgb(${red_100} / 10%)`,
      dark: `rgb(${red_100} / 20%)`,
    },
  },
  grayscale: {
    "gray-50": {
      light: `rgb(${black_100} / 50%)`,
      dark: `rgb(${black_100} / 50%)`,
    },
    "gray-20": {
      light: `rgb(${black_100} / 20%)`,
      dark: `rgb(${black_100} / 20%)`,
    },
    "gray-10": {
      light: `rgb(${black_100} / 10%)`,
      dark: `rgb(${black_100} / 10%)`,
    },
    "gray-5": {
      light: `rgb(${black_100} / 5%)`,
      dark: `rgb(${black_100} / 5%)`,
    },
  },
  elevation: {
    50: {
      light: "0px 0px 1px 0px rgba(0, 0, 0, 0.37)",
      dark: "0px 0px 1px 0px rgba(255, 255, 255, 0.50)",
    },
    100: {
      light:
        "0px 4px 24px 0px rgba(0, 0, 0, 0.02), 0px 4px 8px 0px rgba(0, 0, 0, 0.02), 0px 2px 4px 0px rgba(0, 0, 0, 0.02), 0px 0px 1px 0px rgba(0, 0, 0, 0.20)",
      dark: "0px 4px 24px 0px rgba(0, 0, 0, 0.16), 0px 4px 8px 0px rgba(0, 0, 0, 0.16), 0px 2px 4px 0px rgba(0, 0, 0, 0.16), 0px 0px 1px 0px rgba(0, 0, 0, 0.16)",
    },
    200: {
      light:
        "0px 12px 32px 0px rgba(0, 0, 0, 0.04), 0px 8px 16px 0px rgba(0, 0, 0, 0.02), 0px 2px 4px 0px rgba(0, 0, 0, 0.04), 0px 0px 1px 0px rgba(0, 0, 0, 0.20)",
      dark: "0px 12px 32px 0px rgba(0, 0, 0, 0.24), 0px 8px 16px 0px rgba(0, 0, 0, 0.16), 0px 2px 4px 0px rgba(0, 0, 0, 0.08), 0px 0px 1px 0px rgba(0, 0, 0, 0.24)",
    },
    300: {
      light:
        "0px 16px 40px 0px rgba(0, 0, 0, 0.04), 0px 24px 32px 0px rgba(0, 0, 0, 0.02), 0px 4px 8px 0px rgba(0, 0, 0, 0.04), 0px 0px 1px 0px rgba(0, 0, 0, 0.30)",
      dark: "0px 16px 40px 0px rgba(0, 0, 0, 0.32), 0px 24px 32px 0px rgba(0, 0, 0, 0.16), 0px 4px 8px 0px rgba(0, 0, 0, 0.04), 0px 0px 1px 0px rgba(0, 0, 0, 0.3)",
    },
  },
};