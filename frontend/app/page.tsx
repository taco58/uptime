"use client"

import { useCallback, useEffect, useState } from "react"

interface Website {
  id: number
  name: string
  url: string
  status: "up" | "down" | "unknown" | string
}

export default function Dashboard() {
  const [monitors, setMonitors] = useState<Website[]>([])

  const [name, setName] = useState("")
  const [url, setUrl] = useState("")
  const [isAdding, setIsAdding] = useState(false)

  const fetchMonitors = useCallback(async () => {
    try {
      const res = await fetch("http://localhost:8080/websites")
      const data = await res.json()
      setMonitors(data || [])
    } catch (err) {
      console.error("Failed to fetch monitors: ", err)
    }
  }, [])

  useEffect(() => {
    let active = true

    const load = () => {
      fetch("http://localhost:8080/websites")
        .then((res) => res.json())
        .then((data) => {
          if (active) setMonitors(data || [])
        })
        .catch((err) => console.error(err))
    }

    load()
    const interval = setInterval(load, 3000)

    return () => {
      active = false
      clearInterval(interval)
    }
  }, [])

  const handleAddMonitor = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!name || !url) return

    await fetch("http://localhost:8080/websites", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ name, url }),
    })

    setName("")
    setUrl("")
    setIsAdding(false)
    fetchMonitors()
  }

  const handleDeleteMonitor = async (id: number) => {
    await fetch(`http://localhost:8080/websites/${id}`, {
      method: "DELETE",
    })
    setMonitors((prev) => prev.filter((m) => m.id !== id))
  }

  const total = monitors.length
  const up = monitors.filter((m) => m.status === "up").length
  const down = monitors.filter((m) => m.status === "down").length

  return (
    <div className="min-h-screen bg-white text-black font-mono px-4 py-8 sm:px-8 sm:py-12 md:p-14 max-w-4xl mx-auto flex flex-col justify-between selection:bg-black selection:text-white">
      <div>
        <header className="pb-8 sm:pb-10 border-b border-neutral-200">
          <div className="flex flex-col sm:flex-row justify-between items-start sm:items-baseline gap-2 sm:gap-4 mb-6 sm:mb-8">
            <h1 className="text-base sm:text-lg font-medium tracking-tight uppercase">
              Uptime / Observability
            </h1>
            <span className="text-xs text-neutral-400">
              10s interval · {up}/{total} online
            </span>
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
            onSubmit={handleAddMonitor}
            className="mb-6 sm:mb-8 pb-6 border-b border-neutral-200 text-xs"
          >
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-4 mb-4">
              <div>
                <label className="block text-neutral-400 uppercase text-[10px] mb-1">
                  Label
                </label>
                <input
                  type="text"
                  placeholder="Primary API"
                  value={name}
                  onChange={(e) => setName(e.target.value)}
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
                  placeholder="https://example.com"
                  value={url}
                  onChange={(e) => setUrl(e.target.value)}
                  className="w-full border-b border-neutral-300 py-1.5 focus:outline-none focus:border-black placeholder:text-neutral-300 text-xs sm:text-sm bg-transparent"
                />
              </div>
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

              return (
                <div
                  key={site.id}
                  className="py-3.5 sm:py-4 flex items-center justify-between gap-3 sm:gap-4 hover:bg-neutral-50/70 transition-colors px-1 sm:px-2 -mx-1 sm:-mx-2"
                >
                  <div className="flex items-start sm:items-baseline gap-2.5 sm:gap-4 min-w-0 flex-1">
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
                    </div>
                  </div>

                  <div className="flex items-center gap-3 sm:gap-5 shrink-0">
                    {/* Status Text Indicator */}
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

                    {/* Touch-friendly Delete Trigger */}
                    <button
                      onClick={() => handleDeleteMonitor(site.id)}
                      className="text-neutral-300 hover:text-black transition-colors cursor-pointer text-base sm:text-lg font-light w-6 h-6 flex items-center justify-center"
                      title="Remove"
                    >
                      ×
                    </button>
                  </div>
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
