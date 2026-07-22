import {KeyRound, ShieldCheck} from "lucide-react";
import {useState, type FormEvent} from "react";
import {Button} from "../../components/ui/button";
import {Field, Input} from "../../components/ui/field";
import {ManagementAPIError} from "../../api/client";

export function AuthPage({mode, onSubmit}: {mode: "bootstrap" | "login"; onSubmit: (username: string, password: string) => Promise<void>}) {
  const [username, setUsername] = useState("admin");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [pending, setPending] = useState(false);
  const bootstrap = mode === "bootstrap";

  async function submit(event: FormEvent) {
    event.preventDefault();
    setPending(true);
    setError("");
    try {
      await onSubmit(username, password);
    } catch (caught) {
      setError(caught instanceof ManagementAPIError ? caught.message : "Authentication failed. Try again.");
    } finally {
      setPending(false);
    }
  }

  return <main className="noise grid min-h-screen bg-ink text-paper lg:grid-cols-[1.08fr_.92fr]">
    <section className="relative hidden overflow-hidden border-r border-white/15 p-12 lg:flex lg:flex-col lg:justify-between">
      <div className="absolute inset-0 opacity-25 [background-image:linear-gradient(rgba(255,255,255,.12)_1px,transparent_1px),linear-gradient(90deg,rgba(255,255,255,.12)_1px,transparent_1px)] [background-size:48px_48px]" />
      <div className="relative">
        <div className="eyebrow flex items-center gap-3 text-signal"><span className="h-2 w-2 bg-signal" /> ESGW / SIGNAL DESK</div>
        <h1 className="mt-16 max-w-xl font-display text-7xl font-semibold leading-[.88] tracking-[-.04em]">Keep the edge<br /><em className="text-signal">legible.</em></h1>
      </div>
      <div className="relative grid max-w-lg grid-cols-2 gap-px border border-white/20 bg-white/20">
        <div className="bg-ink p-5"><p className="eyebrow text-white/45">Control</p><p className="mt-3 text-sm">One source of truth.<br />Every change reviewable.</p></div>
        <div className="bg-ink p-5"><p className="eyebrow text-white/45">Observe</p><p className="mt-3 text-sm">Actual Envoy state.<br />No guessed health.</p></div>
      </div>
    </section>
    <section className="canvas-grid flex min-h-screen items-center justify-center p-6 text-ink md:p-12">
      <form onSubmit={submit} className="w-full max-w-md animate-signal-in border border-ink bg-paper p-7 shadow-[8px_8px_0_rgba(16,23,22,.18)] md:p-10">
        <div className="mb-10 flex items-start justify-between gap-4">
          <div><p className="eyebrow text-signal">{bootstrap ? "First run" : "Secure session"}</p><h2 className="mt-3 font-display text-4xl font-semibold">{bootstrap ? "Claim this gateway" : "Welcome back"}</h2></div>
          <div className="grid h-12 w-12 place-items-center border border-ink bg-ink text-paper">{bootstrap ? <ShieldCheck /> : <KeyRound />}</div>
        </div>
        <p className="mb-8 text-sm leading-6 text-muted">{bootstrap ? "Create the local administrator. This one-time window closes 30 minutes after startup." : "Use your local administrator account. Sessions stay on this gateway."}</p>
        <div className="grid gap-5">
          <Field label="Username"><Input autoComplete="username" value={username} onChange={(event) => setUsername(event.target.value)} required /></Field>
          <Field label={bootstrap ? "New password" : "Password"} hint={bootstrap ? "Use at least 12 characters." : undefined}><Input type="password" autoComplete={bootstrap ? "new-password" : "current-password"} minLength={bootstrap ? 12 : undefined} value={password} onChange={(event) => setPassword(event.target.value)} required /></Field>
        </div>
        {error ? <div role="alert" className="mt-5 border-l-4 border-red-700 bg-red-50 px-4 py-3 text-sm text-red-900">{error}</div> : null}
        <Button type="submit" size="lg" className="mt-7 w-full" disabled={pending}>{pending ? "Establishing session…" : bootstrap ? "Create administrator" : "Enter signal desk"}</Button>
        <p className="mt-6 text-center font-mono text-[.66rem] uppercase tracking-widest text-muted">Cookie session · same-origin · no cloud account</p>
      </form>
    </section>
  </main>;
}
