import { ResponsiveValue } from "./types";

export const formatTime = (timestamp: number, alwaysShowMinutes = false): string => {
  const date = new Date(timestamp);
  const hours = date.getHours();
  const minutes = date.getMinutes();
  const ampm = hours >= 12 ? "p" : "a";
  const displayHours = hours % 12 || 12;

  if (minutes === 0 && !alwaysShowMinutes) {
    return `${displayHours}${ampm}`;
  }

  return `${displayHours}:${minutes.toString().padStart(2, "0")}${ampm}`;
};

export const formatTimeRange = (startTimestamp: number, endTimestamp: number): string => {
  const startDate = new Date(startTimestamp);
  const endDate = new Date(endTimestamp);
  const alwaysShowMinutes = startDate.getMinutes() !== 0 || endDate.getMinutes() !== 0;
  const start = formatTime(startTimestamp, alwaysShowMinutes);
  const end = formatTime(endTimestamp, alwaysShowMinutes);
  return `${start} - ${end}`;
};

export const formatDate = (timestamp: number): string => {
  const date = new Date(timestamp);
  return `${date.getMonth() + 1}/${date.getDate()}`;
};

// Helper function to get the responsive value based on current viewport
export const getResponsiveValue = <T>(
  value: T | ResponsiveValue<T> | undefined,
  defaultValue: T,
  viewport: { isPhone: boolean; isTablet: boolean; isLaptop: boolean; isDesktop: boolean },
): T => {
  if (value === undefined) return defaultValue;

  // If it's a plain value, return it
  if (typeof value !== "object" || value === null) return value;

  // If it's a responsive value object
  const responsiveValue = value as ResponsiveValue<T>;

  if (viewport.isPhone && responsiveValue.phone !== undefined) return responsiveValue.phone;
  if (viewport.isTablet && responsiveValue.tablet !== undefined) return responsiveValue.tablet;
  if (viewport.isLaptop && responsiveValue.laptop !== undefined) return responsiveValue.laptop;
  if (viewport.isDesktop && responsiveValue.desktop !== undefined) return responsiveValue.desktop;

  // Fall back to any defined value in order of preference
  if (responsiveValue.desktop !== undefined) return responsiveValue.desktop;
  if (responsiveValue.laptop !== undefined) return responsiveValue.laptop;
  if (responsiveValue.tablet !== undefined) return responsiveValue.tablet;
  if (responsiveValue.phone !== undefined) return responsiveValue.phone;
  return defaultValue;
};
