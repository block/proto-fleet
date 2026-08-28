// Public surface of the MDK v1 adapter.
//
// The generated swagger client under ./generated is a private implementation
// detail — the `no-restricted-imports` boundary in eslint.config.js forbids
// anything outside this adapter from importing it directly. Consumers that
// still depend on v1 wire types or the raw HTTP client (transitional, pending
// the adapter-mediated migration in the #26 epic) import them from here, which
// keeps the dialect dependency explicit and centralized on the adapter.
export * from "./generated/generatedApi";
