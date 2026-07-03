import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  // Standalone output bundles a minimal server (.next/standalone/server.js) so
  // the production Docker image (web/Dockerfile.prod) doesn't need the full
  // node_modules tree. Required by the prod build.
  output: "standalone",
};

export default nextConfig;
