import { RouterProvider } from "react-router-dom";

import router from "./router";

import "@/shared/styles/index.css";

const Main = () => {
  return <RouterProvider router={router} />;
};

export default Main;
