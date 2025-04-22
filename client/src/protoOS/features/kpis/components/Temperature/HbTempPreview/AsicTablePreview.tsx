import { getAsicsRows } from "../utility"; //TODO
import AsicCell from "./AsicCell";
import { AsicStats } from "@/protoOS/api/types";
import Spinner from "@/shared/components/Spinner";

interface AsicTablePreviewProps {
  asics?: AsicStats[];
}

const AsicTablePreview = ({ asics }: AsicTablePreviewProps) => {
  return (
    <div className="relative h-full">
      <div className="flex h-full phone:overflow-x-scroll">
        {asics == undefined ? (
          <div
            data-testid="asic-loader"
            className="flex h-full w-full items-center justify-center pb-4"
          >
            <Spinner />
          </div>
        ) : (
          <div
            className="flex w-full flex-col gap-1"
            data-testid="asic-table-preview"
          >
            {/* Individual ASICs */}
            {getAsicsRows(asics).map((row) => (
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
