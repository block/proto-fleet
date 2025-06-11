export const breakpoints = {
  phone: "phone",
  tablet: "tablet",
  laptop: "laptop",
  desktop: "desktop",
} as const;

export type Breakpoint = (typeof breakpoints)[keyof typeof breakpoints];
