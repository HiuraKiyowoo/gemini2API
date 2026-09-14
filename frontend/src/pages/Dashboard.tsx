import { useEffect, useState, type ReactNode } from "react"
import { Activity, ArrowUpRight, CircleAlert, Clock3, Cpu, Database, FileJson, Gauge, Globe2, ImageIcon, Layers3, Radio, RefreshCw, Server, Shield, ShieldCheck, Sparkles, Zap } from "lucide-react"
import { toast } from "sonner"
import { getAuthHeader } from "../lib/auth"
import { API_BASE } from "../lib/api"

type AccountRow = { email: string; status: string; inflight: number; max_inflight: number; consecutive_failures: number; rate_limit_strikes: number }
type Status = { accounts?: { total?: number; valid?: number; rate_limited?: number; invalid?: number; in_use?: number; global_in_use?: number; waiting?: number; max_inflight_per_account?: number; max_queue_size?: number }; per_account?: AccountRow[]; chat_id_pool?: { total_cached?: number; target_per_account?: number; ttl_seconds?: number } | null; runtime?: { mode?: string }; request_runtime?: { mode?: string }; browser_automation?: { mode?: string } }

const endpointRows = [
  { icon: Globe2, path: "POST /v1beta/models/{model}:generateContent", tag: "Gemini", tone: "amber", sample: '{ "contents": [] }' },
  { icon: FileJson, path: "POST /v1/chat/completions", tag: "OpenAI", tone: "green", sample: '{ "model": "gemini-3-flash-preview" }' },
  { icon: Cpu, path: "POST /v1/messages", tag: "Anthropic", tone: "blue", sample: '{ "model": "gemini-3-flash-preview" }' },
  { icon: ImageIcon, path: "POST /v1/images/generations", tag: "Imagen", tone: "rose", sample: '{ "prompt": "..." }' },
  { icon: Shield, path: "GET /healthz / readyz", tag: "Probe", tone: "orange", sample: '{ "status": "ok" }' },
]

