import { useLocation, useNavigate } from "react-router-dom";
import InlineSelector from "@/shared/components/InlineSelector";

const HashboardSelector = ({
  hashboardList,
  currentHashboard,
}: {
  hashboardList: { serial: string; name: string }[];
  currentHashboard?: string;
}) => {
  const navigate = useNavigate();
  const { search } = useLocation();
  return (
    <InlineSelector
      items={hashboardList.map((board) => ({ id: board.serial, name: board.name }))}
      currentId={currentHashboard}
      onSelect={(serial) =>
        navigate({ pathname: `../${encodeURIComponent(serial)}`, search }, { replace: true, relative: "path" })
      }
    />
  );
};
export default HashboardSelector;
