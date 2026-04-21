import { RouterProvider } from "react-router-dom";

import router from "./router";

import "@/shared/styles/index.css";

// Polyfill BigInt.prototype.toJSON to fix React 19 serialization issue
// React 19 attempts to serialize props during comparison, which fails for BigInt values
// See: https://github.com/facebook/react/issues/35004
if (typeof BigInt !== "undefined") {
  const BigIntPrototype = BigInt.prototype as BigInt & { toJSON?: () => string };
  if (!BigIntPrototype.toJSON) {
    Object.defineProperty(BigInt.prototype, "toJSON", {
      value: function (this: bigint) {
        return this.toString();
      },
    });
  }
}

const Main = () => {
  return <RouterProvider router={router} />;
};

export default Main;
