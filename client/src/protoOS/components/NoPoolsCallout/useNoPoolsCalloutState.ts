import { getNoPoolsCalloutState } from "./utility";
import { useFeedMappingState } from "@/protoOS/features/feedMapping/feedMappingContext";

export function useNoPoolsCalloutState(pools: Parameters<typeof getNoPoolsCalloutState>[0], pathname: string) {
  const mapping = useFeedMappingState();
  return getNoPoolsCalloutState(pools, pathname, mapping?.runningConfirmed === true);
}
