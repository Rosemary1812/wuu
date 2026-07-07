// @wuu/remote-core — the controller-side client of wuu remote control.
// Pure TS, no UI dependencies; runs in React Native, browsers, and Node.
//
// The Go implementation (internal/remote) is the reference. The crypto layer
// here is pinned byte-for-byte by internal/remote/secure/testdata/vectors.json;
// see test/vectors.test.ts.

export * from "./b64.js";
export * from "./bytes.js";
export * from "./secure.js";
export * from "./wire.js";
export * from "./rpc.js";
export * from "./client.js";
