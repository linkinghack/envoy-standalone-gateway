import Editor, {loader} from "@monaco-editor/react";
import * as monaco from "monaco-editor/esm/vs/editor/editor.api";
import editorWorker from "monaco-editor/esm/vs/editor/editor.worker?worker";
import {configureMonacoYaml} from "monaco-yaml";
import yamlWorker from "monaco-yaml/yaml.worker.js?worker";

declare global {
  interface Window {
    MonacoEnvironment?: {getWorker: (_moduleId: string, label: string) => Worker};
  }
}

window.MonacoEnvironment = {
  getWorker(_moduleId, label) {
    return label === "yaml" ? new yamlWorker() : new editorWorker();
  },
};
loader.config({monaco});
configureMonacoYaml(monaco, {
  enableSchemaRequest: false,
  validate: true,
  format: {},
  hover: true,
  completion: true,
  schemas: [],
});

export default function CodeEditor({value, onChange, language = "yaml", readOnly = false}: {value: string; onChange?: (value: string) => void; language?: string; readOnly?: boolean}) {
  return <Editor
    height="min(64vh, 720px)"
    language={language}
    value={value}
    onChange={(next) => onChange?.(next ?? "")}
    theme="vs-dark"
    loading={<div className="grid min-h-[420px] place-items-center bg-ink text-paper"><span className="eyebrow text-white/50">Loading editor</span></div>}
    options={{
      readOnly,
      minimap: {enabled: false},
      fontFamily: "IBM Plex Mono",
      fontSize: 13,
      lineHeight: 21,
      padding: {top: 18, bottom: 18},
      scrollBeyondLastLine: false,
      wordWrap: "on",
      automaticLayout: true,
      renderLineHighlight: "gutter",
    }}
  />;
}
