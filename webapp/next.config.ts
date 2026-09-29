import type { NextConfig } from "next";

// Fully static site: `next build` writes plain HTML/CSS/JS to out/, which Vercel
// (or any static host) serves with no server code.
const nextConfig: NextConfig = {
  output: "export",
  images: { unoptimized: true },
  reactStrictMode: true,
  // `npm run dev` also prints a Network URL (this Mac's LAN address) for testing
  // from another device or browser. Next.js refuses its dev resources, the hot
  // reload socket included, to any origin not listed here, and a page opened that
  // way never becomes interactive: no video, no scroll story, no toggles. These are
  // the private address ranges and Bonjour (.local) names; dev only, no effect on
  // the exported site.
  allowedDevOrigins: ["10.*.*.*", "172.*.*.*", "192.168.*.*", "*.local"],
};

export default nextConfig;
