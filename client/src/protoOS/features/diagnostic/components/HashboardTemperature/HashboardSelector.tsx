import type { ComponentProps } from "react";
import { useLocation, useNavigate } from "react-router-dom";
import InlineSelector from "@/shared/components/InlineSelector";

const HashboardSelector = ({
  hashboardList,
  currentHashboard,
  headingSize,
  onSelect,
}: {
  hashboardList: { serial: string; name: string }[];
  currentHashboard?: string;
  onSelect?: (serial: string) => void;
  headingSize?: ComponentProps<typeof InlineSelector>["headingSize"];
}) => {
  const navigate = useNavigate();
  const { search } = useLocation();
  return (
    <InlineSelector
      headingSize={headingSize}
      items={hashboardList.map((board) => ({ id: board.serial, name: board.name }))}
      currentId={currentHashboard}
      onSelect={
        onSelect ??
        ((serial) =>
          navigate({ pathname: `../${encodeURIComponent(serial)}`, search }, { replace: true, relative: "path" }))
      }
    />
  );
};
export default HashboardSelector;
