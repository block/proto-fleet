import { Fleet, Graph, Home, Logs, Repair, Settings } from "@/shared/assets/icons";

export const navigationItems = {
  home: {
    route: "",
    icon: Home,
  },
  fleet: {
    route: "containers",
    icon: Fleet,
  },
  profitability: {
    route: "profitability",
    icon: Graph,
  },
  repairs: {
    route: "repairs",
    icon: Repair,
  },
  logs: {
    route: "logs",
    icon: Logs,
  },
  settings: {
    route: "settings/settings",
    icon: Settings,
  },
} as const;
