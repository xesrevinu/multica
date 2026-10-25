import { createCliRenderer } from "@opentui/core"
import { createRoot } from "@opentui/react"
import path from "node:path"
import type { Connection } from "../config"
import { App } from "./app"
import { ClipboardProvider } from "./context/clipboard"
import { Keymap } from "./context/keymap"
import { ThemeProvider, createThemeSource } from "./context/theme"
import { createCliConfigService, cliConfigPath } from "./config"
import { ConfigProvider, resolve } from "./opentui-config"
import { PickIssueApp, type PickMode } from "./pick-issue"
import { DialogProvider } from "./ui/dialog"
import { ToastProvider } from "./ui/toast"
import { TuiQueryProvider } from "./query-client"
import { bootSharedApi } from "../api/shared"

const clipboard = {
  read: async () => undefined,
  write: async () => {},
}

export async function runTui(
  connection: Connection,
  options?: { pick?: PickMode; pickIssue?: boolean; output?: string },
) {
  bootSharedApi(connection)
  const service = createCliConfigService()
  const configDir = path.dirname(service.path ?? cliConfigPath())
  const config = resolve(await service.get(), { terminalSuspend: process.platform !== "win32" })

  const renderer = await createCliRenderer({
    exitOnCtrlC: false,
    targetFps: 30,
    useMouse: config.mouse,
    autoFocus: true,
  })

  createRoot(renderer).render(
    <ClipboardProvider value={clipboard}>
      <ConfigProvider config={config} service={service} options={{ terminalSuspend: process.platform !== "win32" }}>
        <Keymap.Provider>
          <ThemeProvider mode="dark" source={createThemeSource(configDir)}>
            <TuiQueryProvider>
              <ToastProvider>
                <DialogProvider>
                  {options?.pick || options?.pickIssue ? (
                    <PickIssueApp connection={connection} output={options.output} mode={options.pick ?? "issue"} />
                  ) : (
                    <App connection={connection} />
                  )}
                </DialogProvider>
              </ToastProvider>
            </TuiQueryProvider>
          </ThemeProvider>
        </Keymap.Provider>
      </ConfigProvider>
    </ClipboardProvider>,
  )
}
