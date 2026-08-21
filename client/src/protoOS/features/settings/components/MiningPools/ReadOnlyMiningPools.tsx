import { useMemo } from "react";

import ContentHeader from "@/shared/components/ContentHeader";
import PoolRow from "@/shared/components/MiningPools/PoolRow";
import { PoolIndex, PoolInfo } from "@/shared/components/MiningPools/types";
import { isValidPool } from "@/shared/components/MiningPools/utility";
import ProgressCircular from "@/shared/components/ProgressCircular";
import Row from "@/shared/components/Row";

interface ReadOnlyMiningPoolsProps {
  pools: PoolInfo[];
  loading?: boolean;
}

/**
 * Read-only pool list for the embedded (fleet-hosted) miner view. Pool edits are
 * blocked at the proxy and go through Fleet's audited UpdateMiningPools flow, but
 * operators still need to see which pools the miner is currently running — so
 * this shows the configuration without any edit/add/delete/reorder affordances.
 */
const ReadOnlyMiningPools = ({ pools, loading = false }: ReadOnlyMiningPoolsProps) => {
  const configuredPools = useMemo(
    () => pools.map((pool, index) => ({ pool, index: index as PoolIndex })).filter(({ pool }) => isValidPool(pool)),
    [pools],
  );

  if (loading) {
    return <ProgressCircular indeterminate />;
  }

  return (
    <>
      <ContentHeader
        title="Pools"
        subtitle="Pools are managed from Fleet. This is a read-only view of the miner's current configuration."
        testId="mining-pool-title"
      />
      {configuredPools.length === 0 ? (
        <Row className="text-text-primary-70" testId="read-only-pools-empty">
          No pools configured.
        </Row>
      ) : (
        configuredPools.map(({ pool, index }, position) => (
          <PoolRow
            key={`${pool.url}-${pool.username}-${index}`}
            poolIndex={index}
            pools={pools}
            priorityNumber={position + 1}
            subtitleExtra={pool.username || undefined}
            testId={`read-only-pool-${index}`}
            readOnly
          />
        ))
      )}
    </>
  );
};

export default ReadOnlyMiningPools;
