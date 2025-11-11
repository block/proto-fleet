/**
 * Format timestamp as "M/D/YY at h:mmA"
 *
 * @param timestamp - Unix timestamp in seconds
 * @returns Formatted date string or empty string if no timestamp
 */
export const formatTimestamp = (timestamp?: number): string => {
  if (!timestamp) return "";

  const date = new Date(timestamp * 1000);

  const month = date.getMonth() + 1;
  const day = date.getDate();
  const year = date.getFullYear().toString().slice(-2);

  let hours = date.getHours();
  const minutes = date.getMinutes().toString().padStart(2, "0");
  const ampm = hours >= 12 ? "PM" : "AM";
  hours = hours % 12 || 12;

  return `${month}/${day}/${year} at ${hours}:${minutes}${ampm}`;
};
