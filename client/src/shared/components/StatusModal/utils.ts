import type { ComponentType } from "./types";

export const getComponentTitle = (type: ComponentType): string => {
  switch (type) {
    case "fan":
      return "Fan status";
    case "hashboard":
      return "Hashboard status";
    case "psu":
      return "PSU status";
    case "controlBoard":
      return "Control board status";
  }
};
