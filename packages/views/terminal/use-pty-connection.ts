"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { api } from "@multica/core/api";
import { type GhosttyHostHandle } from "./ghostty-host";
import {
  isPtySocketReady,
  registerPtyCloser,
  releasePtySocket,
  retainPtySocket,
} from "./pty-lifetime";
import { ptyWebSocketUrl } from "./session";

export type PtyConnectionState = "idle" | "connecting" | "connected" | "error";

export function usePtyConnection({
  enabled,
  daemonId,
  ptyId,
  argv,
  cwd,
  slug,
  holdResize = false,
}: {
  enabled: boolean;
  daemonId: string | null;
  ptyId: string;
  argv: string[];
  cwd: string;
  slug: string;
  holdResize?: boolean;
}) {
  const [status, setStatus] = useState<PtyConnectionState>("idle");
  const [message, setMessage] = useState("");
  const termRef = useRef<GhosttyHostHandle | null>(null);
  const socketRef = useRef<WebSocket | null>(null);
  const holdResizeRef = useRef(holdResize);
  const lastSizeRef = useRef({ cols: 80, rows: 24 });
  const lastSentRef = useRef({ cols: 0, rows: 0 });
  const resizeTimerRef = useRef<number | null>(null);
  const argvKey = argv.join("\0");
  holdResizeRef.current = holdResize;

  const detach = useCallback((kill = false) => {
    if (resizeTimerRef.current !== null) {
      window.clearTimeout(resizeTimerRef.current);
      resizeTimerRef.current = null;
    }
    const socket = socketRef.current;
    socketRef.current = null;
    if (socket) releasePtySocket(ptyId, socket, kill);
    setStatus("idle");
    setMessage("");
  }, [ptyId]);

  const kill = useCallback(() => detach(true), [detach]);

  useEffect(() => registerPtyCloser(ptyId, kill), [kill, ptyId]);

  useEffect(() => {
    if (!enabled || !daemonId || !ptyId) {
      detach();
      return;
    }

    setStatus(isPtySocketReady(ptyId) ? "connected" : "connecting");
    const { socket, reused, markReady } = retainPtySocket(
      ptyId,
      () => {
        const next = new WebSocket(ptyWebSocketUrl(daemonId, slug, ptyId));
        next.binaryType = "arraybuffer";
        return next;
      },
    );
    socketRef.current = socket;

    const sendOpen = () => {
      const cols = termRef.current?.cols ?? lastSizeRef.current.cols;
      const rows = termRef.current?.rows ?? lastSizeRef.current.rows;
      socket.send(
        JSON.stringify({
          type: "pty.open",
          id: ptyId,
          cols,
          rows,
          cwd,
          argv,
        }),
      );
    };

    const onOpen = () => {
      if (reused && socket.readyState === WebSocket.OPEN) return;
      const token = api.getToken?.() ?? null;
      if (token) {
        socket.send(JSON.stringify({ type: "auth", payload: { token } }));
        return;
      }
      sendOpen();
    };

    const onMessage = (event: MessageEvent) => {
      if (typeof event.data === "string") {
        let msg: { type?: string; error?: string } = {};
        try {
          msg = JSON.parse(event.data) as { type?: string; error?: string };
        } catch {
          return;
        }
        if (msg.type === "auth_ack") {
          sendOpen();
          return;
        }
        if (msg.type === "pty.opened") {
          markReady();
          setStatus("connected");
          const cols = termRef.current?.cols ?? lastSizeRef.current.cols;
          const rows = termRef.current?.rows ?? lastSizeRef.current.rows;
          lastSentRef.current = { cols, rows };
          return;
        }
        if (msg.type === "pty.exit") {
          setStatus("idle");
          return;
        }
        if (msg.type === "pty.error" || msg.type === "error" || msg.error) {
          setStatus("error");
          setMessage(msg.error || "error");
        }
        return;
      }
      const bytes =
        event.data instanceof ArrayBuffer
          ? new Uint8Array(event.data)
          : event.data instanceof Blob
            ? null
            : new Uint8Array(event.data as ArrayBuffer);
      if (bytes) termRef.current?.write(bytes);
      else if (event.data instanceof Blob) {
        void event.data.arrayBuffer().then((buf) => termRef.current?.write(new Uint8Array(buf)));
      }
    };

    const onError = () => {
      setStatus("error");
      setMessage("offline");
    };
    const onClose = () => {
      if (socketRef.current === socket) {
        socketRef.current = null;
        setStatus((current) => (current === "connecting" ? "error" : "idle"));
      }
    };

    socket.addEventListener("open", onOpen);
    socket.addEventListener("message", onMessage);
    socket.addEventListener("error", onError);
    socket.addEventListener("close", onClose);
    if (socket.readyState === WebSocket.OPEN) onOpen();

    return () => {
      socket.removeEventListener("open", onOpen);
      socket.removeEventListener("message", onMessage);
      socket.removeEventListener("error", onError);
      socket.removeEventListener("close", onClose);
      if (socketRef.current === socket) detach();
      else releasePtySocket(ptyId, socket);
    };
  }, [enabled, daemonId, ptyId, cwd, slug, argvKey, detach]);

  const onReady = useCallback((handle: GhosttyHostHandle) => {
    termRef.current = handle;
  }, []);

  const copy = useCallback(() => termRef.current?.copy() ?? Promise.resolve(), []);
  const paste = useCallback(() => termRef.current?.paste() ?? Promise.resolve(), []);
  const selectAll = useCallback(() => termRef.current?.selectAll(), []);
  const clear = useCallback(() => termRef.current?.clear(), []);
  const focus = useCallback(() => termRef.current?.focus(), []);
  const zoomIn = useCallback(() => termRef.current?.zoomIn(), []);
  const zoomOut = useCallback(() => termRef.current?.zoomOut(), []);
  const zoomReset = useCallback(() => termRef.current?.zoomReset(), []);

  const onData = useCallback((data: string) => {
    const socket = socketRef.current;
    if (!socket || socket.readyState !== WebSocket.OPEN) return;
    socket.send(new TextEncoder().encode(data));
  }, []);

  const flushResize = useCallback(() => {
    const { cols, rows } = lastSizeRef.current;
    if (cols < 8 || rows < 4) return;
    if (lastSentRef.current.cols === cols && lastSentRef.current.rows === rows) return;
    const socket = socketRef.current;
    if (!socket || socket.readyState !== WebSocket.OPEN) return;
    lastSentRef.current = { cols, rows };
    socket.send(JSON.stringify({ type: "pty.resize", id: ptyId, cols, rows }));
  }, [ptyId]);

  const onResize = useCallback(
    (size: { cols: number; rows: number }) => {
      lastSizeRef.current = size;
      if (holdResizeRef.current) return;
      if (resizeTimerRef.current !== null) window.clearTimeout(resizeTimerRef.current);
      resizeTimerRef.current = window.setTimeout(() => {
        resizeTimerRef.current = null;
        flushResize();
      }, 80);
    },
    [flushResize],
  );

  useEffect(() => {
    if (holdResize) return;
    if (resizeTimerRef.current !== null) {
      window.clearTimeout(resizeTimerRef.current);
      resizeTimerRef.current = null;
    }
    flushResize();
  }, [holdResize, flushResize]);

  return {
    status,
    message,
    onReady,
    onData,
    onResize,
    copy,
    paste,
    selectAll,
    clear,
    focus,
    zoomIn,
    zoomOut,
    zoomReset,
  };
}
