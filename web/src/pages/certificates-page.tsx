import {useMutation, useQuery, useQueryClient} from "@tanstack/react-query";
import {CalendarClock, FileKey2, Plus, ShieldAlert, Trash2} from "lucide-react";
import {useState} from "react";
import type {components} from "../api/generated";
import {api, csrfHeader, ManagementAPIError, unwrap, unwrapVoid} from "../api/client";
import {PageHeading} from "../components/page-heading";
import {EmptyBlock, ErrorBlock, LoadingBlock} from "../components/query-state";
import {Badge} from "../components/ui/badge";
import {Button} from "../components/ui/button";
import {Dialog, DialogContent, DialogTrigger} from "../components/ui/dialog";
import {Field, Input, Textarea} from "../components/ui/field";
import {Panel} from "../components/ui/panel";
import {formatTime} from "../lib/utils";

type Certificate = components["schemas"]["Certificate"];

export function CertificatesPage() {
  const queryClient = useQueryClient();
  const certificates = useQuery({queryKey: ["certificates"], queryFn: async () => unwrap<{items?: Certificate[]; total?: number}>(await api.GET("/certs"))});
  const remove = useMutation({
    mutationFn: async (name: string) => unwrapVoid(await api.DELETE("/certs/{name}", {params: {path: {name}, header: csrfHeader}})),
    onSuccess: async () => queryClient.invalidateQueries({queryKey: ["certificates"]}),
  });
  if (certificates.isPending) return <LoadingBlock label="Reading managed certificates" />;
  if (certificates.isError) return <ErrorBlock message={certificates.error.message} retry={() => void certificates.refetch()} />;
  const items = certificates.data.items ?? [];
  return <div>
    <PageHeading eyebrow="Certificates / managed store" title="Private keys stay private." description="Upload a PEM pair once. The API returns public metadata and references, never the private key." action={<UploadCertificate onCreated={() => void queryClient.invalidateQueries({queryKey: ["certificates"]})} />} />
    {remove.error ? <div role="alert" className="mb-5 border-l-4 border-red-800 bg-red-50 p-4 text-sm text-red-900">{remove.error instanceof ManagementAPIError ? remove.error.message : remove.error.message}</div> : null}
    {items.length === 0 ? <EmptyBlock title="No managed certificates" description="Upload a matching certificate and private key pair, then reference it from a TLS listener." /> : <div className="grid gap-4 xl:grid-cols-2">{items.map((certificate) => <Panel key={certificate.name} className="group overflow-hidden"><div className="grid grid-cols-[6px_1fr]"><div className={new Date(certificate.notAfter ?? 0).getTime() - Date.now() < 30 * 86400_000 ? "bg-signal" : "bg-healthy"} /><div className="p-5"><div className="flex items-start justify-between gap-4"><div><p className="eyebrow text-muted">Managed certificate</p><h2 className="mt-2 font-display text-3xl">{certificate.name}</h2></div><FileKey2 className="text-signal" /></div><p className="mt-4 truncate font-mono text-xs text-muted" title={certificate.subject}>{certificate.subject ?? "Subject unavailable"}</p><div className="mt-5 flex flex-wrap gap-2">{certificate.sans.slice(0, 4).map((san) => <Badge key={san}>{san}</Badge>)}{certificate.sans.length > 4 ? <Badge>+{certificate.sans.length - 4}</Badge> : null}</div><div className="mt-6 grid gap-3 border-t border-line pt-4 text-xs sm:grid-cols-2"><div className="flex items-center gap-2 text-muted"><CalendarClock size={14} /> Expires {formatTime(certificate.notAfter)}</div><div className="flex items-center gap-2 text-muted"><ShieldAlert size={14} /> {certificate.references.length ? certificate.references.join(", ") : "Not referenced"}</div></div><Button className="mt-5" size="sm" variant="danger" disabled={certificate.references.length > 0 || remove.isPending} onClick={() => remove.mutate(certificate.name)} title={certificate.references.length ? "Remove listener references before deletion" : "Delete certificate"}><Trash2 size={14} /> Delete</Button></div></div></Panel>)}</div>}
  </div>;
}

function UploadCertificate({onCreated}: {onCreated: () => void}) {
  const [name, setName] = useState("");
  const [certificatePem, setCertificate] = useState("");
  const [privateKeyPem, setKey] = useState("");
  const create = useMutation({
    mutationFn: async () => unwrap<Certificate>(await api.POST("/certs", {params: {header: csrfHeader}, body: {name, certificatePem, privateKeyPem}})),
    onSuccess: () => { setName(""); setCertificate(""); setKey(""); onCreated(); },
  });
  return <Dialog><DialogTrigger asChild><Button><Plus size={16} /> Upload certificate</Button></DialogTrigger><DialogContent title="Add a managed certificate" description="The pair is validated before an atomic write. The private key cannot be retrieved later."><div className="grid gap-5"><Field label="Reference name" hint="Lowercase DNS-style name used by Listener TLS refs."><Input value={name} pattern="[a-z0-9]([-a-z0-9]*[a-z0-9])?" onChange={(event) => setName(event.target.value)} placeholder="api-example-com" /></Field><Field label="Certificate chain (PEM)"><Textarea className="min-h-36" value={certificatePem} onChange={(event) => setCertificate(event.target.value)} placeholder="-----BEGIN CERTIFICATE-----" /></Field><Field label="Private key (PEM)" hint="Sent once over this same-origin session and stored with mode 0600."><Textarea className="min-h-36" value={privateKeyPem} onChange={(event) => setKey(event.target.value)} placeholder="-----BEGIN PRIVATE KEY-----" /></Field>{create.error ? <p role="alert" className="text-sm text-red-800">{create.error.message}</p> : null}<Button size="lg" onClick={() => create.mutate()} disabled={create.isPending || !name || !certificatePem || !privateKeyPem}>{create.isPending ? "Validating pair…" : "Validate & store"}</Button></div></DialogContent></Dialog>;
}
