import {useQuery} from "@tanstack/react-query";
import {lazy, Suspense, useState} from "react";
import {api, unwrap} from "../api/client";
import {PageHeading} from "../components/page-heading";
import {ErrorBlock, LoadingBlock} from "../components/query-state";
import {Badge} from "../components/ui/badge";
import {Button} from "../components/ui/button";
import {Panel} from "../components/ui/panel";

const CodeEditor = lazy(() => import("../components/code-editor"));

export function ExpertPage() {
  const [mode, setMode] = useState<"xds" | "static">("xds");
  const compiled = useQuery({queryKey: ["config", "compiled", mode], queryFn: async () => unwrap<unknown>(await api.GET("/config/compiled", {params: {query: {mode}}}))});
  if (compiled.isPending) return <LoadingBlock label="Compiling expert view" />;
  if (compiled.isError) return <ErrorBlock message={compiled.error.message} retry={() => void compiled.refetch()} />;
  const content = JSON.stringify(compiled.data, null, 2);
  return <div>
    <PageHeading eyebrow="Expert / compiled output" title="See past the abstraction." description="Inspect the exact resource graph produced from the current draft. This view is read-only by design." action={<div className="flex gap-2"><Button size="sm" variant={mode === "xds" ? "primary" : "secondary"} onClick={() => setMode("xds")}>xDS snapshot</Button><Button size="sm" variant={mode === "static" ? "primary" : "secondary"} onClick={() => setMode("static")}>Static IR</Button></div>} />
    <div className="mb-4 flex flex-wrap items-center gap-2"><Badge>{mode}</Badge><Badge>{content.length.toLocaleString()} bytes</Badge><span className="text-xs text-muted">Generated from the unsaved filesystem draft at request time.</span></div>
    <Panel className="overflow-hidden border-ink bg-ink"><Suspense fallback={<div className="grid min-h-[600px] place-items-center text-white/50">Loading code viewer…</div>}><CodeEditor value={content} language="json" readOnly /></Suspense></Panel>
  </div>;
}
