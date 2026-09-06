export const MINER_COMPONENTS = ["Fan", "Hashboard", "PSU", "Control Board"].map((value) => ({ value, label: value }));
export const INFRA_COMPONENTS = ["Network", "Electrical", "HVAC", "Cleaning", "Building"].map((value) => ({
  value,
  label: value,
}));

export const COMPONENT_FILTER_OPTIONS = [
  { value: "", label: "All components" },
  ...MINER_COMPONENTS,
  ...INFRA_COMPONENTS,
];
