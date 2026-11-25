import { createContext, useContext } from "react";
import type { EquipmentAssignment } from "@/protoFleet/api/generated/poolconfigurations/v1/poolconfigurations_pb";

type AssignmentContext = {
  byIdentifier: Map<string, EquipmentAssignment>;
  byId: Map<string, EquipmentAssignment>;
  boards?: EquipmentAssignment[];
  readable: boolean;
  refresh: () => void;
  loadedAt?: number;
  loading: boolean;
  error: string | null;
};
export const PoolAssignmentsContext = createContext<AssignmentContext | null>(null);
export const useSavedPoolAssignments = () => useContext(PoolAssignmentsContext);
