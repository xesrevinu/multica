import type { Metadata } from "next";
import { DownloadClient } from "./download-client";

export const metadata: Metadata = {
  title: "Install Multica",
  description:
    "Use Multica in the browser. Install the CLI on the computer that will run agents.",
  openGraph: {
    title: "Install Multica",
    description:
      "The web app is a responsive PWA. Install the CLI for servers and remote dev boxes.",
    url: "/download",
  },
  alternates: {
    canonical: "/download",
  },
};

export default function DownloadPage() {
  return <DownloadClient />;
}
