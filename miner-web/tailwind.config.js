/** @type {import('tailwindcss').Config} */
export default {
  content: ["./index.html", "./src/**/*.{js,ts,jsx,tsx}"],
  theme: {
    extend: {
      opacity: {
        4: ".04",
      },
    },
    colors: {
      transparent: "transparent",
      current: "currentColor",
      black: {
        80: "rgb(var(--black-80) / <alpha-value>)",
        100: "rgb(var(--black-100) / <alpha-value>)",
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
        100: "rgb(var(--warning-100) / <alpha-value>)",
      },
      white: {
        100: "rgb(var(--white-100) / <alpha-value>)",
      },
    },
    fontFamily: {
      body: ["'Inter'"],
      "berkeley-mono-variable": ["'BerkeleyMonoVariable'"],
    },
    fontSize: {
      "title-1": [
        "1.25rem",
        { lineHeight: "1.875rem", fontWeight: "500", letterSpacing: "-0.4px" },
      ],
      "title-2": [
        "1.125rem",
        { lineHeight: "2rem", fontWeight: "600", letterSpacing: "-0.8px" },
      ],
      "body-default": [
        "0.938rem",
        { lineHeight: "normal", fontWeight: "500", letterSpacing: "-0.3px" },
      ],
      "body-regular": [
        "0.75rem",
        { lineHeight: "normal", fontWeight: "400", letterSpacing: "-0.25px" },
      ],
      "button": [
        "0.813rem",
        { lineHeight: "normal", fontWeight: "500", letterSpacing: "-0.26px" },
      ],
    },
    keyframes: {
      shimmer: {
        "100%": {
          transform: "translateX(100%)",
        },
      },
    },
  },
  plugins: [],
};
