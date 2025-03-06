import { createBrowserRouter } from "react-router-dom";

import App from "./components/App";
import routes from "./routes";

const router = createBrowserRouter([
  {
    element: <App />,
    children: routes,
  },
]);

export default router;
