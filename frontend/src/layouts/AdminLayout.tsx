import { Outlet, Link, useLocation } from "react-router-dom"
import {
  Activity,
  Image,
  Key,
  LayoutDashboard,
  Menu,
  MessageSquare,
  ScrollText,
  Settings,
  Video,
  X,
  ChevronDown,
  Search,
} from "lucide-react"
import { useState } from "react"

const navItems = [
  { name: "System status", localName: "Status sistem", path: "/", icon: LayoutDashboard },
  { name: "Accounts", localName: "Manajemen akun", path: "/accounts", icon: Activity },
  { name: "API keys", localName: "API key", path: "/tokens", icon: Key },
  { name: "Activity logs", localName: "Log aktivitas", path: "/logs", icon: ScrollText },
  { name: "API playground", localName: "Uji coba API", path: "/test", icon: MessageSquare },
  { name: "Images", localName: "Generate gambar", path: "/images", icon: Image },
  { name: "Videos", localName: "Generate video", path: "/videos", icon: Video },
]

const utilityItems = [{ name: "System settings", path: "/settings", icon: Settings }]

export default function AdminLayout() {
  const loc = useLocation()
  const [mobileOpen, setMobileOpen] = useState(false)
  const activeItem = [...navItems, ...utilityItems].find(item => item.path === loc.pathname) || navItems[0]

  return (
    <div className="canvas-app">
      {mobileOpen && <button aria-label="Close navigation" className="canvas-overlay" onClick={() => setMobileOpen(false)} />}
      <aside className={`canvas-sidebar ${mobileOpen ? "is-open" : ""}`}>
        <div className="canvas-brand-row">
          <Link to="/" className="canvas-brand" onClick={() => setMobileOpen(false)}>
            <span className="canvas-brand-mark">↗</span>
            <span>gemini2<span>API</span><small> gateway studio</small></span>
          </Link>
          <button className="canvas-close" onClick={() => setMobileOpen(false)} aria-label="Close navigation"><X size={17} /></button>
        </div>
        <div className="workspace-switcher">
          <span className="workspace-avatar">N</span>
          <span><strong>Northstar</strong><small>Production</small></span>
          <ChevronDown size={14} />
        </div>
        <div className="canvas-sidebar-tools">
          <div><Search size={14} /> Workspace navigation <kbd>⌘ K</kbd></div>
        </div>
        <div className="canvas-section-label">Workspace</div>
        <nav className="canvas-nav">
          {navItems.map(item => {
            const active = loc.pathname === item.path
            return <Link key={item.path} to={item.path} onClick={() => setMobileOpen(false)} className={`canvas-nav-item ${active ? "is-active" : ""}`}>
              <span className="canvas-nav-icon"><item.icon size={15} /></span>
              <span className="canvas-nav-text"><strong>{item.name}</strong><small>{item.localName}</small></span>
              {active && <span className="canvas-nav-arrow">↗</span>}
            </Link>
          })}
        </nav>
        <div className="canvas-section-label build-label">Configure</div>
        {utilityItems.map(item => <Link key={item.path} to={item.path} onClick={() => setMobileOpen(false)} className={`canvas-nav-item ${loc.pathname === item.path ? "is-active" : ""}`}><span className="canvas-nav-icon"><item.icon size={15} /></span><span className="canvas-nav-text"><strong>{item.name}</strong><small>{item.path.slice(1)}</small></span></Link>)}
        <div className="canvas-sidebar-footer">
          <div className="backend-status"><span className="status-dot" /><div><strong>Runtime status</strong><small>Connect backend to inspect</small></div></div>
        </div>
      </aside>
      <main className="canvas-main">
        <header className="canvas-topbar">
          <div className="canvas-topbar-left">
            <button className="canvas-menu" onClick={() => setMobileOpen(true)} aria-label="Open navigation"><Menu size={18} /></button>
            <div className="canvas-breadcrumb"><span>Northstar</span><ChevronDown size={13} /><span>/</span><strong>{activeItem.name}</strong></div>
          </div>
          <div className="canvas-topbar-right">
            <span className="environment-pill"><span className="status-dot" /> Production</span>
            <div className="canvas-avatar">NR</div>
          </div>
        </header>
        <div className="canvas-page-meta">
          <div><span className="mono-kicker">Workspace / {activeItem.path === "/" ? "overview" : activeItem.path.slice(1)}</span><h1>{activeItem.name}</h1></div>
          <div className="page-meta-actions"><span className="sync-label">Live status</span></div>
        </div>
        <section className="canvas-content"><Outlet /></section>
      </main>
    </div>
  )
}
