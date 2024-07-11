import { Pool } from "apiTypes";

import { positions } from "common/constants";

import { variants } from "components/Button";
import Card, { cardType } from "components/Card";
import Popover from "components/Popover";

import { Alert, Success } from "icons";

import PoolInfoRow from "./PoolInfoRow";
import { PoolInfo } from "./types";
import { getTexts } from "./utility";

interface PoolInfoPopoverProps {
  isConnected: boolean;
  onClickViewPools: () => void;
  poolInfo?: PoolInfo;
  poolsInfo?: Pool[];
}

const PoolInfoPopover = ({
  isConnected,
  onClickViewPools,
  poolInfo,
  poolsInfo,
}: PoolInfoPopoverProps) => {
  const { title, subtitle, button, cardTitle } = getTexts({
    isConnected,
    index: poolInfo?.index,
    url: poolInfo?.url,
  });

  return (
    <Popover
      title={title}
      subtitle={subtitle}
      buttons={[
        {
          text: button,
          onClick: onClickViewPools,
          variant: variants.secondary,
        },
      ]}
      position={positions["bottom left"]}
      testId="pool-info-popover"
    >
      {poolInfo?.url && cardTitle && (
        <Card
          title={cardTitle}
          type={isConnected ? cardType.success : cardType.warning}
        >
          {isConnected ? (
            <PoolInfoRow
              index={poolInfo.index}
              url={poolInfo.url}
              suffixIcon={<Success className="text-intent-success-fill" />}
              hasDivider={!poolsInfo?.length}
            />
          ) : (
            <>
              {poolsInfo?.map((pool, index) => (
                <PoolInfoRow
                  key={pool.priority}
                  index={index}
                  url={pool.url}
                  suffixIcon={<Alert className="text-intent-critical-fill" />}
                  hasDivider={index < poolsInfo.length - 1}
                />
              ))}
            </>
          )}
        </Card>
      )}
    </Popover>
  );
};

export default PoolInfoPopover;
