import { useNavigate } from "react-router-dom";
import InlineSelector from "@/shared/components/InlineSelector";

const HashboardSelector = ({
  hashboardList,
  currentHashboard,
}: {
  hashboardList: { serial: string; name: string }[];
  currentHashboard?: string;
}) => {
  const navigate = useNavigate();
  return (
    <InlineSelector
      items={hashboardList.map((board) => ({ id: board.serial, name: board.name }))}
      currentId={currentHashboard}
      onSelect={(serial) => navigate(`../${encodeURIComponent(serial)}`, { replace: true, relative: "path" })}
    />
  );
};
export default HashboardSelector;
