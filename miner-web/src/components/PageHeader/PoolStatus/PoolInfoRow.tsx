import { ReactNode } from "react";

import Header from "components/Header";
import Row from "components/Row";

interface PoolInfoRowProps {
  hasDivider?: boolean;
  priority?: number;
  suffixIcon?: ReactNode;
  url?: string;
}

const PoolInfoRow = ({ hasDivider, priority, suffixIcon, url }: PoolInfoRowProps) => {
  return (
    <Row suffixIcon={suffixIcon} divider={hasDivider}>
      <Header
        title={`${priority === 0 ? "Default Pool" : `Backup Pool #${priority}`}`}
        titleSize="text-emphasis-300"
        subtitle={url}
        subtitleSize="text-200"
        className="!bg-transparent"
        compact
      />
    </Row>
  );
};

export default PoolInfoRow;
