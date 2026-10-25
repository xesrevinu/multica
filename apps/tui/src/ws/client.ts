import type { WSMessage, WSEventType } from "./events"

type EventHandler = (payload: unknown, actorId?: string, actorType?: string) => void

const UNPARSEABLE_LOG_MAX_CHARS = 200
const RECONNECT_BASE_DELAY_MS = 1_000
const RECONNECT_MAX_DELAY_MS = 30_000

function summarizeUnparseable(data: unknown) {
  const text = typeof data === "string" ? data : String(data)
  if (text.length <= UNPARSEABLE_LOG_MAX_CHARS) return text
  return `${text.slice(0, UNPARSEABLE_LOG_MAX_CHARS)}… (truncated, ${text.length} chars total)`
}

export type WSClientIdentity = {
  platform?: string
  version?: string
  os?: string
}

export class WSClient {
  private ws: WebSocket | null = null
  private baseUrl: string
  private token: string | null = null
  private workspaceSlug: string | null = null
  private identity: WSClientIdentity | undefined
  private handlers = new Map<WSEventType, Set<EventHandler>>()
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null
  private reconnectAttempt = 0
  private hasConnectedBefore = false
  private badFrameLogged = false
  private onReconnectCallbacks = new Set<() => void>()
  private anyHandlers = new Set<(msg: WSMessage) => void>()
  private connected = false

  constructor(url: string, options?: { identity?: WSClientIdentity }) {
    this.baseUrl = url
    this.identity = options?.identity
  }

  get isConnected() {
    return this.connected
  }

  setAuth(token: string, workspaceSlug: string) {
    this.token = token
    this.workspaceSlug = workspaceSlug
  }

  connect() {
    this.badFrameLogged = false
    const url = new URL(this.baseUrl)
    if (this.workspaceSlug) url.searchParams.set("workspace_slug", this.workspaceSlug)
    if (this.identity?.platform) url.searchParams.set("client_platform", this.identity.platform)
    if (this.identity?.version) url.searchParams.set("client_version", this.identity.version)
    if (this.identity?.os) url.searchParams.set("client_os", this.identity.os)

    this.ws = new WebSocket(url.toString())

    this.ws.onopen = () => {
      if (this.token) {
        this.ws!.send(JSON.stringify({ type: "auth", payload: { token: this.token } }))
        return
      }
      this.onAuthenticated()
    }

    this.ws.onmessage = (event) => {
      let msg: WSMessage
      try {
        msg = JSON.parse(event.data as string) as WSMessage
      } catch {
        return
      }
      if (!msg || typeof msg.type !== "string") {
        if (!this.badFrameLogged) this.badFrameLogged = true
        return
      }
      if ((msg as { type: string }).type === "auth_ack") {
        this.onAuthenticated()
        return
      }
      const eventHandlers = this.handlers.get(msg.type)
      if (eventHandlers) {
        for (const handler of eventHandlers) {
          handler(msg.payload, msg.actor_id, msg.actor_type)
        }
      }
      for (const handler of this.anyHandlers) {
        handler(msg)
      }
    }

    this.ws.onclose = () => {
      this.connected = false
      this.scheduleReconnect()
    }

    this.ws.onerror = () => {}
  }

  private scheduleReconnect() {
    const base = Math.min(RECONNECT_BASE_DELAY_MS * 2 ** this.reconnectAttempt, RECONNECT_MAX_DELAY_MS)
    const jitter = base * 0.2 * (Math.random() * 2 - 1)
    const delay = Math.round(Math.min(base + jitter, RECONNECT_MAX_DELAY_MS))
    this.reconnectAttempt++
    this.reconnectTimer = setTimeout(() => this.connect(), delay)
  }

  private onAuthenticated() {
    const recoveredConnection = this.hasConnectedBefore || this.reconnectAttempt > 0
    this.reconnectAttempt = 0
    this.connected = true
    if (recoveredConnection) {
      for (const cb of this.onReconnectCallbacks) {
        try {
          cb()
        } catch {}
      }
    }
    this.hasConnectedBefore = true
  }

  disconnect() {
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer)
      this.reconnectTimer = null
    }
    if (this.ws) {
      this.ws.onclose = null
      this.ws.onerror = null
      this.ws.close()
      this.ws = null
    }
    this.connected = false
    this.hasConnectedBefore = false
    this.reconnectAttempt = 0
    this.handlers.clear()
    this.anyHandlers.clear()
    this.onReconnectCallbacks.clear()
  }

  on(event: WSEventType, handler: EventHandler) {
    if (!this.handlers.has(event)) this.handlers.set(event, new Set())
    this.handlers.get(event)!.add(handler)
    return () => {
      this.handlers.get(event)?.delete(handler)
    }
  }

  onAny(handler: (msg: WSMessage) => void) {
    this.anyHandlers.add(handler)
    return () => {
      this.anyHandlers.delete(handler)
    }
  }

  onReconnect(callback: () => void) {
    this.onReconnectCallbacks.add(callback)
    return () => {
      this.onReconnectCallbacks.delete(callback)
    }
  }
}
