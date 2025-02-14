import { createElement } from "react";
import { createBrowserRouter } from "react-router-dom";

import App from "./components/App";
import SingleMinerWrapper from "./components/SingleMinerWrapper";
import Containers from "./pages/Containers";
import Home from "./pages/Home";
import Miners from "./pages/Miners";
import Racks from "./pages/Racks";
import Auth from "@/protoOS/pages/Auth";
import Onboarding from "@/protoOS/pages/Onboarding";
import { routerConfig as singleMinerRoutes } from "@/protoOS/router";

// copies all Proto OS routes and wraps their element in SingleMinerWrapper
const wrappedMinerRoutes = singleMinerRoutes.map((route) => {
  const wrappedElement = createElement(SingleMinerWrapper, null, route.element);

  return {
    ...route,
    element: wrappedElement,
  };
});

const router = createBrowserRouter([
  {
    path: "/",
    element: (
      <App title="Home">
        <Home />
      </App>
    ),
  },
  {
    path: "/containers",
    element: (
      <App title="Containers">
        <Containers />
      </App>
    ),
  },
  {
    path: "/racks",
    element: (
      <App title="Racks">
        <Racks />
      </App>
    ),
  },
  {
    path: "/miners",
    element: (
      <App title="Miners">
        <Miners />
      </App>
    ),
  },
  {
    path: "/miners/:id",
    children: wrappedMinerRoutes,
  },
  {
    path: "/auth",
    element: <Auth />,
  },
  {
    path: "/onboarding",
    element: <Onboarding />,
  },
]);

export default router;
