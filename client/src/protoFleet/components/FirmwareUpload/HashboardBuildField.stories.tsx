import { useState } from "react";
import HashboardBuildField from "./HashboardBuildField";

export default { title: "Proto Fleet/Settings/Hashboard Build", component: HashboardBuildField };

export const Default = () => {
  const [build, setBuild] = useState("prod");
  return (
    <div className="max-w-sm">
      <HashboardBuildField value={build} onChange={setBuild} />
    </div>
  );
};
