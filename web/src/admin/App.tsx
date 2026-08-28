// 控制台主应用：机器人 / 自建应用 / 回调任务 / 消息流水。
import { useEffect, useState, useCallback } from 'react';
import type { Bot, AgentView, CallbackTask, Chat, Message } from '../shared/types';
import { api } from '../shared/api';

function BotPanel() {
  const [bots, setBots] = useState<Bot[]>([]);
  const [chats, setChats] = useState<Chat[]>([]);
  const [name, setName] = useState('');
  const [res, setRes] = useState<Record<number, string>>({});

  const load = useCallback(async () => {
    const [b, c] = await Promise.all([api.admin.bots(), api.admin.chats()]);
    setBots(b || []);
    setChats(c || []);
  }, []);
  useEffect(() => { load(); }, [load]);

  const create = async () => {
    if (!name.trim()) return;
    await api.admin.createBot(name.trim());
    setName('');
    load();
  };
  const save = async (id: number) => {
    const url = (document.getElementById(`url-${id}`) as HTMLInputElement).value.trim();
    const mode = (document.getElementById(`mode-${id}`) as HTMLSelectElement).value;
    const r: any = await api.admin.saveBotCallback({ bot_id: id, url, mode });
    setRes(p => ({ ...p, [id]: r.verified ? '验证通过' : '验证失败: ' + (r.error || '') }));
    load();
  };
  const join = async (id: number) => {
    if (!chats.length) return;
    await api.admin.joinChat({ chat_id: chats[0].id, bot_id: id });
    load();
  };

  return (
    <section>
      <h2>机器人</h2>
      {bots.map(b => (
        <table key={b.id} style={{ marginBottom: 8 }}>
          <tbody>
            <tr><th style={{ width: 120 }}>名称</th><td>{b.name}
              {b.callback_verified ? <span className="badge ok"> 回调已验证</span> : <span className="badge bad"> 回调未验证</span>}
            </td></tr>
            <tr><th>aibotid</th><td><code>{b.aibotid}</code></td></tr>
            <tr><th>Token</th><td><code>{b.callback_token}</code></td></tr>
            <tr><th>EncodingAESKey</th><td><code>{b.callback_aes_key}</code></td></tr>
            <tr><th>长连接密钥</th><td><code>{b.secret}</code></td></tr>
            <tr><th>回调 URL</th><td>{b.callback_url ? <><code>{b.callback_url}</code>（{b.callback_mode}）</> : <span className="muted">未配置</span>}</td></tr>
            <tr><th>webhook</th><td>{(b.keys || []).length ? (b.keys || []).map(k => <div key={k.chat_id}>{k.name}: <code>/cgi-bin/webhook/send?key={k.webhook_key}</code></div>) : <span className="muted">未入群</span>}</td></tr>
            <tr><th>操作</th><td>
              <div className="row">
                <input type="text" id={`url-${b.id}`} placeholder="回调 URL，如 http://127.0.0.1:9000/wecom" defaultValue={b.callback_url} />
                <select id={`mode-${b.id}`} defaultValue={b.callback_mode || 'encrypted'}>
                  <option value="encrypted">encrypted</option>
                  <option value="plain">plain</option>
                </select>
                <button onClick={() => save(b.id)}>保存并验证</button>
                <button className="ghost" onClick={() => join(b.id)}>加入首个群</button>
              </div>
              <pre className="muted">{res[b.id] || ''}</pre>
            </td></tr>
          </tbody>
        </table>
      ))}
      <div className="row">
        <input type="text" placeholder="新机器人名称" value={name}
          onChange={e => setName(e.target.value)} onKeyDown={e => e.key === 'Enter' && create()} />
        <button onClick={create}>创建机器人</button>
      </div>
    </section>
  );
}