export default function Dashboard() {
  const [status, setStatus] = useState<Status | null>(null)
  const [errOnce, setErrOnce] = useState(false)

  useEffect(() => {
    const fetchStatus = async () => {
      try {
        const res = await fetch(`${API_BASE}/api/admin/status`, { headers: getAuthHeader() })
        if (!res.ok) throw new Error("Unauthorized")
        setStatus(await res.json())
      } catch {
        if (!errOnce) { toast.error("Could not fetch runtime status. Check your session key in System settings."); setErrOnce(true) }
      }
    }
    void fetchStatus()
    const timer = window.setInterval(fetchStatus, 3000)
    return () => window.clearInterval(timer)
  }, [errOnce])

  const acc = status?.accounts || {}
  const pool = status?.chat_id_pool
  const rows = status?.per_account || []
  const requestRuntime = status?.request_runtime
  const browserRuntime = status?.browser_automation

  return <div className="space-y-5">
    <div className="request-workspace-grid">
      <section className="request-composer panel-surface">
        <div className="request-composer-head"><div><span className="mono-kicker">Quick request / playground</span><h2>Test your gateway in context.</h2><p>Compose an OpenAI-compatible request without leaving the runtime overview.</p></div><button className="icon-action" onClick={() => toast.info("Request saved to your collection")}><Layers3 size={15} /></button></div>
        <div className="request-toolbar"><select defaultValue="POST" aria-label="Request method"><option>POST</option><option>GET</option><option>PUT</option></select><input aria-label="Endpoint" value="/v1/chat/completions" readOnly /><button className="send-button" onClick={() => toast.info("Use API playground for a live request")}>Send <ArrowUpRight size={13} /></button></div>
        <div className="editor-tabs"><button className="active">Body</button><button>Headers <span>4</span></button><button>Params <span>2</span></button><button>Auth</button></div>
        <div className="code-block"><div className="line-numbers">1</div><pre><span className="token-comment">// Request body is intentionally empty in overview.</span>{"\n"}<span className="token-comment">// Open API playground to compose a live request.</span></pre></div>
      </section>
      <aside className="request-details panel-surface"><div className="panel-title-row"><div><span className="mono-kicker">Current environment</span><h3>Production</h3></div><span className="status-tag"><span className="status-dot" /> Live</span></div><div className="detail-list"><Detail icon={<Server size={14} />} label="Gateway" value={status?.request_runtime?.mode || "Not reported"} /><Detail icon={<ShieldCheck size={14} />} label="Authentication" value={getAuthHeader().Authorization ? "Session key" : "Not configured"} /><Detail icon={<Database size={14} />} label="Pool status" value={status ? `${acc.valid ?? 0} valid accounts` : "Not reported"} /><Detail icon={<Clock3 size={14} />} label="Last deploy" value="Not reported" /></div><div className="details-note"><Sparkles size={14} /><span>No sample response is shown here. Use API playground for a live request.</span></div></aside>
    </div>

    <section className="runtime-heading"><div><span className="mono-kicker">Runtime overview / live</span><h2>Everything behind the request.</h2><p>Refreshes automatically every 3 seconds from the Gemini gateway.</p></div><div className="runtime-badges"><span>gemini upstream</span><span>model router</span><span>openai compatible</span></div></section>
    <div className="metric-grid"> <Metric icon={<Server />} label="Valid accounts" value={String(acc.valid ?? 0)} sub={`of ${acc.total ?? 0} connected`} tone="green" /><Metric icon={<Activity />} label="In flight" value={String(acc.in_use ?? 0)} sub={`global cap ${acc.global_in_use ?? 0}`} tone="blue" /><Metric icon={<CircleAlert />} label="Queue" value={String(acc.waiting ?? 0)} sub={`max ${acc.max_queue_size ?? 0} waiting`} tone="rose" /><Metric icon={<Gauge />} label="Rate limits" value={`${acc.rate_limited ?? 0} / ${acc.invalid ?? 0}`} sub="rate limited / failed" tone="amber" /></div>
    <div className="runtime-cards"><InfoCard icon={<Zap />} title="Chat_ID pre-warm" value={pool ? String(pool.total_cached ?? 0) : "Not reported"} description={pool ? `Target ${pool.target_per_account} per account · TTL ${Math.round((pool.ttl_seconds || 0) / 60)} min` : "No pool data returned by backend."} /><InfoCard icon={<Radio />} title="Request runtime" value={requestRuntime?.mode || "Not reported"} description="Runtime mode is read from the connected backend." /><InfoCard icon={<RefreshCw />} title="Browser automation" value={browserRuntime?.mode || "Not reported"} description="Automation mode is read from the connected backend." /></div>
    {rows.length > 0 && <section className="table-surface panel-surface"><div className="table-head"><div><span className="mono-kicker">Account pool / diagnostics</span><h3>Concurrency by account</h3></div><button className="text-action" onClick={() => toast.info("Account diagnostics refreshed")}><RefreshCw size={14} /> Refresh</button></div><div className="table-scroll"><table><thead><tr><th>Email</th><th>Status</th><th>In-flight</th><th>Failures</th><th>Rate limits</th></tr></thead><tbody>{rows.map(row => <tr key={row.email}><td><code>{row.email}</code></td><td><span className={statusBadgeClass(row.status)}><span />{row.status}</span></td><td>{row.inflight}<span className="table-muted">/{row.max_inflight}</span></td><td>{row.consecutive_failures}</td><td>{row.rate_limit_strikes}</td></tr>)}</tbody></table></div></section>}
    <section className="endpoint-surface panel-surface"><div className="table-head"><div><span className="mono-kicker">Collections / routes</span><h3>Available API endpoints</h3></div></div><div className="endpoint-list">{endpointRows.map(item => <button className="endpoint-row" key={item.path} onClick={() => toast.info(`${item.path} selected`)}><span className={`endpoint-icon ${item.tone}`}><item.icon size={16} /></span><code>{item.path}</code><span className={`endpoint-tag ${item.tone}`}>{item.tag}</span><span className="endpoint-sample">{item.sample}</span></button>)}</div></section>
  </div>
}

function Detail({ icon, label, value }: { icon: ReactNode; label: string; value: string }) { return <div className="detail-row"><span className="detail-icon">{icon}</span><span><small>{label}</small><strong>{value}</strong></span></div> }
function Metric({ icon, label, value, sub, tone }: { icon: ReactNode; label: string; value: string; sub: string; tone: string }) { return <div className="metric-surface panel-surface"><div className="metric-label"><span>{label}</span><span className={`metric-icon ${tone}`}>{icon}</span></div><strong>{value}</strong><small>{sub}</small><div className={`metric-spark ${tone}`}><i /><i /><i /><i /><i /><i /><i /></div></div> }
function InfoCard({ icon, title, value, description }: { icon: ReactNode; title: string; value: string; description: string }) { return <div className="info-surface panel-surface"><span className="info-icon">{icon}</span><div><span className="info-label">{title}</span><strong>{value}</strong></div><p>{description}</p></div> }
function statusBadgeClass(status: string) { return `status-badge ${status === "valid" ? "valid" : status === "rate_limited" ? "warning" : status === "banned" ? "danger" : "neutral"}` }
