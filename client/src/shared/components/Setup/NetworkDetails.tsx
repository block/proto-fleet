import { useMemo } from "react";
import Row from "@/shared/components/Row";
import SkeletonBar from "@/shared/components/SkeletonBar";
import { cidrToSubnetMask } from "@/shared/utils/network";

interface NetworkDetailsProps {
  gateway?: string;
  // Subnet mask or CIDR notation for the network
  subnet?: string;
}

const NetworkDetails = ({ gateway, subnet }: NetworkDetailsProps) => {
  const subnetMask = useMemo(() => {
    if (!subnet) return null;

    const convertedFromCIDR = cidrToSubnetMask(subnet);
    if (convertedFromCIDR === null) {
      // subnet is already a mask
      return subnet;
    }
    return convertedFromCIDR;
  }, [subnet]);

  const SkeletonLoader = <SkeletonBar className="h-[22px] w-24" />;

  return (
    <div className="rounded-xl bg-surface-5 px-6 py-3">
      <div className="w-full">
        <Row className="flex justify-between">
          <div className="text-emphasis-300">Network details</div>
        </Row>
      </div>

      <div className="w-full text-300">
        <Row className="flex justify-between">
          <div>Gateway</div>
          <div>{gateway ?? SkeletonLoader}</div>
        </Row>
        <Row divider={false} className="flex justify-between">
          <div>Subnet mask</div>
          <div>{subnetMask ?? SkeletonLoader}</div>
        </Row>
      </div>
    </div>
  );
};

export default NetworkDetails;
