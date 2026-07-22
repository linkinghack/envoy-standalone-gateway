import {useMutation, useQuery, useQueryClient} from "@tanstack/react-query";
import type {ReactNode} from "react";
import type {components} from "../../api/generated";
import {api, csrfHeader, ManagementAPIError, unwrap} from "../../api/client";
import {AuthPage} from "./auth-page";

type BootstrapState = components["schemas"]["BootstrapState"];
type Session = components["schemas"]["Session"];

export const authKeys = {bootstrap: ["auth", "bootstrap"] as const, session: ["auth", "session"] as const};

export function AuthBoundary({children}: {children: ReactNode}) {
  const queryClient = useQueryClient();
  const bootstrap = useQuery({
    queryKey: authKeys.bootstrap,
    queryFn: async () => unwrap<BootstrapState>(await api.GET("/auth/bootstrap")),
    retry: 1,
  });
  const session = useQuery({
    queryKey: authKeys.session,
    queryFn: async () => unwrap<Session>(await api.GET("/auth/session")),
    enabled: bootstrap.data?.required === false,
    retry: false,
  });
  const authenticate = useMutation({
    mutationFn: async ({mode, username, password}: {mode: "bootstrap" | "login"; username: string; password: string}) => {
      const result = mode === "bootstrap"
        ? await api.POST("/auth/bootstrap", {params: {header: csrfHeader}, body: {username, password}})
        : await api.POST("/auth/login", {params: {header: csrfHeader}, body: {username, password}});
      return unwrap<Session>(result);
    },
    onSuccess: (data) => {
      queryClient.setQueryData(authKeys.bootstrap, {...bootstrap.data, required: false});
      queryClient.setQueryData(authKeys.session, data);
    },
  });

  if (bootstrap.isPending) return <BootScreen label="Reading gateway bootstrap state" />;
  if (bootstrap.isError) return <BootFailure error={bootstrap.error} retry={() => void bootstrap.refetch()} />;
  if (bootstrap.data.required) return <AuthPage mode="bootstrap" onSubmit={(username, password) => authenticate.mutateAsync({mode: "bootstrap", username, password}).then(() => undefined)} />;
  if (session.isPending) return <BootScreen label="Restoring secure session" />;
  if (session.isError) return <AuthPage mode="login" onSubmit={(username, password) => authenticate.mutateAsync({mode: "login", username, password}).then(() => undefined)} />;
  return children;
}

function BootScreen({label}: {label: string}) {
  return <main className="grid min-h-screen place-items-center bg-ink text-paper"><div className="text-center"><div className="mx-auto h-10 w-10 animate-spin border-2 border-white/20 border-t-signal" /><p className="eyebrow mt-5 text-white/60">{label}</p></div></main>;
}

function BootFailure({error, retry}: {error: Error; retry: () => void}) {
  const message = error instanceof ManagementAPIError ? error.message : "The gateway could not be reached.";
  return <main className="grid min-h-screen place-items-center bg-canvas p-6"><div className="max-w-md border border-ink bg-paper p-8"><p className="eyebrow text-signal">Connection fault</p><h1 className="mt-3 font-display text-4xl">Signal desk is offline</h1><p className="mt-4 text-sm text-muted">{message}</p><button className="mt-6 border border-ink px-4 py-2 font-semibold" onClick={retry}>Try again</button></div></main>;
}
