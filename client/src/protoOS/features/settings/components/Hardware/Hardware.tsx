import { useCallback, useMemo } from "react";
import { HashboardsInfoHashboardsinfo, PsusInfoPsusinfo } from "apiTypes";
import { useHardware } from "@/protoOS/api/useHardware";
import { InternalAsicType } from "@/protoOS/features/settings/components/Hardware/constants";
import {
  FanIndicator,
  HashboardIndicator,
  PsuIndicator,
} from "@/shared/assets/icons";
import { DataNullState } from "@/shared/components/DataNullState";
import ProgressCircular from "@/shared/components/ProgressCircular";
import Row from "@/shared/components/Row";
import SkeletonBar from "@/shared/components/SkeletonBar";

const Hardware = () => {
  const {
    hashboardsInfo,
    controlBoardInfo,
    fansInfo,
    psusInfo,
    pending,
    error,
  } = useHardware();

  const sortedHashboards = useMemo(() => {
    return hashboardsInfo?.sort(
      (a, b) =>
        (a.slot || hashboardsInfo.length) - (b.slot || hashboardsInfo.length),
    );
  }, [hashboardsInfo]);
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

  if (pending) {
    return (
      <>
        <h2 className="mb-10 text-heading-300">Hardware</h2>
        <div className="flex justify-center">
          <ProgressCircular className="my-5" indeterminate />
        </div>
      </>
    );
  }

  if (error) {
    return (
      <>
        <h2 className="mb-10 text-heading-300">Hardware</h2>
        <DataNullState
          title="Could not load hardware details"
          description="Test your connection and try again."
        />
      </>
    );
  }

  return (
    <>
      <h2 className="mb-10 text-heading-300">Hardware</h2>
      <div className="mb-10">
        <h3 className="mb-2 text-heading-100">Control Board</h3>
        <Row className="flex" attributes={{ role: "row" }}>
          <h4 className="w-68 text-emphasis-300">Type</h4>
          <h4 className="w-91 text-emphasis-300">Serial number</h4>
        </Row>
        <Row className="flex items-center">
          <div className="w-68 text-300">
            {controlBoardInfo?.board_id
              ? `Control Board ${controlBoardInfo.board_id}`
              : skeletonBar}
          </div>
          <div className="w-91 text-300">
            {controlBoardInfo?.serial_number ?? skeletonBar}
          </div>
        </Row>
      </div>
      <div className="mb-10" role="table">
        <h3 className="mb-2 text-heading-100">Hashboards</h3>

        {hashboardsInfo?.length ? (
          <>
            <Row className="flex" attributes={{ role: "row" }}>
              <h4 className="w-22 text-emphasis-300">Position</h4>
              <h4 className="w-46 text-emphasis-300">Hashboard</h4>
              <h4 className="w-46 text-emphasis-300">Serial Number</h4>
            </Row>
            {sortedHashboards?.map((hashboard, index) => (
              <Row
                key={index}
                className="flex items-center"
                attributes={{ role: "row" }}
              >
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
              </Row>
            ))}
          </>
        ) : (
          <div className="flex justify-center">
            <p className="text-300">No hashboards found</p>
          </div>
        )}
      </div>
      <div className="mb-10">
        <h3 className="mb-2 text-heading-100">Fans</h3>
        <Row className="flex" attributes={{ role: "row" }}>
          <h4 className="w-22 text-emphasis-300">Position</h4>
          <h4 className="w-46 text-emphasis-300">Fan</h4>
          {/* <h4 className="w-46 text-emphasis-300">Serial number</h4> */}
        </Row>
        {fansInfo?.length ? (
          <>
            {fansInfo?.map((fan, idx) => (
              <Row
                className="flex items-center"
                key={fan.id}
                attributes={{ role: "row" }}
              >
                <div className="w-22 text-300">
                  <FanIndicator
                    fanPosition={(fan.id ?? idx) + 1} // id is 0-indexed, but we want to display it as 1-indexed
                    numFans={fansInfo.length}
                  />
                </div>
                <div className="w-46 text-300">{fan.name}</div>
                {/* <div className="w-46 text-300">{fan.fan_sn}</div> */}
              </Row>
            ))}
          </>
        ) : (
          <div className="flex justify-center">
            <p className="text-300">No fans found</p>
          </div>
        )}
      </div>
      <div className="mb-10">
        <h3 className="mb-2 text-heading-100">Power supply</h3>

        {psusInfo?.length ? (
          <>
            <Row className="flex" attributes={{ role: "row" }}>
              <h4 className="w-22 text-emphasis-300">Position</h4>
              <h4 className="w-46 text-emphasis-300">PSU</h4>
              <h4 className="w-46 text-emphasis-300">Serial number</h4>
            </Row>
            {psusInfo?.map((psu: PsusInfoPsusinfo) => (
              <Row
                className="flex items-center"
                key={psu.psu_sn}
                attributes={{ role: "row" }}
              >
                <div className="w-22 text-300">
                  <PsuIndicator
                    totalSlots={psusInfo.length}
                    slotPlacement={psu.slot}
                  />
                </div>
                <div className="w-46 text-300">{psu.model}</div>
                <div className="w-46 text-300">{psu.psu_sn}</div>
              </Row>
            ))}
          </>
        ) : (
          <div className="flex justify-center">
            <p className="text-300">No power supplies found</p>
          </div>
        )}
      </div>
    </>
  );
};

export default Hardware;
