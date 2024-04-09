/** @type {import('tailwindcss').Config} */
export default {
  content: ["./index.html", "./src/**/*.{js,ts,jsx,tsx}"],
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
        "slide-right": {
          "0%": {
            transform: "translateX(-240px)",
          },
          "100%": {
            transform: "translateX(0)",
          },
        },
        "slide-left": {
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
        "sliding-right": "slide-right .3s ease-in-out",
        "sliding-left": "slide-left .3s ease-in-out",
      },
    },
    boxShadow: {
      50: "0px 0px 1px 0px rgba(0, 0, 0, 0.37)",
      100: "0px 0px 1px 0px rgba(0, 0, 0, 0.20), 0px 2px 4px 0px rgba(0, 0, 0, 0.02), 0px 4px 8px 0px rgba(0, 0, 0, 0.02), 0px 4px 24px 0px rgba(0, 0, 0, 0.02)",
      200: "0px 0px 1px 0px rgba(0, 0, 0, 0.20), 0px 2px 4px 0px rgba(0, 0, 0, 0.04), 0px 8px 16px 0px rgba(0, 0, 0, 0.02), 0px 12px 32px 0px rgba(0, 0, 0, 0.04)",
      300: "0px 0px 1px 0px rgba(0, 0, 0, 0.30), 0px 4px 8px 0px rgba(0, 0, 0, 0.04), 0px 24px 32px 0px rgba(0, 0, 0, 0.02), 0px 16px 40px 0px rgba(0, 0, 0, 0.04)",
    },
    colors: {
      transparent: "transparent",
      current: "currentColor",
      border: {
        primary: "rgb(var(--black-100) / <alpha-value>)",
      },
      core: {
        accent: {
          fill: "rgb(var(--orange-100) / <alpha-value>)",
          text: "rgb(var(--text-accent))",
        },
        indigo: "rgb(var(--indigo-100) / <alpha-value>)",
        primary: "rgb(var(--black-100) / <alpha-value>)",
        "primary-fill": "rgb(var(--black-100) / <alpha-value>)",
      },
      grayscale: {
        gray: "rgb(var(--black-100) / <alpha-value>)",
      },
      intent: {
        critical: {
          fill: "rgb(var(--red-100) / <alpha-value>)",
          text: "rgb(var(--text-critical))",
        },
        info: {
          fill: "rgb(var(--blue-100) / <alpha-value>)",
          text: "rgb(var(--text-info))",
        },
        success: {
          fill: "rgb(var(--green-100) / <alpha-value>)",
          text: "rgb(var(--text-success))",
        },
        warning: {
          fill: "rgb(var(--yellow-100) / <alpha-value>)",
          text: "rgb(var(--text-warning))",
        },
      },
      surface: {
        base: "rgb(var(--white-100))",
        default: "rgb(var(--white-100) / 2%)",
        overlay: "rgb(var(--black-100) / 5%)",
        5: "rgb(var(--gray-5))",
        10: "rgb(var(--gray-10))",
        20: "rgb(var(--gray-20))",
      },
      text: {
        accent: "rgb(var(--orange-100) / 80%)",
        contrast: "rgb(var(--white-100) / <alpha-value>)",
        critical: "rgb(var(--red-100))",
        emphasis: "rgb(var(--orange-100))",
        primary: "rgb(var(--black-100) / <alpha-value>)",
        success: "rgb(var(--green-100))",
        warning: "rgb(var(--yellow-100))",
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
