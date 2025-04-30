import { useHashboards, useSystemInfo } from "@/protoOS/api";
import {
  C1Chip,
  ControlBoard,
  HashboardIndicator,
} from "@/shared/assets/icons";
import ProgressCircular from "@/shared/components/ProgressCircular";
import Row from "@/shared/components/Row";
import SkeletonBar from "@/shared/components/SkeletonBar";

const Hardware = () => {
  const { data: hashboards } = useHashboards();
  const { data: systemInfo } = useSystemInfo({ poll: false });

  // TODO: still need to figure out what exactly this is and what API to use
  const ChipType = "MC2";

  return (
    <>
      <h2 className="mb-10 text-heading-300">Hardware</h2>
      <div className="mb-10">
        <h3 className="mb-2 text-heading-100">Chip</h3>
        <Row className="flex">
          <h4 className="flex w-68 gap-4 text-emphasis-300">
            <C1Chip />
            Type
          </h4>
          <div className="text-300">{ChipType}</div>
        </Row>
      </div>
      <div className="mb-10">
        <h3 className="mb-2 text-heading-100">Control Board</h3>
        <Row className="flex">
          <h4 className="flex w-68 items-center gap-4 text-emphasis-300">
            <ControlBoard />
            Type
          </h4>
          <div className="text-300">
            {systemInfo?.board ? (
              systemInfo.board
            ) : (
              <SkeletonBar className="w-20" />
            )}
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
              <h4 className="text-emphasis-300">Serial Number</h4>
            </Row>
            {hashboards.map((hashboard, index) => (
              <Row key={index} className="flex" attributes={{ role: "row" }}>
                <div className="w-22 text-300">
                  <HashboardIndicator
                    activeHashboard={index}
                    totalHashboards={hashboards.length}
                  />
                </div>
                <div className="w-46 text-300">Hashboard {index + 1}</div>
                <div className="text-300">{hashboard.hb_sn}</div>
              </Row>
            ))}
          </>
        ) : (
          <div className="flex justify-center">
            <ProgressCircular className="my-5" indeterminate />
          </div>
        )}
      </div>
    </>
  );
};

export default Hardware;
