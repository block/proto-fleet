import { useCallback, useMemo } from "react";
import { HashboardsInfoHashboardsinfo } from "apiTypes";
import { useHashboards, useSystemInfo } from "@/protoOS/api";
import {
  ExternalAsicType,
  InternalAsicType,
} from "@/protoOS/features/settings/components/Hardware/constants";
import { HashboardIndicator } from "@/shared/assets/icons";
import ProgressCircular from "@/shared/components/ProgressCircular";
import Row from "@/shared/components/Row";
import SkeletonBar from "@/shared/components/SkeletonBar";

const Hardware = () => {
  const { data: hashboards } = useHashboards();
  const { data: systemInfo } = useSystemInfo({ poll: false });

  const sortedHashboards = useMemo(() => {
    return hashboards?.sort(
      (a, b) => (a.slot || hashboards.length) - (b.slot || hashboards.length),
    );
  }, [hashboards]);
  const totalSlots = sortedHashboards?.length
    ? (sortedHashboards[sortedHashboards.length - 1]?.slot ?? 0)
    : 0;

  const skeletonBar = <SkeletonBar className="h-[22px] w-20" />;

  const getHashboardIdentifier = useCallback(
    (hashboardInfo: HashboardsInfoHashboardsinfo) => {
      let generation = 1;
      if (
        hashboardInfo.mining_asic === InternalAsicType.MC2 ||
        hashboardInfo.mining_asic === InternalAsicType.MC2Sim
      ) {
        generation = 2;
      }
      return `${hashboardInfo.mining_asic_count}C${generation}`;
    },
    [],
  );

  const getExternalAsicType = useCallback((internalAsicName?: string) => {
    if (!internalAsicName) return undefined;
    switch (internalAsicName) {
      case InternalAsicType.MC1:
      case InternalAsicType.BZM2:
        return ExternalAsicType.Chip1;
      case InternalAsicType.MC2:
        return ExternalAsicType.Chip2;
      case InternalAsicType.MC2Sim:
        return ExternalAsicType.Chip2Sim;
      case InternalAsicType.CpuSimulated:
        return ExternalAsicType.ChipSim;
    }
  }, []);

  // TODO get PSU serial number from API
  const psuSerialNumber = "PM-H132435034";

  return (
    <>
      <h2 className="mb-10 text-heading-300">Hardware</h2>
      <div className="mb-10">
        <h3 className="mb-2 text-heading-100">Control Board</h3>
        <Row className="flex" attributes={{ role: "row" }}>
          <h4 className="w-68 text-emphasis-300">Type</h4>
          <h4 className="w-91 text-emphasis-300">Serial number</h4>
        </Row>
        <Row className="flex">
          <div className="w-68 text-300">
            {systemInfo?.board ?? skeletonBar}
          </div>
          <div className="w-91 text-300">
            {systemInfo?.cb_sn ?? skeletonBar}
          </div>
        </Row>
      </div>
      <div className="mb-10" role="table">
        <h3 className="mb-2 text-heading-100">Hashboards</h3>

        {hashboards?.length ? (
          <>
            <Row className="flex" attributes={{ role: "row" }}>
              <h4 className="w-22 text-emphasis-300">Position</h4>
              <h4 className="w-46 text-emphasis-300">Hashboard</h4>
              <h4 className="w-46 text-emphasis-300">Serial Number</h4>
              <h4 className="w-46 text-emphasis-300">Chip</h4>
            </Row>
            {sortedHashboards?.map((hashboard, index) => (
              <Row key={index} className="flex" attributes={{ role: "row" }}>
                <div className="w-22 text-300">
                  <HashboardIndicator
                    activeHashboardSlot={hashboard.slot ?? index + 1}
                    totalHashboards={totalSlots}
                  />
                </div>
                <div className="w-46 text-300">
                  Hashboard {getHashboardIdentifier(hashboard)}
                </div>
                <div className="w-46 text-300">{hashboard.hb_sn}</div>
                <div className="w-46 text-300">
                  {getExternalAsicType(hashboard.mining_asic)}
                </div>
              </Row>
            ))}
          </>
        ) : (
          <div className="flex justify-center">
            <ProgressCircular className="my-5" indeterminate />
          </div>
        )}
      </div>
      <div className="mb-10">
        <h3 className="mb-2 text-heading-100">Power supply</h3>
        <Row className="flex" attributes={{ role: "row" }}>
          <h4 className="w-68 text-emphasis-300">PSU</h4>
          <h4 className="w-91 text-emphasis-300">Serial number</h4>
        </Row>
        <Row className="flex">
          <div className="w-68 text-300">Power supply</div>
          <div className="w-91 text-300">{psuSerialNumber ?? skeletonBar}</div>
        </Row>
      </div>
    </>
  );
};

export default Hardware;
