import { createBrowserRouter } from "react-router-dom";

import App from "components/App";

import Hardware from "pages/Hardware";
import Home from "pages/Home";
import Logs from "pages/MinerLogs";
import Onboarding from "pages/Onboarding";
import MiningPools from "pages/Settings/MiningPools";


const router = createBrowserRouter([
  {
    path: "/",
    element: <App title="Home"><Home /></App>,
  },
  {
    path: "/hardware",
    element: <App title="Hardware"><Hardware /></App>,
  },
  {
    path: "/home",
    element: <App title="Home"><Home /></App>,
  },
  {
    path: "/logs",
    element: <Logs />,
  },
  {
    path: "/onboarding",
    element: <Onboarding />,
  },
  {
    path: "/settings/mining-pools",
    element: <App title="Settings"><MiningPools /></App>,
  },
]);

export default router;
