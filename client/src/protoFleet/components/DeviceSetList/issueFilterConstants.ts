import { useCallback, useRef } from "react";

import { ComponentType } from "@/protoFleet/api/generated/errors/v1/errors_pb";
import { componentIssues } from "@/protoFleet/features/fleetManagement/components/MinerList/constants";

export const issueOptions = [
  { id: componentIssues.controlBoard, label: "Control Board" },
  { id: componentIssues.fans, label: "Fan" },
  { id: componentIssues.hashBoards, label: "Hash Board" },
  { id: componentIssues.psu, label: "PSU" },
];

export const ISSUE_TO_COMPONENT_TYPE: Record<string, ComponentType> = {
  [componentIssues.controlBoard]: ComponentType.CONTROL_BOARD,
  [componentIssues.fans]: ComponentType.FAN,
  [componentIssues.hashBoards]: ComponentType.HASH_BOARD,
  [componentIssues.psu]: ComponentType.PSU,
};

export function useIssueFilter() {
  const selectedIssuesRef = useRef<string[]>([]);

  const getErrorComponentTypes = useCallback((): number[] => {
    return selectedIssuesRef.current
      .map((issue) => ISSUE_TO_COMPONENT_TYPE[issue])
      .filter((ct): ct is ComponentType => ct !== undefined);
  }, []);

  return { selectedIssuesRef, getErrorComponentTypes };
}
