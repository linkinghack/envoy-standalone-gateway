import {useMutation, useQuery, useQueryClient} from "@tanstack/react-query";
import {Braces, Check, FileCode2, Rocket, Save, TriangleAlert} from "lucide-react";
import {lazy, Suspense, useEffect, useMemo, useState} from "react";
import type {components} from "../api/generated";
import {api, csrfHeader, ManagementAPIError, unwrap} from "../api/client";
import {PageHeading} from "../components/page-heading";
import {ErrorBlock, LoadingBlock} from "../components/query-state";
import {Button} from "../components/ui/button";
import {Badge} from "../components/ui/badge";
import {Dialog, DialogContent, DialogTrigger} from "../components/ui/dialog";
import {Field, Input} from "../components/ui/field";
import {Panel} from "../components/ui/panel";
import {cn} from "../lib/utils";

const CodeEditor = lazy(() => import("../components/code-editor"));
type Draft = components["schemas"]["Draft"];
type ValidationResult = components["schemas"]["ValidationResult"];
type ObjectList = components["schemas"]["ObjectList"];

export function ConfigurationPage() {
  const queryClient = useQueryClient();
  const draft = useQuery({queryKey: ["config", "draft"], queryFn: async () => unwrap<Draft>(await api.GET("/config/draft"))});
  const objects = useQuery({queryKey: ["config", "objects"], queryFn: async () => unwrap<ObjectList>(await api.GET("/config/objects"))});
  const [activePath, setActivePath] = useState("");
  const [content, setContent] = useState("");
  const [validation, setValidation] = useState<ValidationResult | null>(null);
  const [message, setMessage] = useState("");
  const activeFile = useMemo(() => draft.data?.files.find((file) => file.path === activePath) ?? draft.data?.files[0], [draft.data, activePath]);
  useEffect(() => {
    if (activeFile) {
      setActivePath(activeFile.path);
      setContent(activeFile.content);
    }
  }, [activeFile?.path, activeFile?.content]);

  const save = useMutation({
    mutationFn: async () => {
      if (!draft.data || !activeFile) return;
      const files = draft.data.files.map((file) => file.path === activeFile.path ? {...file, content} : file);
      return unwrap(await api.PUT("/config/draft", {params: {header: csrfHeader}, body: {sourceType: draft.data.sourceType, expectedResourceVersion: draft.data.resourceVersion, files}}));
    },
    onSuccess: async () => { await queryClient.invalidateQueries({queryKey: ["config"]}); },
  });
  const validate = useMutation({
    mutationFn: async () => unwrap<ValidationResult>(await api.POST("/config/validate", {params: {query: {mode: "xds"}, header: csrfHeader}, body: {envoyValidate: false}})),
    onSuccess: setValidation,
  });
  const publish = useMutation({
    mutationFn: async () => unwrap(await api.POST("/config/publish", {params: {header: csrfHeader}, body: {message, expectedResourceVersion: draft.data?.resourceVersion ?? ""}})),
    onSuccess: async () => { setMessage(""); await queryClient.invalidateQueries({queryKey: ["config"]}); },
  });
  if (draft.isPending || objects.isPending) return <LoadingBlock label="Loading filesystem draft" />;
  if (draft.isError || objects.isError) return <ErrorBlock message={(draft.error ?? objects.error)?.message ?? "Configuration could not be loaded."} retry={() => { void draft.refetch(); void objects.refetch(); }} />;
  const changed = activeFile ? content !== activeFile.content : false;
  return <div>
    <PageHeading eyebrow="Configuration / filesystem truth" title="Shape the gateway." description="Edit the complete source without hiding advanced fields. Save, validate, and publish remain separate decisions." action={<div className="flex flex-wrap gap-3"><Button variant="secondary" onClick={() => validate.mutate()} disabled={validate.isPending || changed}><Check size={16} /> Validate</Button><PublishDialog message={message} setMessage={setMessage} publish={() => publish.mutate()} pending={publish.isPending} disabled={changed || validation?.ok !== true} error={publish.error} /></div>} />
    <div className="mb-5 flex flex-wrap items-center gap-2">
      <Badge>{draft.data.sourceType}</Badge><Badge>{draft.data.files.length} files</Badge><Badge>{objects.data.total} objects</Badge><span className="ml-auto max-w-full truncate font-mono text-[.64rem] text-muted" title={draft.data.resourceVersion}>rv {draft.data.resourceVersion.slice(0, 12)}</span>
    </div>
    {(save.error || validate.error) ? <div role="alert" className="mb-5 border-l-4 border-red-800 bg-red-50 p-4 text-sm text-red-900">{errorMessage(save.error ?? validate.error)}</div> : null}
    {validation ? <ValidationStrip result={validation} /> : null}
    <div className="grid min-h-[600px] overflow-hidden border border-ink bg-ink lg:grid-cols-[230px_1fr]">
      <aside className="border-b border-white/15 bg-ink text-paper lg:border-b-0 lg:border-r">
        <div className="border-b border-white/15 p-4"><p className="eyebrow text-white/40">Source files</p></div>
        <div className="scrollbar-thin max-h-52 overflow-auto py-2 lg:max-h-[650px]">
          {draft.data.files.map((file) => <button key={file.path} className={cn("flex w-full items-center gap-3 border-l-4 border-transparent px-4 py-3 text-left text-xs text-white/60 hover:bg-white/[.06] hover:text-white", file.path === activeFile?.path && "border-signal bg-white/[.08] text-white")} onClick={() => setActivePath(file.path)}><FileCode2 size={15} className="shrink-0" /><span className="truncate font-mono">{file.path.replace("config.d/", "")}</span></button>)}
        </div>
        <div className="border-t border-white/15 p-4"><p className="eyebrow mb-3 text-white/40">Object index</p><div className="flex flex-wrap gap-1.5">{Array.from(new Set(objects.data.items.map((item) => item.kind))).map((kind) => <Badge key={kind} className="border-white/15 bg-white/[.04] text-white/55">{kind}</Badge>)}</div></div>
      </aside>
      <section className="min-w-0 bg-[#171d1c]">
        <div className="flex min-h-14 items-center justify-between gap-4 border-b border-white/15 px-4 text-paper"><div className="min-w-0"><p className="truncate font-mono text-xs">{activeFile?.path ?? "No source file"}</p></div><Button size="sm" disabled={!changed || save.isPending} onClick={() => save.mutate()}><Save size={14} /> {save.isPending ? "Saving…" : "Save draft"}</Button></div>
        {activeFile ? <Suspense fallback={<div className="grid min-h-[520px] place-items-center text-white/50">Loading Monaco…</div>}><CodeEditor value={content} onChange={setContent} language="yaml" /></Suspense> : <div className="grid min-h-[520px] place-items-center text-white/45"><div className="text-center"><Braces className="mx-auto" /><p className="mt-3">No source files yet.</p></div></div>}
      </section>
    </div>
  </div>;
}

