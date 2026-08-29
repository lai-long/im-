// 客户端主应用：登录、会话列表、消息区、WS 实时、发送。
import { useEffect, useState, useCallback } from 'react';
import type { User, Chat, Bot, Message, WsEvent } from '../shared/types';
import { api } from '../shared/api';
import { useWebSocket } from '../shared/ws';
import { MessageList } from './components/MessageView';
import { MentionInput } from './components/MentionInput';

function kindOf(c: Chat) {
  if (c.type === 'single') return { label: '机器人单聊', cls: 'single', hint: '直接发消息即触发机器人回调' };
  if (c.type === 'direct') return { label: '应用单聊', cls: 'direct', hint: '直接发消息即触发应用 XML 回调' };
  return { label: '群聊', cls: 'group', hint: '群内 @机器人 触发回调' };
}

const STATUS_TEXT = { connecting: '连接中', connected: '已连接', reconnecting: '重连中' } as const;

function Login({ onLogin }: { onLogin: (u: User) => void }) {
  const [users, setUsers] = useState<User[]>([]);
  const [userid, setUserid] = useState('');
  const [name, setName] = useState('');
  const [err, setErr] = useState('');
  useEffect(() => {
    api.users().then(us => {
      const list = us || [];
      setUsers(list);
      if (list.length) setUserid(list[0].userid);
    });
  }, []);
  const enter = async () => {
    const u: any = name.trim() ? await api.login({ name: name.trim() }) : await api.login({ userid });
    if (u && u.userid) {
      localStorage.setItem('im-user', JSON.stringify(u));
      onLogin(u);
    } else {
      setErr(u?.errmsg || '登录失败，请确认平台已启动');
    }
  };
  return (
    <div id="login">
      <div className="panel">
        <h1>本地 IM</h1>
        <p className="muted">企微机器人 / 自建应用联调客户端</p>
        <label>选择预置用户</label>
        <select value={userid} onChange={e => setUserid(e.target.value)}>
          {users.map(u => <option key={u.userid} value={u.userid}>{u.name}（{u.userid}）</option>)}
        </select>
        <label>或输入新昵称自动注册</label>
        <input placeholder="新昵称" value={name}
          onChange={e => setName(e.target.value)} onKeyDown={e => e.key === 'Enter' && enter()} />
        <button onClick={enter}>进入</button>
        {err && <div className="err">{err}</div>}
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

  const logout = () => {
    localStorage.removeItem('im-user');
    setMe(null);
  };

  const active = chats.find(c => c.id === activeChatId);
  const k = active ? kindOf(active) : null;

  const openSingle = async (botID: number) => {
    const chat = await api.openSingleChat({ userid: me.userid, bot_id: botID });
    if (chat?.id) { setActiveChatId(chat.id); }
  };
  const send = async (t: string) => {
    if (!activeChatId) return;
    await api.send({ userid: me.userid, text: t, chat_id: activeChatId });
  };

  return (
    <>
      <header>
        <span className={`dot ${status}`} title={STATUS_TEXT[status]} />
        <span className="title">本地 IM · 企微联调</span>
        <span className="spacer" />
        <a className="hdr-link" href="/admin.html">控制台</a>
        <span className="me">{me.name}（{me.userid}）</span>
        <button className="hdr-btn" onClick={logout}>切换用户</button>
      </header>
      <div id="body">
        <aside>
          <div className="chat-head">会话</div>
          {chats.length === 0 && <div className="aside-empty">暂无会话</div>}
          {chats.map(c => { const k2 = kindOf(c); return (
            <div key={c.id} className={`chat-item${c.id === activeChatId ? ' active' : ''}`}
              onClick={() => setActiveChatId(c.id)}>
              <div className="chat-name">{c.name}</div>
              <div className="kind"><span className={`badge ${k2.cls}`}>{k2.label}</span></div>
            </div>
          ); })}
          <div className="chat-head">发起机器人单聊</div>
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
            </> : <span className="hint">从左侧选择一个会话</span>}
          </div>
          {active ? (
            <>
              <MessageList messages={messages} chatId={activeChatId} />
              <footer>
                <MentionInput chatId={active.id} me={me.name}
                  placeholder={k!.hint + '…'} onSend={send} />
              </footer>
            </>
          ) : (
            <div className="nochat">
              <div>💬</div>
              <p>选择左侧会话开始聊天</p>
              <p className="muted">群里 @机器人 或与机器人单聊，即可触发加密回调</p>
            </div>
          )}
        </div>
      </div>
    </>
  );
}