function AgentPanel() {
  const [agents, setAgents] = useState<AgentView[]>([]);
  const [name, setName] = useState('');
  const [res, setRes] = useState<Record<number, string>>({});
  const load = useCallback(async () => { setAgents((await api.admin.agents()) || []); }, []);
  useEffect(() => { load(); }, [load]);
  const create = async () => {
    if (!name.trim()) return;
    await api.admin.createAgent(name.trim());
    setName(''); load();
  };
  const save = async (id: number) => {
    const url = (document.getElementById(`aurl-${id}`) as HTMLInputElement).value.trim();
    const mode = (document.getElementById(`amode-${id}`) as HTMLSelectElement).value;
    const r: any = await api.admin.saveAgentCallback({ agent_id: id, url, mode });
    setRes(p => ({ ...p, [id]: r.verified ? '验证通过' : '验证失败: ' + (r.error || '') }));
    load();
  };
  return (
    <section>
      <h2>自建应用（gettoken / message/send）</h2>
      {agents.map(x => { const a = x.agent; return (
        <table key={a.id} style={{ marginBottom: 8 }}>
          <tbody>
            <tr><th style={{ width: 120 }}>名称</th><td>{a.name}
              {a.callback_verified ? <span className="badge ok"> 回调已验证</span> : <span className="badge bad"> 回调未验证</span>}
            </td></tr>
            <tr><th>agentid</th><td><code>{a.agentid}</code></td></tr>
            <tr><th>corpid</th><td><code>{x.corpid}</code></td></tr>
            <tr><th>corpsecret</th><td><code>{a.corpsecret}</code></td></tr>
            <tr><th>gettoken</th><td><code>{x.gettoken}</code></td></tr>
            <tr><th>回调三元组</th><td>Token <code>{a.callback_token}</code> / AESKey <code>{a.callback_aes_key}</code></td></tr>
            <tr><th>操作</th><td><div className="row">
              <input type="text" id={`aurl-${a.id}`} placeholder="回调 URL（自建应用 XML 回调）" defaultValue={a.callback_url} />
              <select id={`amode-${a.id}`} defaultValue={a.callback_mode || 'encrypted'}>
                <option value="encrypted">encrypted</option>
                <option value="plain">plain</option>
              </select>
              <button onClick={() => save(a.id)}>保存并验证</button>
              <pre className="muted">{res[a.id] || ''}</pre>
            </div></td></tr>
          </tbody>
        </table>
      ); })}
      <div className="row">
        <input type="text" placeholder="新自建应用名称" value={name}
          onChange={e => setName(e.target.value)} onKeyDown={e => e.key === 'Enter' && create()} />
        <button onClick={create}>创建自建应用</button>
      </div>
    </section>
  );
}

function TaskPanel() {
  const [status, setStatus] = useState('');
  const [tasks, setTasks] = useState<CallbackTask[]>([]);
  const load = useCallback(async () => { setTasks((await api.admin.tasks(status)) || []); }, [status]);
  useEffect(() => { load(); const t = setInterval(load, 4000); return () => clearInterval(t); }, [load]);
  return (
    <section>
      <h2>回调任务（推送状态 / 重放）</h2>
      <div className="row">
        <select value={status} onChange={e => setStatus(e.target.value)}>
          <option value="">全部</option>
          <option value="pending">pending</option>
          <option value="processing">processing</option>
          <option value="done">done</option>
          <option value="dead">dead</option>
        </select>
        <button onClick={load}>刷新</button>
      </div>
      {tasks.length ? (
        <table>
          <thead><tr><th>ID</th><th>状态</th><th>尝试</th><th>机器人</th><th>最近错误</th><th>操作</th></tr></thead>
          <tbody>
            {tasks.map(t => (
              <tr key={t.id}>
                <td>{t.id}</td><td>{t.status}</td><td>{t.attempt}</td><td>{t.bot_id}</td>
                <td className="muted">{t.last_error || ''}</td>
                <td><button className="ghost" onClick={() => api.admin.replayTask(t.id).then(load)}>重放</button></td>
              </tr>
            ))}
          </tbody>
        </table>
      ) : <div className="muted">无</div>}
    </section>
  );
}

function MessagePanel() {
  const [chats, setChats] = useState<Chat[]>([]);
  const [chatId, setChatId] = useState<number>(0);
  const [msgs, setMsgs] = useState<Message[]>([]);
  const loadChats = useCallback(async () => {
    const c = (await api.admin.chats()) || [];
    setChats(c);
    if (!chatId && c.length) setChatId(c[0].id);
  }, [chatId]);
  const loadMsgs = useCallback(async () => {
    if (chatId) setMsgs((await api.admin.messages(chatId)) || []);
  }, [chatId]);
  useEffect(() => { loadChats(); }, [loadChats]);
  useEffect(() => { loadMsgs(); const t = setInterval(loadMsgs, 4000); return () => clearInterval(t); }, [loadMsgs]);
  return (
    <section>
      <h2>消息流水</h2>
      <div className="row">
        <select value={chatId} onChange={e => setChatId(Number(e.target.value))}>
          {chats.map(c => <option key={c.id} value={c.id}>{c.name}（{c.type}）</option>)}
        </select>
        <button onClick={loadMsgs}>刷新</button>
      </div>
      {msgs.length ? (
        <table>
          <thead><tr><th>msgid</th><th>发送者</th><th>类型</th><th>内容</th><th>提及</th></tr></thead>
          <tbody>
            {msgs.map(m => (
              <tr key={m.id}>
                <td><code>{m.msgid}</code></td>
                <td>{m.sender}{m.sender_type === 'bot' ? ' (bot)' : ''}</td>
                <td>{m.msgtype}</td>
                <td><pre>{JSON.stringify(m.content)}</pre></td>
                <td className="muted">{(m.mentioned || []).join(', ')}</td>
              </tr>
            ))}
          </tbody>
        </table>
      ) : <div className="muted">无</div>}
    </section>
  );
}

export function App() {
  return (
    <>
      <header>
        <strong>本地 IM 控制台</strong>
        <a href="/">← 回到群聊</a>
      </header>
      <main>
        <BotPanel />
        <AgentPanel />
        <TaskPanel />
        <MessagePanel />
      </main>
    </>
  );
}
