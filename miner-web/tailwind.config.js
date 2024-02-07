/** @type {import('tailwindcss').Config} */
export default {
  content: ["./index.html", "./src/**/*.{js,ts,jsx,tsx}"],
  theme: {
    extend: {
      opacity: {
        4: ".04",
      },
      keyframes: {
        shimmer: {
          "100%": {
            transform: "translateX(100%)",
          },
        },
      },
    },
    colors: {
      transparent: "transparent",
      current: "currentColor",
      black: {
        80: "rgb(var(--black-80) / <alpha-value>)",
        100: "rgb(var(--black-100) / <alpha-value>)",
      },
      critical: {
        100: "rgb(var(--critical-100) / <alpha-value>)",
      },
      error: {
        100: "rgb(var(--error-100) / <alpha-value>)",
      },
      foreground: {
        10: "rgb(var(--foreground-10) / <alpha-value>)",
        20: "rgb(var(--foreground-20) / <alpha-value>)",
        30: "rgb(var(--foreground-30) / <alpha-value>)",
        60: "rgb(var(--foreground-60) / <alpha-value>)",
        80: "rgb(var(--foreground-80) / <alpha-value>)",
        100: "rgb(var(--foreground-100) / <alpha-value>)",
      },
      primary: {
        10: "rgb(var(--primary-10) / <alpha-value>)",
        50: "rgb(var(--primary-50) / <alpha-value>)",
        100: "rgb(var(--primary-100) / <alpha-value>)",
      },
      success: {
        100: "rgb(var(--success-100) / <alpha-value>)",
      },
      tinted: {
        10: "rgb(var(--tinted-10) / <alpha-value>)",
        20: "rgb(var(--tinted-20) / <alpha-value>)",
      },
      warning: {
        90: "rgb(var(--warning-90) / <alpha-value>)",
        100: "rgb(var(--warning-100) / <alpha-value>)",
      },
      white: {
        100: "rgb(var(--white-100) / <alpha-value>)",
      },
    },
    fontFamily: {
      body: ["'Inter'"],
      mono: ["'Fira Code'"],
    },
    fontSize: {
      "heading-300": [
        "1.75rem", // 28px
        { lineHeight: "2.5rem", fontWeight: "500", letterSpacing: "-0.07rem" },
      ],
      "heading-200": [
        "1.25rem", // 20px
        { lineHeight: "1.75rem", fontWeight: "500", letterSpacing: "-0.025rem" },
      ],
      "400": [
        "1rem", // 16px
        { lineHeight: "1.5rem", fontWeight: "400" },
      ],
      "300": [
        "0.875rem", // 14px
        { lineHeight: "1.375rem", fontWeight: "400" },
      ],
      "200": [
        "0.75rem", // 12px
        { lineHeight: "1.25rem", fontWeight: "400" },
      ],
      "emphasis-400": [
        "1rem", // 16px
        { lineHeight: "1.5rem", fontWeight: "500" },
      ],
      "emphasis-300": [
        "0.875rem", // 14px
        { lineHeight: "1.5rem", fontWeight: "500" },
      ],
      // TODO: BTCM-1223 - replace the following with the new typography
      "title-1": [
        "24px",
        { lineHeight: "32px", fontWeight: "600", letterSpacing: "-0.8px" },
      ],
      "body-default": [
        "15px",
        { lineHeight: "normal", fontWeight: "500", letterSpacing: "-0.3px" },
      ],
      "button": [
        "13px",
        { lineHeight: "normal", fontWeight: "500", letterSpacing: "-0.26px" },
      ],
      "body-regular": [
        "12px",
        { lineHeight: "normal", fontWeight: "400", letterSpacing: "-0.25px" },
      ],
    },
  },
  plugins: [],
};
