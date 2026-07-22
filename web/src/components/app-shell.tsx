import {useMutation, useQueryClient} from "@tanstack/react-query";
import {Activity, Braces, Cable, FileKey2, Gauge, LogOut, Menu, ServerCog, X} from "lucide-react";
import {useState, type ReactNode} from "react";
import {NavLink} from "react-router-dom";
import {api, csrfHeader, unwrapVoid} from "../api/client";
import {authKeys} from "../features/auth/auth-boundary";
import {cn} from "../lib/utils";
import {Button} from "./ui/button";

const navigation = [
  {to: "/", label: "Overview", code: "01", icon: Gauge},
  {to: "/configuration", label: "Configuration", code: "02", icon: Braces},
  {to: "/runtime", label: "Runtime", code: "03", icon: Activity},
  {to: "/certificates", label: "Certificates", code: "04", icon: FileKey2},
  {to: "/expert", label: "Expert", code: "05", icon: Cable},
  {to: "/system", label: "System", code: "06", icon: ServerCog},
];

export function AppShell({children}: {children: ReactNode}) {
  const [open, setOpen] = useState(false);
  const queryClient = useQueryClient();
  const logout = useMutation({
    mutationFn: async () => unwrapVoid(await api.POST("/auth/logout", {params: {header: csrfHeader}})),
    onSuccess: () => {
      queryClient.removeQueries();
      window.location.assign("/");
    },
  });
  return <div className="noise min-h-screen bg-canvas text-ink lg:grid lg:grid-cols-[272px_1fr]">
    <header className="sticky top-0 z-30 flex h-16 items-center justify-between border-b border-white/15 bg-ink px-4 text-paper lg:hidden">
      <Brand compact />
      <Button variant="ghost" size="icon" aria-label={open ? "Close navigation" : "Open navigation"} onClick={() => setOpen((value) => !value)}>{open ? <X /> : <Menu />}</Button>
    </header>
    <aside className={cn("fixed inset-x-0 top-16 z-20 h-[calc(100vh-4rem)] -translate-x-full bg-ink text-paper transition-transform lg:sticky lg:top-0 lg:h-screen lg:translate-x-0", open && "translate-x-0")}>
      <div className="flex h-full flex-col">
        <div className="hidden border-b border-white/15 p-7 lg:block"><Brand /></div>
        <nav aria-label="Primary" className="flex-1 overflow-auto py-5">
          {navigation.map(({to, label, code, icon: Icon}) => <NavLink key={to} to={to} end={to === "/"} onClick={() => setOpen(false)} className={({isActive}) => cn("group grid grid-cols-[32px_1fr_20px] items-center gap-3 border-l-4 border-transparent px-6 py-4 text-sm text-white/65 transition hover:bg-white/[.055] hover:text-white", isActive && "border-signal bg-white/[.07] text-white")}>
            <span className="font-mono text-[.62rem] text-white/35">{code}</span><span className="flex items-center gap-3 font-medium"><Icon size={17} /> {label}</span><span className="h-px bg-white/20 transition group-hover:bg-signal" />
          </NavLink>)}
        </nav>
        <div className="border-t border-white/15 p-5">
          <div className="mb-4 flex items-center gap-3 px-1"><span className="relative flex h-2.5 w-2.5"><span className="absolute inline-flex h-full w-full animate-ping bg-healthy opacity-50" /><span className="relative h-2.5 w-2.5 bg-healthy" /></span><span className="eyebrow text-white/50">Management live</span></div>
          <Button className="w-full justify-start" variant="ghost" onClick={() => logout.mutate()} disabled={logout.isPending}><LogOut size={16} /> End session</Button>
        </div>
      </div>
    </aside>
    <main className="canvas-grid min-w-0 px-4 py-7 md:px-8 md:py-10 xl:px-12 xl:py-12">{children}</main>
  </div>;
}

function Brand({compact = false}: {compact?: boolean}) {
  return <div className="flex items-center gap-3"><span className="grid h-9 w-9 place-items-center border border-signal bg-signal font-display text-xl font-semibold text-white">E</span><div><div className="font-display text-xl font-semibold leading-none">Signal Desk</div>{compact ? null : <div className="mt-1 font-mono text-[.58rem] uppercase tracking-[.18em] text-white/40">Envoy standalone gateway</div>}</div></div>;
}
