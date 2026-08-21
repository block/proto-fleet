import type { ActiveAlertGroup } from "@/protoFleet/features/alerts/types";

// A rule title names the one thing it fires on ("Curtailment Source Unreachable"), which reads wrong once
// several are firing at once. Only the built-in device-less rules roll up more than one instance — user rules
// fire per miner and are counted as miners — so the set that needs a plural is closed and named here.
const pluralTitles: Record<string, string> = {
  "Curtailment Source Unreachable": "Curtailment Sources Unreachable",
};

// The rollup row's heading. The group's own alert_name is the display title, already rewritten where a retired
// rule maps to a new name, so the plural is keyed off that rather than the stored name the server filters on.
export const activeGroupTitle = (group: ActiveAlertGroup) => {
  if (group.device_count === 0 && group.alert_count > 1) {
    return pluralTitles[group.alert_name] ?? group.alert_name;
  }
  return group.alert_name;
};
