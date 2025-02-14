import { useMemo } from "react";

import { BackupPoolIndex, PoolInfo } from "./types";
import Button, { sizes, variants } from "@/shared/components/Button";
import Row from "@/shared/components/Row";

interface BackupPoolRowProps {
  backupPoolIndex: BackupPoolIndex;
  onClick: () => void;
  pools: PoolInfo[];
  testId?: string;
}

const BackupPoolRow = ({
  backupPoolIndex,
  onClick,
  pools,
  testId,
}: BackupPoolRowProps) => {
  const url = useMemo(
    () => pools[backupPoolIndex]?.url,
    [pools, backupPoolIndex]
  );

  return (
    <Row className="flex justify-between items-center">
      <div className="flex flex-col">
        <div>Backup pool #{backupPoolIndex}</div>
        {!!url && (
          <div
            className="text-200 text-text-primary-70"
            data-testid={`backup-pool-${backupPoolIndex}-saved-url`}
          >
            {url}
          </div>
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

export default BackupPoolRow;
