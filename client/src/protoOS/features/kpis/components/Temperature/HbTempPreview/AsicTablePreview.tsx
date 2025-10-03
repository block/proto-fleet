import AsicCell from "./AsicCell";
import { useAsicRowsByHbSn, useMinerHashboardAsics } from "@/protoOS/store";
import ProgressCircular from "@/shared/components/ProgressCircular";

interface AsicTablePreviewProps {
  hashboardSerial: string;
}

const AsicTablePreview = ({ hashboardSerial }: AsicTablePreviewProps) => {
  const asics = useMinerHashboardAsics(hashboardSerial);
  const asicRows = useAsicRowsByHbSn(hashboardSerial);
  return (
    <div className="relative h-full">
      <div className="flex h-full phone:overflow-x-scroll">
        {asics.length === 0 ? (
          <div
            data-testid="asic-loader"
            className="flex h-full w-full items-center justify-center pb-4"
          >
            <ProgressCircular indeterminate />
          </div>
        ) : (
          <div
            className="flex w-full flex-col gap-1"
            data-testid="asic-table-preview"
          >
            {/* Individual ASICs */}
            {asicRows.map((row) => (
              <div className="flex gap-1" key={`asic-${row}`}>
                {asics
                  .filter((asic) => asic.row === row)
                  .map((asic) => (
                    <AsicCell key={`asic-${asic.id}`} asic={asic} />
                  ))}
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
};

export default AsicTablePreview;
