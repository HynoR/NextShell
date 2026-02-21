import { useCallback, useEffect, useRef, useState } from "react";
import { message } from "antd";
import type { SessionDescriptor } from "@nextshell/core";
import { monaco } from "../monaco-setup";
import "../languages/toml";
import { detectLanguage } from "../utils/detectLanguage";
import { useEditorTabStore } from "../store/useEditorTabStore";

interface EditorPaneProps {
  session: SessionDescriptor;
}

export const EditorPane = ({ session }: EditorPaneProps) => {
  const containerRef = useRef<HTMLDivElement>(null);
  const editorRef = useRef<monaco.editor.IStandaloneCodeEditor | null>(null);
  const tab = useEditorTabStore((s) => s.getTab(session.id));
  const setDirty = useEditorTabStore((s) => s.setDirty);
  const setSaving = useEditorTabStore((s) => s.setSaving);
  const [saveStatus, setSaveStatus] = useState<"saved" | "saving" | "unsaved">("saved");

  const handleSave = useCallback(async () => {
    if (!tab || !editorRef.current) return;
    const content = editorRef.current.getValue();
    setSaving(session.id, true);
    setSaveStatus("saving");
    try {
      await window.nextshell.sftp.editSaveBuiltin({
        editId: tab.editId,
        connectionId: tab.connectionId,
        remotePath: tab.remotePath,
        content
      });
      setDirty(session.id, false);
      setSaveStatus("saved");
      message.success({ content: `已保存: ${tab.remotePath.split("/").pop()}`, duration: 2 });
    } catch (err) {
      message.error(`保存失败: ${err instanceof Error ? err.message : String(err)}`);
      setSaveStatus("unsaved");
    } finally {
      setSaving(session.id, false);
    }
  }, [tab, session.id, setDirty, setSaving]);

  useEffect(() => {
    if (!containerRef.current || !tab) return;

    const language = detectLanguage(tab.remotePath);
    const editor = monaco.editor.create(containerRef.current, {
      value: tab.initialContent,
      language,
      theme: "vs-dark",
      automaticLayout: true,
      fontSize: 14,
      wordWrap: "on",
      minimap: { enabled: true },
      scrollBeyondLastLine: false,
      renderWhitespace: "selection",
      tabSize: 2,
    });

    editorRef.current = editor;

    editor.onDidChangeModelContent(() => {
      setDirty(session.id, true);
      setSaveStatus("unsaved");
    });

    // Ctrl/Cmd+S
    editor.addCommand(monaco.KeyMod.CtrlCmd | monaco.KeyCode.KeyS, () => {
      void handleSave();
    });

    return () => {
      editor.dispose();
      editorRef.current = null;
    };
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [tab?.sessionId]);

  if (!tab) {
    return <div className="flex-1 flex items-center justify-center text-[var(--t3)]">编辑器数据加载中...</div>;
  }

  const fileName = tab.remotePath.split("/").pop() ?? tab.remotePath;

  return (
    <div className="flex-1 min-h-0 flex flex-col">
      <div className="editor-toolbar">
        <span className="editor-toolbar-path" title={tab.remotePath}>
          <i className="ri-file-code-line" aria-hidden="true" />
          {fileName}
        </span>
        <span className="editor-toolbar-status">
          {saveStatus === "saving" && <><i className="ri-loader-4-line ri-spin" aria-hidden="true" /> 保存中...</>}
          {saveStatus === "saved" && <><i className="ri-check-line" aria-hidden="true" /> 已保存</>}
          {saveStatus === "unsaved" && <><i className="ri-edit-circle-line" aria-hidden="true" /> 未保存</>}
        </span>
        <button
          className="editor-toolbar-save-btn"
          onClick={() => void handleSave()}
          disabled={!tab.dirty || tab.saving}
          title="保存 (Ctrl+S)"
        >
          <i className="ri-save-line" aria-hidden="true" />
          保存
        </button>
      </div>
      <div ref={containerRef} className="flex-1 min-h-0" />
    </div>
  );
};
