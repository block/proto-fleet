import { useNetworkInfo } from "@/protoFleet/api/useNetworkInfo";
import SettingsPageHeader from "@/protoFleet/features/settings/components/SettingsPageHeader";
import Header from "@/shared/components/Header";
import Row from "@/shared/components/Row";
import SkeletonBar from "@/shared/components/SkeletonBar";

const SkeletonLoader = <SkeletonBar className="h-[22px] w-24" />;
const NETWORK_PAGE_DESCRIPTION = "View network details for the selected fleet.";

const Network = () => {
  const { data: networkInfo } = useNetworkInfo();

  return (
    <>
      <div className="flex flex-col gap-6">
        <SettingsPageHeader title="Network" description={NETWORK_PAGE_DESCRIPTION} />
        <div className="flex flex-col gap-4">
          <div className="flex flex-col gap-4 rounded-xl border border-border-5 p-6">
            <Header title="Network details" titleSize="text-heading-200" />
            <div>
              <Row className="flex justify-between" divider>
                <div className="text-300">Subnet mask</div>
                <div className="text-300">{networkInfo?.subnet ?? SkeletonLoader}</div>
              </Row>
              <Row className="flex justify-between" divider={false}>
                <div className="text-300">Gateway</div>
                <div className="text-300">{networkInfo?.gateway ?? SkeletonLoader}</div>
              </Row>
            </div>
          </div>
        </div>
      </div>
    </>
  );
};

export default Network;
