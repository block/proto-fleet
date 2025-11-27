import { useMemo } from "react";

import { PoolIndex, PoolInfo } from "./types";
import Button, { sizes, variants } from "@/shared/components/Button";
import Row from "@/shared/components/Row";

interface PoolRowProps {
  poolIndex: PoolIndex;
  title: string;
  onClick: () => void;
  pools: PoolInfo[];
  testId?: string;
}

const PoolRow = ({ poolIndex, title, onClick, pools, testId }: PoolRowProps) => {
  const url = useMemo(() => pools[poolIndex]?.url, [pools, poolIndex]);

  return (
    <Row className="flex items-center justify-between">
      <div className="flex flex-col">
        <div>{title}</div>
        {url ? (
          <div className="text-200 text-text-primary-70" data-testid={`pool-${poolIndex}-saved-url`}>
            {url}
          </div>
        ) : (
          <div className="text-200 text-text-primary-70">Not configured</div>
        )}
      </div>
      <Button
        variant={variants.secondary}
        size={sizes.compact}
        text={url ? "Edit" : "Add"}
        onClick={onClick}
        testId={testId}
      />
    </Row>
  );
};

export default PoolRow;
