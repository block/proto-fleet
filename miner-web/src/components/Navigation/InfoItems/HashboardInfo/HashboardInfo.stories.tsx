import HashboardInfo, { HashboardInfoProps } from ".";

const serials = [
  "1111111111111111111111",
  "2222222222222222222222",
  "3333333333333333333333",
];

const InfoItemWrapper = ({
  loading,
  hashboardSerials,
}: HashboardInfoProps) => {
  return (
    <div className="w-64">
      <HashboardInfo hashboardSerials={hashboardSerials} loading={loading} />
    </div>
  );
};

export const SingleSerial = () => {
  return <InfoItemWrapper hashboardSerials={[serials[0]]} />;
};

export const MultiSerial = () => {
  return <InfoItemWrapper hashboardSerials={serials} />;
};

export const Loading = () => {
  return <InfoItemWrapper loading />;
};

export const Error = () => {
  return <InfoItemWrapper hashboardSerials={[]} />;
};

export default {
  title: "Navigation Sidebar/Info Items/Hashboard Info",
};
