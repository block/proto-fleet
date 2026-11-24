import type { PoolAssignmentTarget } from "@/protoOS/api/adapters/poolAssignment";
import { asModuleId, type ContainerTopology } from "@/protoOS/store/contract";

export function modulePoolTarget(topology: ContainerTopology, ids: string[]): PoolAssignmentTarget | undefined {
  const modules: string[][] = [];
  for (const id of [...new Set(ids)]) {
    const module = topology.modules.get(asModuleId(id));
    if (!module?.hashboardIds.length) return undefined;
    const macs: string[] = [];
    for (const id of module.hashboardIds) {
      const board = topology.hashboards.get(id);
      if (!board || board.moduleId !== module.id || board.identity.kind !== "mac" || !board.identity.value.trim())
        return undefined;
      macs.push(board.identity.value.toLowerCase());
    }
    modules.push(macs.sort());
  }
  const macs = [...new Set(modules.flat())].sort();
  return macs.length ? { macs, modules } : undefined;
}
