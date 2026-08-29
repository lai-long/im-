// @ 自动补全输入框：光标前最近一个 @token 触发候选弹层，
// ↑/↓ 选择，Enter/Tab 插入，Esc 关闭；无弹层时 Enter 发送。
// 候选来自 GET /api/chats/members（群成员 + 群内机器人）。

import { useEffect, useMemo, useRef, useState } from 'react';
import type { ChatMember } from '../../shared/types';
import { api } from '../../shared/api';

// 光标前最近的 @token（@ 必须在行首或空白后），返回关键字；无则 null。
function atToken(text: string, pos: number): string | null {
  const m = /(^|\s)@([^\s@]*)$/.exec(text.slice(0, pos));
  return m ? m[2] : null;
}

export function MentionInput({ chatId, me, placeholder, onSend }: {
  chatId: number;
  me: string;
  placeholder: string;
  onSend: (text: string) => void;
}) {
  const [text, setText] = useState('');
  const [members, setMembers] = useState<ChatMember[]>([]);
  const [open, setOpen] = useState(false);
  const [idx, setIdx] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    setText('');
    api.members(chatId).then(ms => setMembers(ms || []));
  }, [chatId]);

  const token = atToken(text, inputRef.current?.selectionStart ?? text.length);
  const candidates = useMemo(() => {
    if (token === null) return [];
    const kw = token.toLowerCase();
    return members.filter(m => m.name !== me && m.name.toLowerCase().includes(kw)).slice(0, 8);
  }, [token, members, me]);

  useEffect(() => {
    setOpen(candidates.length > 0);
    setIdx(0);
  }, [token, candidates.length]);

  const pick = (m: ChatMember) => {
    const el = inputRef.current;
    const pos = el?.selectionStart ?? text.length;
    const next = text.slice(0, pos).replace(/@[^\s@]*$/, `@${m.name} `) + text.slice(pos);
    setText(next);
    setOpen(false);
    el?.focus();
  };

  const submit = () => {
    const t = text.trim();
    if (!t) return;
    onSend(t);
    setText('');
    setOpen(false);
  };

  const onKeyDown = (e: React.KeyboardEvent) => {
    if (open && candidates.length > 0) {
      if (e.key === 'ArrowDown') { e.preventDefault(); setIdx(i => (i + 1) % candidates.length); return; }
      if (e.key === 'ArrowUp') { e.preventDefault(); setIdx(i => (i - 1 + candidates.length) % candidates.length); return; }
      if (e.key === 'Enter' || e.key === 'Tab') { e.preventDefault(); pick(candidates[idx]); return; }
      if (e.key === 'Escape') { setOpen(false); return; }
    }
    if (e.key === 'Enter') submit();
  };

  return (
    <div className="mention-wrap">
      {open && candidates.length > 0 && (
        <div className="mention-pop">
          {candidates.map((m, i) => (
            <div key={m.kind + m.name}
              className={`mention-item${i === idx ? ' active' : ''}`}
              onMouseDown={e => { e.preventDefault(); pick(m); }}>
              <span className={`badge ${m.kind === 'bot' ? 'single' : 'group'}`}>
                {m.kind === 'bot' ? 'bot' : '用户'}
              </span>
              <span>{m.name}</span>
              {m.userid && <span className="muted">{m.userid}</span>}
            </div>
          ))}
        </div>
      )}
      <input id="input" ref={inputRef} placeholder={placeholder} value={text}
        onChange={e => setText(e.target.value)}
        onKeyDown={onKeyDown}
        onBlur={() => setTimeout(() => setOpen(false), 150)} />
      <button onClick={submit} disabled={!text.trim()}>发送</button>
    </div>
  );
}
