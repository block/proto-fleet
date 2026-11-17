import { createContext, useContext } from "react";
import type { useFeedMapping } from "./useFeedMapping";

export type FeedMappingState = ReturnType<typeof useFeedMapping>;
export const FeedMappingContext = createContext<FeedMappingState | undefined>(undefined);
export const useFeedMappingState = () => useContext(FeedMappingContext);
