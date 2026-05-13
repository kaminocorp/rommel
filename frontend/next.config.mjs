/** @type {import("next").NextConfig} */
const nextConfig = {
  reactStrictMode: true,
  // @rommel/proto ships raw .ts (see proto/clients/ts/package.json: main = src/index.ts).
  // Next's default build does not transpile node_modules; this is the cheaper
  // alternative to a separate tsc build step in `make proto`. Risk 4.2.
  transpilePackages: ["@rommel/proto"],
  experimental: {
    typedRoutes: false,
  },
};

export default nextConfig;
