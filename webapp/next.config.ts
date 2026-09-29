import type { NextConfig } from "next";

// Fully static site: `next build` writes plain HTML/CSS/JS to out/, which Vercel
// (or any static host) serves with no server code.
const nextConfig: NextConfig = {
  output: "export",
  images: { unoptimized: true },
  reactStrictMode: true,
};

export default nextConfig;
