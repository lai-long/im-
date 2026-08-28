import { useCallback, useEffect, useRef, useState } from 'react';
import { Message, WSEvent, connectWS, fetchMessages } from './api';

function formatTime(ts: number): string {
  return new Date(ts).toLocaleTimeString('zh-CN', { hour12: false });
}

function MessageItem({ msg, self }: { msg: Message; self: string }) {
  if (msg.kind !== 'chat') {
    return <div className="msg-system">{msg.text}</div>;
  }
  const mine = msg.user === self;
  return (
    <div className={`msg-row ${mine ? 'mine' : ''}`}>
      <div className="msg-bubble">
        <div className="msg-meta">
          {msg.user} · {formatTime(msg.ts)} · #{msg.id}
        </div>
        <div className="msg-text">{msg.text}</div>
      </div>
    </div>
  );
}

function Chat({ user, onLogout }: { user: string; onLogout: () => void }) {
  const [messages, setMessages] = useState<Message[]>([]);
  const [users, setUsers] = useState<string[]>([]);
  const [input, setInput] = useState('');
  const [connected, setConnected] = useState(false);
  const wsRef = useRef<WebSocket | null>(null);
  const listRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    fetchMessages().then(setMessages).catch(console.error);
  }, []);

  useEffect(() => {
    let closed = false;
    let retry: ReturnType<typeof setTimeout>;

    const connect = () => {
      const ws = connectWS(user);
      wsRef.current = ws;
      ws.onopen = () => setConnected(true);
      ws.onmessage = (ev) => {
        const data = JSON.parse(ev.data) as WSEvent;
        if (data.type === 'message') {
          setMessages((prev) =>
            prev.some((m) => m.id === data.message.id) ? prev : [...prev, data.message],
          );
        } else if (data.type === 'users') {
          setUsers(data.users);
        }
      };
      ws.onclose = () => {
        setConnected(false);
        if (!closed) retry = setTimeout(connect, 2000);
      };
    };
    connect();

    return () => {
      closed = true;
      clearTimeout(retry);
      wsRef.current?.close();
    };
  }, [user]);

  useEffect(() => {
    listRef.current?.scrollTo({ top: listRef.current.scrollHeight });
  }, [messages]);

  const send = useCallback(() => {
    const text = input.trim();
    if (!text) return;
    wsRef.current?.send(JSON.stringify({ type: 'message', text }));
    setInput('');
  }, [input]);

  return (
    <div className="chat-layout">
      <aside className="sidebar">
        <h2>在线 ({users.length})</h2>
        <ul>
          {users.map((u) => (
            <li key={u}>{u}</li>
          ))}
        </ul>
        <button className="logout" onClick={onLogout}>
          退出
        </button>
      </aside>
      <main className="chat-main">
        <header className="chat-header">
          TestWeb IM · {user}
          <span className={connected ? 'status on' : 'status off'}>
            {connected ? '已连接' : '重连中…'}
          </span>
        </header>
        <div className="msg-list" ref={listRef}>
          {messages.map((m) => (
            <MessageItem key={m.id} msg={m} self={user} />
          ))}
        </div>
        <div className="input-bar">
          <input
            value={input}
            placeholder="输入消息，回车发送"
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={(e) => e.key === 'Enter' && send()}
          />
          <button onClick={send}>发送</button>
        </div>
      </main>
    </div>
  );
}

function Login({ onLogin }: { onLogin: (name: string) => void }) {
  const [name, setName] = useState('');
  const submit = () => {
    const n = name.trim();
    if (n) onLogin(n);
  };
  return (
    <div className="login">
      <h1>TestWeb IM</h1>
      <p>输入昵称进入群聊</p>
      <input
        value={name}
        placeholder="昵称"
        autoFocus
        onChange={(e) => setName(e.target.value)}
        onKeyDown={(e) => e.key === 'Enter' && submit()}
      />
      <button onClick={submit}>进入</button>
    </div>
  );
}

export default function App() {
  const [user, setUser] = useState<string | null>(null);
  return user ? <Chat user={user} onLogout={() => setUser(null)} /> : <Login onLogin={setUser} />;
}
