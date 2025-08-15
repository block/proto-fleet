import ReactDOM from "react-dom/client";

import Main from "./main";
import { logBuildVersion } from "@/shared/utils/version";

logBuildVersion();

ReactDOM.createRoot(document.getElementById("root")!).render(<Main />);
