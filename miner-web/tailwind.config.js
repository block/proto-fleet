/** @type {import('tailwindcss').Config} */
export default {
  content: ["./index.html", "./src/**/*.{js,ts,jsx,tsx}"],
  darkMode: ["selector", '[data-theme="dark"]'],
  theme: {
    extend: {
      keyframes: {
        shimmer: {
          "100%": {
            transform: "translateX(100%)",
          },
        },
        "fade-in": {
          "0%": {
            opacity: "0",
          },
          "100%": {
            opacity: "1",
          },
        },
        "fade-out": {
          "0%": {
            opacity: "1",
          },
          "100%": {
            opacity: "0",
          },
        },
        "slide-down": {
          "100%": {
            transform: "translateY(24px) scale(0.98)",
          },
        },
        "slide-up": {
          "0%": {
            transform: "translateY(24px) scale(0.98)",
          },
          "100%": {
            transform: "translateY(0) scale(1)",
          },
        },
        "slide-right-nav": {
          "0%": {
            transform: "translateX(-240px)",
          },
          "100%": {
            transform: "translateX(0)",
          },
        },
        "slide-left-nav": {
          "100%": {
            transform: "translateX(-240px)",
          },
        },
      },
      transitionTimingFunction: {
        gentle: "cubic-bezier(0.47, 0, 0.23, 1.38)",
      },
      animation: {
        "sliding-down": "slide-down .3s theme('transitionTimingFunction.gentle')",
        "sliding-up": "slide-up .3s theme('transitionTimingFunction.gentle')",
      },
    },
    boxShadow: {
      none: "0 0 #0000;",
      50: "var(--elevation-50)",
      100: "var(--elevation-100)",
      200: "var(--elevation-200)",
      300: "var(--elevation-300)",
    },
    colors: {
      transparent: "transparent",
      current: "currentColor",
      text: {
        "base-static": "var(--typography-base-static)",
        "base-contrast-static": "var(--typography-base-contrast-static)",
        primary: "var(--typography-primary)",
        "primary-70": "var(--typography-primary-70)",
        "primary-50": "var(--typography-primary-50)",
        "primary-30": "var(--typography-primary-30)",
        contrast: "var(--typography-contrast)",
        "contrast-70": "var(--typography-contrast-70)",
        emphasis: "var(--typography-emphasis)",
        accent: "var(--typography-accent)",
        success: "var(--typography-success)",
        warning: "var(--typography-warning)",
        critical: "var(--typography-critical)",
      },
      surface: {
        default: "var(--surface-default)",
        base: "var(--surface-base)",
        // in some instances we want the base color to have different opacity
        // to show some of the content behind the component
        // so define rgb here instead of in the variable
        "elevated-base": "rgb(var(--surface-elevated-base) / <alpha-value>)",
        20: "var(--surface-20)",
        10: "var(--surface-10)",
        5: "var(--surface-5)",
        overlay: "var(--surface-overlay)",
      },
      border: {
        primary: "var(--border-primary)",
        20: "var(--border-20)",
        10: "var(--border-10)",
        5: "var(--border-5)",
      },
      core: {
        primary: {
          fill: "var(--core-primary-fill)",
          80: "var(--core-primary-80)",
          50: "var(--core-primary-50)",
          20: "var(--core-primary-20)",
          10: "var(--core-primary-10)",
          5: "var(--core-primary-5)",
        },
        accent: {
          fill: "var(--core-accent-fill)",
          text: "var(--core-accent-text)",
          80: "var(--core-accent-80)",
          50: "var(--core-accent-50)",
          20: "var(--core-accent-20)",
          10: "var(--core-accent-10)",
        },
        indigo: {
          fill: "var(--core-indigo-fill)",
          text: "var(--core-indigo-text)",
          80: "var(--core-indigo-80)",
          50: "var(--core-indigo-50)",
          20: "var(--core-indigo-20)",
          10: "var(--core-indigo-10)",
        },
      },
      intent: {
        info: {
          fill: "var(--intent-info-fill)",
          text: "var(--intent-info-text)",
          80: "var(--intent-info-80)",
          50: "var(--intent-info-50)",
          20: "var(--intent-info-20)",
          10: "var(--intent-info-10)",
        },
        success: {
          fill: "var(--intent-success-fill)",
          text: "var(--intent-success-text)",
          80: "var(--intent-success-80)",
          50: "var(--intent-success-50)",
          20: "var(--intent-success-20)",
          10: "var(--intent-success-10)",
        },
        warning: {
          fill: "var(--intent-warning-fill)",
          text: "var(--intent-warning-text)",
          80: "var(--intent-warning-80)",
          50: "var(--intent-warning-50)",
          20: "var(--intent-warning-20)",
          10: "var(--intent-warning-10)",
        },
        critical: {
          fill: "var(--intent-critical-fill)",
          text: "var(--intent-critical-text)",
          80: "var(--intent-critical-80)",
          50: "var(--intent-critical-50)",
          20: "var(--intent-critical-20)",
          10: "var(--intent-critical-10)",
        },
      },
      grayscale: {
        gray: {
          50: "var(--grayscale-gray-50)",
          20: "var(--grayscale-gray-20)",
          10: "var(--grayscale-gray-10)",
          5: "var(--grayscale-gray-5)",
        },
      },
    },
    fontFamily: {
      body: ["'Inter'"],
      mono: ["'JetBrains Mono'"],
    },
    fontSize: {
      "heading-300": [
        "1.75rem", // 28px
        { lineHeight: "2.5rem", fontWeight: "500", letterSpacing: "-0.07rem" },
      ],
      "heading-200": [
        "1.25rem", // 20px
        {
          lineHeight: "1.75rem",
          fontWeight: "500",
          letterSpacing: "-0.025rem",
        },
      ],
      "heading-100": [
        "1rem", // 16px
        { lineHeight: "1.5rem", fontWeight: "500", letterSpacing: "-0.02rem" },
      ],
      "heading-50": [
        "0.75rem", // 12px
        { lineHeight: "1.25rem", fontWeight: "500", letterSpacing: "-0.008rem" },
      ],
      400: [
        "1rem", // 16px
        { lineHeight: "1.5rem", fontWeight: "400" },
      ],
      300: [
        "0.875rem", // 14px
        { lineHeight: "1.375rem", fontWeight: "400" },
      ],
      200: [
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
      "emphasis-200": [
        "0.75rem", // 12px
        { lineHeight: "1.25rem", fontWeight: "500" },
      ],
      "mono-text-50": [
        "0.75rem", // 12px
        { lineHeight: "1rem", fontWeight: "500", letterSpacing: "-0.015rem" },
      ],
    },
    screens: {
      phone: { max: "631px" },
      tablet: { min: "632px", max: "959px" },
      laptop: { min: "960px", max: "1279px" },
      desktop: "1280px",
    },
  },
  plugins: [],
};
