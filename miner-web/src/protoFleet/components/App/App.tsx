import { ReactNode } from "react";
import AppLayout from "@/protoFleet/components/AppLayout";

type Props = {
  children: ReactNode;
  title: string;
};

const App = ({ children, title }: Props) => {
  // TODO: need to do checks here for show login modal etc similar to ProtoOS
  return <AppLayout title={title}>{children}</AppLayout>;
};

export default App;
