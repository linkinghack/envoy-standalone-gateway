import {useQuery} from "@tanstack/react-query";
import {Binary, Box, Cpu, GitBranch, Server} from "lucide-react";
import {api, unwrap} from "../api/client";
import {PageHeading} from "../components/page-heading";
import {ErrorBlock, LoadingBlock} from "../components/query-state";
import {Badge} from "../components/ui/badge";
import {Panel, PanelHeader} from "../components/ui/panel";
import {asRecord} from "../lib/utils";

export function SystemPage() {
  const system = useQuery({queryKey: ["system"], queryFn: async () => unwrap<Record<string, unknown>>(await api.GET("/system/info"))});
  if (system.isPending) return <LoadingBlock label="Reading build information" />;
  if (system.isError) return <ErrorBlock message={system.error.message} retry={() => void system.refetch()} />;
  const envoy = asRecord(system.data.envoy);
  const matrix = Array.isArray(envoy.validationMatrix) ? envoy.validationMatrix.map(String) : [];
  return <div>
    <PageHeading eyebrow="System / compatibility" title="Know what is running." description="Build identity, deployment topology, and the exact Envoy support window used by validation." />
    <div className="grid gap-6 lg:grid-cols-2">
      <Panel><PanelHeader><div><p className="eyebrow text-muted">Management plane</p><h2 className="mt-2 font-display text-3xl">ESGW build</h2></div><Binary className="text-signal" /></PanelHeader><div className="divide-y divide-line"><SystemFact icon={<GitBranch />} label="Version" value={String(system.data.version ?? "dev")} mono /><SystemFact icon={<Cpu />} label="Go runtime" value={String(system.data.goVersion ?? "unknown")} mono /><SystemFact icon={<Box />} label="Delivery mode" value={String(system.data.mode ?? "xds")} /><SystemFact icon={<Server />} label="Topology" value={String(system.data.topology ?? "standalone")} /></div></Panel>
      <Panel><PanelHeader><div><p className="eyebrow text-muted">Data plane</p><h2 className="mt-2 font-display text-3xl">Envoy window</h2></div><Server className="text-healthy" /></PanelHeader><div className="p-5"><p className="text-sm text-muted">Observed version</p><p className="mt-2 font-mono text-xl">{String(envoy.version ?? "not connected")}</p><div className="my-6 h-px bg-line" /><p className="text-sm text-muted">Supported minor range</p><p className="mt-2 font-display text-4xl">1.{String(envoy.supportedMinorMin ?? "—")}–1.{String(envoy.supportedMinorMax ?? "—")}</p><p className="eyebrow mb-3 mt-7 text-muted">Validation matrix</p><div className="flex flex-wrap gap-2">{matrix.map((version) => <Badge key={version} tone={version === envoy.version ? "healthy" : "neutral"}>{version}</Badge>)}</div></div></Panel>
    </div>
  </div>;
}

function SystemFact({icon, label, value, mono}: {icon: React.ReactNode; label: string; value: string; mono?: boolean}) {
  return <div className="grid grid-cols-[28px_1fr] gap-3 p-5"><div className="text-muted">{icon}</div><div><p className="text-xs text-muted">{label}</p><p className={`mt-1 text-sm font-semibold ${mono ? "font-mono" : ""}`}>{value}</p></div></div>;
}
