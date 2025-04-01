import { Fragment } from "react";
import { MiningPool } from "../types";
import Checkbox from "@/shared/components/Checkbox";
import Header from "@/shared/components/Header";
import Radio from "@/shared/components/Radio";
import Row from "@/shared/components/Row";
import { SelectType, selectTypes } from "@/shared/constants";

interface MiningPoolsListProps {
  title: string;
  subtitle: string;
  availablePools: MiningPool[];
  selectType: SelectType;
  selectedPools: string[];
  onSelect: (poolUrl: string, selected: boolean) => void;
  createNewLabel: string;
}

const PoolsList = ({
  title,
  subtitle,
  availablePools,
  selectType,
  selectedPools,
  onSelect,
  createNewLabel,
}: MiningPoolsListProps) => {
  return (
    <>
      <Header className="mt-6" inline title={title} description={subtitle} />
      <div className="mt-3">
        <div className="grid grid-cols-2">
          <Row className="text-emphasis-300 text-text-primary">Pool URL</Row>
          <Row className="text-emphasis-300 text-text-primary">Username</Row>
          {availablePools.map((pool) => (
            <Fragment key={pool.poolUrl}>
              <Row>{pool.poolUrl}</Row>
              <Row>
                <div className="flex justify-between">
                  <div>{pool.username}</div>
                  {selectType === selectTypes.radio && (
                    <Radio
                      selected={selectedPools.includes(pool.poolUrl)}
                      onChange={(e) => onSelect(pool.poolUrl, e.target.checked)}
                    />
                  )}
                  {selectType === selectTypes.checkbox && (
                    <Checkbox
                      checked={selectedPools.includes(pool.poolUrl)}
                      onChange={(e) => onSelect(pool.poolUrl, e.target.checked)}
                    />
                  )}
                </div>
              </Row>
            </Fragment>
          ))}
          <div className="col-span-2">
            <Row className="text-text-emphasis">{createNewLabel}</Row>
          </div>
        </div>
      </div>
    </>
  );
};

export default PoolsList;
