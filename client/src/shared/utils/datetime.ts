import { padLeft } from "@/shared/utils/stringUtils";

export const getDateFromEpoch = (epoch?: number) => {
  if (!epoch) return new Date();
  const seconds = epoch.toString().length === 10;
  return new Date(seconds ? epoch * 1000 : epoch);
};

const getHoursFromEpoch = (epoch: number) => {
  return padLeft(getDateFromEpoch(epoch).getHours(), 2);
};

export const getMinutesFromEpoch = (epoch: number) => {
  return padLeft(getDateFromEpoch(epoch).getMinutes(), 2);
};

const getSecondsFromEpoch = (epoch: number) => {
  return padLeft(getDateFromEpoch(epoch).getSeconds(), 2);
};

export const getTimeFromEpoch = (epoch?: number) => {
  if (!epoch) return "";
  return `${getHoursFromEpoch(epoch)}:${getMinutesFromEpoch(epoch)}:${getSecondsFromEpoch(epoch)}`;
};

export const getRelativeTimeFromEpoch = (epoch?: number) => {
  if (!epoch) return "";
  const now = Date.now();
  const diff = now - epoch;

  const seconds = Math.floor(diff / 1000);
  if (seconds < 60) return "Just now";

  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m ago`;

  const hours = Math.floor(minutes / 60);
  if (hours < 24) {
    const remainingMinutes = minutes % 60;
    if (hours === 1) return remainingMinutes !== 0 ? `${hours}h${remainingMinutes}m ago` : `${hours}h ago`;
    return `${hours}hrs ago`;
  }

  const days = Math.floor(hours / 24);
  return `${days}d ago`;
};

export const getShortYearFromEpoch = (epoch?: number) => {
  if (!epoch) return "";
  return getDateFromEpoch(epoch).getFullYear().toString().slice(-2);
};

export const getMonthFromEpoch = (epoch?: number) => {
  if (!epoch) return "";
  return getDateFromEpoch(epoch).getMonth() + 1;
};

export const getDayFromEpoch = (epoch?: number) => {
  if (!epoch) return "";
  return getDateFromEpoch(epoch).getDate();
};
