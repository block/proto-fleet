import type { ModuleListItem } from "./moduleListModel";
import type { HashboardAction } from "@/protoOS/api/adapters/types";
import type { ContainerTopology } from "@/protoOS/store/contract";

export const moduleActions: Array<{ id: HashboardAction; label: string; description: string }> = [
  {
    id: "start",
    label: "Start mining",
    description: "Start mining on these hashboards. Container mining intent stays unchanged.",
  },
  {
    id: "stop",
    label: "Stop mining",
    description: "Stop mining on these hashboards. Container mining intent stays unchanged.",
  },
  { id: "identify", label: "Blink LEDs", description: "Blink the identification LEDs on these hashboards." },
  {
    id: "isolate",
    label: "Isolate",
    description: "Exclude these hashboards from container start/stop commands and automatic mining control.",
  },
  {
    id: "clearIsolation",
    label: "Remove isolation",
    description: "Return these hashboards to automatic control. They will follow the current container mining intent.",
  },
];

export interface ModuleActionTarget {
  moduleIds: string[];
  names: string[];
  identifiers: string[];
}

export const moduleActionTarget = (
  rows: ModuleListItem[],
  ids: string[],
  topology: ContainerTopology,
): ModuleActionTarget | undefined => {
  const selected = [...new Set(ids)].map((id) => rows.find((row) => row.id === id));
  if (!selected.length || selected.some((row) => !row?.module.hashboardIds.length)) return undefined;
  const identifiers: string[] = [];
  for (const row of selected) {
    for (const id of row!.module.hashboardIds) {
      const board = topology.hashboards.get(id);
      if (
        !board ||
        board.moduleId !== row!.module.id ||
        board.identity.kind === "unknown" ||
        !board.identity.value.trim()
      )
        return undefined;
      identifiers.push(board.identity.value);
    }
  }
  return {
    moduleIds: selected.map((row) => row!.id),
    names: selected.map((row) => row!.name),
    identifiers: [...new Set(identifiers)].sort(),
  };
};
