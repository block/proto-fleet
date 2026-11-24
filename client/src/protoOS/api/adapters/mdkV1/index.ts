// Public surface of the MDK v1 adapter.
//
// The generated swagger client under ./generated is a private implementation
// detail — the `no-restricted-imports` boundary in eslint.config.js forbids
// anything outside this adapter from importing it directly. Consumers that
// still depend on v1 wire types or the raw HTTP client (transitional, pending
// the adapter-mediated migration in the #26 epic) import them from here.
//
// The re-export list is explicit (not `export *`) so the transitional surface
// is greppable and can only shrink — never silently widen when the client is
// regenerated. Add a symbol here only when a consumer outside the adapter
// genuinely needs it; the long-term goal (#26) is to empty this list.
export { Api, MetricUnit } from "./generated/generatedApi";
export type {
  ChangePasswordRequest,
  HttpResponse,
  MessageResponse,
  MiningTarget,
  MiningTargetResponse,
  PasswordRequest,
  Pool,
  PoolConfigInner,
  RefreshRequest,
  RequestParams,
  TestConnection,
} from "./generated/generatedApi";
