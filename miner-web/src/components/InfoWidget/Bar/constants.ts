import { Colors } from "./types";

export const chartHeight = 64;

export const colors: Colors = {
  blue: { bg: "#2690C7", gradient: "#00A4FB", id: "blueGradient" },
  green: { bg: "#38A600", gradient: "#90C300", id: "greenGradient" },
  orange: { bg: "#FD8A00", gradient: "#FD8A00", id: "orangeGradient" },
  redOrange: { bg: "#FF5B00", gradient: "#FF5B00", id: "redOrangeGradient" },
  red: { bg: "#FA2B37", gradient: "#FA2B37", id: "redGradient" },
} as const;
