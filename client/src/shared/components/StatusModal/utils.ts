const msPerSecond = 1000;

/**
 * Format timestamp as "Reported on MM/DD/YYYY at H:MMam/pm"
 *
 * @param timestamp - Unix timestamp in seconds
 * @returns Formatted date string or undefined if no timestamp
 */
export const formatReportedTimestamp = (timestamp?: number): string | undefined => {
  if (!timestamp) return undefined;

  const date = new Date(timestamp * msPerSecond);

  const month = (date.getMonth() + 1).toString().padStart(2, "0");
  const day = date.getDate().toString().padStart(2, "0");
  const year = date.getFullYear();

  let hours = date.getHours();
  const minutes = date.getMinutes().toString().padStart(2, "0");
  const ampm = hours >= 12 ? "pm" : "am";
  hours = hours % 12 || 12;

  return `Reported on ${month}/${day}/${year} at ${hours}:${minutes}${ampm}`;
};
