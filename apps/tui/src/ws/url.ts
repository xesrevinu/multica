export function resolveWsUrl(serverUrl: string) {
  const url = new URL(serverUrl)
  url.protocol = url.protocol === "https:" ? "wss:" : "ws:"
  url.pathname = "/ws"
  url.search = ""
  url.hash = ""
  return url.toString()
}
