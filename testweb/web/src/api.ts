export interface Message {
  id: number;
  user: string;
  text: string;
  kind: 'chat' | 'join' | 'leave';
  ts: number;
}

export type WSEvent =
  | { type: 'message'; message: Message }
  | { type: 'users'; users: string[] };

export async function fetchMessages(limit = 100): Promise<Message[]> {
  const resp = await fetch(`/api/messages?limit=${limit}`);
  if (!resp.ok) throw new Error(`fetch messages: ${resp.status}`);
  return resp.json();
}

/** 通过 HTTP 主动发消息（不依赖 WS 连接），模拟 IM 主动发送接口。 */
export async function sendMessage(user: string, text: string): Promise<Message> {
  const resp = await fetch('/api/send', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ user, text }),
  });
  if (!resp.ok) throw new Error(`send message: ${resp.status}`);
  return resp.json();
}

export function connectWS(user: string): WebSocket {
  const proto = location.protocol === 'https:' ? 'wss' : 'ws';
  return new WebSocket(`${proto}://${location.host}/ws?user=${encodeURIComponent(user)}`);
}
