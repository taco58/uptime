"use client"

import { useEffect, useState, useSyncExternalStore } from "react"

interface Website {
  id: number
  name: string
  url: string
  status: "up" | "down" | "unknown" | string
  webhook_url?: string
}

interface CheckSummary {
  status_code: number
  latency_ms: number
  success: boolean
  checked_at: string
}

interface MonitorStats {
  monitor_id: number
  uptime_24h: number
  avg_latency_ms: number
  p95_latency_ms: number
  total_checks_24h: number
  failed_checks_24h: number
  recent_checks: CheckSummary[]
}

function subscribe(callback: () => void) {
  window.addEventListener("storage", callback)
  return () => window.removeEventListener("storage", callback)
}

export default function Dashboard() {
  const token = useSyncExternalStore(
    subscribe,
    () => localStorage.getItem("token"),
    () => null,
  )

  const [authMode, setAuthMode] = useState<"login" | "register">("login")
  const [authError, setAuthError] = useState("")
  const [monitors, setMonitors] = useState<Website[]>([])
  const [isAdding, setIsAdding] = useState(false)

  const [editingId, setEditingId] = useState<number | null>(null)

  const [statsMap, setStatsMap] = useState<Record<number, MonitorStats>>({})

  const handleLogout = () => {
    localStorage.removeItem("token")
    window.dispatchEvent(new Event("storage"))
    setMonitors([])
  }

  const handleAuth = async (formData: FormData) => {
    setAuthError("")
    const email = formData.get("email") as string
    const password = formData.get("password") as string

    if (!email || !password) return

    try {
      const endpoint =
        authMode === "login"
          ? "http://localhost:8080/auth/login"
          : "http://localhost:8080/auth/register"

      const res = await fetch(endpoint, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ email, password }),
      })

      if (!res.ok) {
        const errText = await res.text()
        setAuthError(errText || "Authentication failed")
        return
      }

      const data = await res.json()
      if (data.token) {
        localStorage.setItem("token", data.token)
        window.dispatchEvent(new Event("storage"))
      }
    } catch {
      setAuthError("Could not connect to backend server")
    }
  }

  useEffect(() => {
    if (!token) return

    let ignore = false

    const loadData = async () => {
      try {
        const res = await fetch("http://localhost:8080/websites", {
          headers: { Authorization: `Bearer ${token}` },
        })
        if (res.status === 401) {
          handleLogout()
          return
        }
        const data: Website[] = await res.json()
        if (data && !ignore) {
          setMonitors(data)

          data.forEach(async (site) => {
            try {
              const statsRes = await fetch(
                `http://localhost:8080/websites/${site.id}/stats`,
                { headers: { Authorization: `Bearer ${token}` } },
              )
              if (statsRes.ok) {
                const statsData: MonitorStats = await statsRes.json()
                if (!ignore) {
                  setStatsMap((prev) => ({ ...prev, [site.id]: statsData }))
                }
              }
            } catch (err) {
              console.error(err)
            }
          })
        }
      } catch (err) {
        console.error(err)
      }
    }
    loadData()
    const interval = setInterval(loadData, 3000)

    return () => {
      ignore = true
      clearInterval(interval)
    }
  }, [token])

  const handleAddMonitor = async (formData: FormData) => {
    const name = formData.get("name") as string
    const url = formData.get("url") as string
    const webhook_url = (formData.get("webhook_url") as string) || undefined

    if (!name || !url || !token) return

    try {
      await fetch("http://localhost:8080/websites", {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          Authorization: `Bearer ${token}`,
        },
        body: JSON.stringify({ name, url, webhook_url }),
      })

      setIsAdding(false)
    } catch (err) {
      console.error("Failed to add monitor:", err)
    }
  }

  const handleUpdateMonitor = async (id: number, formData: FormData) => {
    const name = formData.get("name") as string
    const url = formData.get("url") as string
    const webhook_url = (formData.get("webhook_url") as string) || undefined

    if (!name || !url || !token) return

    try {
      const res = await fetch(`http://localhost:8080/websites/${id}`, {
        method: "PUT",
        headers: {
          "Content-Type": "application/json",
          Authorization: `Bearer ${token}`,
        },
        body: JSON.stringify({ name, url, webhook_url: webhook_url || null }),
      })

      if (res.ok) {
        setMonitors((prev) =>
          prev.map((m) => (m.id === id ? { ...m, name, url, webhook_url } : m)),
        )
        setEditingId(null)
      }
    } catch (err) {
      console.error("Failed to update monitor:", err)
    }
  }

  const handleDeleteMonitor = async (id: number) => {
    if (!token) return

    try {
      await fetch(`http://localhost:8080/websites/${id}`, {
        method: "DELETE",
        headers: { Authorization: `Bearer ${token}` },
      })
      setMonitors((prev) => prev.filter((m) => m.id !== id))
    } catch (err) {
      console.error("Failed to delete monitor:", err)
    }
  }

  const total = monitors.length
  const up = monitors.filter((m) => m.status === "up").length
  const down = monitors.filter((m) => m.status === "down").length

  if (!token) {
    return (
      <div className="min-h-screen bg-white text-black font-mono px-4 py-16 md:p-24 max-w-md mx-auto flex flex-col justify-between selection:bg-black selection:text-white">
        <div>
          <div className="mb-10 pb-6 border-b border-neutral-200">
            <h1 className="text-base font-medium tracking-tight uppercase">
              Uptime / Observability
            </h1>
            <span className="text-xs text-neutral-400">
              Authentication required
            </span>
          </div>

          <form action={handleAuth} className="space-y-6 text-xs">
            <div>
              <label className="block text-neutral-400 uppercase text-[10px] mb-1">
                Email
              </label>
              <input
                type="email"
                name="email"
                required
                placeholder="developer@domain.com"
                className="w-full border-b border-neutral-300 py-2 focus:outline-none focus:border-black placeholder:text-neutral-300 bg-transparent text-sm"
                autoFocus
              />
            </div>

            <div>
              <label className="block text-neutral-400 uppercase text-[10px] mb-1">
                Password
              </label>
              <input
                type="password"
                name="password"
                required
                placeholder="••••••••"
                className="w-full border-b border-neutral-300 py-2 focus:outline-none focus:border-black placeholder:text-neutral-300 bg-transparent text-sm"
              />
            </div>

            {authError && (
              <div className="text-red-600 text-xs py-1">[!] {authError}</div>
            )}

            <div className="pt-2">
              <button
                type="submit"
                className="w-full bg-black text-white hover:bg-neutral-800 text-xs py-2.5 transition-colors cursor-pointer uppercase tracking-wider font-medium"
              >
                {authMode === "login" ? "Sign In →" : "Create Account →"}
              </button>
            </div>
          </form>

          <div className="mt-8 pt-4 border-t border-neutral-100 text-center text-xs">
            <button
              type="button"
              onClick={() => {
                setAuthMode(authMode === "login" ? "register" : "login")
                setAuthError("")
              }}
              className="text-neutral-500 hover:text-black transition-colors cursor-pointer underline underline-offset-4"
            >
              {authMode === "login"
                ? "Need an account? Register"
                : "Already registered? Sign In"}
            </button>
          </div>
        </div>

        <footer className="pt-12 text-[10px] text-neutral-400 flex justify-between items-center border-t border-neutral-100 mt-12">
          <span>auth gateway</span>
          <span>bcrypt · jwt · go</span>
        </footer>
      </div>
    )
  }

  return (
    <div className="min-h-screen bg-white text-black font-mono px-4 py-8 sm:px-8 sm:py-12 md:p-14 max-w-4xl mx-auto flex flex-col justify-between selection:bg-black selection:text-white">
      <div>
        <header className="pb-8 sm:pb-10 border-b border-neutral-200">
          <div className="flex flex-col sm:flex-row justify-between items-start sm:items-baseline gap-2 sm:gap-4 mb-6 sm:mb-8">
            <div>
              <h1 className="text-base sm:text-lg font-medium tracking-tight uppercase">
                Uptime / Observability
              </h1>
              <span className="text-xs text-neutral-400">
                10s interval · {up}/{total} online
              </span>
            </div>

            <button
              onClick={handleLogout}
              className="text-xs text-neutral-400 hover:text-black transition-colors cursor-pointer underline underline-offset-4"
            >
              [logout]
            </button>
          </div>

          <div className="grid grid-cols-3 gap-3 sm:gap-6 pt-2">
            <div>
              <span className="text-neutral-400 block mb-1 uppercase text-[10px] sm:text-[11px] tracking-wider truncate">
                Targets
              </span>
              <span className="text-xl sm:text-2xl text-black font-light">
                {String(total).padStart(2, "0")}
              </span>
            </div>
            <div>
              <span className="text-neutral-400 block mb-1 uppercase text-[10px] sm:text-[11px] tracking-wider truncate">
                Online
              </span>
              <span className="text-xl sm:text-2xl text-black font-light">
                {String(up).padStart(2, "0")}
              </span>
            </div>
            <div>
              <span className="text-neutral-400 block mb-1 uppercase text-[10px] sm:text-[11px] tracking-wider truncate">
                Outages
              </span>
              <span
                className={`text-xl sm:text-2xl font-light ${
                  down > 0 ? "text-red-600 font-normal" : "text-neutral-400"
                }`}
              >
                {String(down).padStart(2, "0")}
              </span>
            </div>
          </div>
        </header>

        <div className="flex justify-between items-center py-5 sm:py-6 text-xs">
          <span className="text-neutral-400 uppercase text-[10px] sm:text-[11px] tracking-wider">
            Nodes ({total})
          </span>
          <button
            onClick={() => setIsAdding(!isAdding)}
            className="text-black text-xs hover:text-neutral-500 transition-colors cursor-pointer underline underline-offset-4"
          >
            {isAdding ? "cancel" : "+ add target"}
          </button>
        </div>

        {isAdding && (
          <form
            action={handleAddMonitor}
            className="mb-6 sm:mb-8 pb-6 border-b border-neutral-200 text-xs"
          >
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-4 mb-4">
              <div>
                <label className="block text-neutral-400 uppercase text-[10px] mb-1">
                  Label
                </label>
                <input
                  type="text"
                  name="name"
                  required
                  placeholder="Primary API"
                  className="w-full border-b border-neutral-300 py-1.5 focus:outline-none focus:border-black placeholder:text-neutral-300 text-xs sm:text-sm bg-transparent"
                  autoFocus
                />
              </div>
              <div>
                <label className="block text-neutral-400 uppercase text-[10px] mb-1">
                  Target URL
                </label>
                <input
                  type="url"
                  name="url"
                  required
                  placeholder="https://example.com"
                  className="w-full border-b border-neutral-300 py-1.5 focus:outline-none focus:border-black placeholder:text-neutral-300 text-xs sm:text-sm bg-transparent"
                />
              </div>
            </div>
            <div className="mb-4">
              <label className="block text-neutral-400 uppercase text-[10px] mb-1">
                Discord Webhook URL (Optional)
              </label>
              <input
                type="url"
                name="webhook_url"
                placeholder="https://discord.com/api/webhooks/..."
                className="w-full border-b border-neutral-300 py-1.5 focus:outline-none focus:border-black placeholder:text-neutral-300 text-xs
                            sm:text-sm bg-transparent"
              />
            </div>
            <div className="flex justify-end items-center gap-4 text-xs pt-1">
              <button
                type="button"
                onClick={() => setIsAdding(false)}
                className="text-neutral-400 hover:text-black cursor-pointer p-1"
              >
                cancel
              </button>
              <button
                type="submit"
                className="text-black font-semibold hover:underline underline-offset-4 cursor-pointer py-1 px-2"
              >
                submit →
              </button>
            </div>
          </form>
        )}

        <div className="divide-y divide-neutral-100 border-t border-b border-neutral-200">
          {monitors.length === 0 ? (
            <div className="py-12 text-center text-xs text-neutral-400">
              No targets monitored. Click &quot;+ add target&quot; to begin.
            </div>
          ) : (
            monitors.map((site, index) => {
              const isUp = site.status === "up"
              const isDown = site.status === "down"
              const isEditing = editingId === site.id

              if (isEditing) {
                return (
                  <div
                    key={site.id}
                    className="py-4 border-b border-neutral-200"
                  >
                    <form
                      action={(formData) =>
                        handleUpdateMonitor(site.id, formData)
                      }
                      className="space-y-3 text-xs"
                    >
                      <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                        <div>
                          <label className="block text-neutral-400 uppercase text-[9px] mb-1">
                            Label
                          </label>
                          <input
                            type="text"
                            name="name"
                            defaultValue={site.name}
                            required
                            className="w-full border-b border-neutral-300 py-1 focus:outline-none focus:border-black text-xs bg-transparent"
                          />
                        </div>
                        <div>
                          <label className="block text-neutral-400 uppercase text-[9px] mb-1">
                            Target URL
                          </label>
                          <input
                            type="url"
                            name="url"
                            defaultValue={site.url}
                            required
                            className="w-full border-b border-neutral-300 py-1 focus:outline-none focus:border-black text-xs bg-transparent"
                          />
                        </div>
                      </div>
                      <div>
                        <label className="block text-neutral-400 uppercase text-[9px] mb-1">
                          Discord Webhook URL (Optional)
                        </label>
                        <input
                          type="url"
                          name="webhook_url"
                          defaultValue={site.webhook_url || ""}
                          placeholder="https://discord.com/api/webhooks/..."
                          className="w-full border-b border-neutral-300 py-1 focus:outline-none focus:border-black text-xs bg-transparent"
                        />
                      </div>
                      <div className="flex justify-end gap-3 pt-1">
                        <button
                          type="button"
                          onClick={() => setEditingId(null)}
                          className="text-neutral-400 hover:text-black cursor-pointer"
                        >
                          cancel
                        </button>
                        <button
                          type="submit"
                          className="text-black font-semibold hover:underline cursor-pointer"
                        >
                          save changes →
                        </button>
                      </div>
                    </form>
                  </div>
                )
              }

              const siteStats = statsMap[site.id]
              const history = siteStats?.recent_checks ? [...siteStats.recent_checks].reverse() : []

              return (
                <div
                  key={site.id}
                  className="py-4 flex flex-col gap-3 hover:bg-neutral-50/60 transition-colors px-2 -mx-2 rounded"
                >
                  <div className="flex items-center justify-between gap-4">
                    <div className="flex items-baseline gap-3 min-w-0 flex-1">
                      <span className="text-neutral-400 text-[10px] sm:text-[11px] w-4 sm:w-5 shrink-0 pt-0.5 sm:pt-0">
                        {String(index + 1).padStart(2, "0")}
                      </span>
                      <div className="min-w-0 flex-1 flex flex-col sm:flex-row sm:items-baseline sm:gap-3">
                        <span className="text-black text-xs sm:text-sm font-medium truncate">
                          {site.name}
                        </span>
                        <span className="text-neutral-400 text-[10px] sm:text-xs truncate">
                          {site.url}
                        </span>
                        {site.webhook_url && (
                          <span className="text-[9px] text-neutral-400 border border-neutral-400 px-1 py-0.5 rounded tracking-wider uppercase shrink-0">
                            Discord
                          </span>
                        )}
                      </div>
                    </div>

                    <div className="flex items-center gap-3 sm:gap-4 shrink-0">
                      <span
                        className={`text-[11px] sm:text-xs tracking-wider uppercase ${
                          isUp
                            ? "text-neutral-900"
                            : isDown
                              ? "text-red-600 font-semibold"
                              : "text-neutral-400"
                        }`}
                      >
                        {isUp ? "● up" : isDown ? "■ down" : "○ unknown"}
                      </span>

                      <button
                        onClick={() => setEditingId(site.id)}
                        className="text-xs text-neutral-400 hover:text-black transition-colors cursor-pointer underline underline-offset-2"
                      >
                        edit
                      </button>

                      <button
                        onClick={() => handleDeleteMonitor(site.id)}
                        className="text-neutral-300 hover:text-black transition-colors cursor-pointer text-base sm:text-lg font-light w-5 h-5 flex items-center justify-center"
                        title="Remove"
                      >
                        ×
                      </button>
                    </div>
                  </div>

                  {siteStats && (
                    <div className="pl-7 space-y-2">
                      <div className="flex flex-wrap items-center gap-x-6 gap-y-1 text-[10px] text-neutral-400 uppercase tracking-wider">
                        <div>
                          Uptime 24h:{" "}
                          <span className={`font-semibold ${siteStats.uptime_24h < 99 ? "text-red-600" : "text-black"}`}>
                            {siteStats.uptime_24h.toFixed(1)}%
                          </span>
                        </div>
                        <div>
                          Avg: <span className="font-semibold text-black">{siteStats.avg_latency_ms}ms</span>
                        </div>
                        <div>
                          P95: <span className="font-semibold text-black">{siteStats.p95_latency_ms}ms</span>
                        </div>
                        <div>
                          Checks: <span className="font-semibold text-black">{siteStats.total_checks_24h}</span>
                        </div>
                      </div>

                      <div className="flex items-center gap-1 h-3">
                        {history.length === 0 ? (
                          <span className="text-[10px] text-neutral-300">Collecting checks...</span>
                        ) : (
                          history.map((check, i) => (
                            <div
                              key={i}
                              title={`${check.status_code} · ${check.latency_ms}ms · ${new Date(check.checked_at).toLocaleTimeString()}`}
                              className={`flex-1 h-full rounded-[1px] transition-all cursor-help ${
                                check.success
                                  ? "bg-green-900 hover:bg-green-600"
                                  : "bg-red-600 hover:bg-red-400"
                              }`}
                            />
                          ))
                        )}
                      </div>
                    </div>
                  )}
                </div>
              )
            })
          )}
        </div>
      </div>

      <footer className="pt-12 sm:pt-16 text-[10px] sm:text-[11px] text-neutral-400 flex flex-col sm:flex-row justify-between items-start sm:items-center gap-2 border-t border-neutral-100 mt-12 sm:mt-16">
        <span>uptime engine</span>
        <span className="text-neutral-300 sm:text-neutral-400">
          go · postgres · next.js
        </span>
      </footer>
    </div>
  )
}
