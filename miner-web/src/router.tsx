import { createBrowserRouter } from "react-router-dom";

import App from "components/App";

import Home from "pages/Home";
import Logs from "pages/MinerLogs";
import Onboarding from "pages/Onboarding";
import MiningPools from "pages/Settings/MiningPools";
import Temperature from "pages/Temperature";


const router = createBrowserRouter([
  {
    path: "/",
    element: <App title="Home"><Home /></App>,
  },
  {
    path: "/temperature",
    element: <App title="Temperature"><Temperature /></App>,
  },
  {
    path: "/home",
    element: <App title="Home"><Home /></App>,
  },
  {
    path: "/logs",
    element: <App title="Logs" fullScreen hideErrors><Logs /></App>,
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
