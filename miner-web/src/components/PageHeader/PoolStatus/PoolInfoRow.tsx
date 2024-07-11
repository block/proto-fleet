import { ReactNode } from "react";

import Header from "components/Header";
import Row from "components/Row";

interface PoolInfoRowProps {
  hasDivider?: boolean;
  index?: number;
  suffixIcon?: ReactNode;
  url?: string;
}

const PoolInfoRow = ({ hasDivider, index, suffixIcon, url }: PoolInfoRowProps) => {
  return (
    <Row suffixIcon={suffixIcon} divider={hasDivider}>
      <Header
        title={`${index === 0 ? "Default Pool" : `Backup Pool #${index}`}`}
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
