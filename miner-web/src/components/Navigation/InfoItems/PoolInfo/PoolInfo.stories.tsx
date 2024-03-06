import Row from "components/Row";

import PoolInfo, { PoolProps } from ".";

const poolUrlV1 = "stratum+tcp://host.docker.internal:3333";
const poolUrlV2 =
  "stratum2+tcp://v2.stratum.braiins.com/u95GEReVMjK6k5YqiSFNqqTnKU4ypU2Wm8awa6tmbmDmk1bWt";

const InfoItemWrapper = ({ error, loading, status, url }: PoolProps) => {
  return (
    <Row className="w-64 bg-core-primary-fill rounded-md p-3 pb-3" compact divider={false}>
      <PoolInfo error={error} loading={loading} status={status} url={url} />
    </Row>
  );
};

export const StratumV1 = () => {
  return <InfoItemWrapper url={poolUrlV1} status="Alive" />;
};

export const StratumV2 = () => {
  return <InfoItemWrapper url={poolUrlV2} status="Alive" />;
};

export const Loading = () => {
  return <InfoItemWrapper loading />;
};

export const Error = () => {
  return <InfoItemWrapper error url={poolUrlV1} status="Dead" />;
};

export const Empty = () => {
  return <InfoItemWrapper error status="Dead" />;
};

export default {
  title: "Navigation Sidebar/Info Items/Pool Info",
};
