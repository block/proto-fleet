import HashboardInfo, { HashboardInfoProps } from ".";

const serials = [
  "1111111111111111111111",
  "2222222222222222222222",
  "3333333333333333333333",
];

const InfoItemWrapper = ({
  loading,
  hashboard_serials,
}: HashboardInfoProps) => {
  return (
    <div className="w-64">
      <HashboardInfo hashboard_serials={hashboard_serials} loading={loading} />
    </div>
  );
};

export const SingleSerial = () => {
  return <InfoItemWrapper hashboard_serials={[serials[0]]} />;
};

export const MultiSerial = () => {
  return <InfoItemWrapper hashboard_serials={serials} />;
};

export const Loading = () => {
  return <InfoItemWrapper loading />;
};

export const Error = () => {
  return <InfoItemWrapper hashboard_serials={[]} />;
};

export default {
  component: HashboardInfo,
  title: "Navigation Sidebar/Info Items/Hashboard Info",
};
