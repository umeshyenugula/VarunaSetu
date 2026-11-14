import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import './index.css'
import App from './App.tsx'
import { docsUrl } from './config.ts'

if (window.location.pathname.replace(/\/+$/, '') === docsUrl().replace(/\/+$/, '')) {
  window.location.replace(docsUrl() + window.location.search + window.location.hash)
}

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
