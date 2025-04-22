import { createClient } from "@connectrpc/connect";
import { AuthService } from "./generated/auth/v1/auth_pb";
import { transport } from "./transport";

const authServiceClient = createClient(AuthService, transport);

export { authServiceClient };
