import Row from "components/Row";

import MacAddressInfo, { MacAddressInfoProps } from ".";

const InfoItemWrapper = ({ loading, value }: MacAddressInfoProps) => {
  return (
    <Row className="w-64 bg-core-primary-fill rounded-md p-3 pb-3" compact divider={false}>
      <MacAddressInfo loading={loading} value={value} />
    </Row>
  );
};

export const Default = () => {
  return <InfoItemWrapper value="42.08.59.58.84.c6" />;
};

export const Loading = () => {
  return <InfoItemWrapper loading />;
};

export const Error = () => {
  return <InfoItemWrapper />;
};

export default {
  title: "Navigation Sidebar/Info Items/Controller Mac Address Info",
};
