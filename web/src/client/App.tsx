// 客户端主应用：登录、会话列表、消息区、WS 实时、发送。
import { useEffect, useState, useCallback } from 'react';
import type { User, Chat, Bot, Message, WsEvent } from '../shared/types';
import { api } from '../shared/api';
import { useWebSocket } from '../shared/ws';
import { MessageList } from './components/MessageView';

function kindOf(c: Chat) {
  if (c.type === 'single') return { label: '机器人单聊', cls: 'single', hint: '直接发消息即触发机器人回调' };
  if (c.type === 'direct') return { label: '应用单聊', cls: 'direct', hint: '直接发消息即触发应用 XML 回调' };
  return { label: '群聊', cls: 'group', hint: '群内 @机器人 触发回调' };
}

function Login({ onLogin }: { onLogin: (u: User) => void }) {
  const [users, setUsers] = useState<User[]>([]);
  const [name, setName] = useState('');
  useEffect(() => { api.users().then(setUsers); }, []);
  const enter = async () => {
    const u = name.trim()
      ? await api.login({ name: name.trim() })
      : await api.login({ userid: (document.getElementById('preset') as HTMLSelectElement).value });
    if (u && u.userid) { localStorage.setItem('im-user', JSON.stringify(u)); onLogin(u); }
  };
  return (
    <div id="login">
      <div className="panel">
        <strong>选择或注册用户</strong>
        <select id="preset">
          {users.map(u => <option key={u.userid} value={u.userid}>{u.name}（{u.userid}）</option>)}
        </select>
        <input id="nickname" placeholder="或输入新昵称自动注册" value={name}
          onChange={e => setName(e.target.value)} onKeyDown={e => e.key === 'Enter' && enter()} />
        <button onClick={enter}>进入</button>
      </div>
    </div>
  );
}

export function App() {
  const [me, setMe] = useState<User | null>(() => {
    const s = localStorage.getItem('im-user');
    return s ? JSON.parse(s) : null;
  });
  const [chats, setChats] = useState<Chat[]>([]);
  const [bots, setBots] = useState<Bot[]>([]);
  const [activeChatId, setActiveChatId] = useState<number | null>(null);
  const [messages, setMessages] = useState<Message[]>([]);
  const [text, setText] = useState('');

  const loadChats = useCallback(async () => {
    if (!me) return;
    const [cs, bs] = await Promise.all([api.chats(me.userid), api.bots()]);
    setChats(cs || []);
    setBots(bs || []);
  }, [me]);

  const loadHistory = useCallback(async (chatId: number) => {
    const msgs = await api.messages(chatId);
    setMessages(msgs || []);
  }, []);

  useEffect(() => { if (me) loadChats(); }, [me, loadChats]);
  useEffect(() => { if (activeChatId) loadHistory(activeChatId); }, [activeChatId, loadHistory]);

  const onEvent = useCallback((ev: WsEvent) => {
    if (ev.kind === 'message' || ev.kind === 'replay') {
      loadChats();
      if (ev.message.chat_id === activeChatId) {
        setMessages(prev => prev.some(m => m.msgid === ev.message.msgid) ? prev : [...prev, ev.message]);
      }
    } else if (ev.kind === 'stream') {
      if (ev.message.chat_id === activeChatId) {
        setMessages(prev => {
          const i = prev.findIndex(m => m.msgid === ev.message.msgid);
          if (i >= 0) { const next = [...prev]; next[i] = ev.message; return next; }
          return [...prev, ev.message];
        });
      }
    }
  }, [activeChatId, loadChats]);

  const status = useWebSocket(me?.userid ?? null, onEvent);

  if (!me) return <Login onLogin={setMe} />;

  const active = chats.find(c => c.id === activeChatId);
  const k = active ? kindOf(active) : null;

  const openSingle = async (botID: number) => {
    const chat = await api.openSingleChat({ userid: me.userid, bot_id: botID });
    if (chat?.id) { setActiveChatId(chat.id); }
  };
  const send = async () => {
    const t = text.trim();
    if (!t || !activeChatId) return;
    await api.send({ userid: me.userid, text: t, chat_id: activeChatId });
    setText('');
  };

  return (
    <>
      <header>
        <span className={`dot ${status}`} />本地 IM（企微机器人 / 自建应用联调）
      </header>
      <div id="body">
        <aside>
          {chats.map(c => { const k2 = kindOf(c); return (
            <div key={c.id} className={`chat-item${c.id === activeChatId ? ' active' : ''}`}
              onClick={() => setActiveChatId(c.id)}>
              {c.name}
              <div className="kind"><span className={`badge ${k2.cls}`}>{k2.label}</span></div>
            </div>
          ); })}
          <div className="chat-head">发起单聊</div>
          {bots.map(b => (
            <div key={b.id} className="chat-item bot" onClick={() => openSingle(b.id)}>💬 {b.name}</div>
          ))}
        </aside>
        <div id="main">
          <div id="chatbar">
            {active ? <>
              <span>{active.name}</span>
              <span className={`badge ${k!.cls}`}>{k!.label}</span>
              <span className="hint">{k!.hint}</span>
            </> : null}
          </div>
          <MessageList messages={messages} />
          <footer>
            <input id="input" placeholder={k ? k.hint + '…' : ''} value={text}
              onChange={e => setText(e.target.value)} onKeyDown={e => e.key === 'Enter' && send()} />
            <button onClick={send}>发送</button>
          </footer>
        </div>
      </div>
    </>
  );
}
