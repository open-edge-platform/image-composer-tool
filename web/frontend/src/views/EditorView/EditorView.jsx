import React, { useEffect, useRef, useState, useCallback, useMemo } from 'react';
import CodeMirror from '@uiw/react-codemirror';
import { yaml } from '@codemirror/lang-yaml';
import { linter, lintGutter } from '@codemirror/lint';
import { oneDark } from '@codemirror/theme-one-dark';
import { useEditor } from '../../hooks/EditorContext';
import { useChatContext } from '../../hooks/ChatContext';
import { validateTemplate } from '../../api/templates';
import styles from './EditorView.module.css';

export function EditorView() {
  const { editorValue, setEditorValue } = useEditor();
  const { isStreaming } = useChatContext();
  const [validationErrors, setValidationErrors] = useState([]);
  const [isValid, setIsValid] = useState(null);
  const [isValidating, setIsValidating] = useState(false);
  
  const debounceTimerRef = useRef(null);
  const errorsRef = useRef([]); // Used to keep linter function stable

  // Keep errorsRef in sync with state for the CodeMirror linter
  useEffect(() => {
    errorsRef.current = validationErrors;
  }, [validationErrors]);

  const runValidation = useCallback(async (yamlContent, skipValidation) => {
    if (skipValidation || !yamlContent.trim()) {
      setValidationErrors([]);
      setIsValid(null);
      return;
    }

    setIsValidating(true);
    try {
      const result = await validateTemplate(yamlContent);
      setIsValid(result.valid);
      setValidationErrors(result.errors || []);
    } catch (err) {
      console.error('Validation request failed:', err);
      setValidationErrors([{ path: '/', message: 'Validation service unavailable' }]);
      setIsValid(null);
    } finally {
      setIsValidating(false);
    }
  }, []);

  const handleChange = useCallback((value) => {
    setEditorValue(value);
  }, [setEditorValue]);

  useEffect(() => {
    if (debounceTimerRef.current) {
      clearTimeout(debounceTimerRef.current);
    }
    debounceTimerRef.current = setTimeout(() => {
      runValidation(editorValue, isStreaming);
    }, 500);

    return () => {
      if (debounceTimerRef.current) {
        clearTimeout(debounceTimerRef.current);
      }
    };
  }, [editorValue, runValidation, isStreaming]);

  // Stable linter extension that reads from the ref
  const yamlLinter = useMemo(() => {
    return linter((view) => {
      return errorsRef.current.map((err) => {
        const lineNum = findLineForPath(view.state.doc.toString(), err.path);
        const line = view.state.doc.line(lineNum);
        return {
          from: line.from,
          to: line.to,
          severity: 'error',
          message: err.message,
          source: err.path,
        };
      });
    }, { delay: 0 });
  }, []);

  return (
    <div className={styles.container}>
      <div className={styles.header}>
        <h2 className={styles.title}>Template Editor</h2>
        <div className={styles.statusBar}>
          {isStreaming && <span className={styles.validating}>AI is generating...</span>}
          {!isStreaming && isValidating && <span className={styles.validating}>Validating…</span>}
          {!isStreaming && !isValidating && isValid === true && (
            <span className={styles.statusValid}>✓ Valid</span>
          )}
          {!isStreaming && !isValidating && isValid === false && (
            <span className={styles.statusInvalid}>
              ✕ {validationErrors.length} error{validationErrors.length !== 1 ? 's' : ''}
            </span>
          )}
        </div>
      </div>

      <div className={styles.editorWrapper}>
        <CodeMirror
          value={editorValue}
          onChange={handleChange}
          readOnly={isStreaming}
          height="100%"
          theme={oneDark}
          extensions={[yaml(), lintGutter(), yamlLinter]}
          basicSetup={{
            lineNumbers: true,
            foldGutter: true,
            bracketMatching: true,
            indentOnInput: true,
          }}
          className={styles.codeMirror}
        />
      </div>

      {validationErrors.length > 0 && (
        <div className={styles.errorPanel}>
          <div className={styles.errorPanelHeader}>
            Validation Errors ({validationErrors.length})
          </div>
          <ul className={styles.errorList}>
            {validationErrors.map((err, idx) => (
              <li key={idx} className={styles.errorItem}>
                <span className={styles.errorPath}>{err.path}</span>
                <span className={styles.errorMessage}>{err.message}</span>
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  );
}

function findLineForPath(yamlText, jsonPath) {
  if (!jsonPath || jsonPath === '/') return 1;

  const parts = jsonPath.split('/').filter(Boolean);
  const lastKey = parts[parts.length - 1];
  const lines = yamlText.split('\n');

  for (let i = 0; i < lines.length; i++) {
    if (lines[i].trimStart().startsWith(lastKey + ':')) {
      return i + 1;
    }
  }

  return 1;
}
