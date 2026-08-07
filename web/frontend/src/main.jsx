import React from 'react';
import { createRoot } from 'react-dom/client';
import { BrowserRouter, Routes, Route } from 'react-router-dom';
import './index.css';

import App from './App';
import { ChatView } from './views/ChatView/ChatView';
import { EditorView } from './views/EditorView/EditorView';
import { TemplateLibraryView } from './views/TemplateLibraryView/TemplateLibraryView';
import { BuildDashboardView } from './views/BuildDashboardView/BuildDashboardView';

createRoot(document.getElementById('root')).render(
  <React.StrictMode>
    <BrowserRouter>
      <Routes>
        <Route path="/" element={<App />}>
          <Route index element={<ChatView />} />
          <Route path="editor" element={<EditorView />} />
          <Route path="templates" element={<TemplateLibraryView />} />
          <Route path="builds" element={<BuildDashboardView />} />
        </Route>
      </Routes>
    </BrowserRouter>
  </React.StrictMode>
);
