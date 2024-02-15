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
      },
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
        primary: "rgb(var(--black-100) / <alpha-value>)",
        "primary-fill": "rgb(var(--black-100))",
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
      "emphasis-200": [
        "0.75rem", // 12px
        { lineHeight: "1.25rem", fontWeight: "500" },
      ],
    },
  },
  plugins: [],
};
