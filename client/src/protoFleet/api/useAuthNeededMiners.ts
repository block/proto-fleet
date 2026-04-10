import useFleet from "./useFleet";
import { MinerStateSnapshot, PairingStatus } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { MinerListFilter } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";

type UseAuthNeededMinersOptions = {
  pageSize?: number;
  filter?: MinerListFilter;
  enabled?: boolean;
};

type UseAuthNeededMinersReturn = {
  /** Array of miner device identifiers */
  minerIds: string[];
  /** Map of miner device identifier to miner state snapshot (only for local scope) */
  miners: Record<string, MinerStateSnapshot>;
  /** Total number of miners matching the filter */
  totalMiners: number;
  /** Whether there are more miners to load */
  hasMore: boolean;
  /** Whether the hook is currently loading data */
  isLoading: boolean;
  /** Whether the initial load has completed */
  hasInitialLoadCompleted: boolean;
  /** Load the next page of miners */
  loadMore: () => void;
  /** Refetch the miner list from the beginning */
  refetch: () => void;
  /** Available models for filter dropdown */
  availableModels: string[];
};

/**
 * Hook for fetching miners that require authentication credentials.
 * This is a convenience wrapper around useFleet that filters for devices
 * with AUTHENTICATION_NEEDED pairing status.
 *
 * These are devices that have been discovered and require user credentials
 * to complete the pairing process.
 *
 * Uses local scope to avoid conflicting with the main fleet view's global state.
 * This allows CompleteSetup and AuthenticateMiners to fetch auth-needed miners
 * without affecting the MinerList component's data.
 *
 * @param options - Configuration options for the hook
 * @param options.pageSize - Number of devices to fetch per page (default: 100)
 * @param options.filter - Optional filter to apply to the auth-needed miners (e.g., status, tags, etc.)
 * @returns Object containing miner data and pagination controls
 *
 * @example
 * ```tsx
 * const {
 *   minerIds,
 *   miners,
 *   totalMiners,
 *   hasMore,
 *   isLoading,
 *   loadMore
 * } = useAuthNeededMiners({ pageSize: 50 });
 *
 * // Load more miners when user scrolls
 * if (hasMore && !isLoading) {
 *   loadMore();
 * }
 * ```
 */
const useAuthNeededMiners = (options: UseAuthNeededMinersOptions = {}): UseAuthNeededMinersReturn => {
  const { pageSize = 100, filter, enabled = true } = options;

  return useFleet({
    enabled,
    pageSize,
    filter,
    pairingStatuses: [PairingStatus.AUTHENTICATION_NEEDED],
  }) as UseAuthNeededMinersReturn;
};

export default useAuthNeededMiners;
