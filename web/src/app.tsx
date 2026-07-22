import {Navigate, Route, Routes} from "react-router-dom";
import {AppShell} from "./components/app-shell";
import {AuthBoundary} from "./features/auth/auth-boundary";
import {CertificatesPage} from "./pages/certificates-page";
import {ConfigurationPage} from "./pages/configuration-page";
import {ExpertPage} from "./pages/expert-page";
import {OverviewPage} from "./pages/overview-page";
import {RuntimePage} from "./pages/runtime-page";
import {SystemPage} from "./pages/system-page";

export function App() {
  return <AuthBoundary><AppShell><Routes><Route path="/" element={<OverviewPage />} /><Route path="/configuration" element={<ConfigurationPage />} /><Route path="/runtime" element={<RuntimePage />} /><Route path="/certificates" element={<CertificatesPage />} /><Route path="/expert" element={<ExpertPage />} /><Route path="/system" element={<SystemPage />} /><Route path="*" element={<Navigate to="/" replace />} /></Routes></AppShell></AuthBoundary>;
}
