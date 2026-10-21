"use client";

import { LandingHeader } from "@/features/landing/components/landing-header";
import { LandingFooter } from "@/features/landing/components/landing-footer";
import { CliSection } from "@/features/landing/components/download/cli-section";
import { CloudSection } from "@/features/landing/components/download/cloud-section";

export function DownloadClient() {
  return (
    <>
      <div className="relative">
        <LandingHeader variant="dark" />
        <section className="bg-[#0a0d12] px-4 pb-16 pt-28 text-white sm:px-6 sm:pt-32 lg:px-8">
          <div className="mx-auto max-w-[820px]">
            <h1 className="landing-serif text-[2.6rem] leading-[1.05] tracking-[-0.03em] sm:text-[3.4rem]">
              Use Multica in the browser
            </h1>
            <p className="mt-4 max-w-[620px] text-body-lg leading-7 text-white/72">
              The web app is a responsive PWA, including on phones and tablets.
              Install the CLI on the computer that will run agents.
            </p>
          </div>
        </section>
      </div>
      <CliSection />
      <CloudSection />
      <LandingFooter />
    </>
  );
}
