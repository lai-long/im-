// 消息渲染：气泡布局（自己靠右），按 msgtype 分支，markdown/stream 走 Markdown 组件。
// 模板卡片交互按钮走事件委托；流式消息由父组件按 msgid 覆盖更新后传入。
// MessageList 负责自动滚动：用户停留在底部附近时跟随新消息，切换会话时强制回底。

import { useEffect, useRef } from 'react';
import type { Message } from '../../shared/types';
import { api } from '../../shared/api';
import { Markdown } from '../../shared/markdown';

const userID = () => JSON.parse(localStorage.getItem('im-user') || '{}').userid as string;

// 文本中的 @userid 高亮（拆分渲染，避免 dangerouslySetInnerHTML）。
function TextWithMentions({ text }: { text: string }) {
  const parts: React.ReactNode[] = [];
  const re = /(@[^\s，,。@]+)/g;
  let last = 0;
  let i = 0;
  let m: RegExpExecArray | null;
  while ((m = re.exec(text)) !== null) {
    if (m.index > last) parts.push(text.slice(last, m.index));
    parts.push(<span key={i++} className="at">{m[0]}</span>);
    last = m.index + m[0].length;
  }
  if (last < text.length) parts.push(text.slice(last));
  return <>{parts}</>;
}

// ts 为 Unix 秒；今天只显示时分，跨天带日期。
function fmtTime(ts: number): string {
  const d = new Date(ts * 1000);
  const now = new Date();
  const sameDay = d.toDateString() === now.toDateString();
  const hm = d.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' });
  if (sameDay) return hm;
  return `${d.getMonth() + 1}-${d.getDate()} ${hm}`;
}

function Body({ m }: { m: Message }) {
  const c = m.content || {};
  switch (m.msgtype) {
    case 'image':
      return <img className="msg-img" src={`data:image/png;base64,${c.base64 || ''}`} alt="" />;
    case 'news': {
      const arts: any[] = Array.isArray(c.articles) ? c.articles : [];
      return (
        <>
          {arts.map((a, i) => (
            <div className="news" key={i}>
              <a href={a.url || '#'} target="_blank" rel="noreferrer">{a.title || ''}</a>
              <div className="news-desc">{a.description || ''}</div>
            </div>
          ))}
        </>
      );
    }
    case 'template_card': {
      const title = c.main_title?.title || '';
      const desc = c.main_title?.desc || '';
      const hlist: any[] = Array.isArray(c.horizontal_content_list) ? c.horizontal_content_list : [];
      const btns: any[] = Array.isArray(c.button_list) ? c.button_list : [];
      const sels: any[] = Array.isArray(c.button_selection) ? c.button_selection : [];
      return (
        <div className="card">
          <div className="card-kind">模板卡片 · {c.card_type || ''}</div>
          <div className="card-title">{title}</div>
          {desc ? <div className="card-desc">{desc}</div> : null}
          {hlist.map((h, i) => (
            <div className="card-h" key={i}>
              <span>{h.keyname || ''}</span>
              <span>{h.value || ''}</span>
            </div>
          ))}
          {btns.length > 0 && (
            <div className="card-btns">
              {btns.map((b, i) => (
                <button key={i} data-card-key={b.key || ''} data-card-msgid={m.msgid}>
                  {b.text || ''}
                </button>
              ))}
            </div>
          )}
          {sels.length > 0 && (
            <>
              {sels.map((sel, i) => (
                <select key={i} data-card-sel="1" data-card-msgid={m.msgid}>
                  {(sel.option_list || []).map((o: any) => (
                    <option key={o.id} value={o.id || ''}>{o.text || ''}</option>
                  ))}
                </select>
              ))}
              <button data-card-sel-confirm="1" data-card-msgid={m.msgid}>确定</button>
            </>
          )}
        </div>
      );
    }
    case 'file':
      return <a className="msg-file" href={`/api/media/${c.media_id || ''}`}>📎 文件 {c.media_id || ''}</a>;
    case 'voice':
      return <a className="msg-file" href={`/api/media/${c.media_id || ''}`}>🔊 语音 {c.media_id || ''}</a>;
    case 'markdown':
    case 'markdown_v2':
      return <Markdown text={c.content || ''} />;
    case 'stream':
      return (
        <>
          <Markdown text={c.content || ''} />
          {c.finish === false && <span className="typing">▍生成中…</span>}
        </>
      );
    default:
      return <TextWithMentions text={c.content || ''} />;
  }
}

function Avatar({ name, bot }: { name: string; bot: boolean }) {
  return (
    <span className={`avatar${bot ? ' bot' : ''}`}>
      {bot ? '🤖' : (name || '?').slice(0, 1).toUpperCase()}
    </span>
  );
}

export function MessageBubble({ m, self }: { m: Message; self: boolean }) {
  if (m.msgtype === 'event') return null;
  const isBot = m.sender_type === 'bot' || m.sender_type === 'agent';
  return (
    <div className={`msg${self ? ' self' : ''}`} data-msgid={m.msgid}>
      <Avatar name={m.sender} bot={isBot} />
      <div className="msg-main">
        <div className="meta">
          {!self && <span className="sender">{m.sender}</span>}
          {isBot && <span className="bot-tag">bot</span>}
          <span className="time">{fmtTime(m.ts)}</span>
        </div>
        <div className="bubble"><Body m={m} /></div>
      </div>
    </div>
  );
}

// 列表：事件委托处理卡片按钮点击；自动滚动到底部。
export function MessageList({ messages, chatId }: { messages: Message[]; chatId: number | null }) {
  const ref = useRef<HTMLDivElement>(null);
  const meName = () => JSON.parse(localStorage.getItem('im-user') || '{}').name as string;

  // 切换会话时强制回底
  useEffect(() => {
    const el = ref.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [chatId]);

  // 新消息/流式更新：用户停留在底部附近（<=120px）时跟随
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    if (el.scrollHeight - el.scrollTop - el.clientHeight < 120) {
      el.scrollTop = el.scrollHeight;
    }
  }, [messages]);

  const onClick = async (e: React.MouseEvent) => {
    const btn = (e.target as HTMLElement).closest('button') as HTMLButtonElement | null;
    if (!btn || !btn.dataset.cardMsgid) return;
    const msgid = btn.dataset.cardMsgid;
    let eventKey = btn.dataset.cardKey || '';
    if (btn.dataset.cardSelConfirm) {
      const sel = (btn.closest('.msg')?.querySelector('select[data-card-sel="1"]')) as HTMLSelectElement | null;
      if (sel) eventKey = sel.value;
    }
    if (!eventKey && !btn.dataset.cardSelConfirm) return;
    await api.cardInteract({ userid: userID(), msgid, event_key: String(eventKey) });
  };

  return (
    <div id="msgs" ref={ref} onClick={onClick}>
      {messages.length === 0 && <div className="empty">暂无消息，发一条试试吧</div>}
      {messages.map(m => <MessageBubble key={m.msgid} m={m} self={m.sender === meName() && m.sender_type === 'user'} />)}
    </div>
  );
}
