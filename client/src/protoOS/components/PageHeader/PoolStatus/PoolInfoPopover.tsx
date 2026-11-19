import type { ReactNode } from "react";
import ConnectedPoolsCard from "./ConnectedPoolsCard";
import PoolInfoRow from "./PoolInfoRow";
import { PoolInfo } from "./types";
import { getTexts } from "./utility";
import type { PoolInfo as NormalizedPoolInfo } from "@/protoOS/store/contract";
import { Alert } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import Card, { cardType } from "@/shared/components/Card";
import Popover from "@/shared/components/Popover";
import { positions } from "@/shared/constants";

type PoolStatusEntry = Pick<NormalizedPoolInfo, "status" | "url"> & Partial<Pick<NormalizedPoolInfo, "priority">>;

interface PoolInfoPopoverProps {
  assignmentSummary?: ReactNode;
  isConnected: boolean;
  onClickViewPools: () => void;
  poolInfo?: PoolInfo;
  poolsInfo?: PoolStatusEntry[];
  showPoolStatusLink: boolean;
}

const PoolInfoPopover = ({
  assignmentSummary,
  isConnected,
  onClickViewPools,
  poolInfo,
  poolsInfo,
  showPoolStatusLink,
}: PoolInfoPopoverProps) => {
  const { title, subtitle, button } = getTexts({
    isConnected,
    index: poolInfo?.index,
    url: poolInfo?.url,
    showPoolStatusLink,
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
      {assignmentSummary}
      {!showPoolStatusLink && poolInfo?.url && isConnected ? (
        <ConnectedPoolsCard pools={[{ id: poolInfo.index, priority: poolInfo.index, url: poolInfo.url }]} />
      ) : null}
      {!showPoolStatusLink && poolInfo?.url && !isConnected ? (
        <Card title="Not connected" type={cardType.warning}>
          {poolsInfo?.map((pool, index) => (
            <PoolInfoRow
              key={pool.priority}
              index={index}
              url={pool.url}
              suffixIcon={<Alert className="text-intent-critical-fill" />}
              hasDivider={index < poolsInfo.length - 1}
            />
          ))}
        </Card>
      ) : null}
    </Popover>
  );
};

export default PoolInfoPopover;