function ValidationStrip({result}: {result: ValidationResult}) {
  return <Panel className={`mb-5 border-l-4 ${result.ok ? "border-l-healthy" : "border-l-red-700"}`}><div className="flex flex-wrap items-start gap-4 p-4"><div className={`grid h-9 w-9 place-items-center ${result.ok ? "bg-healthy text-white" : "bg-red-700 text-white"}`}>{result.ok ? <Check size={18} /> : <TriangleAlert size={18} />}</div><div className="min-w-0 flex-1"><p className="font-semibold">{result.ok ? "Draft compiles cleanly" : "Validation found blocking diagnostics"}</p><p className="mt-1 text-xs text-muted">{result.results.length ? result.results.map((item) => `${item.stage}: ${item.message}`).join(" · ") : `IR ${result.irVersion ?? "ready"}`}</p></div><Badge tone={result.ok ? "healthy" : "danger"}>{result.mode}</Badge></div></Panel>;
}

function PublishDialog({message, setMessage, publish, pending, disabled, error}: {message: string; setMessage: (value: string) => void; publish: () => void; pending: boolean; disabled: boolean; error: Error | null}) {
  return <Dialog><DialogTrigger asChild><Button disabled={disabled}><Rocket size={16} /> Review & publish</Button></DialogTrigger><DialogContent title="Publish this draft" description="The compiled IR will be applied to xDS, then held until Envoy confirms the observed version."><div className="grid gap-5"><div className="border border-line bg-white/50 p-4 text-sm"><p className="font-semibold">Preflight</p><ul className="mt-3 grid gap-2 text-muted"><li>✓ Draft saved at current resource version</li><li>✓ Compile validation succeeded</li><li>• Envoy confirmation follows delivery</li></ul></div><Field label="Change message" hint="Describe operator intent; this is stored with the immutable version."><Input value={message} maxLength={500} onChange={(event) => setMessage(event.target.value)} placeholder="Route checkout traffic to v2" /></Field>{error ? <p role="alert" className="text-sm text-red-800">{errorMessage(error)}</p> : null}<Button size="lg" onClick={publish} disabled={pending || !message.trim()}>{pending ? "Publishing…" : "Publish to Envoy"}</Button></div></DialogContent></Dialog>;
}

function errorMessage(error: Error | null) {
  return error instanceof ManagementAPIError ? `${error.code}: ${error.message}` : error?.message ?? "Request failed";
}
