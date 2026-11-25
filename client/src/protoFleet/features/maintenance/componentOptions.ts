import type { TicketCategoryValue } from "./types";

const option = (value: string) => ({ value, label: value });
const UNKNOWN_COMPONENT = option("Unknown");
const OTHER_COMPONENT = option("Other");
const MINER_NAMED_COMPONENTS = ["Fan", "Hashboard", "PSU", "Control Board"].map(option);
const INFRA_NAMED_COMPONENTS = ["Network", "Electrical", "HVAC", "Cleaning", "Building"].map(option);
export const MINER_COMPONENTS = [UNKNOWN_COMPONENT, ...MINER_NAMED_COMPONENTS, OTHER_COMPONENT];
export const INFRA_COMPONENTS = [UNKNOWN_COMPONENT, ...INFRA_NAMED_COMPONENTS, OTHER_COMPONENT];
const OTHER_COMPONENTS = [UNKNOWN_COMPONENT, OTHER_COMPONENT];

export const getComponentOptions = (category: TicketCategoryValue, current?: string) => {
  const choices =
    category === "infrastructure"
      ? INFRA_COMPONENTS
      : category === "miner" || category === "module" || category === "hashboard"
        ? MINER_COMPONENTS
        : OTHER_COMPONENTS;
  return current && !choices.some((choice) => choice.value === current) ? [...choices, option(current)] : choices;
};

export const COMPONENT_FILTER_OPTIONS = [
  { value: "", label: "All components" },
  UNKNOWN_COMPONENT,
  ...MINER_NAMED_COMPONENTS,
  ...INFRA_NAMED_COMPONENTS,
  OTHER_COMPONENT,
];
