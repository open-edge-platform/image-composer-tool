import React, { createContext, useContext, useState, useCallback } from 'react';

const DEFAULT_YAML = `# Image Composer Template
# Edit your template YAML here. Validation runs automatically.
image:
  name: my-custom-image
  version: "1.0"
target:
  os: wind-river-elxr
  dist: elxr12
  arch: x86_64
  imageType: raw
`;

const EditorContext = createContext(null);

export function EditorProvider({ children }) {
  const [editorValue, setEditorValue] = useState(DEFAULT_YAML);

  const sendToEditor = useCallback((yaml) => {
    setEditorValue(yaml);
  }, []);

  return (
    <EditorContext.Provider value={{ editorValue, setEditorValue, sendToEditor }}>
      {children}
    </EditorContext.Provider>
  );
}

export const useEditor = () => useContext(EditorContext);
