import {useQuery} from "@tanstack/react-query";
import {ArrowRight, CheckCircle2, CircleDashed, RadioTower} from "lucide-react";
import {Link} from "react-router-dom";
import {api, unwrap} from "../api/client";
import {PageHeading} from "../components/page-heading";
import {ErrorBlock, LoadingBlock} from "../components/query-state";
import {Badge} from "../components/ui/badge";
import {Panel, PanelHeader} from "../components/ui/panel";
import {asRecord, formatTime} from "../lib/utils";

export function OverviewPage() {
  const summary = useQuery({queryKey: ["status", "summary"], queryFn: async () => unwrap<Record<string, unknown>>(await api.GET("/status/summary")), refetchInterval: 15_000});
  const config = useQuery({queryKey: ["config", "status"], queryFn: async () => unwrap<Record<string, unknown>>(await api.GET("/config/status"))});
  if (summary.isPending || config.isPending) return <LoadingBlock label="Assembling operational picture" />;
  if (summary.isError || config.isError) return <ErrorBlock message={(summary.error ?? config.error)?.message ?? "Overview could not be loaded."} retry={() => { void summary.refetch(); void config.refetch(); }} />;
  const counts = asRecord(summary.data.counts);
  const envoy = asRecord(summary.data.envoy);
  const delivery = asRecord(config.data.delivery);
  const ready = summary.data.ready === true;
  const stale = summary.data.stale === true;
  return <div>
    <PageHeading eyebrow="Operational picture / live" title="The edge, at a glance." description="What Envoy is actually serving, what configuration is moving, and where attention is needed." action={<Link to="/configuration" className="inline-flex min-h-11 items-center gap-3 border border-ink bg-ink px-5 text-sm font-semibold text-paper hover:bg-signal">Open configuration <ArrowRight size={16} /></Link>} />
    <section className="mb-6 grid gap-px border border-ink bg-ink md:grid-cols-2 xl:grid-cols-5">
      <SignalCell label="Data plane" value={ready ? "Ready" : "Not ready"} meta={String(summary.data.readyStatus ?? "Unknown")} accent={ready ? "healthy" : "warning"} />
      <SignalCell label="Listeners" value={String(counts.listeners ?? 0)} meta="active resources" />
      <SignalCell label="Clusters" value={String(counts.clusters ?? 0)} meta={`${String(counts.endpoints ?? 0)} endpoints`} />
      <SignalCell label="Routes" value={String(counts.routes ?? 0)} meta="route configurations" />
      <SignalCell label="Delivery" value={String(delivery.state ?? "idle").replaceAll("_", " ")} meta={String(delivery.mode ?? "xds")} accent={String(delivery.state).includes("confirm") ? "warning" : "neutral"} />
    </section>
    <div className="grid gap-6 xl:grid-cols-[1.25fr_.75fr]">
      <Panel className="animate-signal-in [animation-delay:120ms]">
        <PanelHeader><div><p className="eyebrow text-muted">Data-plane signal</p><h2 className="mt-2 font-display text-3xl">Envoy heartbeat</h2></div><Badge tone={stale ? "warning" : ready ? "healthy" : "danger"}>{stale ? "stale snapshot" : ready ? "live" : "degraded"}</Badge></PanelHeader>
        <div className="grid gap-px bg-line sm:grid-cols-2">
          <Fact label="Envoy version" value={String(envoy.version ?? "Unknown")} mono />
          <Fact label="Server state" value={String(envoy.state ?? "Unknown")} />
          <Fact label="Collected" value={formatTime(String(summary.data.collectedAt ?? ""))} />
          <Fact label="Node" value={String(summary.data.nodeId ?? "—")} mono />
        </div>
      </Panel>
      <Panel className="animate-signal-in [animation-delay:180ms]">
        <PanelHeader><div><p className="eyebrow text-muted">Next action</p><h2 className="mt-2 font-display text-3xl">Operator queue</h2></div><RadioTower className="text-signal" /></PanelHeader>
        <div className="divide-y divide-line">
          <QueueItem done={!stale} title={stale ? "Envoy state is stale" : "State collection current"} detail={stale ? "Check the admin endpoint before publishing." : "No collector intervention required."} />
          <QueueItem done={!String(delivery.state).includes("confirm")} title={String(delivery.state).includes("confirm") ? "Publish awaits confirmation" : "No publish in flight"} detail="Review delivery state before the next change." />
          <QueueItem done title="Session protected" detail="Same-origin cookie and CSRF boundary active." />
        </div>
      </Panel>
    </div>
  </div>;
}

function SignalCell({label, value, meta, accent = "neutral"}: {label: string; value: string; meta: string; accent?: "healthy" | "warning" | "neutral"}) {
  return <div className="bg-paper p-5"><p className="eyebrow text-muted">{label}</p><div className="mt-5 flex items-end justify-between gap-3"><div><div className="metric-value text-4xl capitalize">{value}</div><p className="mt-1 text-xs text-muted">{meta}</p></div><span className={`mb-1 h-2.5 w-2.5 ${accent === "healthy" ? "bg-healthy" : accent === "warning" ? "bg-signal" : "bg-line"}`} /></div></div>;
}

function Fact({label, value, mono}: {label: string; value: string; mono?: boolean}) {
  return <div className="bg-paper p-5"><p className="text-xs text-muted">{label}</p><p className={`mt-2 text-sm font-medium ${mono ? "font-mono" : ""}`}>{value}</p></div>;
}

function QueueItem({done, title, detail}: {done: boolean; title: string; detail: string}) {
  return <div className="flex gap-4 p-5">{done ? <CheckCircle2 className="mt-0.5 shrink-0 text-healthy" size={18} /> : <CircleDashed className="mt-0.5 shrink-0 text-signal" size={18} />}<div><p className="text-sm font-semibold">{title}</p><p className="mt-1 text-xs leading-5 text-muted">{detail}</p></div></div>;
}
