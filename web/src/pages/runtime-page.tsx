import {useQuery} from "@tanstack/react-query";
import {Boxes, Cable, Network, Route, ShieldCheck} from "lucide-react";
import {api, unwrap} from "../api/client";
import {PageHeading} from "../components/page-heading";
import {EmptyBlock, ErrorBlock, LoadingBlock} from "../components/query-state";
import {Badge} from "../components/ui/badge";
import {Panel, PanelHeader} from "../components/ui/panel";
import {asRecord, formatTime} from "../lib/utils";

type StatePayload = Record<string, unknown>;

export function RuntimePage() {
  const queries = {
    summary: useQuery({queryKey: ["status", "summary"], queryFn: () => state("/status/summary"), refetchInterval: 10_000}),
    listeners: useQuery({queryKey: ["status", "listeners"], queryFn: () => state("/status/listeners"), refetchInterval: 15_000}),
    clusters: useQuery({queryKey: ["status", "clusters"], queryFn: () => state("/status/clusters"), refetchInterval: 15_000}),
    routes: useQuery({queryKey: ["status", "routes"], queryFn: () => state("/status/routes"), refetchInterval: 30_000}),
    certs: useQuery({queryKey: ["status", "certs"], queryFn: () => state("/status/certs"), refetchInterval: 60_000}),
  };
  const all = Object.values(queries);
  if (all.some((query) => query.isPending)) return <LoadingBlock label="Reading Envoy admin state" />;
  const failed = all.find((query) => query.isError);
  if (failed?.error) return <ErrorBlock message={failed.error.message} retry={() => all.forEach((query) => void query.refetch())} />;
  const summary = queries.summary.data ?? {};
  const stale = summary.stale === true;
  return <div>
    <PageHeading eyebrow="Runtime / observed state" title="What Envoy sees." description="Normalized read-only state from the local admin endpoint. Stale snapshots remain visible and are never presented as current." action={<Badge tone={stale ? "warning" : summary.ready === true ? "healthy" : "danger"}>{stale ? "stale" : summary.ready === true ? "ready" : "not ready"}</Badge>} />
    <div className="mb-6 flex flex-wrap items-center gap-3 border-y border-ink/20 py-3 text-xs text-muted"><span>Collected {formatTime(String(summary.collectedAt ?? ""))}</span><span className="h-1 w-1 rounded-full bg-line" /><span className="font-mono">node {String(summary.nodeId ?? "—")}</span><span className="h-1 w-1 rounded-full bg-line" /><span>Envoy {String(asRecord(summary.envoy).version ?? "unknown")}</span></div>
    <div className="grid gap-6 xl:grid-cols-2">
      <ResourcePanel title="Listeners" icon={<Cable />} data={queries.listeners.data} render={(item) => <><div><p className="font-mono text-sm font-medium">{String(item.name ?? "unnamed")}</p><p className="mt-1 text-xs text-muted">{String(item.address ?? "address not reported")}</p></div><Owner item={item} /></>} />
      <ResourcePanel title="Clusters" icon={<Network />} data={queries.clusters.data} render={(item) => <><div><p className="font-mono text-sm font-medium">{String(item.name ?? "unnamed")}</p><p className="mt-1 text-xs text-muted">{Array.isArray(item.endpoints) ? item.endpoints.length : 0} endpoints</p></div><Owner item={item} /></>} />
      <ResourcePanel title="Routes" icon={<Route />} data={queries.routes.data} render={(item) => <><div><p className="font-mono text-sm font-medium">{String(item.name ?? "unnamed")}</p><p className="mt-1 text-xs text-muted">{Array.isArray(item.virtual_hosts) ? item.virtual_hosts.length : Array.isArray(item.virtualHosts) ? item.virtualHosts.length : 0} virtual hosts</p></div><Owner item={item} /></>} />
      <ResourcePanel title="Data-plane certificates" icon={<ShieldCheck />} data={queries.certs.data} render={(item) => <><div><p className="font-mono text-sm font-medium">{String(item.name ?? item.subject ?? "certificate")}</p><p className="mt-1 text-xs text-muted">{String(item.subject ?? "subject not reported")}</p></div><Badge tone={Number(item.days_left ?? item.daysLeft ?? 99) < 30 ? "warning" : "healthy"}>{String(item.days_left ?? item.daysLeft ?? "—")} days</Badge></>} />
    </div>
  </div>;
}

async function state(path: "/status/summary" | "/status/listeners" | "/status/clusters" | "/status/routes" | "/status/certs") {
  return unwrap<StatePayload>(await api.GET(path));
}

function ResourcePanel({title, icon, data, render}: {title: string; icon: React.ReactNode; data?: StatePayload; render: (item: Record<string, unknown>) => React.ReactNode}) {
  const items = Array.isArray(data?.items) ? data.items.map(asRecord) : [];
  return <Panel><PanelHeader><div><p className="eyebrow text-muted">Observed resources</p><h2 className="mt-2 font-display text-3xl">{title}</h2></div><div className="text-signal">{icon}</div></PanelHeader>{items.length ? <div className="divide-y divide-line">{items.map((item, index) => <div key={String(item.name ?? index)} className="flex items-center justify-between gap-4 p-5">{render(item)}</div>)}</div> : <EmptyBlock title={`No ${title.toLowerCase()}`} description="Envoy did not report any resources in this snapshot." />}</Panel>;
}

function Owner({item}: {item: Record<string, unknown>}) {
  const owner = asRecord(item.owner);
  return owner.name ? <Badge>{String(owner.kind)}/{String(owner.name)}</Badge> : <Boxes size={16} className="text-line" />;
}
