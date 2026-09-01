"use client";

import { useMemo, useState } from "react";
import { Bot, Cloud, Plus, Server } from "lucide-react";
import { useWorkspacePaths } from "@multica/core/paths";
import { AgentsPage, type AgentsPageProps } from "../../agents/components/agents-page";
import {
  CollectionPageHeader,
  CollectionPageHeaderAction,
} from "../../layout/collection-page";
import { PAGE_GUTTER } from "../../layout/page-header";
import { useNavigation } from "../../navigation";
import { useT } from "../../i18n";
import { CloudRuntimeDialog } from "./cloud-runtime-dialog";
import { ConnectRemoteDialog } from "./connect-remote-dialog";
import { daemonRuntimesDocsHref } from "./runtime-docs";
import {
  OrphanRuntimeProfiles,
  type RuntimesPageProps,
} from "./runtimes-page";
import { MachineRail } from "./machine-rail";
import { useWorkspaceRuntimeCollection } from "./use-workspace-runtime-collection";
import { Skeleton } from "@multica/ui/components/ui/skeleton";

export type AgentsRuntimesPageProps = AgentsPageProps & RuntimesPageProps;

export function AgentsRuntimesPage({
  localDaemonId,
  localMachineName,
  hasLocalMachine,
  bootstrapping,
  cloudRuntimeEnabled = false,
}: AgentsRuntimesPageProps = {}) {
  const { t, i18n } = useT("runtimes");
  const { t: agentsT } = useT("agents");
  const navigation = useNavigation();
  const paths = useWorkspacePaths();
  const [selectedMachineId, setSelectedMachineId] = useState<string | null>(null);
  const [showConnectDialog, setShowConnectDialog] = useState(false);
  const [showCloudRuntimeDialog, setShowCloudRuntimeDialog] = useState(false);
  const collection = useWorkspaceRuntimeCollection({
    localDaemonId,
    localMachineName,
    hasLocalMachine,
  });
  const {
    loading,
    machines,
    orphanProfileRuntimes,
    now,
  } = collection;

  const selectedMachine =
    machines.find((machine) => machine.id === selectedMachineId) ?? null;
  const machineRuntimeIds = useMemo(() => {
    if (!selectedMachine) return null;
    return new Set(selectedMachine.runtimes.map((runtime) => runtime.id));
  }, [selectedMachine]);

  if (loading) {
    return (
      <div className="flex min-h-0 flex-1 flex-col">
        <CollectionPageHeader
          icon={Server}
          title={t(($) => $.page.title)}
          description={t(($) => $.page.fused_tagline)}
        />
        <div className={PAGE_GUTTER + " flex h-12 items-center gap-2 border-b"}>
          <Skeleton className="h-8 w-24 rounded-md" />
          <Skeleton className="h-8 w-40 rounded-md" />
          <Skeleton className="h-8 w-36 rounded-md" />
        </div>
        <div className="p-6">
          <Skeleton className="h-16 w-full" />
          <Skeleton className="mt-2 h-16 w-full" />
        </div>
      </div>
    );
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
      <CollectionPageHeader
        icon={Server}
        title={t(($) => $.page.title)}
        description={t(($) => $.page.fused_tagline)}
        learnMore={{
          href: daemonRuntimesDocsHref(i18n.language),
          label: t(($) => $.page.learn_more),
        }}
        actions={
          <>
            {cloudRuntimeEnabled && (
              <CollectionPageHeaderAction
                icon={Cloud}
                label={t(($) => $.cloud_runtime.action)}
                onClick={() => setShowCloudRuntimeDialog(true)}
              />
            )}
            <CollectionPageHeaderAction
              icon={Plus}
              label={t(($) => $.page.connect_remote)}
              onClick={() => setShowConnectDialog(true)}
            />
            <CollectionPageHeaderAction
              icon={Bot}
              label={agentsT(($) => $.page.new_agent)}
              onClick={() => navigation.push(paths.newAgent())}
            />
          </>
        }
      />

      <MachineRail
        machines={machines}
        selectedId={selectedMachine ? selectedMachine.id : null}
        onSelect={setSelectedMachineId}
        onConnectRemote={() => setShowConnectDialog(true)}
        bootstrapping={bootstrapping}
      />

      {orphanProfileRuntimes.length > 0 ? (
        <div className={PAGE_GUTTER + " pt-4"}>
          <OrphanRuntimeProfiles
            runtimes={orphanProfileRuntimes}
            now={now}
            hasMachines={false}
          />
        </div>
      ) : null}

      <AgentsPage
        hideHeader
        machineRuntimeIds={machineRuntimeIds}
        machineTitle={selectedMachine?.title ?? null}
        localDaemonId={localDaemonId}
        localMachineName={localMachineName}
        hasLocalMachine={hasLocalMachine}
      />

      {showConnectDialog && (
        <ConnectRemoteDialog onClose={() => setShowConnectDialog(false)} />
      )}
      {cloudRuntimeEnabled && showCloudRuntimeDialog && (
        <CloudRuntimeDialog onClose={() => setShowCloudRuntimeDialog(false)} />
      )}
    </div>
  );
}
