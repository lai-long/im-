// useWebSocket：连接 /ws?user=<userid>，分发 message/stream/replay 事件，断线指数退避重连。
// 返回连接状态，供 UI 显示指示灯。

import { useEffect, useRef, useState } from 'react';
import type { WsEvent } from './types';

type Status = 'connecting' | 'connected' | 'reconnecting';

export function useWebSocket(userid: string | null, onEvent: (ev: WsEvent) => void) {
  const [status, setStatus] = useState<Status>('connecting');
  const onEventRef = useRef(onEvent);
  onEventRef.current = onEvent; // 始终用最新的回调，避免重连重建

  useEffect(() => {
    if (!userid) return;
    let ws: WebSocket | null = null;
    let retry = 0;
    let timer: ReturnType<typeof setTimeout> | null = null;
    let stopped = false;

    const connect = () => {
      setStatus(retry === 0 ? 'connecting' : 'reconnecting');
      ws = new WebSocket(`ws://${location.host}/ws?user=${encodeURIComponent(userid!)}`);
      ws.onopen = () => {
        retry = 0;
        setStatus('connected');
      };
      ws.onmessage = (e) => {
        try {
          const ev = JSON.parse(e.data) as WsEvent;
          onEventRef.current(ev);
        } catch {
          // 忽略非法帧
        }
      };
      ws.onclose = () => {
        if (stopped) return;
        setStatus('reconnecting');
        timer = setTimeout(connect, Math.min(1000 * ++retry, 5000));
      };
      ws.onerror = () => {
        // close 会随后触发，这里不做处理
      };
    };
    connect();

    return () => {
      stopped = true;
      if (timer) clearTimeout(timer);
      if (ws) ws.close();
    };
  }, [userid]);

  return status;
}
