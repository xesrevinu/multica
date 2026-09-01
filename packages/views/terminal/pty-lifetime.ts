type SharedPtySocket = {
  socket: WebSocket;
  refs: number;
  ready: boolean;
};

const shared = new Map<string, SharedPtySocket>();

export function retainPtySocket(ptyId: string, create: () => WebSocket): {
  socket: WebSocket;
  reused: boolean;
  markReady: () => void;
} {
  const existing = shared.get(ptyId);
  if (existing && existing.socket.readyState < WebSocket.CLOSING) {
    existing.refs += 1;
    return {
      socket: existing.socket,
      reused: true,
      markReady: () => {
        existing.ready = true;
      },
    };
  }
  const socket = create();
  const entry: SharedPtySocket = { socket, refs: 1, ready: false };
  shared.set(ptyId, entry);
  socket.addEventListener("close", () => {
    if (shared.get(ptyId)?.socket === socket) shared.delete(ptyId);
  });
  return {
    socket,
    reused: false,
    markReady: () => {
      entry.ready = true;
    },
  };
}

export function releasePtySocket(ptyId: string, socket: WebSocket, kill = false) {
  const entry = shared.get(ptyId);
  if (!entry || entry.socket !== socket) {
    if (socket.readyState < WebSocket.CLOSING) socket.close();
    return;
  }
  entry.refs -= 1;
  if (kill) {
    shared.delete(ptyId);
    if (socket.readyState === WebSocket.OPEN) {
      try {
        socket.send(JSON.stringify({ type: "pty.close", id: ptyId }));
      } catch {
        // ignore
      }
    }
    if (socket.readyState < WebSocket.CLOSING) socket.close();
    return;
  }
  if (entry.refs > 0) return;
  window.setTimeout(() => {
    const current = shared.get(ptyId);
    if (!current || current.socket !== socket || current.refs > 0) return;
    shared.delete(ptyId);
    if (socket.readyState < WebSocket.CLOSING) socket.close();
  }, 0);
}

export function isPtySocketReady(ptyId: string): boolean {
  return shared.get(ptyId)?.ready === true;
}

const closers = new Map<string, () => void>();

export function registerPtyCloser(ptyId: string, close: () => void): () => void {
  closers.set(ptyId, close);
  return () => {
    if (closers.get(ptyId) === close) closers.delete(ptyId);
  };
}

/** Kill the daemon PTY. React unmount must not call this. */
export function closePtyProcess(ptyId: string) {
  const close = closers.get(ptyId);
  if (close) close();
}

export function closePtyProcesses(ptyIds: string[]) {
  for (const id of ptyIds) closePtyProcess(id);
}
