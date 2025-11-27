export const components = ["all", "fans", "hashboards", "psus", "controlBoard"] as const;

export const componentLabels: Record<(typeof components)[number], string> = {
  all: "All",
  fans: "Fans",
  hashboards: "Hashboards",
  psus: "PSUs",
  controlBoard: "Control Board",
};
